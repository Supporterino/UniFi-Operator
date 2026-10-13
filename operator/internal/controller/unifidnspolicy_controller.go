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
	// dnsPolicyDependencyRequeueAfter bounds how long a DNS policy waits before
	// retrying when its site or controller is not yet ready.
	dnsPolicyDependencyRequeueAfter = 30 * time.Second

	// dnsPolicyOriginUserDefined is the upstream metadata.origin of a record the
	// operator may manage. Every other origin is adopted read-only.
	dnsPolicyOriginUserDefined = "USER_DEFINED"
)

// UnifiDnsPolicyReconciler reconciles a UnifiDnsPolicy object. It resolves the
// policy's UnifiSite, connects through the site's controller, maps the selected
// variant onto the upstream create/update body, and creates, updates, or deletes
// the matching upstream DNS record. The upstream record UUID is recorded in
// status only; spec never carries a UniFi internal identifier.
type UnifiDnsPolicyReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifidnspolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifidnspolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the policy's site and controller, applies the declarative
// DNS policy to the console, and reports the result in status. It is idempotent.
// It fails closed (Ready=False, no upstream mutation) on an unresolved site, a
// version below the firewall/DNS minimum, a missing variant sub-object, or a
// non-user-defined upstream record.
func (r *UnifiDnsPolicyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	policy := &unifiv1alpha1.UnifiDnsPolicy{}
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
// adopted an upstream site.
func (r *UnifiDnsPolicyReconciler) reconcileSite(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
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
// UnifiSite and adds the policy finalizer.
func (r *UnifiDnsPolicyReconciler) ensureOwnership(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(policy, site) {
		if err := controllerutil.SetControllerReference(site, policy, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(policy, unifiv1alpha1.UnifiDnsPolicyFinalizer) {
		controllerutil.AddFinalizer(policy, unifiv1alpha1.UnifiDnsPolicyFinalizer)
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

// reconcileUpstream resolves the console through the site's controller, gates on
// the firewall/DNS capability, maps the variant, and creates, updates, or adopts
// the upstream DNS record. A non-user-defined record is adopted read-only.
func (r *UnifiDnsPolicyReconciler) reconcileUpstream(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
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
	// DNS policies are gated on the firewall/DNS minimum (docs/unifi-api.md).
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityFirewallDNS); err != nil {
		return r.updateStatus(ctx, policy, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}

	request, err := buildDnsPolicyRequest(policy)
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}

	apiClient, err := resolver.Resolve(ctx, policy.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, policy, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	policies, err := apiClient.ListDnsPolicies(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}
	existing, err := resolveExistingDnsPolicy(ctx, apiClient, site.Status.SiteID, policy, policies)
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}
	if existing != nil && existing.Metadata.Origin != dnsPolicyOriginUserDefined {
		origin := firstNonEmpty(existing.Metadata.Origin, policy.Status.Origin)
		return r.updateStatus(ctx, policy, false, reasonSystemObjectReadOnly,
			systemDnsPolicyMessage(*existing, origin), nil, &origin)
	}

	var (
		result unifi.DnsPolicy
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateDnsPolicy(ctx, site.Status.SiteID, request)
		action = broadcastActionCreated
	case dnsPolicyMatchesRequest(*existing, request):
		policyID := firstNonEmpty(existing.ID, policy.Status.PolicyID)
		origin := firstNonEmpty(existing.Metadata.Origin, policy.Status.Origin)
		summary := fmt.Sprintf("upstream DNS record %q is up to date", policy.Spec.Domain)
		return r.updateStatus(ctx, policy, true, reasonReconciled, summary, &policyID, &origin)
	default:
		result, err = apiClient.UpdateDnsPolicy(ctx, site.Status.SiteID, existing.ID, request)
		action = broadcastActionUpdated
	}
	if err != nil {
		return r.handleUpstreamError(ctx, policy, err)
	}

	policyID := firstNonEmpty(result.ID, policy.Status.PolicyID)
	origin := firstNonEmpty(result.Metadata.Origin, policy.Status.Origin)
	if existing != nil {
		policyID = firstNonEmpty(policyID, existing.ID)
		origin = firstNonEmpty(origin, existing.Metadata.Origin)
	}
	if existing == nil && origin == "" {
		origin = dnsPolicyOriginUserDefined
	}

	summary := fmt.Sprintf("%s upstream DNS record %q", action, policy.Spec.Domain)
	return r.updateStatus(ctx, policy, true, reasonReconciled, summary, &policyID, &origin)
}

// reconcileDelete deletes the upstream record recorded in status and removes the
// finalizer. When status.policyID is empty there is no upstream record to
// delete.
func (r *UnifiDnsPolicyReconciler) reconcileDelete(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(policy, unifiv1alpha1.UnifiDnsPolicyFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, policy); err != nil {
		return ctrl.Result{}, err
	}
	controllerutil.RemoveFinalizer(policy, unifiv1alpha1.UnifiDnsPolicyFinalizer)
	if err := r.Update(ctx, policy); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the record recorded in status from the console. A 404
// means it is already gone. A non-user-defined record is never deleted.
func (r *UnifiDnsPolicyReconciler) deleteUpstream(ctx context.Context, policy *unifiv1alpha1.UnifiDnsPolicy) error {
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

	existing, err := apiClient.GetDnsPolicy(ctx, site.Status.SiteID, policy.Status.PolicyID)
	if err != nil {
		if errors.Is(err, unifi.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get upstream DNS record %q for cleanup: %w", policy.Status.PolicyID, err)
	}
	if existing.Metadata.Origin != dnsPolicyOriginUserDefined {
		return nil
	}
	if err := apiClient.DeleteDnsPolicy(ctx, site.Status.SiteID, policy.Status.PolicyID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream DNS record %q: %w", policy.Status.PolicyID, err)
	}
	return nil
}

// handleUpstreamError classifies a failed record write.
func (r *UnifiDnsPolicyReconciler) handleUpstreamError(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, policy, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, policy, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, policy, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, policy, false, reasonInvalidSpec, specErr.Error(), nil, nil)
	}
	log.Error(err, "transient error reconciling DNS policy")
	return ctrl.Result{}, fmt.Errorf("reconcile DNS policy: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiDnsPolicyReconciler) requeueStatus(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, policy, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = dnsPolicyDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed. A nil identifier
// pointer leaves the recorded value untouched.
func (r *UnifiDnsPolicyReconciler) updateStatus(
	ctx context.Context,
	policy *unifiv1alpha1.UnifiDnsPolicy,
	ready bool,
	reason, message string,
	policyID, origin *string,
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

// buildDnsPolicyRequest maps the selected variant onto the flat upstream body. A
// missing variant sub-object is a terminal spec error: CEL rejects it at the API
// server, but it is defended here too.
func buildDnsPolicyRequest(policy *unifiv1alpha1.UnifiDnsPolicy) (unifi.DnsPolicyRequest, error) {
	spec := policy.Spec
	request := unifi.DnsPolicyRequest{
		Type:    spec.Type,
		Domain:  spec.Domain,
		Enabled: boolDefaultTrue(spec.Enabled),
	}
	switch spec.Type {
	case "A_RECORD":
		if spec.ARecord == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		request.IPv4Address = spec.ARecord.IPv4Address
		ttl := spec.ARecord.TTLSeconds
		request.TTLSeconds = &ttl
	case "AAAA_RECORD":
		if spec.AaaaRecord == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		request.IPv6Address = spec.AaaaRecord.IPv6Address
		ttl := spec.AaaaRecord.TTLSeconds
		request.TTLSeconds = &ttl
	case "CNAME_RECORD":
		if spec.CnameRecord == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		request.TargetDomain = spec.CnameRecord.TargetDomain
		ttl := spec.CnameRecord.TTLSeconds
		request.TTLSeconds = &ttl
	case "MX_RECORD":
		if spec.MxRecord == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		request.MailServerDomain = spec.MxRecord.MailServerDomain
		priority := spec.MxRecord.Priority
		request.Priority = &priority
	case "SRV_RECORD":
		if spec.SrvRecord == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		port := spec.SrvRecord.Port
		priority := spec.SrvRecord.Priority
		weight := spec.SrvRecord.Weight
		request.Port = &port
		request.Priority = &priority
		request.Protocol = spec.SrvRecord.Protocol
		request.ServerDomain = spec.SrvRecord.ServerDomain
		request.Service = spec.SrvRecord.Service
		request.Weight = &weight
	case "TXT_RECORD":
		if spec.TxtRecord == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		request.Text = spec.TxtRecord.Text
	case "FORWARD_DOMAIN":
		if spec.ForwardDomain == nil {
			return unifi.DnsPolicyRequest{}, missingDnsVariant(spec.Type)
		}
		request.IPAddress = spec.ForwardDomain.IPAddress
	default:
		return unifi.DnsPolicyRequest{}, &unifi.InvalidSpecError{
			Field:  "type",
			Reason: fmt.Sprintf("%q is not a known DNS policy type", spec.Type),
		}
	}
	return request, nil
}

// missingDnsVariant reports the terminal spec error for a type whose variant
// sub-object is not set.
func missingDnsVariant(policyType string) error {
	return &unifi.InvalidSpecError{
		Field:  "type",
		Reason: fmt.Sprintf("DNS policy type %s is missing its variant value", policyType),
	}
}

// resolveExistingDnsPolicy returns the upstream record this CR manages, or nil
// when none exists. It resolves status.policyID first so a rename updates the
// same upstream object, then falls back to a (domain, type) match for adoption.
func resolveExistingDnsPolicy(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	policy *unifiv1alpha1.UnifiDnsPolicy,
	policies []unifi.DnsPolicy,
) (*unifi.DnsPolicy, error) {
	if policy.Status.PolicyID != "" {
		existing, err := apiClient.GetDnsPolicy(ctx, siteID, policy.Status.PolicyID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the domain/type match: the recorded object is gone.
		default:
			return nil, err
		}
	}
	for i := range policies {
		if policies[i].Domain == policy.Spec.Domain && policies[i].Type == policy.Spec.Type {
			return &policies[i], nil
		}
	}
	return nil, nil
}

// dnsPolicyMatchesRequest reports whether the observed upstream record already
// matches the desired write, so the reconciler can skip the PUT and stay
// idempotent. Only authored fields are compared; observed fields (id, metadata)
// are ignored.
func dnsPolicyMatchesRequest(existing unifi.DnsPolicy, req unifi.DnsPolicyRequest) bool {
	o := existing.DnsPolicyRequest
	return o.Type == req.Type &&
		o.Domain == req.Domain &&
		o.Enabled == req.Enabled &&
		o.IPv4Address == req.IPv4Address &&
		o.IPv6Address == req.IPv6Address &&
		optionalInt32PtrEqual(o.TTLSeconds, req.TTLSeconds) &&
		o.TargetDomain == req.TargetDomain &&
		o.MailServerDomain == req.MailServerDomain &&
		optionalInt32PtrEqual(o.Priority, req.Priority) &&
		optionalInt32PtrEqual(o.Port, req.Port) &&
		o.Protocol == req.Protocol &&
		o.ServerDomain == req.ServerDomain &&
		o.Service == req.Service &&
		optionalInt32PtrEqual(o.Weight, req.Weight) &&
		o.Text == req.Text &&
		o.IPAddress == req.IPAddress
}

// systemDnsPolicyMessage explains why an upstream record is read-only, without
// echoing credential material.
func systemDnsPolicyMessage(existing unifi.DnsPolicy, origin string) string {
	if origin == "" {
		origin = broadcastOriginUnknown
	}
	return fmt.Sprintf("upstream DNS record %q has metadata.origin %s, not %s; refusing to overwrite a system-managed object",
		existing.Domain, origin, dnsPolicyOriginUserDefined)
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiDnsPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiDnsPolicy{}, builder.WithPredicates(dnsPolicyPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.policiesInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.policiesForSite)).
		Named("unifidnspolicy").
		Complete(r)
}

// policiesInNamespace enqueues every UnifiDnsPolicy in the changed object's
// namespace.
func (r *UnifiDnsPolicyReconciler) policiesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listPolicyRequests(ctx, obj.GetNamespace(), "")
}

// policiesForSite enqueues the policies that reference the changed UnifiSite.
func (r *UnifiDnsPolicyReconciler) policiesForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listPolicyRequests(ctx, obj.GetNamespace(), obj.GetName())
}

// listPolicyRequests lists UnifiDnsPolicies in namespace, optionally filtered to
// a site.
func (r *UnifiDnsPolicyReconciler) listPolicyRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var policies unifiv1alpha1.UnifiDnsPolicyList
	if err := r.List(ctx, &policies, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list DNS policies for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(policies.Items))
	for i := range policies.Items {
		item := &policies.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// dnsPolicyPredicate triggers reconciles on generation changes and when a
// deletionTimestamp is first set.
func dnsPolicyPredicate() predicate.Predicate {
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
