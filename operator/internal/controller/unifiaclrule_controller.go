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
	// aclRuleDependencyRequeueAfter bounds how long an ACL rule waits before
	// retrying when a dependency (site, controller, network, or device tag) is
	// not yet ready.
	aclRuleDependencyRequeueAfter = 30 * time.Second

	// aclDeviceFilterType is the only upstream enforcing-device filter variant
	// the operator authors.
	aclDeviceFilterType = "DEVICES"

	// aclRuleOriginUserDefined is the upstream metadata.origin of a rule the
	// operator may manage. Every other origin (DERIVED) is read-only.
	aclRuleOriginUserDefined = "USER_DEFINED"
)

// UnifiAclRuleReconciler reconciles a UnifiAclRule object. It resolves the
// rule's UnifiSite, connects through the site's controller, resolves its typed
// source/destination filters and network references (by Kubernetes identity) and
// its enforcing-device tag selectors (against the read-only device-tags list),
// and creates, updates, or deletes the matching upstream ACL rule. Derived
// upstream rules are adopted read-only. The upstream rule UUID is recorded in
// status only; spec never carries a UniFi internal identifier.
type UnifiAclRuleReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiaclrules,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiaclrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the rule's site and controller, applies the declarative
// rule to the console, and reports the result in status. It is idempotent. It
// fails closed (Ready=False, no upstream mutation) on an unresolved site, a
// version below the firewall/DNS minimum, an unresolved network reference, an
// unknown or empty enforcing-device tag selector, or a derived upstream rule.
func (r *UnifiAclRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	rule := &unifiv1alpha1.UnifiAclRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !rule.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, rule)
	}

	site, stop, err := r.reconcileSite(ctx, rule)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, rule, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, rule, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site.
func (r *UnifiAclRuleReconciler) reconcileSite(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: rule.Namespace, Name: rule.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, rule, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", rule.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", rule.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, rule, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, rule, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite and adds the rule finalizer, in a single update when either is
// missing. The site's drain finalizer relies on the ownerReference.
func (r *UnifiAclRuleReconciler) ensureOwnership(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(rule, site) {
		if err := controllerutil.SetControllerReference(site, rule, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(rule, unifiv1alpha1.UnifiAclRuleFinalizer) {
		controllerutil.AddFinalizer(rule, unifiv1alpha1.UnifiAclRuleFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, rule); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console through the site's controller, gates on
// the firewall/DNS capability, resolves the typed filters, networks, and
// enforcing devices, and creates, updates, or adopts the upstream ACL rule. A
// derived upstream rule is adopted read-only.
func (r *UnifiAclRuleReconciler) reconcileUpstream(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, rule.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, rule, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, rule, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityFirewallDNS); err != nil {
		return r.updateStatus(ctx, rule, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}

	apiClient, err := resolver.Resolve(ctx, rule.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, rule, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	source, err := r.resolveEndpoint(ctx, rule, rule.Spec.Source)
	if err != nil {
		return ctrl.Result{}, err
	}
	if source.stop != nil {
		return *source.stop, nil
	}
	destination, err := r.resolveEndpoint(ctx, rule, rule.Spec.Destination)
	if err != nil {
		return ctrl.Result{}, err
	}
	if destination.stop != nil {
		return *destination.stop, nil
	}

	networkID := ""
	if rule.Spec.NetworkRef != nil {
		res := resolveAclNetworkRef(ctx, r.Client, rule.Namespace, rule.Spec.SiteRef.Name, *rule.Spec.NetworkRef)
		if result, stopped, err := r.selectorStatus(ctx, rule, res); stopped {
			return result, err
		}
		networkID = res.value
	}

	deviceIDs, reason, message, err := resolveEnforcingDeviceIDs(ctx, apiClient, site.Status.SiteID, rule.Spec.EnforcingDeviceTags)
	if err != nil {
		return r.handleUpstreamError(ctx, rule, err)
	}
	if reason != "" {
		return r.updateStatus(ctx, rule, false, reason, message, nil, nil)
	}

	request := buildAclRuleRequest(rule, source.value, destination.value, networkID, deviceIDs)

	rules, err := apiClient.ListAclRules(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, rule, err)
	}
	existing, err := resolveExistingAclRule(ctx, apiClient, site.Status.SiteID, rule, rules)
	if err != nil {
		return r.handleUpstreamError(ctx, rule, err)
	}
	if existing != nil && existing.Metadata.Origin != aclRuleOriginUserDefined {
		origin := firstNonEmpty(existing.Metadata.Origin, rule.Status.Origin)
		return r.updateStatus(ctx, rule, false, reasonSystemObjectReadOnly,
			systemAclRuleMessage(*existing, origin), nil, &origin)
	}

	var (
		result unifi.AclRule
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateAclRule(ctx, site.Status.SiteID, request)
		action = broadcastActionCreated
	case aclRuleMatchesRequest(*existing, request):
		ruleID := firstNonEmpty(existing.ID, rule.Status.RuleID)
		origin := firstNonEmpty(existing.Metadata.Origin, rule.Status.Origin)
		summary := fmt.Sprintf("upstream ACL rule %q is up to date", rule.Spec.Name)
		return r.updateStatus(ctx, rule, true, reasonReconciled, summary, &ruleID, &origin)
	default:
		result, err = apiClient.UpdateAclRule(ctx, site.Status.SiteID, existing.ID, request)
		action = broadcastActionUpdated
	}
	if err != nil {
		return r.handleUpstreamError(ctx, rule, err)
	}

	ruleID := firstNonEmpty(result.ID, rule.Status.RuleID)
	origin := firstNonEmpty(result.Metadata.Origin, rule.Status.Origin)
	if existing != nil {
		ruleID = firstNonEmpty(ruleID, existing.ID)
		origin = firstNonEmpty(origin, existing.Metadata.Origin)
	}
	if existing == nil && origin == "" {
		origin = aclRuleOriginUserDefined
	}

	summary := fmt.Sprintf("%s upstream ACL rule %q", action, rule.Spec.Name)
	return r.updateStatus(ctx, rule, true, reasonReconciled, summary, &ruleID, &origin)
}

// endpointResolution is a resolved endpoint plus a non-nil stop result when the
// resolve failed and the reconcile must finish.
type endpointResolution struct {
	value unifi.AclRuleEndpoint
	stop  *ctrl.Result
}

// resolveEndpoint resolves an ACL endpoint, mapping a failed selector resolution
// to a status write.
func (r *UnifiAclRuleReconciler) resolveEndpoint(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	endpoint unifiv1alpha1.AclRuleEndpoint,
) (endpointResolution, error) {
	value, res := resolveAclEndpoint(ctx, r.Client, rule.Namespace, rule.Spec.SiteRef.Name, endpoint)
	result, stopped, err := r.selectorStatus(ctx, rule, res)
	if stopped {
		return endpointResolution{stop: &result}, err
	}
	return endpointResolution{value: value}, nil
}

// reconcileDelete deletes the upstream rule recorded in status and removes the
// finalizer. When status.ruleID is empty there is no upstream rule to delete.
func (r *UnifiAclRuleReconciler) reconcileDelete(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(rule, unifiv1alpha1.UnifiAclRuleFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, rule); err != nil {
		return ctrl.Result{}, err
	}
	controllerutil.RemoveFinalizer(rule, unifiv1alpha1.UnifiAclRuleFinalizer)
	if err := r.Update(ctx, rule); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the rule recorded in status from the console. A 404
// means it is already gone and is treated as success. A derived rule is never
// deleted.
func (r *UnifiAclRuleReconciler) deleteUpstream(ctx context.Context, rule *unifiv1alpha1.UnifiAclRule) error {
	if rule.Status.RuleID == "" {
		return nil
	}
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: rule.Namespace, Name: rule.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		return fmt.Errorf("get UnifiSite %q for cleanup: %w", rule.Spec.SiteRef.Name, err)
	}
	if site.Status.SiteID == "" {
		return nil
	}
	apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, rule.Namespace, site.Spec.ControllerRef)
	if err != nil {
		return fmt.Errorf("resolve controller for cleanup: %w", err)
	}

	existing, err := apiClient.GetAclRule(ctx, site.Status.SiteID, rule.Status.RuleID)
	if err != nil {
		if errors.Is(err, unifi.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get upstream ACL rule %q for cleanup: %w", rule.Status.RuleID, err)
	}
	if existing.Metadata.Origin != aclRuleOriginUserDefined {
		return nil
	}
	if err := apiClient.DeleteAclRule(ctx, site.Status.SiteID, rule.Status.RuleID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream ACL rule %q: %w", rule.Status.RuleID, err)
	}
	return nil
}

// handleUpstreamError classifies a failed rule write. Certificate, auth, and
// bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiAclRuleReconciler) handleUpstreamError(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, rule, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, rule, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, rule, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, rule, false, reasonInvalidSpec, specErr.Error(), nil, nil)
	}
	log.Error(err, "transient error reconciling ACL rule")
	return ctrl.Result{}, fmt.Errorf("reconcile ACL rule: %w", err)
}

// selectorStatus maps a failed selector resolution to a status write or a
// transient error. stopped is false when the resolution succeeded.
func (r *UnifiAclRuleReconciler) selectorStatus(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	res selectorResolution,
) (ctrl.Result, bool, error) {
	if res.err != nil {
		return ctrl.Result{}, true, res.err
	}
	if res.reason == "" {
		return ctrl.Result{}, false, nil
	}
	if res.retry {
		result, err := r.requeueStatus(ctx, rule, res.reason, res.message)
		return result, true, err
	}
	result, err := r.updateStatus(ctx, rule, false, res.reason, res.message, nil, nil)
	return result, true, err
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiAclRuleReconciler) requeueStatus(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, rule, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = aclRuleDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil identifier pointer leaves the recorded value
// untouched.
func (r *UnifiAclRuleReconciler) updateStatus(
	ctx context.Context,
	rule *unifiv1alpha1.UnifiAclRule,
	ready bool,
	reason, message string,
	ruleID, origin *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if rule.Status.ObservedGeneration != rule.Generation {
		rule.Status.ObservedGeneration = rule.Generation
		changed = true
	}
	if rule.Status.Summary != message {
		rule.Status.Summary = message
		changed = true
	}
	if ruleID != nil && rule.Status.RuleID != *ruleID {
		rule.Status.RuleID = *ruleID
		changed = true
	}
	if origin != nil && rule.Status.Origin != *origin {
		rule.Status.Origin = *origin
		changed = true
	}
	if setReadyCondition(&rule.Status.Conditions, rule.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, rule); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// buildAclRuleRequest maps the declarative spec and the resolved network and
// device UUIDs onto the upstream create/update body.
func buildAclRuleRequest(
	rule *unifiv1alpha1.UnifiAclRule,
	source, destination unifi.AclRuleEndpoint,
	networkID string,
	deviceIDs []string,
) unifi.AclRuleRequest {
	request := unifi.AclRuleRequest{
		Type:            rule.Spec.Type,
		Name:            rule.Spec.Name,
		Enabled:         boolDefaultTrue(rule.Spec.Enabled),
		Action:          rule.Spec.Action,
		Source:          source,
		Destination:     destination,
		NetworkIDFilter: networkID,
	}
	if rule.Spec.Description != nil {
		request.Description = *rule.Spec.Description
	}
	for _, protocol := range rule.Spec.ProtocolFilter {
		request.ProtocolFilter = append(request.ProtocolFilter, string(protocol))
	}
	if len(deviceIDs) > 0 {
		request.EnforcingDeviceFilter = &unifi.AclRuleDeviceFilter{Type: aclDeviceFilterType, DeviceIDs: deviceIDs}
	}
	return request
}

// findAclRuleByName returns the upstream rule matching name, or nil.
func findAclRuleByName(rules []unifi.AclRule, name string) *unifi.AclRule {
	for i := range rules {
		if rules[i].Name == name {
			return &rules[i]
		}
	}
	return nil
}

// resolveExistingAclRule returns the upstream rule this CR manages, or nil when
// none exists. It resolves status.ruleID first so a spec.name rename updates the
// same upstream object, then falls back to a name match.
func resolveExistingAclRule(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	rule *unifiv1alpha1.UnifiAclRule,
	rules []unifi.AclRule,
) (*unifi.AclRule, error) {
	if rule.Status.RuleID != "" {
		existing, err := apiClient.GetAclRule(ctx, siteID, rule.Status.RuleID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the name match: the recorded object is gone.
		default:
			return nil, err
		}
	}
	return findAclRuleByName(rules, rule.Spec.Name), nil
}

// aclRuleMatchesRequest reports whether the observed upstream rule already
// matches the desired write, so the reconciler can skip the PUT and stay
// idempotent. Only authored fields are compared; observed fields (id, index,
// metadata) are ignored; collection order is insignificant.
func aclRuleMatchesRequest(existing unifi.AclRule, req unifi.AclRuleRequest) bool {
	observed := existing.AclRuleRequest
	if observed.Type != req.Type ||
		observed.Name != req.Name ||
		observed.Enabled != req.Enabled ||
		observed.Description != req.Description ||
		observed.Action != req.Action ||
		observed.NetworkIDFilter != req.NetworkIDFilter {
		return false
	}
	if !stringSlicesMatch(observed.ProtocolFilter, req.ProtocolFilter) {
		return false
	}
	if !aclEndpointMatches(observed.Source, req.Source) || !aclEndpointMatches(observed.Destination, req.Destination) {
		return false
	}
	return stringSlicesMatch(aclDeviceFilterIDs(observed.EnforcingDeviceFilter), aclDeviceFilterIDs(req.EnforcingDeviceFilter))
}

// aclEndpointMatches compares a source/destination filter, ignoring collection
// order.
func aclEndpointMatches(observed, req unifi.AclRuleEndpoint) bool {
	return observed.Type == req.Type &&
		stringSlicesMatch(observed.IPAddressesOrSubnets, req.IPAddressesOrSubnets) &&
		stringSlicesMatch(observed.NetworkIDs, req.NetworkIDs) &&
		int32SlicesMatch(observed.PortFilter, req.PortFilter) &&
		stringSlicesMatch(observed.MacAddresses, req.MacAddresses) &&
		optionalInt32PtrEqual(observed.PrefixLength, req.PrefixLength)
}

// aclDeviceFilterIDs returns the device IDs of an enforcing-device filter,
// treating nil as empty.
func aclDeviceFilterIDs(filter *unifi.AclRuleDeviceFilter) []string {
	if filter == nil {
		return nil
	}
	return filter.DeviceIDs
}

// int32SlicesMatch reports whether two int32 slices hold the same values,
// treating nil and empty as equal and ignoring order.
func int32SlicesMatch(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	as := append([]int32(nil), a...)
	bs := append([]int32(nil), b...)
	sort.Slice(as, func(i, j int) bool { return as[i] < as[j] })
	sort.Slice(bs, func(i, j int) bool { return bs[i] < bs[j] })
	return slices.Equal(as, bs)
}

// optionalInt32PtrEqual compares two optional int32s, treating two nils as
// equal.
func optionalInt32PtrEqual(a, b *int32) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// systemAclRuleMessage explains why an upstream rule is read-only, without
// echoing credential material.
func systemAclRuleMessage(existing unifi.AclRule, origin string) string {
	if origin == "" {
		origin = broadcastOriginUnknown
	}
	return fmt.Sprintf("upstream ACL rule %q has metadata.origin %s, not %s; refusing to overwrite a derived object",
		existing.Name, origin, aclRuleOriginUserDefined)
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiAclRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiAclRule{}, builder.WithPredicates(aclRulePredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.rulesInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.rulesForSite)).
		Watches(&unifiv1alpha1.UnifiNetwork{},
			handler.EnqueueRequestsFromMapFunc(r.rulesForNetwork),
			builder.WithPredicates(networkDependencyPredicate())).
		Named("unifiaclrule").
		Complete(r)
}

// rulesInNamespace enqueues every UnifiAclRule in the changed object's
// namespace.
func (r *UnifiAclRuleReconciler) rulesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listRuleRequests(ctx, obj.GetNamespace(), "", nil)
}

// rulesForSite enqueues the rules that reference the changed UnifiSite.
func (r *UnifiAclRuleReconciler) rulesForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listRuleRequests(ctx, obj.GetNamespace(), obj.GetName(), nil)
}

// rulesForNetwork enqueues the rules that reference the changed network by name.
func (r *UnifiAclRuleReconciler) rulesForNetwork(ctx context.Context, obj client.Object) []reconcile.Request {
	network, ok := obj.(*unifiv1alpha1.UnifiNetwork)
	if !ok {
		return nil
	}
	return r.listRuleRequests(ctx, network.Namespace, "", func(item *unifiv1alpha1.UnifiAclRule) bool {
		return referencedAclNetworkNames(item)[network.Name]
	})
}

// listRuleRequests lists UnifiAclRules in namespace with an optional site filter
// and predicate filter.
func (r *UnifiAclRuleReconciler) listRuleRequests(
	ctx context.Context,
	namespace, siteName string,
	keep func(*unifiv1alpha1.UnifiAclRule) bool,
) []reconcile.Request {
	var rules unifiv1alpha1.UnifiAclRuleList
	if err := r.List(ctx, &rules, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list ACL rules for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(rules.Items))
	for i := range rules.Items {
		item := &rules.Items[i]
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

// referencedAclNetworkNames returns the set of network names an ACL rule
// references (the MAC networkRef and any NETWORKS endpoint references).
func referencedAclNetworkNames(rule *unifiv1alpha1.UnifiAclRule) map[string]bool {
	names := make(map[string]bool, 2)
	if rule.Spec.NetworkRef != nil {
		names[rule.Spec.NetworkRef.Name] = true
	}
	for _, endpoint := range []unifiv1alpha1.AclRuleEndpoint{rule.Spec.Source, rule.Spec.Destination} {
		if endpoint.Networks == nil {
			continue
		}
		for _, ref := range endpoint.Networks.NetworkRefs {
			names[ref.Name] = true
		}
	}
	return names
}

// aclRulePredicate is the generation-or-first-deletion predicate shared by the
// ACL rule and ordering controllers.
func aclRulePredicate() predicate.Predicate {
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
