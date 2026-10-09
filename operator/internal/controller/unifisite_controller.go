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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	// siteAdoptionRequeueAfter bounds how long the site waits before retrying
	// when its controller, Secret, or upstream site is not yet present. Those
	// changes do not alter the site's generation, so the periodic requeue is
	// what lets a late-appearing dependency self-heal.
	siteAdoptionRequeueAfter = 30 * time.Second

	// childDrainRequeueAfter bounds how long the site waits before re-checking
	// whether its owned children have finished their own finalizers.
	childDrainRequeueAfter = 10 * time.Second
)

// UnifiSiteReconciler reconciles a UnifiSite object. It resolves the site's
// controller, adopts the matching upstream site by name, and — as the root of
// the ownership tree — drains its owned child resources before being removed.
// It never creates or deletes upstream sites.
type UnifiSiteReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallzones,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile adopts the upstream site named by spec.internalReference, records
// its UUID in status, and drains owned children before the site is removed. It
// is idempotent and never mutates the upstream site.
func (r *UnifiSiteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	if err := r.Get(ctx, req.NamespacedName, site); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !site.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, site)
	}

	if !controllerutil.ContainsFinalizer(site, unifiv1alpha1.UnifiSiteFinalizer) {
		controllerutil.AddFinalizer(site, unifiv1alpha1.UnifiSiteFinalizer)
		if err := r.Update(ctx, site); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
	}

	return r.reconcileAdopt(ctx, site)
}

// reconcileAdopt resolves the controller and adopts the matching upstream site
// by name. A missing dependency fails closed: the site reports Ready=False and
// no upstream state is changed.
func (r *UnifiSiteReconciler) reconcileAdopt(
	ctx context.Context,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, site.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, site, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, site, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, site, false, reasonVersionUnsupported, err.Error(), "")
	}

	apiClient, err := resolver.Resolve(ctx, site.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, site, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}

	sites, err := apiClient.ListSites(ctx)
	if err != nil {
		return r.handleListError(ctx, site, err)
	}

	siteID := matchSite(site.Spec.InternalReference, sites)
	if siteID == "" {
		message := fmt.Sprintf("no upstream site with internalReference %q; nothing adopted", site.Spec.InternalReference)
		return r.requeueStatus(ctx, site, reasonSiteNotFound, message)
	}

	summary := fmt.Sprintf("adopted upstream site %q", site.Spec.InternalReference)
	return r.updateStatus(ctx, site, true, reasonSiteAdopted, summary, siteID)
}

// reconcileDelete drains owned children before removing the site finalizer. The
// upstream site is never deleted.
func (r *UnifiSiteReconciler) reconcileDelete(
	ctx context.Context,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(site, unifiv1alpha1.UnifiSiteFinalizer) {
		// The finalizer is already gone; there is nothing left to drain.
		return ctrl.Result{}, nil
	}

	var networks unifiv1alpha1.UnifiNetworkList
	if err := r.List(ctx, &networks, client.InNamespace(site.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list owned networks: %w", err)
	}
	var zones unifiv1alpha1.UnifiFirewallZoneList
	if err := r.List(ctx, &zones, client.InNamespace(site.Namespace)); err != nil {
		return ctrl.Result{}, fmt.Errorf("list owned firewall zones: %w", err)
	}

	remaining := 0
	for i := range networks.Items {
		child := &networks.Items[i]
		if !metav1.IsControlledBy(child, site) {
			continue
		}
		remaining++
		if !child.DeletionTimestamp.IsZero() {
			// Deletion already initiated; wait for the child's own finalizer.
			continue
		}
		if err := r.Delete(ctx, child); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete owned network %q: %w", child.Name, err)
		}
	}
	for i := range zones.Items {
		child := &zones.Items[i]
		if !metav1.IsControlledBy(child, site) {
			continue
		}
		remaining++
		if !child.DeletionTimestamp.IsZero() {
			// Deletion already initiated; wait for the child's own finalizer.
			continue
		}
		if err := r.Delete(ctx, child); err != nil && !apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("delete owned firewall zone %q: %w", child.Name, err)
		}
	}

	if remaining > 0 {
		// Garbage collection only starts after the owner is actually gone, so a
		// blocked owner must drive child deletion and re-check.
		return ctrl.Result{RequeueAfter: childDrainRequeueAfter}, nil
	}

	controllerutil.RemoveFinalizer(site, unifiv1alpha1.UnifiSiteFinalizer)
	if err := r.Update(ctx, site); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// handleListError classifies a failed site listing. Certificate and auth
// failures are terminal and fail closed; a transient failure is returned so
// controller-runtime backs off.
func (r *UnifiSiteReconciler) handleListError(
	ctx context.Context,
	site *unifiv1alpha1.UnifiSite,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, site, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", "")
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, site, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", "")
		case !apiErr.Retryable():
			return r.updateStatus(ctx, site, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), "")
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, site, false, reasonInvalidSpec, specErr.Error(), "")
	}
	log.Error(err, "transient error listing sites")
	return ctrl.Result{}, fmt.Errorf("list sites: %w", err)
}

// matchSite returns the UUID of the upstream site whose internalReference
// matches, or an empty string when none does.
func matchSite(internalReference string, sites []unifi.Site) string {
	for _, site := range sites {
		if site.InternalReference == internalReference {
			return site.ID
		}
	}
	return ""
}

// requeueStatus writes a non-ready status and re-checks after a bounded delay
// for a failure a later edit can fix without changing this object's generation.
func (r *UnifiSiteReconciler) requeueStatus(
	ctx context.Context,
	site *unifiv1alpha1.UnifiSite,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, site, false, reason, message, "")
	if err != nil {
		return result, err
	}
	result.RequeueAfter = siteAdoptionRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op.
func (r *UnifiSiteReconciler) updateStatus(
	ctx context.Context,
	site *unifiv1alpha1.UnifiSite,
	ready bool,
	reason, message, siteID string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if site.Status.ObservedGeneration != site.Generation {
		site.Status.ObservedGeneration = site.Generation
		changed = true
	}
	if site.Status.Summary != message {
		site.Status.Summary = message
		changed = true
	}
	if site.Status.SiteID != siteID {
		site.Status.SiteID = siteID
		changed = true
	}
	if setReadyCondition(&site.Status.Conditions, site.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, site); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager. It watches the site
// for generation changes and deletion, and owns UnifiNetwork and UnifiFirewallZone
// children so their deletion progresses the drain.
func (r *UnifiSiteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiSite{}, builder.WithPredicates(sitePredicate())).
		Owns(&unifiv1alpha1.UnifiNetwork{}).
		Owns(&unifiv1alpha1.UnifiFirewallZone{}).
		Named("unifisite").
		Complete(r)
}

// sitePredicate triggers reconciles on generation changes and when a
// deletionTimestamp is first set. GenerationChangedPredicate drops
// metadata-only updates, but the finalizer drain runs on exactly such an
// update, so deletion detection is added alongside it.
func sitePredicate() predicate.Predicate {
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
