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
	// aclOrderingDependencyRequeueAfter bounds how long an ordering object waits
	// before retrying when a dependency (site, controller, an ordered rule, or a
	// conflicting claimant) is not yet ready.
	aclOrderingDependencyRequeueAfter = 30 * time.Second
)

// UnifiAclRuleOrderingReconciler reconciles a UnifiAclRuleOrdering object. It is
// the authoritative writer of the site-global ACL-rule order: it detects
// duplicate ordering objects for the site, resolves the ordered rule references,
// applies the flat order upstream, and reports drift without silently
// reordering. System/derived rules are anchored and are never reordered. It
// reuses the shared ordering primitive (ordering.go) with a site-global scope
// callback.
type UnifiAclRuleOrderingReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiaclruleorderings,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiaclruleorderings/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiaclrules,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the ordering's site and controller, resolves the ordered
// rule references, and applies the site order upstream. It is idempotent. It
// fails closed (Ready=False, no upstream mutation) on an unresolved site, a
// version below the firewall/DNS minimum, a duplicate site ordering object, or an
// unresolved ordered rule reference.
func (r *UnifiAclRuleOrderingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ordering := &unifiv1alpha1.UnifiAclRuleOrdering{}
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

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite.
func (r *UnifiAclRuleOrderingReconciler) reconcileSite(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
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
// UnifiSite and adds the ordering finalizer.
func (r *UnifiAclRuleOrderingReconciler) ensureOwnership(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(ordering, site) {
		if err := controllerutil.SetControllerReference(site, ordering, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(ordering, unifiv1alpha1.UnifiAclRuleOrderingFinalizer) {
		controllerutil.AddFinalizer(ordering, unifiv1alpha1.UnifiAclRuleOrderingFinalizer)
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

// reconcileUpstream detects duplicate site ordering objects, resolves the
// ordered rule references, then applies the site order and reports drift.
func (r *UnifiAclRuleOrderingReconciler) reconcileUpstream(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
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
		return r.updateStatus(ctx, ordering, false, reasonVersionUnsupported, err.Error(), nil)
	}

	// Duplicate-scope conflict: one ordering object is authoritative for the
	// site; a peer fails closed on every claimant before any order is applied.
	all, err := r.listOrderingsInNamespace(ctx, ordering)
	if err != nil {
		return ctrl.Result{}, err
	}
	if claimants := OrderingClaimants(aclOrderingObjects(all), ordering, r.scopeKey); len(claimants) > 1 {
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

	resolveRule := func(name string) (string, bool) {
		rule := &unifiv1alpha1.UnifiAclRule{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: ordering.Namespace, Name: name}, rule); err != nil {
			return "", false
		}
		if rule.Spec.SiteRef.Name != ordering.Spec.SiteRef.Name || rule.Status.RuleID == "" {
			return "", false
		}
		return rule.Status.RuleID, true
	}
	ids, missing := OrderingIDs(ordering.Spec.OrderedAclRules, resolveRule)
	if len(missing) > 0 {
		return r.requeueStatus(ctx, ordering, reasonAclRuleRefNotFound,
			fmt.Sprintf("ordered ACL rule reference(s) %v do not resolve to a provisioned UnifiAclRule on the site", missing))
	}

	rules, err := apiClient.ListAclRules(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}
	drift := OrderingDrift(OrderingOrderedSet(ids, nil), upstreamAclRuleEntries(rules))

	request := unifi.AclRuleOrdering{OrderedAclRuleIDs: ids}
	existing, err := apiClient.GetAclRuleOrdering(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}
	if slices.Equal(existing.OrderedAclRuleIDs, request.OrderedAclRuleIDs) {
		result, err := r.updateStatus(ctx, ordering, true, reasonReconciled,
			"upstream ACL rule ordering is up to date", &drift)
		return result, err
	}
	if _, err := apiClient.UpdateAclRuleOrdering(ctx, site.Status.SiteID, request); err != nil {
		return r.handleUpstreamError(ctx, ordering, err)
	}

	summary := fmt.Sprintf("applied ACL rule ordering for %d rule(s)", len(ids))
	return r.updateStatus(ctx, ordering, true, reasonReconciled, summary, &drift)
}

// reconcileDelete removes the finalizer. Clearing ordering has no
// upstream-delete semantics, so no upstream call is made.
func (r *UnifiAclRuleOrderingReconciler) reconcileDelete(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(ordering, unifiv1alpha1.UnifiAclRuleOrderingFinalizer) {
		return ctrl.Result{}, nil
	}
	controllerutil.RemoveFinalizer(ordering, unifiv1alpha1.UnifiAclRuleOrderingFinalizer)
	if err := r.Update(ctx, ordering); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// handleUpstreamError classifies a failed ordering write.
func (r *UnifiAclRuleOrderingReconciler) handleUpstreamError(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, ordering, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, ordering, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, ordering, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, ordering, false, reasonInvalidSpec, specErr.Error(), nil)
	}
	log.Error(err, "transient error reconciling ACL rule ordering")
	return ctrl.Result{}, fmt.Errorf("reconcile ACL rule ordering: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiAclRuleOrderingReconciler) requeueStatus(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, ordering, false, reason, message, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = aclOrderingDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed. A nil drift pointer
// leaves the recorded value untouched.
func (r *UnifiAclRuleOrderingReconciler) updateStatus(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
	ready bool,
	reason, message string,
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

// scopeKey returns the site-global scope identity for an ordering object. The
// shared ordering primitive uses it to detect duplicate site ordering objects.
func (r *UnifiAclRuleOrderingReconciler) scopeKey(obj client.Object) string {
	ordering, ok := obj.(*unifiv1alpha1.UnifiAclRuleOrdering)
	if !ok {
		return ""
	}
	return ordering.Spec.SiteRef.Name
}

// listOrderingsInNamespace lists every UnifiAclRuleOrdering in the ordering's
// namespace.
func (r *UnifiAclRuleOrderingReconciler) listOrderingsInNamespace(
	ctx context.Context,
	ordering *unifiv1alpha1.UnifiAclRuleOrdering,
) ([]unifiv1alpha1.UnifiAclRuleOrdering, error) {
	var list unifiv1alpha1.UnifiAclRuleOrderingList
	if err := r.List(ctx, &list, client.InNamespace(ordering.Namespace)); err != nil {
		return nil, fmt.Errorf("list ACL rule orderings in namespace %q: %w", ordering.Namespace, err)
	}
	return list.Items, nil
}

// aclOrderingObjects returns the ordering objects as client.Objects, preserving
// the slice so status writes reach the right object.
func aclOrderingObjects(items []unifiv1alpha1.UnifiAclRuleOrdering) []client.Object {
	objs := make([]client.Object, 0, len(items))
	for i := range items {
		objs = append(objs, &items[i])
	}
	return objs
}

// markConflictingOrderings fails closed on every ordering object that shares its
// site with another, refreshing each claimant's Ready condition. It returns a
// bounded requeue so a resolved conflict is re-checked.
func (r *UnifiAclRuleOrderingReconciler) markConflictingOrderings(
	ctx context.Context,
	all []unifiv1alpha1.UnifiAclRuleOrdering,
) (ctrl.Result, error) {
	objs := aclOrderingObjects(all)
	for i := range all {
		claimants := OrderingClaimants(objs, &all[i], r.scopeKey)
		if len(claimants) <= 1 {
			continue
		}
		message := fmt.Sprintf("the site is claimed by more than one ACL rule ordering (%v); no order applied", claimants)
		if _, err := r.updateStatus(ctx, &all[i], false, reasonSiteOrderingConflict, message, nil); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{RequeueAfter: aclOrderingDependencyRequeueAfter}, nil
}

// upstreamAclRuleEntries returns the upstream ACL rules as ordering entries,
// marking non-USER_DEFINED (derived) ones as system (anchored).
func upstreamAclRuleEntries(rules []unifi.AclRule) []OrderingEntry {
	entries := make([]OrderingEntry, 0, len(rules))
	for i := range rules {
		rule := &rules[i]
		entries = append(entries, OrderingEntry{
			ID:     rule.ID,
			Name:   rule.Name,
			System: rule.Metadata.Origin != aclRuleOriginUserDefined,
		})
	}
	return entries
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiAclRuleOrderingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiAclRuleOrdering{}, builder.WithPredicates(aclRulePredicate())).
		Watches(&unifiv1alpha1.UnifiAclRuleOrdering{},
			handler.EnqueueRequestsFromMapFunc(r.orderingsForOrderingChange),
			builder.WithPredicates(aclRulePredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.orderingsInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.orderingsForSite)).
		Watches(&unifiv1alpha1.UnifiAclRule{},
			handler.EnqueueRequestsFromMapFunc(r.orderingsForRule),
			builder.WithPredicates(aclRuleStatusPredicate())).
		Named("unifiaclruleordering").
		Complete(r)
}

// orderingsInNamespace enqueues every UnifiAclRuleOrdering in the changed
// object's namespace.
func (r *UnifiAclRuleOrderingReconciler) orderingsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listOrderingRequests(ctx, obj.GetNamespace(), "", nil)
}

// orderingsForSite enqueues the orderings that reference the changed UnifiSite.
func (r *UnifiAclRuleOrderingReconciler) orderingsForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listOrderingRequests(ctx, obj.GetNamespace(), obj.GetName(), nil)
}

// orderingsForRule enqueues the orderings that list the changed ACL rule.
func (r *UnifiAclRuleOrderingReconciler) orderingsForRule(ctx context.Context, obj client.Object) []reconcile.Request {
	rule, ok := obj.(*unifiv1alpha1.UnifiAclRule)
	if !ok {
		return nil
	}
	return r.listOrderingRequests(ctx, rule.Namespace, "", func(item *unifiv1alpha1.UnifiAclRuleOrdering) bool {
		return orderingReferencesAclRule(item, rule.Name)
	})
}

// orderingsForOrderingChange enqueues the orderings sharing the changed
// ordering's site, so a conflict flips on all claimants and a resolved conflict
// clears the survivor.
func (r *UnifiAclRuleOrderingReconciler) orderingsForOrderingChange(ctx context.Context, obj client.Object) []reconcile.Request {
	changed, ok := obj.(*unifiv1alpha1.UnifiAclRuleOrdering)
	if !ok {
		return nil
	}
	key := r.scopeKey(changed)
	return r.listOrderingRequests(ctx, changed.Namespace, "", func(item *unifiv1alpha1.UnifiAclRuleOrdering) bool {
		return r.scopeKey(item) == key
	})
}

// listOrderingRequests lists UnifiAclRuleOrderings in namespace with an optional
// site filter and predicate filter.
func (r *UnifiAclRuleOrderingReconciler) listOrderingRequests(
	ctx context.Context,
	namespace, siteName string,
	keep func(*unifiv1alpha1.UnifiAclRuleOrdering) bool,
) []reconcile.Request {
	var orderings unifiv1alpha1.UnifiAclRuleOrderingList
	if err := r.List(ctx, &orderings, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list ACL rule orderings for dependency watch", "namespace", namespace)
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

// orderingReferencesAclRule reports whether an ordering lists the given rule.
func orderingReferencesAclRule(ordering *unifiv1alpha1.UnifiAclRuleOrdering, name string) bool {
	for _, ref := range ordering.Spec.OrderedAclRules {
		if ref.Name == name {
			return true
		}
	}
	return false
}

// aclRuleStatusPredicate gates the UnifiAclRule watch: an ordering resolves a
// referenced rule through the rule's status.ruleID, so it must react when that
// UUID is first resolved, the rule is deleted, or its spec changes.
func aclRuleStatusPredicate() predicate.Predicate {
	return predicate.Or(
		predicate.GenerationChangedPredicate{},
		predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldRule, ok := e.ObjectOld.(*unifiv1alpha1.UnifiAclRule)
				if !ok {
					return false
				}
				newRule, ok := e.ObjectNew.(*unifiv1alpha1.UnifiAclRule)
				if !ok {
					return false
				}
				if oldRule.GetDeletionTimestamp().IsZero() && !newRule.GetDeletionTimestamp().IsZero() {
					return true
				}
				return oldRule.Status.RuleID != newRule.Status.RuleID
			},
		},
	)
}
