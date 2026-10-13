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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const siteToSiteTunnelDependencyRequeueAfter = 30 * time.Second

// UnifiSiteToSiteTunnelReconciler reconciles a UnifiSiteToSiteTunnel object. The
// frozen Integration v1 v10.4.57 surface exposes site-to-site tunnels read-only
// (and no credential), so the reconciler is adoption-only: it resolves the
// tunnel's UnifiSite, connects through the site's controller, adopts the
// existing upstream tunnel named by spec.name, and records the resolved UUID and
// the observed type in status. It never mutates the upstream tunnel, reads no
// secret, and carries no finalizer.
type UnifiSiteToSiteTunnelReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	APIReader client.Reader

	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisitetositetunnels,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisitetositetunnels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile adopts the existing upstream site-to-site tunnel named by spec.name
// and reports its UUID and observed type. It is idempotent and fails closed
// (Ready=False, no upstream mutation) on an unresolved site, a version below the
// Official API minimum, or a spec.name that names no upstream tunnel.
func (r *UnifiSiteToSiteTunnelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	tunnel := &unifiv1alpha1.UnifiSiteToSiteTunnel{}
	if err := r.Get(ctx, req.NamespacedName, tunnel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !tunnel.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	resolved := resolveAdoptionSite(ctx, r.Client, tunnel.Namespace, tunnel.Spec.SiteRef.Name)
	if resolved.failed() {
		if resolved.err != nil {
			return ctrl.Result{}, resolved.err
		}
		if resolved.retry {
			return r.requeueStatus(ctx, tunnel, resolved.reason, resolved.message)
		}
		return r.updateStatus(ctx, tunnel, false, resolved.reason, resolved.message, nil, nil)
	}

	if err := r.ensureOwnership(ctx, tunnel, resolved.site); err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcileUpstream(ctx, tunnel, resolved.site)
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite. Adoption has no upstream effect, so no finalizer is added.
func (r *UnifiSiteToSiteTunnelReconciler) ensureOwnership(
	ctx context.Context,
	tunnel *unifiv1alpha1.UnifiSiteToSiteTunnel,
	site *unifiv1alpha1.UnifiSite,
) error {
	if metav1.IsControlledBy(tunnel, site) {
		return nil
	}
	if err := controllerutil.SetControllerReference(site, tunnel, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
	}
	if err := r.Update(ctx, tunnel); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console, gates on the Official API capability,
// and adopts the existing upstream site-to-site tunnel named by spec.name.
func (r *UnifiSiteToSiteTunnelReconciler) reconcileUpstream(
	ctx context.Context,
	tunnel *unifiv1alpha1.UnifiSiteToSiteTunnel,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, tunnel.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, tunnel, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, tunnel, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, tunnel, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}
	apiClient, err := resolver.Resolve(ctx, tunnel.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, tunnel, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	tunnels, err := apiClient.ListSiteToSiteTunnels(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, tunnel, err)
	}
	existing := findSiteToSiteTunnelByName(tunnels, tunnel.Spec.Name)
	if existing == nil {
		emptyID, emptyType := "", ""
		result, err := r.updateStatus(ctx, tunnel, false, reasonTunnelNotFound,
			fmt.Sprintf("no upstream site-to-site tunnel named %q; nothing adopted", tunnel.Spec.Name), &emptyID, &emptyType)
		if err != nil {
			return result, err
		}
		result.RequeueAfter = siteToSiteTunnelDependencyRequeueAfter
		return result, nil
	}

	tunnelID := existing.ID
	tunnelType := existing.Type
	summary := fmt.Sprintf("adopted upstream site-to-site tunnel %q", tunnel.Spec.Name)
	return r.updateStatus(ctx, tunnel, true, reasonReconciled, summary, &tunnelID, &tunnelType)
}

// handleUpstreamError classifies a failed tunnel listing.
func (r *UnifiSiteToSiteTunnelReconciler) handleUpstreamError(
	ctx context.Context,
	tunnel *unifiv1alpha1.UnifiSiteToSiteTunnel,
	err error,
) (ctrl.Result, error) {
	reason, message, retry := classifyUpstreamError(err)
	if retry {
		logf.FromContext(ctx).Error(err, "transient error reconciling site-to-site tunnel")
		return ctrl.Result{}, fmt.Errorf("reconcile site-to-site tunnel: %w", err)
	}
	return r.updateStatus(ctx, tunnel, false, reason, message, nil, nil)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiSiteToSiteTunnelReconciler) requeueStatus(
	ctx context.Context,
	tunnel *unifiv1alpha1.UnifiSiteToSiteTunnel,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, tunnel, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = siteToSiteTunnelDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed.
func (r *UnifiSiteToSiteTunnelReconciler) updateStatus(
	ctx context.Context,
	tunnel *unifiv1alpha1.UnifiSiteToSiteTunnel,
	ready bool,
	reason, message string,
	tunnelID, tunnelType *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if tunnel.Status.ObservedGeneration != tunnel.Generation {
		tunnel.Status.ObservedGeneration = tunnel.Generation
		changed = true
	}
	if tunnel.Status.Summary != message {
		tunnel.Status.Summary = message
		changed = true
	}
	if tunnelID != nil && tunnel.Status.TunnelID != *tunnelID {
		tunnel.Status.TunnelID = *tunnelID
		changed = true
	}
	if tunnelType != nil && tunnel.Status.Type != *tunnelType {
		tunnel.Status.Type = *tunnelType
		changed = true
	}
	if setReadyCondition(&tunnel.Status.Conditions, tunnel.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, tunnel); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// findSiteToSiteTunnelByName returns the upstream tunnel matching name, or nil.
func findSiteToSiteTunnelByName(tunnels []unifi.SiteToSiteTunnel, name string) *unifi.SiteToSiteTunnel {
	for i := range tunnels {
		if tunnels[i].Name == name {
			return &tunnels[i]
		}
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiSiteToSiteTunnelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiSiteToSiteTunnel{}, builder.WithPredicates(switchingAdoptionPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.tunnelsInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.tunnelsForSite)).
		Named("unifisitetositetunnel").
		Complete(r)
}

func (r *UnifiSiteToSiteTunnelReconciler) tunnelsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listTunnelRequests(ctx, obj.GetNamespace(), "")
}

func (r *UnifiSiteToSiteTunnelReconciler) tunnelsForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listTunnelRequests(ctx, obj.GetNamespace(), obj.GetName())
}

func (r *UnifiSiteToSiteTunnelReconciler) listTunnelRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var tunnels unifiv1alpha1.UnifiSiteToSiteTunnelList
	if err := r.List(ctx, &tunnels, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list site-to-site tunnels for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(tunnels.Items))
	for i := range tunnels.Items {
		item := &tunnels.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}
