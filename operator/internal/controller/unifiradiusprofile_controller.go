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
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/metrics"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	// radiusProfileDependencyRequeueAfter bounds how long a RADIUS profile
	// waits before retrying when its site, controller, or named upstream
	// profile is not yet present. Those changes do not alter this object's
	// generation, so the periodic requeue is what lets a late-appearing
	// dependency self-heal.
	radiusProfileDependencyRequeueAfter = 30 * time.Second
)

// UnifiRadiusProfileReconciler reconciles a UnifiRadiusProfile object. The
// frozen Integration v1 v10.4.57 surface exposes RADIUS profiles read-only, so
// the reconciler is adoption-only: it resolves the profile's UnifiSite,
// connects through the site's controller, adopts the existing upstream profile
// named by spec.name, and records the resolved profile UUID and origin in
// status. It never creates, updates, or deletes the upstream profile and
// carries no finalizer, so adoption has no upstream side effect and spec never
// carries a UniFi internal identifier.
type UnifiRadiusProfileReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader reads credential Secrets through an uncached reader so the
	// manager never caches Secrets (see connection.go). Nil falls back to the
	// cached client in tests.
	APIReader client.Reader

	// NewClient constructs the UniFi client for the resolved controller;
	// defaults to unifi.NewClient and is injectable for tests.
	NewClient ConnectionFactory

	// Recorder records Ready-condition transitions as Kubernetes Events. It is
	// constructed in cmd/main.go with the same name as the controller so the
	// Event source matches kubectl describe.
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiradiusprofiles,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiradiusprofiles/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the profile's site and controller, adopts the existing
// upstream profile named by spec.name, and reports the result in status. It is
// idempotent: re-running it on an unchanged object performs no writes. It fails
// closed (Ready=False, no upstream mutation) on an unresolved site, a version
// below the Official API minimum, or a spec.name that names no upstream
// profile. Adoption has no upstream effect, so the reconciler adds no finalizer.
func (r *UnifiRadiusProfileReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	profile := &unifiv1alpha1.UnifiRadiusProfile{}
	if err := r.Get(ctx, req.NamespacedName, profile); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !profile.DeletionTimestamp.IsZero() {
		// Adoption has no upstream effect and carries no finalizer, so there is
		// nothing to clean up; the object is removed without further action.
		return ctrl.Result{}, nil
	}

	site, stop, err := r.reconcileSite(ctx, profile)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, profile, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, profile, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site. An absent, deleting, or not-yet-adopted site fails
// closed: a non-nil result means the reconcile has finished and the caller must
// stop, while a nil result means the returned site is usable.
func (r *UnifiRadiusProfileReconciler) reconcileSite(
	ctx context.Context,
	profile *unifiv1alpha1.UnifiRadiusProfile,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: profile.Namespace, Name: profile.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, profile, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", profile.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", profile.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, profile, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, profile, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite so the site's drain finalizer can hold the site until the profile is
// gone. Adoption has no upstream effect, so no finalizer is added.
func (r *UnifiRadiusProfileReconciler) ensureOwnership(
	ctx context.Context,
	profile *unifiv1alpha1.UnifiRadiusProfile,
	site *unifiv1alpha1.UnifiSite,
) error {
	if metav1.IsControlledBy(profile, site) {
		return nil
	}
	if err := controllerutil.SetControllerReference(site, profile, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
	}
	if err := r.Update(ctx, profile); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console through the site's controller, gates
// on the Official API capability, and adopts the existing upstream profile named
// by spec.name. A name that matches no profile on the site fails closed and is
// retried, so a late-appearing profile self-heals.
func (r *UnifiRadiusProfileReconciler) reconcileUpstream(
	ctx context.Context,
	profile *unifiv1alpha1.UnifiRadiusProfile,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, profile.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, profile, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, profile, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	// The read-only RADIUS profile list is part of the overall Integration v1
	// surface, so it is gated on the Official API minimum.
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, profile, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}

	apiClient, err := resolver.Resolve(ctx, profile.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, profile, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	profiles, err := apiClient.ListRadiusProfiles(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, profile, err)
	}

	existing := findRadiusProfileByName(profiles, profile.Spec.Name)
	if existing == nil {
		// Clear a previously recorded UUID so a stale identifier cannot be
		// consumed after the profile is renamed or deleted upstream.
		emptyID, emptyOrigin := "", ""
		result, err := r.updateStatus(ctx, profile, false, reasonProfileNotFound,
			fmt.Sprintf("no upstream RADIUS profile named %q; nothing adopted", profile.Spec.Name), &emptyID, &emptyOrigin)
		if err != nil {
			return result, err
		}
		result.RequeueAfter = radiusProfileDependencyRequeueAfter
		return result, nil
	}

	profileID := existing.ID
	origin := existing.Metadata.Origin
	summary := fmt.Sprintf("adopted upstream RADIUS profile %q", profile.Spec.Name)
	return r.updateStatus(ctx, profile, true, reasonReconciled, summary, &profileID, &origin)
}

// handleUpstreamError classifies a failed profile listing. Certificate and auth
// failures are terminal and fail closed; a transient failure is returned so
// controller-runtime backs off.
func (r *UnifiRadiusProfileReconciler) handleUpstreamError(
	ctx context.Context,
	profile *unifiv1alpha1.UnifiRadiusProfile,
	err error,
) (ctrl.Result, error) {
	if isCertificateError(err) {
		return r.updateStatus(ctx, profile, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, profile, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, profile, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, profile, false, reasonInvalidSpec, specErr.Error(), nil, nil)
	}
	return ctrl.Result{}, fmt.Errorf("reconcile RADIUS profile: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency failure a later edit can fix without changing this object's
// generation.
func (r *UnifiRadiusProfileReconciler) requeueStatus(
	ctx context.Context,
	profile *unifiv1alpha1.UnifiRadiusProfile,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, profile, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = radiusProfileDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil profileID or origin leaves the recorded value
// untouched; the unknown-profile path passes an explicit empty value to clear a
// stale UUID.
func (r *UnifiRadiusProfileReconciler) updateStatus(
	ctx context.Context,
	profile *unifiv1alpha1.UnifiRadiusProfile,
	ready bool,
	reason, message string,
	profileID, origin *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if profile.Status.ObservedGeneration != profile.Generation {
		profile.Status.ObservedGeneration = profile.Generation
		changed = true
	}
	if profile.Status.Summary != message {
		profile.Status.Summary = message
		changed = true
	}
	if profileID != nil && profile.Status.RadiusProfileID != *profileID {
		profile.Status.RadiusProfileID = *profileID
		changed = true
	}
	if origin != nil && profile.Status.Origin != *origin {
		profile.Status.Origin = *origin
		changed = true
	}
	readyChanged := setReadyCondition(&profile.Status.Conditions, profile.Generation, ready, reason, message)
	if readyChanged {
		changed = true
	}

	if changed {
		if err := r.Status().Update(ctx, profile); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status: %w", err)
		}
	}
	metrics.RecordReconcile(reconcileKindRadiusProfile, ready, reason)
	recordConditionTransition(r.Recorder, profile, readyChanged, ready, reason, message)
	return ctrl.Result{}, nil
}

// findRadiusProfileByName returns the upstream profile matching name, or nil.
func findRadiusProfileByName(profiles []unifi.RadiusProfile, name string) *unifi.RadiusProfile {
	for i := range profiles {
		if profiles[i].Name == name {
			return &profiles[i]
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager. Adoption writes
// status only, and the ownerReference is a metadata-only update, so
// GenerationChangedPredicate drops the resulting no-op reconciles while still
// handling spec changes and creates. The controller also watches its UnifiSite
// and UnifiController dependencies so a status-only change there (for example a
// late-adopted siteID or a detected application version) retriggers
// reconciliation; there is no self-watch because adoption has no conflict.
func (r *UnifiRadiusProfileReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiRadiusProfile{}, builder.WithPredicates(radiusProfilePredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.profilesInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.profilesForSite)).
		Named("unifiradiusprofile").
		Complete(r)
}

// profilesInNamespace enqueues every UnifiRadiusProfile in the changed object's
// namespace. A dependency (UnifiController or UnifiSite) may be referenced by any
// profile, so the dependency's status-only change retriggers all of them.
func (r *UnifiRadiusProfileReconciler) profilesInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listProfileRequests(ctx, obj.GetNamespace(), "")
}

// profilesForSite enqueues the UnifiRadiusProfiles that reference the changed
// UnifiSite via spec.siteRef.
func (r *UnifiRadiusProfileReconciler) profilesForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listProfileRequests(ctx, obj.GetNamespace(), obj.GetName())
}

// listProfileRequests lists UnifiRadiusProfiles in namespace and optionally
// filters them to those whose spec.siteRef.name matches siteName.
func (r *UnifiRadiusProfileReconciler) listProfileRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var profiles unifiv1alpha1.UnifiRadiusProfileList
	if err := r.List(ctx, &profiles, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list radius profiles for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(profiles.Items))
	for i := range profiles.Items {
		item := &profiles.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// radiusProfilePredicate triggers reconciles on generation changes. Adoption
// writes status only and a metadata-only ownerReference update, neither of which
// bumps generation, so no deletion handling is needed.
func radiusProfilePredicate() predicate.Predicate {
	return predicate.GenerationChangedPredicate{}
}
