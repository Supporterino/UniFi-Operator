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
	"strconv"
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
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/metrics"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	// trafficMatchingListDependencyRequeueAfter bounds how long a matching list
	// waits before retrying when its site or controller is not yet ready. Those
	// changes do not alter this object's generation, so the periodic requeue is
	// what lets a late-appearing dependency self-heal.
	trafficMatchingListDependencyRequeueAfter = 30 * time.Second
)

// UnifiTrafficMatchingListReconciler reconciles a UnifiTrafficMatchingList
// object. It resolves the list's UnifiSite, connects through the site's
// controller, and creates, updates, or deletes the matching upstream traffic
// matching list and its typed entries. The upstream list UUID is recorded in
// status only; spec never carries a UniFi internal identifier.
type UnifiTrafficMatchingListReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifitrafficmatchinglists,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifitrafficmatchinglists/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the list's site and controller, applies the declarative
// matching list to the console, and reports the result in status. It is
// idempotent: re-running it on an unchanged object performs no writes. It fails
// closed (Ready=False, no upstream mutation) on an unresolved site or a version
// below the firewall/DNS minimum, and adds/removes its finalizer symmetrically
// around the upstream list lifecycle.
func (r *UnifiTrafficMatchingListReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	list := &unifiv1alpha1.UnifiTrafficMatchingList{}
	if err := r.Get(ctx, req.NamespacedName, list); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !list.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, list)
	}

	site, stop, err := r.reconcileSite(ctx, list)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, list, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, list, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site. An absent, deleting, or not-yet-adopted site fails
// closed: a non-nil result means the reconcile has finished and the caller must
// stop, while a nil result means the returned site is usable.
func (r *UnifiTrafficMatchingListReconciler) reconcileSite(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: list.Namespace, Name: list.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, list, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", list.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", list.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, list, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, list, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite and adds the list finalizer, in a single update when either is
// missing. The site's drain finalizer relies on the ownerReference.
func (r *UnifiTrafficMatchingListReconciler) ensureOwnership(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(list, site) {
		if err := controllerutil.SetControllerReference(site, list, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(list, unifiv1alpha1.UnifiTrafficMatchingListFinalizer) {
		controllerutil.AddFinalizer(list, unifiv1alpha1.UnifiTrafficMatchingListFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, list); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console through the site's controller, gates
// on the firewall/DNS capability, and creates, updates, or adopts the upstream
// matching list.
func (r *UnifiTrafficMatchingListReconciler) reconcileUpstream(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, list.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, list, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, list, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	// Traffic matching lists are part of the firewall surface, gated on the
	// firewall/DNS minimum (design D6).
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityFirewallDNS); err != nil {
		return r.updateStatus(ctx, list, false, reasonVersionUnsupported, err.Error(), nil)
	}

	apiClient, err := resolver.Resolve(ctx, list.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, list, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	request, err := buildTrafficMatchingListRequest(list)
	if err != nil {
		return r.handleUpstreamError(ctx, list, err)
	}

	lists, err := apiClient.ListTrafficMatchingLists(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, list, err)
	}
	existing, err := resolveExistingTrafficMatchingList(ctx, apiClient, site.Status.SiteID, list, lists)
	if err != nil {
		return r.handleUpstreamError(ctx, list, err)
	}

	var (
		result unifi.TrafficMatchingList
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateTrafficMatchingList(ctx, site.Status.SiteID, request)
		action = broadcastActionCreated
	case trafficMatchingListMatchesRequest(*existing, request):
		// The upstream object already matches the desired state; skip the PUT so
		// re-reconciling an unchanged object performs no write.
		listID := firstNonEmpty(existing.ID, list.Status.ListID)
		summary := fmt.Sprintf("upstream traffic matching list %q is up to date", list.Spec.Name)
		return r.updateStatus(ctx, list, true, reasonReconciled, summary, &listID)
	default:
		result, err = apiClient.UpdateTrafficMatchingList(ctx, site.Status.SiteID, existing.ID, request)
		action = broadcastActionUpdated
	}
	if err != nil {
		return r.handleUpstreamError(ctx, list, err)
	}

	// Keep the last-known identifier when a response omits it, so a transiently
	// incomplete body cannot erase a correlation ID.
	listID := firstNonEmpty(result.ID, list.Status.ListID)
	if existing != nil {
		listID = firstNonEmpty(listID, existing.ID)
	}

	summary := fmt.Sprintf("%s upstream traffic matching list %q", action, list.Spec.Name)
	return r.updateStatus(ctx, list, true, reasonReconciled, summary, &listID)
}

// reconcileDelete deletes the upstream list recorded in status and removes the
// finalizer. The site still resolves because the site's own finalizer drains its
// children before the site (and its controller chain) disappears. When
// status.listID is empty there is no upstream list to delete.
func (r *UnifiTrafficMatchingListReconciler) reconcileDelete(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(list, unifiv1alpha1.UnifiTrafficMatchingListFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, list); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(list, unifiv1alpha1.UnifiTrafficMatchingListFinalizer)
	if err := r.Update(ctx, list); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the list recorded in status from the console. A 404
// means it is already gone and is treated as success.
func (r *UnifiTrafficMatchingListReconciler) deleteUpstream(ctx context.Context, list *unifiv1alpha1.UnifiTrafficMatchingList) error {
	if list.Status.ListID == "" {
		return nil
	}
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: list.Namespace, Name: list.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		return fmt.Errorf("get UnifiSite %q for cleanup: %w", list.Spec.SiteRef.Name, err)
	}
	if site.Status.SiteID == "" {
		// No upstream site was ever adopted, so there is no upstream list.
		return nil
	}
	apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, list.Namespace, site.Spec.ControllerRef)
	if err != nil {
		return fmt.Errorf("resolve controller for cleanup: %w", err)
	}

	if err := apiClient.DeleteTrafficMatchingList(ctx, site.Status.SiteID, list.Status.ListID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream traffic matching list %q: %w", list.Status.ListID, err)
	}
	return nil
}

// handleUpstreamError classifies a failed list write. Certificate, auth, and
// bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiTrafficMatchingListReconciler) handleUpstreamError(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, list, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, list, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, list, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, list, false, reasonInvalidSpec, specErr.Error(), nil)
	}
	log.Error(err, "transient error reconciling traffic matching list")
	return ctrl.Result{}, fmt.Errorf("reconcile traffic matching list: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency failure a later edit can fix without changing this object's
// generation.
func (r *UnifiTrafficMatchingListReconciler) requeueStatus(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, list, false, reason, message, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = trafficMatchingListDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil listID leaves the recorded value untouched,
// preserving the last-known identifier on failing paths.
func (r *UnifiTrafficMatchingListReconciler) updateStatus(
	ctx context.Context,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
	ready bool,
	reason, message string,
	listID *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if list.Status.ObservedGeneration != list.Generation {
		list.Status.ObservedGeneration = list.Generation
		changed = true
	}
	if list.Status.Summary != message {
		list.Status.Summary = message
		changed = true
	}
	if listID != nil && list.Status.ListID != *listID {
		list.Status.ListID = *listID
		changed = true
	}
	readyChanged := setReadyCondition(&list.Status.Conditions, list.Generation, ready, reason, message)
	if readyChanged {
		changed = true
	}

	if changed {
		if err := r.Status().Update(ctx, list); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status: %w", err)
		}
	}
	metrics.RecordReconcile(reconcileKindTrafficMatchingList, ready, reason)
	recordConditionTransition(r.Recorder, list, readyChanged, ready, reason, message)
	return ctrl.Result{}, nil
}

// buildTrafficMatchingListRequest maps the declarative spec onto the upstream
// create/update body.
func buildTrafficMatchingListRequest(list *unifiv1alpha1.UnifiTrafficMatchingList) (unifi.TrafficMatchingListRequest, error) {
	request := unifi.TrafficMatchingListRequest{Type: list.Spec.Type, Name: list.Spec.Name}
	items := make([]unifi.TrafficMatchingListItem, 0, len(list.Spec.Items))
	for i := range list.Spec.Items {
		item, err := convertTrafficMatchingEntry(list.Spec.Items[i])
		if err != nil {
			return unifi.TrafficMatchingListRequest{}, err
		}
		items = append(items, item)
	}
	request.Items = items
	return request, nil
}

// convertTrafficMatchingEntry flattens a discriminated list entry onto the
// upstream item shape. A missing variant is a terminal spec error: CEL rejects
// it at the API server, but it is defended here too.
func convertTrafficMatchingEntry(entry unifiv1alpha1.TrafficMatchingEntry) (unifi.TrafficMatchingListItem, error) {
	item := unifi.TrafficMatchingListItem{Type: entry.Type}
	switch entry.Type {
	case "IP_ADDRESS", "SUBNET":
		if entry.Address == nil {
			return unifi.TrafficMatchingListItem{}, missingEntryVariant(entry.Type)
		}
		item.Value = entry.Address.Value
	case "IP_ADDRESS_RANGE":
		if entry.AddressRange == nil {
			return unifi.TrafficMatchingListItem{}, missingEntryVariant(entry.Type)
		}
		item.Start = entry.AddressRange.Start
		item.Stop = entry.AddressRange.Stop
	case "PORT_NUMBER":
		if entry.Port == nil {
			return unifi.TrafficMatchingListItem{}, missingEntryVariant(entry.Type)
		}
		item.Value = strconv.FormatInt(int64(entry.Port.Value), 10)
	case "PORT_NUMBER_RANGE":
		if entry.PortRange == nil {
			return unifi.TrafficMatchingListItem{}, missingEntryVariant(entry.Type)
		}
		item.Start = strconv.FormatInt(int64(entry.PortRange.Start), 10)
		item.Stop = strconv.FormatInt(int64(entry.PortRange.Stop), 10)
	default:
		return unifi.TrafficMatchingListItem{}, &unifi.InvalidSpecError{
			Field:  "items",
			Reason: fmt.Sprintf("%q is not a known matching item type", entry.Type),
		}
	}
	return item, nil
}

// missingEntryVariant reports the terminal spec error for an entry whose type
// requires a variant sub-object that is not set.
func missingEntryVariant(entryType string) error {
	return &unifi.InvalidSpecError{
		Field:  "items",
		Reason: fmt.Sprintf("entry type %s is missing its variant value", entryType),
	}
}

// findTrafficMatchingListByName returns the upstream list matching name, or nil.
func findTrafficMatchingListByName(lists []unifi.TrafficMatchingList, name string) *unifi.TrafficMatchingList {
	for i := range lists {
		if lists[i].Name == name {
			return &lists[i]
		}
	}
	return nil
}

// resolveExistingTrafficMatchingList returns the upstream list this CR manages,
// or nil when none exists. It resolves status.listID first so a spec.name
// rename updates the same upstream object instead of orphaning it, then falls
// back to a name match for adoption. A 404 while resolving status.listID is
// treated as absent so a stale ID cannot block reconciliation.
func resolveExistingTrafficMatchingList(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	list *unifiv1alpha1.UnifiTrafficMatchingList,
	lists []unifi.TrafficMatchingList,
) (*unifi.TrafficMatchingList, error) {
	if list.Status.ListID != "" {
		existing, err := apiClient.GetTrafficMatchingList(ctx, siteID, list.Status.ListID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the name match: the recorded object is gone.
		default:
			return nil, err
		}
	}
	return findTrafficMatchingListByName(lists, list.Spec.Name), nil
}

// trafficMatchingListMatchesRequest reports whether the observed upstream list
// already matches the desired write, so the reconciler can skip the PUT and stay
// idempotent. Only authored fields (name, type, items) are compared; the
// observed id is ignored. Item order is insignificant.
func trafficMatchingListMatchesRequest(existing unifi.TrafficMatchingList, req unifi.TrafficMatchingListRequest) bool {
	return existing.Name == req.Name &&
		existing.Type == req.Type &&
		trafficMatchingItemsMatch(existing.Items, req.Items)
}

// trafficMatchingItemsMatch reports whether two item sets hold the same entries,
// ignoring order and treating nil and empty as equal.
func trafficMatchingItemsMatch(a, b []unifi.TrafficMatchingListItem) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	ak := make([]string, 0, len(a))
	for _, item := range a {
		ak = append(ak, trafficMatchingItemKey(item))
	}
	bk := make([]string, 0, len(b))
	for _, item := range b {
		bk = append(bk, trafficMatchingItemKey(item))
	}
	sort.Strings(ak)
	sort.Strings(bk)
	return slices.Equal(ak, bk)
}

// trafficMatchingItemKey builds a canonical comparison key for an item from its
// authored fields.
func trafficMatchingItemKey(item unifi.TrafficMatchingListItem) string {
	return item.Type + "\x00" + item.Value + "\x00" + item.Start + "\x00" + item.Stop
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes and the first deletion. The
// controller also watches its UnifiSite and UnifiController dependencies so a
// status-only change there (for example a late-adopted siteID or a detected
// application version) retriggers reconciliation.
func (r *UnifiTrafficMatchingListReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiTrafficMatchingList{}, builder.WithPredicates(trafficMatchingListPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.listsInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.listsForSite)).
		Named("unifitrafficmatchinglist").
		Complete(r)
}

// listsInNamespace enqueues every UnifiTrafficMatchingList in the changed
// object's namespace. A dependency (UnifiController or UnifiSite) may be
// referenced by any list, so the dependency's status-only change retriggers all
// of them.
func (r *UnifiTrafficMatchingListReconciler) listsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listRequests(ctx, obj.GetNamespace(), "")
}

// listsForSite enqueues the UnifiTrafficMatchingLists that reference the changed
// UnifiSite via spec.siteRef.
func (r *UnifiTrafficMatchingListReconciler) listsForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listRequests(ctx, obj.GetNamespace(), obj.GetName())
}

// listRequests lists UnifiTrafficMatchingLists in namespace and optionally
// filters them to those whose spec.siteRef.name matches siteName.
func (r *UnifiTrafficMatchingListReconciler) listRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var lists unifiv1alpha1.UnifiTrafficMatchingListList
	if err := r.List(ctx, &lists, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list traffic matching lists for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(lists.Items))
	for i := range lists.Items {
		item := &lists.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// trafficMatchingListPredicate triggers reconciles on generation changes and
// when a deletionTimestamp is first set. GenerationChangedPredicate drops
// metadata-only updates, but the finalizer cleanup runs on exactly such an
// update, so deletion detection is added alongside it.
func trafficMatchingListPredicate() predicate.Predicate {
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
