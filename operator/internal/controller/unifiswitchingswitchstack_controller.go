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

const switchingStackDependencyRequeueAfter = 30 * time.Second

// UnifiSwitchingSwitchStackReconciler reconciles a UnifiSwitchingSwitchStack
// object. The frozen Integration v1 v10.4.57 surface exposes switch stacks
// read-only, so the reconciler is adoption-only: it resolves the stack's
// UnifiSite, connects through the site's controller, adopts the existing
// upstream switch stack named by spec.name, and records the resolved UUID and
// its observed members in status. It never mutates the upstream stack and
// carries no finalizer.
type UnifiSwitchingSwitchStackReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	APIReader client.Reader

	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiswitchingswitchstacks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiswitchingswitchstacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile adopts the existing upstream switch stack named by spec.name and
// reports its UUID and observed members. It is idempotent and fails closed on an
// unresolved site, a version below the Official API minimum, or a spec.name that
// names no upstream stack.
func (r *UnifiSwitchingSwitchStackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	stack := &unifiv1alpha1.UnifiSwitchingSwitchStack{}
	if err := r.Get(ctx, req.NamespacedName, stack); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !stack.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	resolved := resolveAdoptionSite(ctx, r.Client, stack.Namespace, stack.Spec.SiteRef.Name)
	if resolved.failed() {
		if resolved.err != nil {
			return ctrl.Result{}, resolved.err
		}
		if resolved.retry {
			return r.requeueStatus(ctx, stack, resolved.reason, resolved.message)
		}
		return r.updateStatus(ctx, stack, false, resolved.reason, resolved.message, nil, nil)
	}
	site := resolved.site

	if err := r.ensureOwnership(ctx, stack, site); err != nil {
		return ctrl.Result{}, err
	}
	return r.reconcileUpstream(ctx, stack, site)
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite. Adoption has no upstream effect, so no finalizer is added.
func (r *UnifiSwitchingSwitchStackReconciler) ensureOwnership(
	ctx context.Context,
	stack *unifiv1alpha1.UnifiSwitchingSwitchStack,
	site *unifiv1alpha1.UnifiSite,
) error {
	if metav1.IsControlledBy(stack, site) {
		return nil
	}
	if err := controllerutil.SetControllerReference(site, stack, r.Scheme); err != nil {
		return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
	}
	if err := r.Update(ctx, stack); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console, gates on the Official API capability,
// and adopts the existing upstream switch stack named by spec.name.
func (r *UnifiSwitchingSwitchStackReconciler) reconcileUpstream(
	ctx context.Context,
	stack *unifiv1alpha1.UnifiSwitchingSwitchStack,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, stack.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, stack, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, stack, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
		return r.updateStatus(ctx, stack, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}
	apiClient, err := resolver.Resolve(ctx, stack.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, stack, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	stacks, err := apiClient.ListSwitchStacks(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, stack, err)
	}
	existing := findSwitchStackByName(stacks, stack.Spec.Name)
	if existing == nil {
		emptyID := ""
		emptyMembers := []unifiv1alpha1.SwitchingStackMember{}
		result, err := r.updateStatus(ctx, stack, false, reasonSwitchStackNotFound,
			fmt.Sprintf("no upstream switch stack named %q; nothing adopted", stack.Spec.Name), &emptyID, &emptyMembers)
		if err != nil {
			return result, err
		}
		result.RequeueAfter = switchingStackDependencyRequeueAfter
		return result, nil
	}

	stackID := existing.ID
	members := convertStackMembers(existing.Members)
	summary := fmt.Sprintf("adopted upstream switch stack %q", stack.Spec.Name)
	return r.updateStatus(ctx, stack, true, reasonReconciled, summary, &stackID, &members)
}

// handleUpstreamError classifies a failed switch-stack listing.
func (r *UnifiSwitchingSwitchStackReconciler) handleUpstreamError(
	ctx context.Context,
	stack *unifiv1alpha1.UnifiSwitchingSwitchStack,
	err error,
) (ctrl.Result, error) {
	reason, message, retry := classifyUpstreamError(err)
	if retry {
		logf.FromContext(ctx).Error(err, "transient error reconciling switch stack")
		return ctrl.Result{}, fmt.Errorf("reconcile switch stack: %w", err)
	}
	return r.updateStatus(ctx, stack, false, reason, message, nil, nil)
}

// requeueStatus writes status and re-checks after a bounded delay.
func (r *UnifiSwitchingSwitchStackReconciler) requeueStatus(
	ctx context.Context,
	stack *unifiv1alpha1.UnifiSwitchingSwitchStack,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, stack, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = switchingStackDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed.
func (r *UnifiSwitchingSwitchStackReconciler) updateStatus(
	ctx context.Context,
	stack *unifiv1alpha1.UnifiSwitchingSwitchStack,
	ready bool,
	reason, message string,
	stackID *string,
	members *[]unifiv1alpha1.SwitchingStackMember,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if stack.Status.ObservedGeneration != stack.Generation {
		stack.Status.ObservedGeneration = stack.Generation
		changed = true
	}
	if stack.Status.Summary != message {
		stack.Status.Summary = message
		changed = true
	}
	if stackID != nil && stack.Status.StackID != *stackID {
		stack.Status.StackID = *stackID
		changed = true
	}
	if members != nil && !reflect.DeepEqual(stack.Status.Members, *members) {
		stack.Status.Members = *members
		changed = true
	}
	if setReadyCondition(&stack.Status.Conditions, stack.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, stack); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// findSwitchStackByName returns the upstream stack matching name, or nil.
func findSwitchStackByName(stacks []unifi.SwitchStack, name string) *unifi.SwitchStack {
	for i := range stacks {
		if stacks[i].Name == name {
			return &stacks[i]
		}
	}
	return nil
}

// convertStackMembers maps upstream stack members to the status shape.
func convertStackMembers(members []unifi.SwitchStackMember) []unifiv1alpha1.SwitchingStackMember {
	out := make([]unifiv1alpha1.SwitchingStackMember, 0, len(members))
	for _, member := range members {
		out = append(out, unifiv1alpha1.SwitchingStackMember{DeviceID: member.DeviceID})
	}
	return out
}

// SetupWithManager sets up the controller with the Manager.
func (r *UnifiSwitchingSwitchStackReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiSwitchingSwitchStack{}, builder.WithPredicates(switchingAdoptionPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.stacksInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.stacksForSite)).
		Named("unifiswitchingswitchstack").
		Complete(r)
}

func (r *UnifiSwitchingSwitchStackReconciler) stacksInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listStackRequests(ctx, obj.GetNamespace(), "")
}

func (r *UnifiSwitchingSwitchStackReconciler) stacksForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listStackRequests(ctx, obj.GetNamespace(), obj.GetName())
}

func (r *UnifiSwitchingSwitchStackReconciler) listStackRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var stacks unifiv1alpha1.UnifiSwitchingSwitchStackList
	if err := r.List(ctx, &stacks, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list switch stacks for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(stacks.Items))
	for i := range stacks.Items {
		item := &stacks.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}
