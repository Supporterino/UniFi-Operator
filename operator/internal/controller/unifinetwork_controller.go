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

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	// conditionReady is the primary readiness signal for a UnifiNetwork.
	conditionReady = "Ready"

	reasonReconciled       = "Reconciled"
	reasonUpstreamNotFound = "UpstreamNotFound"
	reasonInvalidSpec      = "InvalidSpec"
	reasonAuthFailed       = "AuthenticationFailed"

	// upstreamNotFoundRequeueAfter bounds how long a resource whose upstream
	// network does not yet exist waits before the controller re-checks. A
	// status-only write does not change the generation, so the periodic requeue
	// is what lets a late-appearing upstream object self-heal.
	upstreamNotFoundRequeueAfter = 30 * time.Second

	// maxStatusMessageLen caps how much controller-provided text is copied into
	// status.summary and the Ready condition message. Condition messages have
	// an API-server maxLength; clamping also stops an oversized body from
	// re-entering the retry loop.
	maxStatusMessageLen = 1024
)

// UnifiNetworkReconciler reconciles a UnifiNetwork object.
type UnifiNetworkReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// UniFi is the UniFi controller client. It returns plain Go structs and is
	// backed by a fixture in tests so no live controller is contacted.
	UniFi unifi.Client
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks/status,verbs=get;update;patch

// Reconcile matches the UnifiNetwork to its upstream network through the UniFi
// client and reports the result in status. It is idempotent: re-running it on an
// unchanged object performs no writes. The upstream identifier is recorded in
// status only and never in spec.
func (r *UnifiNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	network := &unifiv1alpha1.UnifiNetwork{}
	if err := r.Get(ctx, req.NamespacedName, network); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	upstreams, err := r.UniFi.ListNetworks(ctx, network.Spec.Site)
	if err != nil {
		var apiErr *unifi.APIError
		var specErr *unifi.InvalidSpecError
		var reason string
		switch {
		case errors.As(err, &specErr):
			// Typed bad-input error from the client (for example an unsafe site).
			reason = reasonInvalidSpec
		case errors.As(err, &apiErr) && isAuthError(apiErr):
			// 401/403 is an authentication failure, not a spec problem. The
			// documented one-shot re-auth is deferred until real
			// credential/session support lands (the scaffold has no login).
			reason = reasonAuthFailed
		case errors.As(err, &apiErr) && !apiErr.Retryable():
			// Other non-retryable controller responses (bad-input 4xx).
			reason = reasonInvalidSpec
		default:
			// Transient or unclassified failure (transport, DNS, TLS, timeout,
			// context, decode): propagate so controller-runtime backs off
			// instead of pinning the resource NotReady.
			return ctrl.Result{}, fmt.Errorf("list networks: %w", err)
		}
		log.Error(err, "terminal controller client error")
		return r.updateStatus(ctx, network, false, reason, err.Error(), "")
	}

	controllerID, summary := resolveUpstream(network.Spec.Name, upstreams)
	if controllerID == "" {
		// The stub client does not create upstream networks, so an unmatched
		// network is reported as not ready rather than silently ready, and is
		// re-checked periodically so it self-heals if the network appears.
		result, err := r.updateStatus(ctx, network, false, reasonUpstreamNotFound, summary, "")
		if err != nil {
			return result, err
		}
		result.RequeueAfter = upstreamNotFoundRequeueAfter
		return result, nil
	}
	return r.updateStatus(ctx, network, true, reasonReconciled, summary, controllerID)
}

// isAuthError reports whether the controller rejected the request because the
// caller is not authenticated or authorized.
func isAuthError(err *unifi.APIError) bool {
	return err.StatusCode == http.StatusUnauthorized || err.StatusCode == http.StatusForbidden
}

// resolveUpstream matches a network by its human-readable name and returns the
// upstream identifier plus a summary. An empty identifier means no match.
func resolveUpstream(name string, upstreams []unifi.Network) (string, string) {
	for _, upstream := range upstreams {
		if upstream.Name == name {
			return upstream.ID, fmt.Sprintf(
				"network %q matched upstream by name; vlan/subnet/enabled are not yet reconciled", name)
		}
	}
	return "", fmt.Sprintf("no upstream network named %q; nothing to reconcile", name)
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op.
func (r *UnifiNetworkReconciler) updateStatus(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
	ready bool,
	reason, message, controllerID string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if network.Status.ObservedGeneration != network.Generation {
		network.Status.ObservedGeneration = network.Generation
		changed = true
	}
	if network.Status.Summary != message {
		network.Status.Summary = message
		changed = true
	}
	if network.Status.ControllerID != controllerID {
		network.Status.ControllerID = controllerID
		changed = true
	}

	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	if apimeta.SetStatusCondition(&network.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: network.Generation,
	}) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, network); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// clampMessage bounds a status message to maxStatusMessageLen runes so
// controller-provided text cannot exceed the CRD's condition message limit. The
// ellipsis is included in the bound: a truncated result is maxStatusMessageLen
// runes or fewer. It is applied to every message in updateStatus; the UniFi
// client never puts credentials in error text.
func clampMessage(message string) string {
	const suffix = "…"
	runes := []rune(message)
	if len(runes) <= maxStatusMessageLen {
		return message
	}
	return string(runes[:maxStatusMessageLen-1]) + suffix
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes and deletion.
func (r *UnifiNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiNetwork{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named("unifinetwork").
		Complete(r)
}
