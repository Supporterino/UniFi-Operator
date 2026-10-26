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
	"slices"
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
	// firewallOrderingDependencyRequeueAfter bounds how long an ordering object
	// waits before retrying when a dependency (site, controller, zone, ordered
	// policy, or the resolution of a conflicting claimant) is not yet ready.
	firewallOrderingDependencyRequeueAfter = 30 * time.Second

	// firewallOrderingUnconfiguredZBFMessage explains an ordering reconcile the
	// console cannot serve because Zone-Based Firewall is off. The zone-read gate
	// and the upstream error classifier share it so the condition text cannot
	// drift between the two entry points.
	firewallOrderingUnconfiguredZBFMessage = "console reports Zone-Based Firewall is not configured; no firewall policy ordering was applied"
)

// UnifiFirewallPolicyOrderingReconciler reconciles a UnifiFirewallPolicyOrdering
// object. It is authoritative for the user-defined firewall policy ordering of
// one source/destination zone pair within a site: it resolves the zone pair,
// detects duplicate ordering objects for the same pair, resolves the ordered
// policy references, applies the before/after-system-defined order upstream, and
// reports upstream drift without silently reordering. System-defined policies
// are anchored and are never reordered. It uses the shared ordering primitive
// (ordering.go).
type UnifiFirewallPolicyOrderingReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallpolicyorderings,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallpolicyorderings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallzones,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the ordering's site and controller, resolves the zone pair
// and ordered policy references, and applies the order upstream. It is
// idempotent. It fails closed (Ready=False, no upstream mutation) on an
// unresolved site, a version below the firewall/DNS minimum, an unresolved zone
// pair, a duplicate ordering object for the zone pair, or an unresolved ordered
// policy reference.
func (r *UnifiFirewallPolicyOrderingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ordering := &unifiv1alpha1.UnifiFirewallPolicyOrdering{}
	if err := r.Get(ctx, req.NamespacedName, ordering); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !ordering.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, ordering)
	}

	site, stop, err := r.reconcileSite(ctx, ordering)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, ordering, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, ordering, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site.
func (r *UnifiFirewallPolicyOrderingReconciler) reconcileSite(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: ordering.Namespace, Name: ordering.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, ordering, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", ordering.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", ordering.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, ordering, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, ordering, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite and adds the ordering finalizer, in a single update when either is
// missing. The site's drain finalizer relies on the ownerReference.
func (r *UnifiFirewallPolicyOrderingReconciler) ensureOwnership(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(ordering, site) {
		if err := controllerutil.SetControllerReference(site, ordering, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(ordering, unifiv1alpha1.UnifiFirewallPolicyOrderingFinalizer) {
		controllerutil.AddFinalizer(ordering, unifiv1alpha1.UnifiFirewallPolicyOrderingFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, ordering); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream detects duplicate ordering objects for the zone pair,
// resolves the zone pair and the ordered policy references, then applies the
// before/after-system-defined order and reports drift.
func (r *UnifiFirewallPolicyOrderingReconciler) reconcileUpstream(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, ordering.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, ordering, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, ordering, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityFirewallDNS); err != nil {
		return r.updateStatus(ctx, ordering, false, reasonVersionUnsupported, err.Error(), nil, nil, nil)
	}

	// Duplicate-scope conflict: CRD markers cannot express cross-object
	// uniqueness, so a peer sharing the zone pair fails closed on every claimant
	// before any order is applied (design D2).
	all, err := r.listOrderingsInNamespace(ctx, ordering)
	if err != nil {
		return ctrl.Result{}, err
	}
	if claimants := OrderingClaimants(orderingObjects(all), ordering, r.scopeKey); len(claimants) > 1 {
		return r.markConflictingOrderings(ctx, all)
	}

	apiClient, err := resolver.Resolve(ctx, ordering.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, ordering, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	zones, zbfConfigured, err := apiClient.ListZones(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}
	// A console with Zone-Based Firewall off answers the zone read with the
	// subsystem code, which maps to an empty set and no error. Resolving the zone
	// pair against that set would report a missing zone (or a missing built-in
	// zone) and hot-requeue forever, so report the absent subsystem terminally
	// here, before any selector resolution, and apply no order (design D5).
	if !zbfConfigured {
		return r.updateStatus(ctx, ordering, false, reasonZoneBasedFirewallNotConfigured,
			firewallOrderingUnconfiguredZBFMessage, nil, nil, nil)
	}
	sourceRes := resolveZoneSelector(ctx, r.Client, ordering.Namespace, ordering.Spec.SiteRef.Name, ordering.Spec.Source.ZoneRef, ordering.Spec.Source.BuiltInZone, zones)
	if result, stopped, err := r.selectorStatus(ctx, ordering, sourceRes); stopped {
		return result, err
	}
	destinationRes := resolveZoneSelector(ctx, r.Client, ordering.Namespace, ordering.Spec.SiteRef.Name, ordering.Spec.Destination.ZoneRef, ordering.Spec.Destination.BuiltInZone, zones)
	if result, stopped, err := r.selectorStatus(ctx, ordering, destinationRes); stopped {
		return result, err
	}
	sourceZoneID := sourceRes.value
	destinationZoneID := destinationRes.value

	var mismatched []string
	resolvePolicy := func(name string) (string, bool) {
		policy := &unifiv1alpha1.UnifiFirewallPolicy{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: ordering.Namespace, Name: name}, policy); err != nil {
			return "", false
		}
		if policy.Spec.SiteRef.Name != ordering.Spec.SiteRef.Name || policy.Status.PolicyID == "" {
			return "", false
		}
		// A policy ordered for this zone pair must itself belong to that pair.
		// Only compare when the policy has resolved its zone pair; an
		// unresolved policy is already handled as PolicyRefNotFound below.
		if policy.Status.SourceZoneID != "" && policy.Status.DestinationZoneID != "" &&
			(policy.Status.SourceZoneID != sourceZoneID || policy.Status.DestinationZoneID != destinationZoneID) {
			mismatched = append(mismatched, name)
			return "", false
		}
		return policy.Status.PolicyID, true
	}
	beforeIDs, missingBefore := OrderingIDs(ordering.Spec.BeforeSystemDefined, resolvePolicy)
	afterIDs, missingAfter := OrderingIDs(ordering.Spec.AfterSystemDefined, resolvePolicy)
	if len(mismatched) > 0 {
		slices.Sort(mismatched)
		return r.requeueStatus(ctx, ordering, reasonFirewallPolicyZonePairMismatch,
			fmt.Sprintf("ordered policy reference(s) %v belong to a different source/destination zone pair than this ordering; no order applied", mismatched))
	}
	if missing := append(missingBefore, missingAfter...); len(missing) > 0 {
		slices.Sort(missing)
		return r.requeueStatus(ctx, ordering, reasonPolicyRefNotFound,
			fmt.Sprintf("ordered policy reference(s) %v do not resolve to a provisioned UnifiFirewallPolicy on the site", missing))
	}

	policies, err := apiClient.ListFirewallPolicies(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}
	ordered := OrderingOrderedSet(beforeIDs, afterIDs)
	drift := OrderingDrift(ordered, upstreamFirewallPolicyEntries(policies, sourceZoneID, destinationZoneID))

	request := unifi.FirewallPolicyOrdering{
		OrderedFirewallPolicyIDs: unifi.FirewallPolicyOrderingIDs{
			BeforeSystemDefined: beforeIDs,
			AfterSystemDefined:  afterIDs,
		},
	}
	existing, err := apiClient.GetFirewallPolicyOrdering(ctx, site.Status.SiteID, sourceZoneID, destinationZoneID)
	if err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}
	if firewallOrderingMatches(existing, request) {
		result, err := r.updateStatus(ctx, ordering, true, reasonReconciled,
			"upstream firewall policy ordering is up to date", &sourceZoneID, &destinationZoneID, &drift)
		return result, err
	}
	if _, err := apiClient.UpdateFirewallPolicyOrdering(ctx, site.Status.SiteID, sourceZoneID, destinationZoneID, request); err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}

	summary := fmt.Sprintf("applied firewall policy ordering for the %s -> %s zone pair",
		firewallZoneSelectorLabel(ordering.Spec.Source), firewallZoneSelectorLabel(ordering.Spec.Destination))
	return r.updateStatus(ctx, ordering, true, reasonReconciled, summary, &sourceZoneID, &destinationZoneID, &drift)
}

// reconcileDelete removes the finalizer. Clearing ordering has no
// upstream-delete semantics, so no upstream call is made.
func (r *UnifiFirewallPolicyOrderingReconciler) reconcileDelete(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ordering, unifiv1alpha1.UnifiFirewallPolicyOrderingFinalizer) {
		return ctrl.Result{}, nil
	}
	controllerutil.RemoveFinalizer(ordering, unifiv1alpha1.UnifiFirewallPolicyOrderingFinalizer)
	if err := r.Update(ctx, ordering); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// handleUpstreamError classifies a failed ordering write. Certificate, auth, and
// bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiFirewallPolicyOrderingReconciler) handleUpstreamError(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, ordering, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, ordering, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil, nil)
		case unifi.IsZoneBasedFirewallUnconfigured(err):
			// The spec is valid; the console's firewall subsystem is absent.
			// Report that distinctly from a bad spec and apply no order
			// (design D5). This covers a policy read or ordering write the
			// console rejects after the zone read succeeded.
			return r.updateStatus(ctx, ordering, false, reasonZoneBasedFirewallNotConfigured,
				firewallOrderingUnconfiguredZBFMessage, nil, nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, ordering, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, ordering, false, reasonInvalidSpec, specErr.Error(), nil, nil, nil)
	}
	log.Error(err, "transient error reconciling firewall policy ordering")
	return ctrl.Result{}, fmt.Errorf("reconcile firewall policy ordering: %w", err)
}

// selectorStatus maps a failed selector resolution to a status write or a
// transient error. stopped is false when the resolution succeeded.
func (r *UnifiFirewallPolicyOrderingReconciler) selectorStatus(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
	res selectorResolution,
) (ctrl.Result, bool, error) {
	if res.err != nil {
		return ctrl.Result{}, true, res.err
	}
	if res.reason == "" {
		return ctrl.Result{}, false, nil
	}
	if res.retry {
		result, err := r.requeueStatus(ctx, ordering, res.reason, res.message)
		return result, true, err
	}
	result, err := r.updateStatus(ctx, ordering, false, res.reason, res.message, nil, nil, nil)
	return result, true, err
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency failure a later edit can fix without changing this object's
// generation.
func (r *UnifiFirewallPolicyOrderingReconciler) requeueStatus(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, ordering, false, reason, message, nil, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = firewallOrderingDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil identifier or drift pointer leaves the recorded
// value untouched.
func (r *UnifiFirewallPolicyOrderingReconciler) updateStatus(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
	ready bool,
	reason, message string,
	sourceZoneID, destinationZoneID *string,
	drift *[]string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if ordering.Status.ObservedGeneration != ordering.Generation {
		ordering.Status.ObservedGeneration = ordering.Generation
		changed = true
	}
	if ordering.Status.Summary != message {
		ordering.Status.Summary = message
		changed = true
	}
	if sourceZoneID != nil && ordering.Status.SourceZoneID != *sourceZoneID {
		ordering.Status.SourceZoneID = *sourceZoneID
		changed = true
	}
	if destinationZoneID != nil && ordering.Status.DestinationZoneID != *destinationZoneID {
		ordering.Status.DestinationZoneID = *destinationZoneID
		changed = true
	}
	if drift != nil && !stringSlicesMatch(ordering.Status.Drift, *drift) {
		ordering.Status.Drift = *drift
		changed = true
	}
	if setReadyCondition(&ordering.Status.Conditions, ordering.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, ordering); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// scopeKey returns the authoritative scope identity for an ordering object: its
// site plus its source/destination zone selectors. The shared ordering primitive
// uses it to detect duplicate ordering objects for the same zone pair.
func (r *UnifiFirewallPolicyOrderingReconciler) scopeKey(obj client.Object) string {
	ordering, ok := obj.(*unifiv1alpha1.UnifiFirewallPolicyOrdering)
	if !ok {
		return ""
	}
	return ordering.Spec.SiteRef.Name + "|" +
		firewallZoneSelectorLabel(ordering.Spec.Source) + "|" +
		firewallZoneSelectorLabel(ordering.Spec.Destination)
}

// firewallZoneSelectorLabel renders a zone selector as a stable scope component.
func firewallZoneSelectorLabel(selector unifiv1alpha1.FirewallZoneSelector) string {
	switch {
	case selector.ZoneRef != nil:
		return "ref:" + selector.ZoneRef.Name
	case selector.BuiltInZone != nil:
		return "builtin:" + selector.BuiltInZone.Name
	default:
		return ""
	}
}

// listOrderingsInNamespace lists every UnifiFirewallPolicyOrdering in the
// ordering's namespace.
func (r *UnifiFirewallPolicyOrderingReconciler) listOrderingsInNamespace(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering,
) ([]unifiv1alpha1.UnifiFirewallPolicyOrdering, error) {
	var list unifiv1alpha1.UnifiFirewallPolicyOrderingList
	if err := r.List(ctx, &list, client.InNamespace(ordering.Namespace)); err != nil {
		return nil, fmt.Errorf("list firewall policy orderings in namespace %q: %w", ordering.Namespace, err)
	}
	return list.Items, nil
}

// orderingObjects returns the ordering objects as client.Objects for the shared
// primitive, preserving the slice so status writes reach the right object.
func orderingObjects(items []unifiv1alpha1.UnifiFirewallPolicyOrdering) []client.Object {
	objs := make([]client.Object, 0, len(items))
	for i := range items {
		objs = append(objs, &items[i])
	}
	return objs
}

// markConflictingOrderings fails closed on every ordering object that shares its
// zone pair with another, not only the object currently reconciling, so an
// existing claimant's Ready condition is refreshed rather than left stale. It
// returns a bounded requeue so a resolved conflict is re-checked.
func (r *UnifiFirewallPolicyOrderingReconciler) markConflictingOrderings(
	ctx context.Context,
	all []unifiv1alpha1.UnifiFirewallPolicyOrdering,
) (ctrl.Result, error) {
	objs := orderingObjects(all)
	for i := range all {
		claimants := OrderingClaimants(objs, &all[i], r.scopeKey)
		if len(claimants) <= 1 {
			continue
		}
		message := fmt.Sprintf("the source/destination zone pair is claimed by more than one firewall policy ordering (%v); no order applied", claimants)
		if _, err := r.updateStatus(ctx, &all[i], false, reasonZonePairConflict, message, nil, nil, nil); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: firewallOrderingDependencyRequeueAfter}, nil
}

// upstreamFirewallPolicyEntries returns the upstream firewall policies for the
// given zone pair as ordering entries, marking non-USER_DEFINED ones as system
// (anchored).
func upstreamFirewallPolicyEntries(policies []unifi.FirewallPolicy, sourceZoneID, destinationZoneID string) []OrderingEntry {
	entries := make([]OrderingEntry, 0, len(policies))
	for i := range policies {
		policy := &policies[i]
		if policy.Source.ZoneID != sourceZoneID || policy.Destination.ZoneID != destinationZoneID {
			continue
		}
		entries = append(entries, OrderingEntry{
			ID:     policy.ID,
			Name:   policy.Name,
			System: policy.Metadata.Origin != firewallPolicyOriginUserDefined,
		})
	}
	return entries
}

// firewallOrderingMatches reports whether the observed upstream ordering already
// matches the desired one, so the reconciler can skip the PUT and stay
// idempotent. Unlike set comparison, ordering is significant.
func firewallOrderingMatches(existing, req unifi.FirewallPolicyOrdering) bool {
	return slices.Equal(existing.OrderedFirewallPolicyIDs.BeforeSystemDefined, req.OrderedFirewallPolicyIDs.BeforeSystemDefined) &&
		slices.Equal(existing.OrderedFirewallPolicyIDs.AfterSystemDefined, req.OrderedFirewallPolicyIDs.AfterSystemDefined)
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiFirewallPolicyOrderingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiFirewallPolicyOrdering{}, builder.WithPredicates(firewallPolicyPredicate())).
		Watches(&unifiv1alpha1.UnifiFirewallPolicyOrdering{},
			handler.EnqueueRequestsFromMapFunc(r.orderingsForOrderingChange),
			builder.WithPredicates(firewallPolicyPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.orderingsInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.orderingsForSite)).
		Watches(&unifiv1alpha1.UnifiFirewallZone{},
			handler.EnqueueRequestsFromMapFunc(r.orderingsForZone),
			builder.WithPredicates(firewallZoneStatusPredicate())).
		Watches(&unifiv1alpha1.UnifiFirewallPolicy{},
			handler.EnqueueRequestsFromMapFunc(r.orderingsForPolicy),
			builder.WithPredicates(firewallPolicyStatusPredicate())).
		Named("unififirewallpolicyordering").
		Complete(r)
}

// orderingsInNamespace enqueues every UnifiFirewallPolicyOrdering in the changed
// object's namespace.
func (r *UnifiFirewallPolicyOrderingReconciler) orderingsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listOrderingRequests(ctx, obj.GetNamespace(), "", nil)
}

// orderingsForSite enqueues the orderings that reference the changed UnifiSite.
func (r *UnifiFirewallPolicyOrderingReconciler) orderingsForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listOrderingRequests(ctx, obj.GetNamespace(), obj.GetName(), nil)
}

// orderingsForZone enqueues the orderings whose source or destination zoneRef
// names the changed zone.
func (r *UnifiFirewallPolicyOrderingReconciler) orderingsForZone(ctx context.Context, obj client.Object) []reconcile.Request {
	zone, ok := obj.(*unifiv1alpha1.UnifiFirewallZone)
	if !ok {
		return nil
	}
	return r.listOrderingRequests(ctx, zone.Namespace, "", func(item *unifiv1alpha1.UnifiFirewallPolicyOrdering) bool {
		return orderingReferencesZone(item, zone.Name)
	})
}

// orderingsForPolicy enqueues the orderings that list the changed policy.
func (r *UnifiFirewallPolicyOrderingReconciler) orderingsForPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	policy, ok := obj.(*unifiv1alpha1.UnifiFirewallPolicy)
	if !ok {
		return nil
	}
	return r.listOrderingRequests(ctx, policy.Namespace, "", func(item *unifiv1alpha1.UnifiFirewallPolicyOrdering) bool {
		return orderingReferencesPolicy(item, policy.Name)
	})
}

// orderingsForOrderingChange enqueues the orderings sharing the changed
// ordering's zone pair, so a conflict flips on all claimants and a resolved
// conflict clears the survivor.
func (r *UnifiFirewallPolicyOrderingReconciler) orderingsForOrderingChange(ctx context.Context, obj client.Object) []reconcile.Request {
	changed, ok := obj.(*unifiv1alpha1.UnifiFirewallPolicyOrdering)
	if !ok {
		return nil
	}
	key := r.scopeKey(changed)
	return r.listOrderingRequests(ctx, changed.Namespace, "", func(item *unifiv1alpha1.UnifiFirewallPolicyOrdering) bool {
		return r.scopeKey(item) == key
	})
}

// listOrderingRequests lists UnifiFirewallPolicyOrderings in namespace and
// applies an optional site filter and an optional predicate filter.
func (r *UnifiFirewallPolicyOrderingReconciler) listOrderingRequests(
	ctx context.Context,
	namespace, siteName string,
	keep func(*unifiv1alpha1.UnifiFirewallPolicyOrdering) bool,
) []reconcile.Request {
	var orderings unifiv1alpha1.UnifiFirewallPolicyOrderingList
	if err := r.List(ctx, &orderings, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list firewall policy orderings for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(orderings.Items))
	for i := range orderings.Items {
		item := &orderings.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		if keep != nil && !keep(item) {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// orderingReferencesZone reports whether an ordering's source or destination
// zoneRef names the given zone.
func orderingReferencesZone(ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering, name string) bool {
	for _, selector := range []unifiv1alpha1.FirewallZoneSelector{ordering.Spec.Source, ordering.Spec.Destination} {
		if selector.ZoneRef != nil && selector.ZoneRef.Name == name {
			return true
		}
	}
	return false
}

// orderingReferencesPolicy reports whether an ordering lists the given policy in
// its before/after-system-defined reference lists.
func orderingReferencesPolicy(ordering *unifiv1alpha1.UnifiFirewallPolicyOrdering, name string) bool {
	for _, refs := range [][]unifiv1alpha1.CoreRef{ordering.Spec.BeforeSystemDefined, ordering.Spec.AfterSystemDefined} {
		for _, ref := range refs {
			if ref.Name == name {
				return true
			}
		}
	}
	return false
}

// firewallPolicyStatusPredicate gates the UnifiFirewallPolicy watch: an ordering
// resolves a referenced policy through the policy's status.policyID, so it must
// react when that UUID is first resolved, the policy is deleted, or its spec
// changes.
func firewallPolicyStatusPredicate() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldPolicy, ok := e.ObjectOld.(*unifiv1alpha1.UnifiFirewallPolicy)
				if !ok {
					return false
				}
				newPolicy, ok := e.ObjectNew.(*unifiv1alpha1.UnifiFirewallPolicy)
				if !ok {
					return false
				}
				if oldPolicy.GetDeletionTimestamp().IsZero() && !newPolicy.GetDeletionTimestamp().IsZero() {
					return true
				}
				return oldPolicy.Status.PolicyID != newPolicy.Status.PolicyID
			},
		},
	)
}
