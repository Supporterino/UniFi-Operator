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

const vpnServerDependencyRequeueAfter = 30 * time.Second

// UnifiVpnServerReconciler reconciles a UnifiVpnServer object. The frozen
// Integration v1 v10.4.57 surface exposes VPN servers read-only (and no
// credential), so the reconciler is adoption-only: it resolves the server's
// UnifiSite, connects through the site's controller, adopts the existing
// upstream server named by spec.name, and records the resolved UUID and the
// observed type and enabled state in status. It never mutates the upstream
// server, reads no secret, and carries no finalizer.
type UnifiVpnServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	APIReader client.Reader

	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifivpnservers,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifivpnservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile adopts the existing upstream VPN server named by spec.name and
// reports its UUID and observed type/enabled. It is idempotent and fails closed
// (Ready=False, no upstream mutation) on an unresolved site, a version below the
// Official API minimum, or a spec.name that names no upstream server.
func (r *UnifiVpnServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	server := &unifiv1alpha1.UnifiVpnServer{}
	if err := r.Get(ctx, req.NamespacedName, server); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !server.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	resolved := resolveAdoptionSite(ctx, r.Client, server.Namespace, server.Spec.SiteRef.Name)
	if resolved.failed() {
		if resolved.err != nil {
			return ctrl.Result{}, resolved.err
		}
		if resolved.retry {
			return r.requeueStatus(ctx, server, resolved.reason, resolved.message)
		}
		return r.updateStatus(ctx, server, false, resolved.reason, resolved.message, nil, nil, nil)
	}

	if err := r.ensureOwnership(ctx, server, resolved.site); err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcileUpstream(ctx, server, resolved.site)
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite. Adoption has no upstream effect, so no finalizer is added.
func (r *UnifiVpnServerReconciler) ensureOwnership(
	ctx context.Context,
	server *unifiv1alpha1.UnifiVpnServer,
	site *unifiv1alpha1.UnifiSite,
) error {
	if metav1.IsControlledBy(server, site) {
		return nil
	}
	if err := controllerutil.SetControllerReference(site, server, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
	}
	if err := r.Update(ctx, server); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console, gates on the Official API capability,
// and adopts the existing upstream VPN server named by spec.name.
func (r *UnifiVpnServerReconciler) reconcileUpstream(
	ctx context.Context,
	server *unifiv1alpha1.UnifiVpnServer,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, server.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, server, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, server, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, server, false, reasonVersionUnsupported, err.Error(), nil, nil, nil)
	}
	apiClient, err := resolver.Resolve(ctx, server.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, server, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	servers, err := apiClient.ListVpnServers(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, server, err)
	}
	existing := findVpnServerByName(servers, server.Spec.Name)
	if existing == nil {
		emptyID, emptyType := "", ""
		result, err := r.updateStatus(ctx, server, false, reasonVpnServerNotFound,
			fmt.Sprintf("no upstream VPN server named %q; nothing adopted", server.Spec.Name), &emptyID, &emptyType, nil)
		if err != nil {
			return result, err
		}
		result.RequeueAfter = vpnServerDependencyRequeueAfter
		return result, nil
	}

	serverID := existing.ID
	serverType := existing.Type
	enabled := existing.Enabled
	summary := fmt.Sprintf("adopted upstream VPN server %q", server.Spec.Name)
	return r.updateStatus(ctx, server, true, reasonReconciled, summary, &serverID, &serverType, &enabled)
}

// handleUpstreamError classifies a failed VPN server listing.
func (r *UnifiVpnServerReconciler) handleUpstreamError(
	ctx context.Context,
	server *unifiv1alpha1.UnifiVpnServer,
	err error,
) (ctrl.Result, error) {
	reason, message, retry := classifyUpstreamError(err)
	if retry {
		logf.FromContext(ctx).Error(err, "transient error reconciling VPN server")
		return ctrl.Result{}, fmt.Errorf("reconcile VPN server: %w", err)
	}
	return r.updateStatus(ctx, server, false, reason, message, nil, nil, nil)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiVpnServerReconciler) requeueStatus(
	ctx context.Context,
	server *unifiv1alpha1.UnifiVpnServer,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, server, false, reason, message, nil, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = vpnServerDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed. A nil identifier leaves
// the recorded value untouched; the not-found path passes empty values to clear a
// stale UUID and observed configuration.
func (r *UnifiVpnServerReconciler) updateStatus(
	ctx context.Context,
	server *unifiv1alpha1.UnifiVpnServer,
	ready bool,
	reason, message string,
	serverID, serverType *string,
	enabled *bool,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if server.Status.ObservedGeneration != server.Generation {
		server.Status.ObservedGeneration = server.Generation
		changed = true
	}
	if server.Status.Summary != message {
		server.Status.Summary = message
		changed = true
	}
	if serverID != nil && server.Status.ServerID != *serverID {
		server.Status.ServerID = *serverID
		changed = true
	}
	if serverType != nil && server.Status.Type != *serverType {
		server.Status.Type = *serverType
		changed = true
	}
	if enabled != nil && !boolPtrEqual(server.Status.Enabled, enabled) {
		server.Status.Enabled = enabled
		changed = true
	}
	if setReadyCondition(&server.Status.Conditions, server.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, server); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// findVpnServerByName returns the upstream server matching name, or nil.
func findVpnServerByName(servers []unifi.VpnServer, name string) *unifi.VpnServer {
	for i := range servers {
		if servers[i].Name == name {
			return &servers[i]
		}
	}
	return nil
}

// boolPtrEqual compares two optional bools, treating two nils as equal.
func boolPtrEqual(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiVpnServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiVpnServer{}, builder.WithPredicates(switchingAdoptionPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.serversInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.serversForSite)).
		Named("unifivpnserver").
		Complete(r)
}

func (r *UnifiVpnServerReconciler) serversInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listServerRequests(ctx, obj.GetNamespace(), "")
}

func (r *UnifiVpnServerReconciler) serversForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listServerRequests(ctx, obj.GetNamespace(), obj.GetName())
}

func (r *UnifiVpnServerReconciler) listServerRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var servers unifiv1alpha1.UnifiVpnServerList
	if err := r.List(ctx, &servers, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list VPN servers for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(servers.Items))
	for i := range servers.Items {
		item := &servers.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}
