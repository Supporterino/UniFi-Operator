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
	// firewallPolicyDependencyRequeueAfter bounds how long a firewall policy
	// waits before retrying when a dependency (site, controller, zone, or
	// traffic matching list) is not yet ready. Those changes do not alter this
	// object's generation, so the periodic requeue is what lets a
	// late-appearing dependency self-heal.
	firewallPolicyDependencyRequeueAfter = 30 * time.Second

	// firewallPolicyOriginUserDefined is the upstream metadata.origin of a policy
	// the operator may manage. Every other origin (SYSTEM_DEFINED, DERIVED,
	// ORCHESTRATED) is system-managed and adopted read-only (design D2).
	firewallPolicyOriginUserDefined = "USER_DEFINED"

	// firewallFilterTrafficMatchingList is the IP/port filter discriminator for
	// a filter backed by a traffic matching list.
	firewallFilterTrafficMatchingList = "TRAFFIC_MATCHING_LIST"

	// firewallPolicyUnconfiguredZBFMessage explains a firewall policy reconcile
	// the console cannot serve because Zone-Based Firewall is off. The zone-read
	// gate and the upstream error classifier share it so the condition text
	// cannot drift between the two entry points.
	firewallPolicyUnconfiguredZBFMessage = "console reports Zone-Based Firewall is not configured; no upstream firewall policy was created or updated"
)

// UnifiFirewallPolicyReconciler reconciles a UnifiFirewallPolicy object. It
// resolves the policy's UnifiSite, connects through the site's controller,
// resolves its configurable and built-in zone selectors and its typed
// UnifiTrafficMatchingList filters, and creates, updates, or deletes the
// matching upstream firewall policy. System-defined upstream policies are
// adopted read-only. The upstream policy UUID and resolved zone UUIDs are
// recorded in status only; spec never carries a UniFi internal identifier.
type UnifiFirewallPolicyReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallzones,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifitrafficmatchinglists,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the policy's site and controller, resolves its zone and
// filter references, applies the declarative policy to the console, and reports
// the result in status. It is idempotent: re-running it on an unchanged object
// performs no writes. It fails closed (Ready=False, no upstream mutation) on an
// unresolved site, a version below the firewall/DNS minimum, an unresolved zone
// reference or built-in zone, an unresolvable traffic filter, or a
// system-defined upstream policy, and adds/removes its finalizer symmetrically.
func (r *UnifiFirewallPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policy := &unifiv1alpha1.UnifiFirewallPolicy{}
	if err := r.Get(ctx, req.NamespacedName, policy); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !policy.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, policy)
	}

	site, stop, err := r.reconcileSite(ctx, policy)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, policy, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, policy, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site. An absent, deleting, or not-yet-adopted site fails
// closed: a non-nil result means the reconcile has finished and the caller must
// stop, while a nil result means the returned site is usable.
func (r *UnifiFirewallPolicyReconciler) reconcileSite(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: policy.Namespace, Name: policy.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, policy, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", policy.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", policy.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, policy, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, policy, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite and adds the policy finalizer, in a single update when either is
// missing. The site's drain finalizer relies on the ownerReference.
func (r *UnifiFirewallPolicyReconciler) ensureOwnership(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(policy, site) {
		if err := controllerutil.SetControllerReference(site, policy, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(policy, unifiv1alpha1.UnifiFirewallPolicyFinalizer) {
		controllerutil.AddFinalizer(policy, unifiv1alpha1.UnifiFirewallPolicyFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, policy); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// selectorStatus maps a failed selector resolution to a status write or a
// transient error. stopped is false when the resolution succeeded.
func (r *UnifiFirewallPolicyReconciler) selectorStatus(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	res selectorResolution,
) (ctrl.Result, bool, error) {
	if res.err != nil {
		return ctrl.Result{}, true, res.err
	}
	if res.reason == "" {
		return ctrl.Result{}, false, nil
	}
	if res.retry {
		result, err := r.requeueStatus(ctx, policy, res.reason, res.message)
		return result, true, err
	}
	result, err := r.updateStatus(ctx, policy, false, res.reason, res.message, nil, nil, nil, nil)
	return result, true, err
}

// reconcileUpstream resolves the console through the site's controller, gates on
// the firewall/DNS capability, resolves the source and destination zones and
// their typed traffic filters, and creates, updates, or adopts the upstream
// policy. A system-defined upstream policy is adopted read-only.
func (r *UnifiFirewallPolicyReconciler) reconcileUpstream(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, policy.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, policy, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, policy, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityFirewallDNS); err != nil {
		return r.updateStatus(ctx, policy, false, reasonVersionUnsupported, err.Error(), nil, nil, nil, nil)
	}

	apiClient, err := resolver.Resolve(ctx, policy.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, policy, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	zones, zbfConfigured, err := apiClient.ListZones(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}
	// A console with Zone-Based Firewall off answers the zone read with the
	// subsystem code, which maps to an empty set and no error. Resolving a zone
	// selector against that set would report a missing zone (or a missing
	// built-in zone) and hot-requeue forever, so report the absent subsystem
	// terminally here, before any selector resolution, and write nothing
	// upstream (design D5).
	if !zbfConfigured {
		return r.updateStatus(ctx, policy, false, reasonZoneBasedFirewallNotConfigured,
			firewallPolicyUnconfiguredZBFMessage, nil, nil, nil, nil)
	}

	sourceRes := resolveZoneSelector(ctx, r.Client, policy.Namespace, policy.Spec.SiteRef.Name, policy.Spec.Source.ZoneRef, policy.Spec.Source.BuiltInZone, zones)
	if result, stopped, err := r.selectorStatus(ctx, policy, sourceRes); stopped {
		return result, err
	}
	destinationRes := resolveZoneSelector(ctx, r.Client, policy.Namespace, policy.Spec.SiteRef.Name, policy.Spec.Destination.ZoneRef, policy.Spec.Destination.BuiltInZone, zones)
	if result, stopped, err := r.selectorStatus(ctx, policy, destinationRes); stopped {
		return result, err
	}
	sourceFilterRes := resolveTrafficFilter(ctx, r.Client, policy.Namespace, policy.Spec.SiteRef.Name, policy.Spec.Source.Filter)
	if result, stopped, err := r.selectorStatus(ctx, policy, sourceFilterRes); stopped {
		return result, err
	}
	destinationFilterRes := resolveTrafficFilter(ctx, r.Client, policy.Namespace, policy.Spec.SiteRef.Name, policy.Spec.Destination.Filter)
	if result, stopped, err := r.selectorStatus(ctx, policy, destinationFilterRes); stopped {
		return result, err
	}
	sourceZoneID := sourceRes.value
	destinationZoneID := destinationRes.value
	sourceFilterID := sourceFilterRes.value
	destinationFilterID := destinationFilterRes.value

	request := buildFirewallPolicyRequest(policy, sourceZoneID, destinationZoneID, sourceFilterID, destinationFilterID)

	policies, err := apiClient.ListFirewallPolicies(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}
	existing, err := resolveExistingFirewallPolicy(ctx, apiClient, site.Status.SiteID, policy, policies)
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}

	// Fail closed on a system/derived/orchestrated policy: the operator only
	// owns USER_DEFINED policies. An empty origin is the operator's own.
	if existing != nil {
		origin := firstNonEmpty(existing.Metadata.Origin, policy.Status.Origin)
		if origin != "" && origin != firewallPolicyOriginUserDefined {
			return r.updateStatus(ctx, policy, false, reasonSystemObjectReadOnly,
				systemFirewallPolicyMessage(*existing, origin), nil, &sourceZoneID, &destinationZoneID, &origin)
		}
	}

	var (
		result unifi.FirewallPolicy
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateFirewallPolicy(ctx, site.Status.SiteID, request)
		action = broadcastActionCreated
	case firewallPolicyMatchesRequest(*existing, request):
		// The upstream object already matches the desired state; skip the PUT so
		// re-reconciling an unchanged object performs no write.
		policyID := firstNonEmpty(existing.ID, policy.Status.PolicyID)
		origin := firstNonEmpty(existing.Metadata.Origin, policy.Status.Origin)
		summary := fmt.Sprintf("upstream firewall policy %q is up to date", policy.Spec.Name)
		return r.updateStatus(ctx, policy, true, reasonReconciled, summary, &policyID, &sourceZoneID, &destinationZoneID, &origin)
	default:
		result, err = apiClient.UpdateFirewallPolicy(ctx, site.Status.SiteID, existing.ID, request)
		action = broadcastActionUpdated
	}
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}

	// Keep the last-known identifiers when a response omits them, so a
	// transiently incomplete body cannot erase a correlation ID.
	policyID := firstNonEmpty(result.ID, policy.Status.PolicyID)
	origin := firstNonEmpty(result.Metadata.Origin, policy.Status.Origin)
	if existing != nil {
		policyID = firstNonEmpty(policyID, existing.ID)
		origin = firstNonEmpty(origin, existing.Metadata.Origin)
	}
	if existing == nil && origin == "" {
		origin = firewallPolicyOriginUserDefined
	}

	summary := fmt.Sprintf("%s upstream firewall policy %q", action, policy.Spec.Name)
	return r.updateStatus(ctx, policy, true, reasonReconciled, summary, &policyID, &sourceZoneID, &destinationZoneID, &origin)
}

// reconcileDelete deletes the upstream policy recorded in status and removes the
// finalizer. The site still resolves because the site's own finalizer drains its
// children before the site (and its controller chain) disappears. When
// status.policyID is empty there is no upstream policy to delete.
func (r *UnifiFirewallPolicyReconciler) reconcileDelete(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(policy, unifiv1alpha1.UnifiFirewallPolicyFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, policy); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(policy, unifiv1alpha1.UnifiFirewallPolicyFinalizer)
	if err := r.Update(ctx, policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the policy recorded in status from the console. A 404
// means it is already gone and is treated as success. A system-managed policy is
// never deleted.
func (r *UnifiFirewallPolicyReconciler) deleteUpstream(ctx context.Context, policy *unifiv1alpha1.UnifiFirewallPolicy) error {
	if policy.Status.PolicyID == "" {
		return nil
	}
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: policy.Namespace, Name: policy.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		return fmt.Errorf("get UnifiSite %q for cleanup: %w", policy.Spec.SiteRef.Name, err)
	}
	if site.Status.SiteID == "" {
		return nil
	}
	apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, policy.Namespace, site.Spec.ControllerRef)
	if err != nil {
		return fmt.Errorf("resolve controller for cleanup: %w", err)
	}

	existing, err := apiClient.GetFirewallPolicy(ctx, site.Status.SiteID, policy.Status.PolicyID)
	if err != nil {
		if errors.Is(err, unifi.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get upstream firewall policy %q for cleanup: %w", policy.Status.PolicyID, err)
	}
	if existing.Metadata.Origin != firewallPolicyOriginUserDefined {
		return nil
	}

	if err := apiClient.DeleteFirewallPolicy(ctx, site.Status.SiteID, policy.Status.PolicyID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream firewall policy %q: %w", policy.Status.PolicyID, err)
	}
	return nil
}

// handleUpstreamError classifies a failed policy write. Certificate, auth, and
// bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiFirewallPolicyReconciler) handleUpstreamError(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, policy, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil, nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, policy, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil, nil, nil)
		case unifi.IsZoneBasedFirewallUnconfigured(err):
			// The spec is valid; the console's firewall subsystem is absent.
			// Report that distinctly from a bad spec and write nothing upstream
			// (design D5). This covers a policy read or write the console
			// rejects after the zone read succeeded.
			return r.updateStatus(ctx, policy, false, reasonZoneBasedFirewallNotConfigured,
				firewallPolicyUnconfiguredZBFMessage, nil, nil, nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, policy, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil, nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, policy, false, reasonInvalidSpec, specErr.Error(), nil, nil, nil, nil)
	}
	log.Error(err, "transient error reconciling firewall policy")
	return ctrl.Result{}, fmt.Errorf("reconcile firewall policy: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency failure a later edit can fix without changing this object's
// generation.
func (r *UnifiFirewallPolicyReconciler) requeueStatus(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, policy, false, reason, message, nil, nil, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = firewallPolicyDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil identifier pointer leaves the recorded value
// untouched, preserving the last-known identifiers on failing paths.
func (r *UnifiFirewallPolicyReconciler) updateStatus(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	ready bool,
	reason, message string,
	policyID, sourceZoneID, destinationZoneID, origin *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if policy.Status.ObservedGeneration != policy.Generation {
		policy.Status.ObservedGeneration = policy.Generation
		changed = true
	}
	if policy.Status.Summary != message {
		policy.Status.Summary = message
		changed = true
	}
	if policyID != nil && policy.Status.PolicyID != *policyID {
		policy.Status.PolicyID = *policyID
		changed = true
	}
	if sourceZoneID != nil && policy.Status.SourceZoneID != *sourceZoneID {
		policy.Status.SourceZoneID = *sourceZoneID
		changed = true
	}
	if destinationZoneID != nil && policy.Status.DestinationZoneID != *destinationZoneID {
		policy.Status.DestinationZoneID = *destinationZoneID
		changed = true
	}
	if origin != nil && policy.Status.Origin != *origin {
		policy.Status.Origin = *origin
		changed = true
	}
	if setReadyCondition(&policy.Status.Conditions, policy.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// buildFirewallPolicyRequest maps the declarative spec and the resolved zone and
// filter UUIDs onto the upstream create/update body.
func buildFirewallPolicyRequest(
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	sourceZoneID, destinationZoneID, sourceFilterID, destinationFilterID string,
) unifi.FirewallPolicyRequest {
	spec := policy.Spec
	request := unifi.FirewallPolicyRequest{
		Name:            spec.Name,
		Enabled:         boolDefaultTrue(spec.Enabled),
		LoggingEnabled:  boolValue(spec.LoggingEnabled),
		Action:          unifi.FirewallPolicyAction{Type: spec.Action.Type, AllowReturnTraffic: spec.Action.AllowReturnTraffic},
		IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: spec.IPProtocolScope.IPVersion},
		Source:          unifi.FirewallPolicyZone{ZoneID: sourceZoneID, TrafficFilter: buildFirewallTrafficFilter(spec.Source.Filter, sourceFilterID)},
		Destination:     unifi.FirewallPolicyZone{ZoneID: destinationZoneID, TrafficFilter: buildFirewallTrafficFilter(spec.Destination.Filter, destinationFilterID)},
	}
	if spec.Description != nil {
		request.Description = *spec.Description
	}
	return request
}

// buildFirewallTrafficFilter maps a typed traffic filter and the resolved
// matching-list UUID onto the upstream TRAFFIC_MATCHING_LIST filter. It returns
// nil for an absent filter.
func buildFirewallTrafficFilter(filter *unifiv1alpha1.FirewallTrafficFilter, listID string) *unifi.FirewallPolicyTrafficFilter {
	if filter == nil {
		return nil
	}
	out := &unifi.FirewallPolicyTrafficFilter{Type: filter.Type}
	switch {
	case filter.Port != nil:
		out.PortFilter = &unifi.FirewallPolicyPortFilter{
			Type:                  firewallFilterTrafficMatchingList,
			TrafficMatchingListID: listID,
			MatchOpposite:         boolValue(filter.Port.MatchOpposite),
		}
	case filter.IPAddress != nil:
		out.IPAddressFilter = &unifi.FirewallPolicyIPAddressFilter{
			Type:                  firewallFilterTrafficMatchingList,
			TrafficMatchingListID: listID,
			MatchOpposite:         boolValue(filter.IPAddress.MatchOpposite),
		}
	}
	return out
}

// findFirewallPolicyByName returns the upstream policy matching name, or nil.
func findFirewallPolicyByName(policies []unifi.FirewallPolicy, name string) *unifi.FirewallPolicy {
	for i := range policies {
		if policies[i].Name == name {
			return &policies[i]
		}
	}
	return nil
}

// resolveExistingFirewallPolicy returns the upstream policy this CR manages, or
// nil when none exists. It resolves status.policyID first so a spec.name rename
// updates the same upstream object instead of orphaning it, then falls back to a
// name match for adoption. A 404 while resolving status.policyID is treated as
// absent so a stale ID cannot block reconciliation.
func resolveExistingFirewallPolicy(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	policy *unifiv1alpha1.UnifiFirewallPolicy,
	policies []unifi.FirewallPolicy,
) (*unifi.FirewallPolicy, error) {
	if policy.Status.PolicyID != "" {
		existing, err := apiClient.GetFirewallPolicy(ctx, siteID, policy.Status.PolicyID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the name match: the recorded object is gone.
		default:
			return nil, err
		}
	}
	return findFirewallPolicyByName(policies, policy.Spec.Name), nil
}

// firewallPolicyMatchesRequest reports whether the observed upstream policy
// already matches the desired write, so the reconciler can skip the PUT and stay
// idempotent. Only authored fields are compared; observed fields (id, index,
// metadata) are ignored.
func firewallPolicyMatchesRequest(existing unifi.FirewallPolicy, req unifi.FirewallPolicyRequest) bool {
	observed := existing.FirewallPolicyRequest
	return observed.Name == req.Name &&
		observed.Enabled == req.Enabled &&
		observed.LoggingEnabled == req.LoggingEnabled &&
		observed.Description == req.Description &&
		observed.Action.Type == req.Action.Type &&
		optionalBoolPtrEqual(observed.Action.AllowReturnTraffic, req.Action.AllowReturnTraffic) &&
		observed.IPProtocolScope.IPVersion == req.IPProtocolScope.IPVersion &&
		firewallPolicyZoneMatches(observed.Source, req.Source) &&
		firewallPolicyZoneMatches(observed.Destination, req.Destination)
}

// firewallPolicyZoneMatches compares a source/destination zone and its traffic
// filter.
func firewallPolicyZoneMatches(observed, req unifi.FirewallPolicyZone) bool {
	if observed.ZoneID != req.ZoneID {
		return false
	}
	return firewallTrafficFilterMatches(observed.TrafficFilter, req.TrafficFilter)
}

// firewallTrafficFilterMatches compares two traffic filters, where an unset
// desired filter matches only an unset observed filter.
func firewallTrafficFilterMatches(observed, req *unifi.FirewallPolicyTrafficFilter) bool {
	switch {
	case req == nil:
		return observed == nil
	case observed == nil || observed.Type != req.Type:
		return false
	case req.IPAddressFilter != nil:
		return observed.IPAddressFilter != nil &&
			observed.IPAddressFilter.Type == req.IPAddressFilter.Type &&
			observed.IPAddressFilter.TrafficMatchingListID == req.IPAddressFilter.TrafficMatchingListID &&
			observed.IPAddressFilter.MatchOpposite == req.IPAddressFilter.MatchOpposite
	case req.PortFilter != nil:
		return observed.PortFilter != nil &&
			observed.PortFilter.Type == req.PortFilter.Type &&
			observed.PortFilter.TrafficMatchingListID == req.PortFilter.TrafficMatchingListID &&
			observed.PortFilter.MatchOpposite == req.PortFilter.MatchOpposite
	default:
		return true
	}
}

// optionalBoolPtrEqual compares two optional bools, treating two nils as equal.
func optionalBoolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// systemFirewallPolicyMessage explains why an upstream policy is read-only,
// without echoing credential material.
func systemFirewallPolicyMessage(existing unifi.FirewallPolicy, origin string) string {
	if origin == "" {
		origin = broadcastOriginUnknown
	}
	return fmt.Sprintf("upstream firewall policy %q has metadata.origin %s, not %s; refusing to overwrite a system-managed object",
		existing.Name, origin, firewallPolicyOriginUserDefined)
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes and the first deletion. The
// controller watches its UnifiSite and UnifiController dependencies, and the
// UnifiFirewallZone and UnifiTrafficMatchingList resources it references, so a
// late-reported upstream zone or list UUID retriggers reconciliation.
func (r *UnifiFirewallPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiFirewallPolicy{}, builder.WithPredicates(firewallPolicyPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.policiesInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.policiesForSite)).
		Watches(&unifiv1alpha1.UnifiFirewallZone{},
			handler.EnqueueRequestsFromMapFunc(r.policiesForZone),
			builder.WithPredicates(firewallZoneStatusPredicate())).
		Watches(&unifiv1alpha1.UnifiTrafficMatchingList{},
			handler.EnqueueRequestsFromMapFunc(r.policiesForTrafficList),
			builder.WithPredicates(trafficListStatusPredicate())).
		Named("unififirewallpolicy").
		Complete(r)
}

// policiesInNamespace enqueues every UnifiFirewallPolicy in the changed object's
// namespace. A UnifiController may be referenced by any policy, so its
// status-only change retriggers all of them.
func (r *UnifiFirewallPolicyReconciler) policiesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listPolicyRequests(ctx, obj.GetNamespace(), "", nil)
}

// policiesForSite enqueues the UnifiFirewallPolicies that reference the changed
// UnifiSite via spec.siteRef.
func (r *UnifiFirewallPolicyReconciler) policiesForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listPolicyRequests(ctx, obj.GetNamespace(), obj.GetName(), nil)
}

// policiesForZone enqueues the policies that reference the changed
// UnifiFirewallZone by name in their source or destination zoneRef.
func (r *UnifiFirewallPolicyReconciler) policiesForZone(ctx context.Context, obj client.Object) []reconcile.Request {
	zone, ok := obj.(*unifiv1alpha1.UnifiFirewallZone)
	if !ok {
		return nil
	}
	return r.listPolicyRequests(ctx, zone.Namespace, "", func(item *unifiv1alpha1.UnifiFirewallPolicy) bool {
		return referencedZoneRefNames(item)[zone.Name]
	})
}

// policiesForTrafficList enqueues the policies whose source or destination
// traffic filter references the changed UnifiTrafficMatchingList by name.
func (r *UnifiFirewallPolicyReconciler) policiesForTrafficList(ctx context.Context, obj client.Object) []reconcile.Request {
	list, ok := obj.(*unifiv1alpha1.UnifiTrafficMatchingList)
	if !ok {
		return nil
	}
	return r.listPolicyRequests(ctx, list.Namespace, "", func(item *unifiv1alpha1.UnifiFirewallPolicy) bool {
		return referencedTrafficListNames(item)[list.Name]
	})
}

// listPolicyRequests lists UnifiFirewallPolicies in namespace and applies an
// optional site filter and an optional predicate filter.
func (r *UnifiFirewallPolicyReconciler) listPolicyRequests(
	ctx context.Context,
	namespace, siteName string,
	keep func(*unifiv1alpha1.UnifiFirewallPolicy) bool,
) []reconcile.Request {
	var policies unifiv1alpha1.UnifiFirewallPolicyList
	if err := r.List(ctx, &policies, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list firewall policies for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(policies.Items))
	for i := range policies.Items {
		item := &policies.Items[i]
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

// referencedZoneRefNames returns the set of zoneRef names a policy references.
func referencedZoneRefNames(policy *unifiv1alpha1.UnifiFirewallPolicy) map[string]bool {
	names := make(map[string]bool, 2)
	for _, ref := range []*unifiv1alpha1.CoreRef{policy.Spec.Source.ZoneRef, policy.Spec.Destination.ZoneRef} {
		if ref != nil {
			names[ref.Name] = true
		}
	}
	return names
}

// referencedTrafficListNames returns the set of UnifiTrafficMatchingList names a
// policy's filters reference.
func referencedTrafficListNames(policy *unifiv1alpha1.UnifiFirewallPolicy) map[string]bool {
	names := make(map[string]bool, 2)
	for _, filter := range []*unifiv1alpha1.FirewallTrafficFilter{policy.Spec.Source.Filter, policy.Spec.Destination.Filter} {
		if filter == nil {
			continue
		}
		if ref, _ := filterListRef(filter); ref != nil {
			names[ref.Name] = true
		}
	}
	return names
}

// firewallPolicyPredicate triggers reconciles on generation changes and when a
// deletionTimestamp is first set. GenerationChangedPredicate drops metadata-only
// updates, but the finalizer cleanup runs on exactly such an update, so deletion
// detection is added alongside it.
func firewallPolicyPredicate() predicate.Predicate {
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

// firewallZoneStatusPredicate gates the UnifiFirewallZone watch: a policy
// resolves a configurable zone through the zone's status.zoneID, so it must
// react when that UUID is first resolved, the zone is deleted, or its spec
// changes. Other status-only updates are dropped to avoid reconcile churn.
func firewallZoneStatusPredicate() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldZone, ok := e.ObjectOld.(*unifiv1alpha1.UnifiFirewallZone)
				if !ok {
					return false
				}
				newZone, ok := e.ObjectNew.(*unifiv1alpha1.UnifiFirewallZone)
				if !ok {
					return false
				}
				if oldZone.GetDeletionTimestamp().IsZero() && !newZone.GetDeletionTimestamp().IsZero() {
					return true
				}
				return oldZone.Status.ZoneID != newZone.Status.ZoneID
			},
		},
	)
}

// trafficListStatusPredicate gates the UnifiTrafficMatchingList watch: a policy
// resolves a filter through the list's status.listID, so it must react when that
// UUID is first resolved, the list is deleted, or its spec changes.
func trafficListStatusPredicate() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldList, ok := e.ObjectOld.(*unifiv1alpha1.UnifiTrafficMatchingList)
				if !ok {
					return false
				}
				newList, ok := e.ObjectNew.(*unifiv1alpha1.UnifiTrafficMatchingList)
				if !ok {
					return false
				}
				if oldList.GetDeletionTimestamp().IsZero() && !newList.GetDeletionTimestamp().IsZero() {
					return true
				}
				return oldList.Status.ListID != newList.Status.ListID
			},
		},
	)
}
