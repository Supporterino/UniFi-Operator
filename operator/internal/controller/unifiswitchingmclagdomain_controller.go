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
	"reflect"
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

const switchingMcLagDomainDependencyRequeueAfter = 30 * time.Second

// UnifiSwitchingMcLagDomainReconciler reconciles a UnifiSwitchingMcLagDomain
// object. The frozen Integration v1 v10.4.57 surface exposes MC-LAG domains
// read-only, so the reconciler is adoption-only: it resolves the domain's
// UnifiSite, connects through the site's controller, adopts the existing
// upstream MC-LAG domain named by spec.name, and records the resolved UUID and
// the observed peers and members in status. It never mutates the upstream domain
// and carries no finalizer.
type UnifiSwitchingMcLagDomainReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	APIReader client.Reader

	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiswitchingmclagdomains,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiswitchingmclagdomains/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile adopts the existing upstream MC-LAG domain named by spec.name and
// reports its UUID and observed configuration. It is idempotent and fails closed
// on an unresolved site, a version below the Official API minimum, or a
// spec.name that names no upstream domain.
func (r *UnifiSwitchingMcLagDomainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	domain := &unifiv1alpha1.UnifiSwitchingMcLagDomain{}
	if err := r.Get(ctx, req.NamespacedName, domain); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !domain.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	resolved := resolveAdoptionSite(ctx, r.Client, domain.Namespace, domain.Spec.SiteRef.Name)
	if resolved.failed() {
		if resolved.err != nil {
			return ctrl.Result{}, resolved.err
		}
		if resolved.retry {
			return r.requeueStatus(ctx, domain, resolved.reason, resolved.message)
		}
		return r.updateStatus(ctx, domain, false, resolved.reason, resolved.message, nil, nil, nil)
	}
	site := resolved.site

	if err := r.ensureOwnership(ctx, domain, site); err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcileUpstream(ctx, domain, site)
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite. Adoption has no upstream effect, so no finalizer is added.
func (r *UnifiSwitchingMcLagDomainReconciler) ensureOwnership(
	ctx context.Context,
	domain *unifiv1alpha1.UnifiSwitchingMcLagDomain,
	site *unifiv1alpha1.UnifiSite,
) error {
	if metav1.IsControlledBy(domain, site) {
		return nil
	}
	if err := controllerutil.SetControllerReference(site, domain, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
	}
	if err := r.Update(ctx, domain); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console, gates on the Official API capability,
// and adopts the existing upstream MC-LAG domain named by spec.name.
func (r *UnifiSwitchingMcLagDomainReconciler) reconcileUpstream(
	ctx context.Context,
	domain *unifiv1alpha1.UnifiSwitchingMcLagDomain,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, domain.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, domain, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, domain, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, domain, false, reasonVersionUnsupported, err.Error(), nil, nil, nil)
	}
	apiClient, err := resolver.Resolve(ctx, domain.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, domain, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	domains, err := apiClient.ListMcLagDomains(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, domain, err)
	}
	existing := findMcLagDomainByName(domains, domain.Spec.Name)
	if existing == nil {
		emptyID := ""
		emptyMembers := []unifiv1alpha1.SwitchingLagMember{}
		emptyPeers := []unifiv1alpha1.SwitchingMcLagPeer{}
		result, err := r.updateStatus(ctx, domain, false, reasonMcLagDomainNotFound,
			fmt.Sprintf("no upstream MC-LAG domain named %q; nothing adopted", domain.Spec.Name), &emptyID, &emptyMembers, &emptyPeers)
		if err != nil {
			return result, err
		}
		result.RequeueAfter = switchingMcLagDomainDependencyRequeueAfter
		return result, nil
	}

	domainID := existing.ID
	members := mcLagMembers(existing.Lags)
	peers := convertMcLagPeers(existing.Peers)
	summary := fmt.Sprintf("adopted upstream MC-LAG domain %q", domain.Spec.Name)
	return r.updateStatus(ctx, domain, true, reasonReconciled, summary, &domainID, &members, &peers)
}

// handleUpstreamError classifies a failed MC-LAG domain listing.
func (r *UnifiSwitchingMcLagDomainReconciler) handleUpstreamError(
	ctx context.Context,
	domain *unifiv1alpha1.UnifiSwitchingMcLagDomain,
	err error,
) (ctrl.Result, error) {
	reason, message, retry := classifyUpstreamError(err)
	if retry {
		logf.FromContext(ctx).Error(err, "transient error reconciling MC-LAG domain")
		return ctrl.Result{}, fmt.Errorf("reconcile MC-LAG domain: %w", err)
	}
	return r.updateStatus(ctx, domain, false, reason, message, nil, nil, nil)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiSwitchingMcLagDomainReconciler) requeueStatus(
	ctx context.Context,
	domain *unifiv1alpha1.UnifiSwitchingMcLagDomain,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, domain, false, reason, message, nil, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = switchingMcLagDomainDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed.
func (r *UnifiSwitchingMcLagDomainReconciler) updateStatus(
	ctx context.Context,
	domain *unifiv1alpha1.UnifiSwitchingMcLagDomain,
	ready bool,
	reason, message string,
	domainID *string,
	members *[]unifiv1alpha1.SwitchingLagMember,
	peers *[]unifiv1alpha1.SwitchingMcLagPeer,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if domain.Status.ObservedGeneration != domain.Generation {
		domain.Status.ObservedGeneration = domain.Generation
		changed = true
	}
	if domain.Status.Summary != message {
		domain.Status.Summary = message
		changed = true
	}
	if domainID != nil && domain.Status.DomainID != *domainID {
		domain.Status.DomainID = *domainID
		changed = true
	}
	if members != nil && !reflect.DeepEqual(domain.Status.Members, *members) {
		domain.Status.Members = *members
		changed = true
	}
	if peers != nil && !reflect.DeepEqual(domain.Status.Peers, *peers) {
		domain.Status.Peers = *peers
		changed = true
	}
	if setReadyCondition(&domain.Status.Conditions, domain.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, domain); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// findMcLagDomainByName returns the upstream domain matching name, or nil.
func findMcLagDomainByName(domains []unifi.McLagDomain, name string) *unifi.McLagDomain {
	for i := range domains {
		if domains[i].Name == name {
			return &domains[i]
		}
	}
	return nil
}

// mcLagMembers aggregates the members of a domain's local LAGs.
func mcLagMembers(lags []unifi.McLagLocal) []unifiv1alpha1.SwitchingLagMember {
	total := 0
	for _, lag := range lags {
		total += len(lag.Members)
	}
	out := make([]unifiv1alpha1.SwitchingLagMember, 0, total)
	for _, lag := range lags {
		out = append(out, convertLagMembers(lag.Members)...)
	}
	return out
}

// convertMcLagPeers maps upstream MC-LAG peers to the status shape.
func convertMcLagPeers(peers []unifi.McLagPeer) []unifiv1alpha1.SwitchingMcLagPeer {
	out := make([]unifiv1alpha1.SwitchingMcLagPeer, 0, len(peers))
	for _, peer := range peers {
		out = append(out, unifiv1alpha1.SwitchingMcLagPeer{
			DeviceID:     peer.DeviceID,
			LinkPortIdxs: peer.LinkPortIdxs,
			Role:         peer.Role,
		})
	}
	return out
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiSwitchingMcLagDomainReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiSwitchingMcLagDomain{}, builder.WithPredicates(switchingAdoptionPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.domainsInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.domainsForSite)).
		Named("unifiswitchingmclagdomain").
		Complete(r)
}

func (r *UnifiSwitchingMcLagDomainReconciler) domainsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listDomainRequests(ctx, obj.GetNamespace(), "")
}

func (r *UnifiSwitchingMcLagDomainReconciler) domainsForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listDomainRequests(ctx, obj.GetNamespace(), obj.GetName())
}

func (r *UnifiSwitchingMcLagDomainReconciler) listDomainRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var domains unifiv1alpha1.UnifiSwitchingMcLagDomainList
	if err := r.List(ctx, &domains, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list MC-LAG domains for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(domains.Items))
	for i := range domains.Items {
		item := &domains.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}
