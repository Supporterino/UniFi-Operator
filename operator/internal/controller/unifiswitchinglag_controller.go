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
	"slices"
	"time"

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

const switchingLagDependencyRequeueAfter = 30 * time.Second

// UnifiSwitchingLagReconciler reconciles a UnifiSwitchingLag object. The frozen
// Integration v1 v10.4.57 surface exposes LAGs read-only and without a name, so
// the reconciler is adoption-only: it resolves the LAG's UnifiSite, connects
// through the site's controller, adopts the unique upstream LAG matching the
// typed composite selector (spec.type + spec.members), and records the resolved
// UUID and the observed type and members in status. It never mutates the
// upstream LAG and carries no finalizer.
type UnifiSwitchingLagReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	APIReader client.Reader

	NewClient ConnectionFactory

	// Recorder records Ready-condition transitions as Kubernetes Events. It is
	// constructed in cmd/main.go with the same name as the controller so the
	// Event source matches kubectl describe.
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiswitchinglags,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiswitchinglags/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile adopts the unique upstream LAG matching the typed composite selector
// and reports its UUID and observed configuration. It is idempotent and fails
// closed (Ready=False, no upstream mutation) on an unresolved site, a version
// below the Official API minimum, or a selector that matches zero or more than
// one upstream LAG.
func (r *UnifiSwitchingLagReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lag := &unifiv1alpha1.UnifiSwitchingLag{}
	if err := r.Get(ctx, req.NamespacedName, lag); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !lag.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	resolved := resolveAdoptionSite(ctx, r.Client, lag.Namespace, lag.Spec.SiteRef.Name)
	if resolved.failed() {
		if resolved.err != nil {
			return ctrl.Result{}, resolved.err
		}
		if resolved.retry {
			return r.requeueStatus(ctx, lag, resolved.reason, resolved.message)
		}
		return r.updateStatus(ctx, lag, false, resolved.reason, resolved.message, nil, nil, nil)
	}
	site := resolved.site

	if err := r.ensureOwnership(ctx, lag, site); err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcileUpstream(ctx, lag, site)
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite. Adoption has no upstream effect, so no finalizer is added.
func (r *UnifiSwitchingLagReconciler) ensureOwnership(
	ctx context.Context,
	lag *unifiv1alpha1.UnifiSwitchingLag,
	site *unifiv1alpha1.UnifiSite,
) error {
	if metav1.IsControlledBy(lag, site) {
		return nil
	}
	if err := controllerutil.SetControllerReference(site, lag, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
	}
	if err := r.Update(ctx, lag); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console, gates on the Official API capability,
// and adopts the unique upstream LAG matching the typed composite selector
// (spec.type plus the member device-tag/port selector).
func (r *UnifiSwitchingLagReconciler) reconcileUpstream(
	ctx context.Context,
	lag *unifiv1alpha1.UnifiSwitchingLag,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, lag.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, lag, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, lag, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, lag, false, reasonVersionUnsupported, err.Error(), nil, nil, nil)
	}
	apiClient, err := resolver.Resolve(ctx, lag.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, lag, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	tagDevices, reason, message, err := resolveLagSelectorDevices(ctx, apiClient, site.Status.SiteID, lag.Spec.Members)
	if err != nil {
		return r.handleUpstreamError(ctx, lag, err)
	}
	if reason != "" {
		return r.updateStatus(ctx, lag, false, reason, message, nil, nil, nil)
	}

	lags, err := apiClient.ListLags(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, lag, err)
	}
	matches := matchLags(lags, lag.Spec, tagDevices)
	switch len(matches) {
	case 1:
		existing := matches[0]
		lagID := existing.ID
		lagType := existing.Type
		members := convertLagMembers(existing.Members)
		summary := fmt.Sprintf("adopted upstream LAG %s", existing.ID)
		return r.updateStatus(ctx, lag, true, reasonReconciled, summary, &lagID, &lagType, &members)
	case 0:
		emptyID, emptyType := "", ""
		emptyMembers := []unifiv1alpha1.SwitchingLagMember{}
		result, err := r.updateStatus(ctx, lag, false, reasonLagSelectorNoMatch,
			"the typed selector matches no upstream LAG; nothing adopted", &emptyID, &emptyType, &emptyMembers)
		if err != nil {
			return result, err
		}
		result.RequeueAfter = switchingLagDependencyRequeueAfter
		return result, nil
	default:
		emptyID, emptyType := "", ""
		emptyMembers := []unifiv1alpha1.SwitchingLagMember{}
		return r.updateStatus(ctx, lag, false, reasonLagSelectorAmbiguous,
			fmt.Sprintf("the typed selector matches %d upstream LAGs; exactly one is required", len(matches)),
			&emptyID, &emptyType, &emptyMembers)
	}
}

// handleUpstreamError classifies a failed LAG listing.
func (r *UnifiSwitchingLagReconciler) handleUpstreamError(
	ctx context.Context,
	lag *unifiv1alpha1.UnifiSwitchingLag,
	err error,
) (ctrl.Result, error) {
	reason, message, retry := classifyUpstreamError(err)
	if retry {
		logf.FromContext(ctx).Error(err, "transient error reconciling switching LAG")
		return ctrl.Result{}, fmt.Errorf("reconcile switching LAG: %w", err)
	}
	return r.updateStatus(ctx, lag, false, reason, message, nil, nil, nil)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiSwitchingLagReconciler) requeueStatus(
	ctx context.Context,
	lag *unifiv1alpha1.UnifiSwitchingLag,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, lag, false, reason, message, nil, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = switchingLagDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed. A nil identifier leaves
// the recorded value untouched; the not-found path passes empty values to clear a
// stale UUID and observed configuration.
func (r *UnifiSwitchingLagReconciler) updateStatus(
	ctx context.Context,
	lag *unifiv1alpha1.UnifiSwitchingLag,
	ready bool,
	reason, message string,
	lagID, lagType *string,
	members *[]unifiv1alpha1.SwitchingLagMember,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if lag.Status.ObservedGeneration != lag.Generation {
		lag.Status.ObservedGeneration = lag.Generation
		changed = true
	}
	if lag.Status.Summary != message {
		lag.Status.Summary = message
		changed = true
	}
	if lagID != nil && lag.Status.LagID != *lagID {
		lag.Status.LagID = *lagID
		changed = true
	}
	if lagType != nil && lag.Status.Type != *lagType {
		lag.Status.Type = *lagType
		changed = true
	}
	if members != nil && !reflect.DeepEqual(lag.Status.Members, *members) {
		lag.Status.Members = *members
		changed = true
	}
	readyChanged := setReadyCondition(&lag.Status.Conditions, lag.Generation, ready, reason, message)
	if readyChanged {
		changed = true
	}

	if changed {
		if err := r.Status().Update(ctx, lag); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status: %w", err)
		}
	}
	metrics.RecordReconcile(reconcileKindSwitchingLag, ready, reason)
	recordConditionTransition(r.Recorder, lag, readyChanged, ready, reason, message)
	return ctrl.Result{}, nil
}

// resolveLagSelectorDevices resolves each selector's device tag to its member
// device UUIDs against the read-only device-tags list. An unknown tag fails
// closed with DeviceTagNotFound.
func resolveLagSelectorDevices(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	selectors []unifiv1alpha1.SwitchingLagMemberSelector,
) (map[string][]string, string, string, error) {
	tags, err := apiClient.ListDeviceTags(ctx, siteID)
	if err != nil {
		return nil, "", "", err
	}
	out := make(map[string][]string, len(selectors))
	for _, selector := range selectors {
		name := selector.DeviceTag.Name
		if _, ok := out[name]; ok {
			continue
		}
		tag := findDeviceTagByName(tags, name)
		if tag == nil {
			return nil, reasonDeviceTagNotFound, fmt.Sprintf(
				"device tag %q does not exist on the site; no upstream LAG was adopted", name), nil
		}
		out[name] = tag.DeviceIDs
	}
	return out, "", "", nil
}

// matchLags returns every upstream LAG whose type and members match the selector.
func matchLags(lags []unifi.Lag, spec unifiv1alpha1.UnifiSwitchingLagSpec, tagDevices map[string][]string) []unifi.Lag {
	matches := make([]unifi.Lag, 0)
	for i := range lags {
		if lagMatchesSelector(lags[i], spec, tagDevices) {
			matches = append(matches, lags[i])
		}
	}
	return matches
}

// lagMatchesSelector reports whether a LAG's type matches and each member
// selector maps to a distinct member whose device belongs to the selector's
// resolved tag set and whose ports match.
func lagMatchesSelector(lag unifi.Lag, spec unifiv1alpha1.UnifiSwitchingLagSpec, tagDevices map[string][]string) bool {
	if lag.Type != spec.Type || len(lag.Members) != len(spec.Members) {
		return false
	}
	used := make([]bool, len(lag.Members))
	for _, selector := range spec.Members {
		deviceIDs, ok := tagDevices[selector.DeviceTag.Name]
		if !ok {
			return false
		}
		found := false
		for i := range lag.Members {
			if used[i] {
				continue
			}
			if memberMatchesSelector(lag.Members[i], deviceIDs, selector.PortIdxs) {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// memberMatchesSelector reports whether a member's device belongs to the resolved
// tag device set and its ports match (order-insensitive).
func memberMatchesSelector(member unifi.LagMember, deviceIDs []string, portIdxs []int32) bool {
	if !slices.Contains(deviceIDs, member.DeviceID) {
		return false
	}
	return int32SlicesMatch(member.PortIdxs, portIdxs)
}

// convertLagMembers maps upstream LAG members to the status shape.
func convertLagMembers(members []unifi.LagMember) []unifiv1alpha1.SwitchingLagMember {
	out := make([]unifiv1alpha1.SwitchingLagMember, 0, len(members))
	for _, member := range members {
		out = append(out, unifiv1alpha1.SwitchingLagMember{DeviceID: member.DeviceID, PortIdxs: member.PortIdxs})
	}
	return out
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiSwitchingLagReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiSwitchingLag{}, builder.WithPredicates(switchingAdoptionPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.lagsInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.lagsForSite)).
		Named("unifiswitchinglag").
		Complete(r)
}

func (r *UnifiSwitchingLagReconciler) lagsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listLagRequests(ctx, obj.GetNamespace(), "")
}

func (r *UnifiSwitchingLagReconciler) lagsForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listLagRequests(ctx, obj.GetNamespace(), obj.GetName())
}

func (r *UnifiSwitchingLagReconciler) listLagRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var lags unifiv1alpha1.UnifiSwitchingLagList
	if err := r.List(ctx, &lags, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list switching LAGs for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(lags.Items))
	for i := range lags.Items {
		item := &lags.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// switchingAdoptionPredicate triggers reconciles on generation changes. Adoption
// writes status only and a metadata-only ownerReference update, neither of which
// bumps generation, so no deletion handling is needed.
func switchingAdoptionPredicate() predicate.Predicate {
	return predicate.GenerationChangedPredicate{}
}
