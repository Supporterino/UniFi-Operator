/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	// firewallZoneDependencyRequeueAfter bounds how long a firewall zone waits
	// before retrying when a dependency (site, controller, or member network) is
	// not yet ready or a conflict may have cleared. Those changes do not alter
	// this object's generation, so the periodic requeue is what lets a
	// late-appearing dependency or a resolved conflict self-heal.
	firewallZoneDependencyRequeueAfter = 30 * time.Second

	// firewallZoneOriginUserDefined is the upstream metadata.origin of a zone
	// the operator may manage. Every other origin (SYSTEM_DEFINED, DERIVED,
	// ORCHESTRATED) is system-managed and adopted read-only (design D2).
	firewallZoneOriginUserDefined = "USER_DEFINED"
)

// UnifiFirewallZoneReconciler reconciles a UnifiFirewallZone object. It resolves
// the zone's UnifiSite, connects through the site's controller, and creates,
// updates, or deletes the matching custom upstream zone and its network
// membership. System-defined upstream zones are adopted read-only. The upstream
// zone UUID and origin are recorded in status only; spec never carries a UniFi
// internal identifier.
type UnifiFirewallZoneReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader reads credential Secrets through an uncached reader so the
	// manager never caches Secrets (see connection.go). Nil falls back to the
	// cached client in tests.
	APIReader client.Reader

	// NewClient constructs the UniFi client for the resolved controller;
	// defaults to unifi.NewClient and is injectable for tests.
	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallzones,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallzones/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch

// Reconcile resolves the zone's site and controller, applies the declarative
// zone to the console, and reports the result in status. It is idempotent:
// re-running it on an unchanged object performs no writes. It fails closed
// (Ready=False, no upstream mutation) on an unresolved site, a version below
// the firewall minimum, a system-defined upstream zone, a conflict with another
// zone, or an unresolved member network, and adds/removes its finalizer
// symmetrically around the upstream zone's lifecycle.
func (r *UnifiFirewallZoneReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	zone := &unifiv1alpha1.UnifiFirewallZone{}
	if err := r.Get(ctx, req.NamespacedName, zone); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !zone.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, zone)
	}

	site, stop, err := r.reconcileSite(ctx, zone)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, zone, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, zone, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site. An absent, deleting, or not-yet-adopted site fails
// closed: a non-nil result means the reconcile has finished and the caller must
// stop, while a nil result means the returned site is usable.
func (r *UnifiFirewallZoneReconciler) reconcileSite(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: zone.Namespace, Name: zone.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, zone, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", zone.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", zone.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, zone, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, zone, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite and adds the zone finalizer, in a single update when either is
// missing. The site's drain finalizer relies on the ownerReference.
func (r *UnifiFirewallZoneReconciler) ensureOwnership(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(zone, site) {
		if err := controllerutil.SetControllerReference(site, zone, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(zone, unifiv1alpha1.UnifiFirewallZoneFinalizer) {
		controllerutil.AddFinalizer(zone, unifiv1alpha1.UnifiFirewallZoneFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, zone); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console through the site's controller, gates
// on the firewall capability, detects runtime conflicts, resolves member
// networks, and creates, updates, or adopts the upstream zone. A system-defined
// zone fails closed read-only.
func (r *UnifiFirewallZoneReconciler) reconcileUpstream(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, zone.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, zone, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, zone, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityFirewallDNS); err != nil {
		return r.updateStatus(ctx, zone, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}

	// Runtime uniqueness: CRD markers cannot express cross-object uniqueness.
	// Every claimant is failed closed, not only the zone currently reconciling,
	// so an existing claimant's Ready condition cannot go stale (D3).
	allZones, err := r.listZonesInNamespace(ctx, zone)
	if err != nil {
		return ctrl.Result{}, err
	}
	if _, conflicted := zoneConflictFor(zone, allZones); conflicted {
		return r.markConflictingZones(ctx, allZones)
	}

	networkIDs, stop, err := r.resolveMemberNetworks(ctx, zone)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	apiClient, err := resolver.Resolve(ctx, zone.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, zone, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	zones, err := apiClient.ListZones(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, zone, err)
	}

	existing, err := resolveExistingZone(ctx, apiClient, site.Status.SiteID, zone, zones)
	if err != nil {
		return r.handleUpstreamError(ctx, zone, err)
	}

	// Fail closed on a system/derived/orchestrated object: the operator only
	// owns USER_DEFINED zones (design D2). Record the observed origin for
	// correlation but deliberately leave status.zoneID unset: persisting a
	// system zone's ID would make resolveExistingZone keep resolving that same
	// object after a spec.name rename, stranding the CR on a read-only zone
	// instead of letting it manage a new custom zone. Mirrors the network
	// reconciler's system-object path.
	if existing != nil && existing.Metadata.Origin != firewallZoneOriginUserDefined {
		origin := firstNonEmpty(existing.Metadata.Origin, zone.Status.Origin)
		return r.updateStatus(ctx, zone, false, reasonSystemObjectReadOnly,
			systemZoneMessage(*existing), nil, &origin)
	}

	request := unifi.FirewallZoneRequest{Name: zone.Spec.Name, NetworkIDs: networkIDs}

	var (
		result unifi.FirewallZone
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateZone(ctx, site.Status.SiteID, request)
		action = "created"
	case zoneMatchesRequest(*existing, request):
		// The upstream object already matches the desired state; skip the PUT so
		// re-reconciling an unchanged object performs no write.
		zoneID := firstNonEmpty(existing.ID, zone.Status.ZoneID)
		origin := firstNonEmpty(existing.Metadata.Origin, zone.Status.Origin)
		summary := fmt.Sprintf("upstream firewall zone %q is up to date", zone.Spec.Name)
		return r.updateStatus(ctx, zone, true, reasonReconciled, summary, &zoneID, &origin)
	default:
		result, err = apiClient.UpdateZone(ctx, site.Status.SiteID, existing.ID, request)
		action = "updated"
	}
	if err != nil {
		return r.handleUpstreamError(ctx, zone, err)
	}

	// Keep the last-known identifiers when a response omits them, so a
	// transiently incomplete body cannot erase a correlation ID.
	zoneID := firstNonEmpty(result.ID, zone.Status.ZoneID)
	origin := firstNonEmpty(result.Metadata.Origin, zone.Status.Origin)
	if existing != nil {
		zoneID = firstNonEmpty(zoneID, existing.ID)
		origin = firstNonEmpty(origin, existing.Metadata.Origin)
	}
	if existing == nil && origin == "" {
		// The operator just created the zone, so it is user-defined by
		// construction even if the response omitted metadata.
		origin = firewallZoneOriginUserDefined
	}

	summary := fmt.Sprintf("%s upstream firewall zone %q", action, zone.Spec.Name)
	return r.updateStatus(ctx, zone, true, reasonReconciled, summary, &zoneID, &origin)
}

// reconcileDelete deletes the upstream zone recorded in status and removes the
// finalizer. The site still resolves because the site's own finalizer drains its
// children before the site (and its controller chain) disappears. When
// status.zoneID is empty there is no upstream zone to delete.
func (r *UnifiFirewallZoneReconciler) reconcileDelete(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(zone, unifiv1alpha1.UnifiFirewallZoneFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, zone); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(zone, unifiv1alpha1.UnifiFirewallZoneFinalizer)
	if err := r.Update(ctx, zone); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the zone recorded in status from the console. A 404
// means it is already gone and is treated as success. A system-managed object
// is never deleted.
func (r *UnifiFirewallZoneReconciler) deleteUpstream(ctx context.Context, zone *unifiv1alpha1.UnifiFirewallZone) error {
	if zone.Status.ZoneID == "" {
		return nil
	}
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: zone.Namespace, Name: zone.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		return fmt.Errorf("get UnifiSite %q for cleanup: %w", zone.Spec.SiteRef.Name, err)
	}
	if site.Status.SiteID == "" {
		// No upstream site was ever adopted, so there is no upstream zone.
		return nil
	}
	apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, zone.Namespace, site.Spec.ControllerRef)
	if err != nil {
		return fmt.Errorf("resolve controller for cleanup: %w", err)
	}

	// Never delete a system-managed object: the operator only owns USER_DEFINED
	// zones (design D2). A 404 means the object is already gone.
	existing, err := apiClient.GetZone(ctx, site.Status.SiteID, zone.Status.ZoneID)
	if err != nil {
		if errors.Is(err, unifi.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get upstream firewall zone %q for cleanup: %w", zone.Status.ZoneID, err)
	}
	if existing.Metadata.Origin != firewallZoneOriginUserDefined {
		return nil
	}

	if err := apiClient.DeleteZone(ctx, site.Status.SiteID, zone.Status.ZoneID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream firewall zone %q: %w", zone.Status.ZoneID, err)
	}
	return nil
}

// listZonesInNamespace lists every UnifiFirewallZone in zone's namespace. The
// runtime conflict checks build their keys from this set.
func (r *UnifiFirewallZoneReconciler) listZonesInNamespace(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
) ([]unifiv1alpha1.UnifiFirewallZone, error) {
	var list unifiv1alpha1.UnifiFirewallZoneList
	if err := r.List(ctx, &list, client.InNamespace(zone.Namespace)); err != nil {
		return nil, fmt.Errorf("list firewall zones in namespace %q: %w", zone.Namespace, err)
	}
	return list.Items, nil
}

// resolveMemberNetworks resolves spec.networkRefs to upstream network UUIDs. It
// fails closed, writing no membership, when a referenced UnifiNetwork is
// missing or has not reported its upstream UUID (DependencyNotReady, retried) or
// belongs to a different site (CrossSiteReference). The returned stop result is
// non-nil when the reconcile must finish without writing.
func (r *UnifiFirewallZoneReconciler) resolveMemberNetworks(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
) ([]string, *ctrl.Result, error) {
	ids := make([]string, 0, len(zone.Spec.NetworkRefs))
	seen := make(map[string]struct{}, len(zone.Spec.NetworkRefs))
	for _, ref := range zone.Spec.NetworkRefs {
		if _, ok := seen[ref.Name]; ok {
			continue
		}
		seen[ref.Name] = struct{}{}

		network := &unifiv1alpha1.UnifiNetwork{}
		key := types.NamespacedName{Namespace: zone.Namespace, Name: ref.Name}
		if err := r.Get(ctx, key, network); err != nil {
			if apierrors.IsNotFound(err) {
				result, err := r.requeueStatus(ctx, zone, reasonDependencyNotReady,
					fmt.Sprintf("UnifiNetwork %q not found", ref.Name))
				return nil, &result, err
			}
			return nil, nil, fmt.Errorf("get UnifiNetwork %q: %w", ref.Name, err)
		}
		if network.Spec.SiteRef.Name != zone.Spec.SiteRef.Name {
			result, err := r.updateStatus(ctx, zone, false, reasonCrossSiteReference,
				fmt.Sprintf("UnifiNetwork %q belongs to UnifiSite %q, not %q; no membership written",
					ref.Name, network.Spec.SiteRef.Name, zone.Spec.SiteRef.Name), nil, nil)
			return nil, &result, err
		}
		if network.Status.NetworkID == "" {
			result, err := r.requeueStatus(ctx, zone, reasonDependencyNotReady,
				fmt.Sprintf("UnifiNetwork %q has not reported its upstream network ID yet", ref.Name))
			return nil, &result, err
		}
		ids = append(ids, network.Status.NetworkID)
	}
	return ids, nil, nil
}

// handleUpstreamError classifies a failed zone write. Certificate, auth, and
// bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiFirewallZoneReconciler) handleUpstreamError(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
	err error,
) (ctrl.Result, error) {
	if isCertificateError(err) {
		return r.updateStatus(ctx, zone, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, zone, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, zone, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, zone, false, reasonInvalidSpec, specErr.Error(), nil, nil)
	}
	return ctrl.Result{}, fmt.Errorf("reconcile firewall zone: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency or conflict failure a later edit can fix without changing this
// object's generation.
func (r *UnifiFirewallZoneReconciler) requeueStatus(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, zone, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = firewallZoneDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil zoneID or origin leaves the recorded value
// untouched, preserving the last-known identifiers on failing paths.
func (r *UnifiFirewallZoneReconciler) updateStatus(
	ctx context.Context,
	zone *unifiv1alpha1.UnifiFirewallZone,
	ready bool,
	reason, message string,
	zoneID, origin *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if zone.Status.ObservedGeneration != zone.Generation {
		zone.Status.ObservedGeneration = zone.Generation
		changed = true
	}
	if zone.Status.Summary != message {
		zone.Status.Summary = message
		changed = true
	}
	if zoneID != nil && zone.Status.ZoneID != *zoneID {
		zone.Status.ZoneID = *zoneID
		changed = true
	}
	if origin != nil && zone.Status.Origin != *origin {
		zone.Status.Origin = *origin
		changed = true
	}
	if setReadyCondition(&zone.Status.Conditions, zone.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, zone); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// zoneNameClaimants returns the names of every UnifiFirewallZone in the same
// site whose spec.name matches zone's spec.name, sorted. More than one claimant
// is a conflict because an upstream zone is identified by name (design D3).
func zoneNameClaimants(zone *unifiv1alpha1.UnifiFirewallZone, all []unifiv1alpha1.UnifiFirewallZone) []string {
	var claimants []string
	for i := range all {
		other := &all[i]
		if other.Spec.SiteRef.Name != zone.Spec.SiteRef.Name || other.Spec.Name != zone.Spec.Name {
			continue
		}
		claimants = append(claimants, other.Name)
	}
	sort.Strings(claimants)
	return claimants
}

// conflictedNetworks returns the zone's networkRefs that another zone in the
// same site also claims, sorted and de-duplicated. The key is
// (siteRef, networkRef), not the namespace alone, because one namespace may hold
// several sites (design D3).
func conflictedNetworks(zone *unifiv1alpha1.UnifiFirewallZone, all []unifiv1alpha1.UnifiFirewallZone) []string {
	type claimKey struct{ site, network string }

	claims := make(map[claimKey]map[string]struct{})
	for i := range all {
		other := &all[i]
		for _, ref := range other.Spec.NetworkRefs {
			key := claimKey{site: other.Spec.SiteRef.Name, network: ref.Name}
			if claims[key] == nil {
				claims[key] = make(map[string]struct{})
			}
			claims[key][other.Name] = struct{}{}
		}
	}

	conflicted := make([]string, 0, len(zone.Spec.NetworkRefs))
	seen := make(map[string]struct{}, len(zone.Spec.NetworkRefs))
	for _, ref := range zone.Spec.NetworkRefs {
		if _, ok := seen[ref.Name]; ok {
			continue
		}
		seen[ref.Name] = struct{}{}
		if len(claims[claimKey{site: zone.Spec.SiteRef.Name, network: ref.Name}]) > 1 {
			conflicted = append(conflicted, ref.Name)
		}
	}
	sort.Strings(conflicted)
	return conflicted
}

// zoneConflict is the runtime uniqueness failure a zone is subject to, as
// reported in its Ready condition.
type zoneConflict struct {
	reason  string
	message string
}

// zoneConflictFor returns the runtime uniqueness conflict zone is subject to
// given every zone in the namespace, or false when zone is unambiguous. A
// spec.name clash in the same site takes precedence over a network-membership
// clash because upstream zone identity is by name (design D3).
func zoneConflictFor(
	zone *unifiv1alpha1.UnifiFirewallZone,
	all []unifiv1alpha1.UnifiFirewallZone,
) (zoneConflict, bool) {
	if claimants := zoneNameClaimants(zone, all); len(claimants) > 1 {
		return zoneConflict{
			reason: reasonZoneNameConflict,
			message: fmt.Sprintf("spec.name %q is claimed by more than one firewall zone (%v) in the same site; no upstream zone written",
				zone.Spec.Name, claimants),
		}, true
	}
	if networks := conflictedNetworks(zone, all); len(networks) > 0 {
		return zoneConflict{
			reason: reasonMembershipConflict,
			message: fmt.Sprintf("network(s) %v are claimed by more than one firewall zone in the same site; no membership written",
				networks),
		}, true
	}
	return zoneConflict{}, false
}

// markConflictingZones fails closed on every zone with a runtime uniqueness
// conflict, not only the zone currently reconciling, so an existing claimant's
// Ready condition is refreshed rather than left stale. It returns a bounded
// requeue so a resolved conflict is re-checked even without a watch event.
func (r *UnifiFirewallZoneReconciler) markConflictingZones(
	ctx context.Context,
	all []unifiv1alpha1.UnifiFirewallZone,
) (ctrl.Result, error) {
	for i := range all {
		other := &all[i]
		conflict, conflicted := zoneConflictFor(other, all)
		if !conflicted {
			continue
		}
		if _, err := r.updateStatus(ctx, other, false, conflict.reason, conflict.message, nil, nil); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: firewallZoneDependencyRequeueAfter}, nil
}

// findZoneByName returns the upstream zone matching name, or nil.
func findZoneByName(zones []unifi.FirewallZone, name string) *unifi.FirewallZone {
	for i := range zones {
		if zones[i].Name == name {
			return &zones[i]
		}
	}
	return nil
}

// resolveExistingZone returns the upstream zone this CR manages, or nil when
// none exists. It resolves status.zoneID first so a spec.name rename updates
// the same upstream object instead of orphaning it, then falls back to a name
// match for adoption. A 404 while resolving status.zoneID is treated as absent
// so a stale ID cannot block reconciliation.
func resolveExistingZone(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	zone *unifiv1alpha1.UnifiFirewallZone,
	zones []unifi.FirewallZone,
) (*unifi.FirewallZone, error) {
	if zone.Status.ZoneID != "" {
		existing, err := apiClient.GetZone(ctx, siteID, zone.Status.ZoneID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the name match: the recorded object is gone.
		default:
			return nil, err
		}
	}
	return findZoneByName(zones, zone.Spec.Name), nil
}

// zoneMatchesRequest reports whether the observed upstream zone already matches
// the desired write, so the reconciler can skip the PUT and stay idempotent.
// Only the authored fields (name, membership) are compared; observed fields
// (id, metadata) are ignored. Membership is order-insensitive.
func zoneMatchesRequest(existing unifi.FirewallZone, req unifi.FirewallZoneRequest) bool {
	return existing.Name == req.Name && stringSlicesMatch(existing.NetworkIDs, req.NetworkIDs)
}

// systemZoneMessage explains why an upstream zone is read-only, without echoing
// credential material.
func systemZoneMessage(existing unifi.FirewallZone) string {
	origin := existing.Metadata.Origin
	if origin == "" {
		origin = "unknown"
	}
	return fmt.Sprintf("upstream firewall zone %q has metadata.origin %s, not %s; refusing to overwrite a system-managed object",
		existing.Name, origin, firewallZoneOriginUserDefined)
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes and the first deletion. The
// controller also watches its UnifiSite and UnifiController dependencies so a
// status-only change there (for example a late-adopted siteID or a detected
// application version) retriggers reconciliation. It additionally watches
// UnifiFirewallZone itself (filtered the same way) so a claimant added, edited,
// or deleted re-reconciles the zones it shares a site with, which is what
// clears a stale conflict condition on the survivor (design D3). It watches
// UnifiNetwork so a member network that is deleted or reports a new upstream
// networkID re-reconciles the zones that reference it, rather than leaving a
// Ready zone with stale membership.
func (r *UnifiFirewallZoneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiFirewallZone{}, builder.WithPredicates(firewallZonePredicate())).
		Watches(&unifiv1alpha1.UnifiFirewallZone{},
			handler.EnqueueRequestsFromMapFunc(r.zonesForZoneChange),
			builder.WithPredicates(firewallZonePredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.zonesInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.zonesForSite)).
		Watches(&unifiv1alpha1.UnifiNetwork{},
			handler.EnqueueRequestsFromMapFunc(r.zonesForNetwork),
			builder.WithPredicates(networkDependencyPredicate())).
		Named("unififirewallzone").
		Complete(r)
}

// zonesForZoneChange enqueues the UnifiFirewallZones sharing the changed zone's
// site. Creating, editing, or deleting one claimant must re-reconcile the
// others so the conflict is reflected on all of them and a resolved conflict
// clears the stale Ready condition of the survivor (design D3).
func (r *UnifiFirewallZoneReconciler) zonesForZoneChange(ctx context.Context, obj client.Object) []reconcile.Request {
	changed, ok := obj.(*unifiv1alpha1.UnifiFirewallZone)
	if !ok {
		return nil
	}
	return r.listZoneRequests(ctx, changed.Namespace, changed.Spec.SiteRef.Name)
}

// zonesInNamespace enqueues every UnifiFirewallZone in the changed object's
// namespace. A dependency (UnifiController or UnifiSite) may be referenced by
// any zone, so the dependency's status-only change retriggers all of them.
func (r *UnifiFirewallZoneReconciler) zonesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listZoneRequests(ctx, obj.GetNamespace(), "")
}

// zonesForSite enqueues the UnifiFirewallZones that reference the changed
// UnifiSite via spec.siteRef.
func (r *UnifiFirewallZoneReconciler) zonesForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listZoneRequests(ctx, obj.GetNamespace(), obj.GetName())
}

// zonesForNetwork enqueues the UnifiFirewallZones that list the changed
// UnifiNetwork in spec.networkRefs. Membership resolves through
// UnifiNetwork.status.networkID, so a member network deleted or re-identified
// must re-reconcile every zone claiming it; zones in another namespace or not
// referencing the network are not enqueued. The filter is on
// (namespace, spec.networkRefs contains name), never the site, because a zone's
// member network is named directly.
func (r *UnifiFirewallZoneReconciler) zonesForNetwork(ctx context.Context, obj client.Object) []reconcile.Request {
	network, ok := obj.(*unifiv1alpha1.UnifiNetwork)
	if !ok {
		return nil
	}
	var zones unifiv1alpha1.UnifiFirewallZoneList
	if err := r.List(ctx, &zones, client.InNamespace(network.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list firewall zones for network watch", "namespace", network.Namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(zones.Items))
	for i := range zones.Items {
		item := &zones.Items[i]
		claims := false
		for _, ref := range item.Spec.NetworkRefs {
			if ref.Name == network.Name {
				claims = true
				break
			}
		}
		if !claims {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// listZoneRequests lists UnifiFirewallZones in namespace and optionally filters
// them to those whose spec.siteRef.name matches siteName.
func (r *UnifiFirewallZoneReconciler) listZoneRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var zones unifiv1alpha1.UnifiFirewallZoneList
	if err := r.List(ctx, &zones, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list firewall zones for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(zones.Items))
	for i := range zones.Items {
		item := &zones.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// firewallZonePredicate triggers reconciles on generation changes and when a
// deletionTimestamp is first set. GenerationChangedPredicate drops
// metadata-only updates, but the finalizer cleanup runs on exactly such an
// update, so deletion detection is added alongside it.
func firewallZonePredicate() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				if e.ObjectOld == nil || e.ObjectNew == nil {
					return false
				}
				return e.ObjectOld.GetDeletionTimestamp().IsZero() &&
					!e.ObjectNew.GetDeletionTimestamp().IsZero()
			},
		},
	)
}

// networkDependencyPredicate gates the UnifiNetwork watch. The zone resolves
// membership through UnifiNetwork.status.networkID, so it must react to a
// member network being deleted or reporting a new upstream networkID. Other
// status-only updates (conditions, summary) are dropped: the network controller
// rewrites its status on every reconcile, and reacting to that churn would
// bounce reconciles between the two controllers. Generation changes (spec
// edits, and the create event) and the first deletion are also handled.
func networkDependencyPredicate() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldNetwork, ok := e.ObjectOld.(*unifiv1alpha1.UnifiNetwork)
				if !ok {
					return false
				}
				newNetwork, ok := e.ObjectNew.(*unifiv1alpha1.UnifiNetwork)
				if !ok {
					return false
				}
				if oldNetwork.GetDeletionTimestamp().IsZero() && !newNetwork.GetDeletionTimestamp().IsZero() {
					return true
				}
				return oldNetwork.Status.NetworkID != newNetwork.Status.NetworkID
			},
		},
	)
}
