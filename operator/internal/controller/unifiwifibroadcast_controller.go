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
	"reflect"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
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
	// wifiBroadcastDependencyRequeueAfter bounds how long the broadcast waits
	// before retrying when its network, site, or controller is not yet ready.
	// Those changes do not alter this object's generation, so the periodic
	// requeue is what lets a late-appearing dependency self-heal.
	wifiBroadcastDependencyRequeueAfter = 30 * time.Second

	// wifiBroadcastNetworkReferenceSpecific is the upstream network-reference
	// variant the operator authors. The NATIVE variant is never authored.
	wifiBroadcastNetworkReferenceSpecific = "SPECIFIC"

	// wifiBroadcastDeviceFilterDeviceTags is the upstream broadcasting-device
	// filter variant the operator authors. The raw DEVICES variant is never
	// authored.
	wifiBroadcastDeviceFilterDeviceTags = "DEVICE_TAGS"

	// wifiBroadcastOriginUserDefined is the upstream metadata.origin of a
	// broadcast the operator may manage. Every other origin (DERIVED,
	// ORCHESTRATED) is system-managed and adopted read-only.
	wifiBroadcastOriginUserDefined = "USER_DEFINED"

	// Action labels for the human-readable status summary; originUnknown is the
	// placeholder for an absent upstream metadata.origin.
	broadcastActionCreated = "created"
	broadcastActionUpdated = "updated"
	broadcastOriginUnknown = "unknown"
)

// UnifiWifiBroadcastReconciler reconciles a UnifiWifiBroadcast object. It
// resolves the broadcast's UnifiNetwork, then the network's UnifiSite, connects
// through the site's controller, and creates, updates, or deletes the matching
// upstream broadcast. The broadcast is owned by its network, not the site
// (design D2/D4). The upstream broadcast UUID and the resolved network and site
// UUIDs are recorded in status only; spec never carries a UniFi internal
// identifier.
type UnifiWifiBroadcastReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader reads credential Secrets through an uncached reader so the
	// manager never caches Secrets (see connection.go). Passphrases are read
	// through this reader. Nil falls back to the cached client in tests.
	APIReader client.Reader

	// NewClient constructs the UniFi client for the resolved controller;
	// defaults to unifi.NewClient and is injectable for tests.
	NewClient ConnectionFactory
}

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiwifibroadcasts,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifiwifibroadcasts/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifisites,verbs=get;list;watch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unificontrollers,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get

// Reconcile resolves the broadcast's network, site, and controller transitively,
// applies the declarative broadcast to the console, and reports the result in
// status. It is idempotent: re-running it on an unchanged object performs no
// writes. It fails closed (Ready=False, no upstream mutation) on an unresolved
// network or site, a version below the WiFi minimum, a security configuration
// that references an unimplemented UnifiRadiusProfile, a device tag that does
// not resolve to exactly one device, or an unreadable passphrase Secret, and it
// adds/removes its finalizer symmetrically around the upstream broadcast's
// lifecycle.
func (r *UnifiWifiBroadcastReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	broadcast := &unifiv1alpha1.UnifiWifiBroadcast{}
	if err := r.Get(ctx, req.NamespacedName, broadcast); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !broadcast.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, broadcast)
	}

	network, site, stop, err := r.resolveDependencies(ctx, broadcast)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, broadcast, network); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, broadcast, network, site)
}

// resolveDependencies resolves spec.networkRef to a same-namespace UnifiNetwork
// that has recorded its upstream network UUID, then the network's spec.siteRef
// to a UnifiSite that has adopted an upstream site (the broadcast carries no
// siteRef of its own, design D2). An absent or deleting network, an
// unprovisioned network, an absent or deleting site, or a not-yet-adopted site
// fails closed: a non-nil result means the reconcile has finished and the
// caller must stop, while a nil result means the returned objects are usable.
func (r *UnifiWifiBroadcastReconciler) resolveDependencies(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
) (*unifiv1alpha1.UnifiNetwork, *unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	network := &unifiv1alpha1.UnifiNetwork{}
	networkKey := types.NamespacedName{Namespace: broadcast.Namespace, Name: broadcast.Spec.NetworkRef.Name}
	if err := r.Get(ctx, networkKey, network); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, broadcast, reasonNetworkRefNotFound,
				fmt.Sprintf("UnifiNetwork %q not found", broadcast.Spec.NetworkRef.Name))
			return nil, nil, &result, err
		}
		return nil, nil, nil, fmt.Errorf("get UnifiNetwork %q: %w", broadcast.Spec.NetworkRef.Name, err)
	}
	if !network.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, broadcast, reasonNetworkRefNotFound,
			fmt.Sprintf("UnifiNetwork %q is being deleted", network.Name))
		return nil, nil, &result, err
	}
	if network.Status.NetworkID == "" {
		result, err := r.requeueStatus(ctx, broadcast, reasonDependencyNotReady,
			fmt.Sprintf("UnifiNetwork %q has not reported its upstream network ID yet", network.Name))
		return nil, nil, &result, err
	}

	site := &unifiv1alpha1.UnifiSite{}
	siteKey := types.NamespacedName{Namespace: broadcast.Namespace, Name: network.Spec.SiteRef.Name}
	if err := r.Get(ctx, siteKey, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, broadcast, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", network.Spec.SiteRef.Name))
			return nil, nil, &result, err
		}
		return nil, nil, nil, fmt.Errorf("get UnifiSite %q: %w", network.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, broadcast, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, broadcast, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, nil, &result, err
	}
	return network, site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiNetwork (not the site, design D2) and adds the broadcast finalizer, in a
// single update when either is missing. The network's drain finalizer relies on
// the ownerReference.
func (r *UnifiWifiBroadcastReconciler) ensureOwnership(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	network *unifiv1alpha1.UnifiNetwork,
) error {
	changed := false
	if !metav1.IsControlledBy(broadcast, network) {
		if err := controllerutil.SetControllerReference(network, broadcast, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiNetwork %q: %w", network.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(broadcast, unifiv1alpha1.UnifiWifiBroadcastFinalizer) {
		controllerutil.AddFinalizer(broadcast, unifiv1alpha1.UnifiWifiBroadcastFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, broadcast); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console through the site's controller, gates
// on the WiFi capability, fails closed on a security configuration that
// references an unimplemented UnifiRadiusProfile, resolves the device-tag scope
// and passphrase Secrets, and creates, updates, or adopts the upstream
// broadcast. A non-USER_DEFINED broadcast is adopted read-only.
func (r *UnifiWifiBroadcastReconciler) reconcileUpstream(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	network *unifiv1alpha1.UnifiNetwork,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	networkID := network.Status.NetworkID
	siteID := site.Status.SiteID

	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, broadcast.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, broadcast, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, broadcast, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityWifi); err != nil {
		return r.updateStatus(ctx, broadcast, false, reasonVersionUnsupported, err.Error(), nil, &networkID, &siteID, nil)
	}

	// UnifiRadiusProfile has no Go type yet, so any security configuration that
	// references one fails closed before any upstream call (design D4).
	if reason, message := unsupportedSecurityReason(broadcast.Spec.SecurityConfiguration); reason != "" {
		return r.updateStatus(ctx, broadcast, false, reason, message, nil, &networkID, &siteID, nil)
	}

	apiClient, err := resolver.Resolve(ctx, broadcast.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, broadcast, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	tagIDs, reason, message, err := resolveDeviceTagIDs(ctx, apiClient, siteID, referencedDeviceTagNames(broadcast.Spec))
	if err != nil {
		return r.handleUpstreamError(ctx, broadcast, err)
	}
	if reason != "" {
		return r.updateStatus(ctx, broadcast, false, reason, message, nil, &networkID, &siteID, nil)
	}

	networkIDsByRef, stop, err := r.resolveReferencedNetworkIDs(ctx, broadcast, network, networkID, siteID)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	reader := readerOrCached(r.APIReader, r.Client)
	security, err := convertSecurityConfiguration(
		broadcast.Spec.SecurityConfiguration, networkIDsByRef,
		func(selector *corev1.SecretKeySelector) (string, error) {
			return resolveAPIKey(ctx, reader, broadcast.Namespace, selector)
		},
	)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.updateStatus(ctx, broadcast, false, resolveErr.Reason, resolveErr.Message, nil, &networkID, &siteID, nil)
		}
		return ctrl.Result{}, err
	}

	request, err := buildWifiBroadcastRequest(broadcast, networkID, security, tagIDs, networkIDsByRef)
	if err != nil {
		return r.handleUpstreamError(ctx, broadcast, err)
	}

	overviews, err := apiClient.ListWifiBroadcasts(ctx, siteID)
	if err != nil {
		return r.handleUpstreamError(ctx, broadcast, err)
	}
	existing, err := resolveExistingBroadcast(ctx, apiClient, siteID, broadcast, overviews)
	if err != nil {
		return r.handleUpstreamError(ctx, broadcast, err)
	}

	// Fail closed on a system/derived/orchestrated object: the operator only
	// owns USER_DEFINED broadcasts. An empty origin is the operator's own: a
	// console that omits metadata.origin on a just-created broadcast must not
	// flip it read-only, so only a non-empty non-USER_DEFINED origin fails closed.
	if existing != nil {
		origin := firstNonEmpty(existing.Metadata.Origin, broadcast.Status.Origin)
		if origin != "" && origin != wifiBroadcastOriginUserDefined {
			return r.updateStatus(ctx, broadcast, false, reasonSystemObjectReadOnly,
				systemBroadcastMessage(*existing, origin), nil, &networkID, &siteID, &origin)
		}
	}

	var (
		result unifi.WifiBroadcast
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateWifiBroadcast(ctx, siteID, request)
		action = broadcastActionCreated
	case broadcastMatchesRequest(*existing, request):
		// The upstream object already matches the desired state; skip the PUT so
		// re-reconciling an unchanged object performs no write.
		wifiBroadcastID := firstNonEmpty(existing.ID, broadcast.Status.WifiBroadcastID)
		origin := firstNonEmpty(existing.Metadata.Origin, broadcast.Status.Origin)
		summary := fmt.Sprintf("upstream wifi broadcast %q is up to date", broadcast.Spec.Name)
		return r.updateStatus(ctx, broadcast, true, reasonReconciled, summary, &wifiBroadcastID, &networkID, &siteID, &origin)
	default:
		result, err = apiClient.UpdateWifiBroadcast(ctx, siteID, existing.ID, request)
		action = broadcastActionUpdated
	}
	if err != nil {
		return r.handleUpstreamError(ctx, broadcast, err)
	}

	// Keep the last-known identifiers when a response omits them, so a
	// transiently incomplete body cannot erase a correlation ID.
	wifiBroadcastID := firstNonEmpty(result.ID, broadcast.Status.WifiBroadcastID)
	origin := firstNonEmpty(result.Metadata.Origin, broadcast.Status.Origin)
	if existing != nil {
		wifiBroadcastID = firstNonEmpty(wifiBroadcastID, existing.ID)
		origin = firstNonEmpty(origin, existing.Metadata.Origin)
	}
	if existing == nil && origin == "" {
		origin = wifiBroadcastOriginUserDefined
	}

	summary := fmt.Sprintf("%s upstream wifi broadcast %q", action, broadcast.Spec.Name)
	return r.updateStatus(ctx, broadcast, true, reasonReconciled, summary, &wifiBroadcastID, &networkID, &siteID, &origin)
}

// reconcileDelete deletes the upstream broadcast recorded in status and removes
// the finalizer. It resolves the console from the status-recorded site when the
// live reference chain is broken, so a dangling networkRef does not strand
// upstream state (design D2). When status.wifiBroadcastID is empty there is no
// upstream broadcast to delete.
func (r *UnifiWifiBroadcastReconciler) reconcileDelete(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(broadcast, unifiv1alpha1.UnifiWifiBroadcastFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, broadcast); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(broadcast, unifiv1alpha1.UnifiWifiBroadcastFinalizer)
	if err := r.Update(ctx, broadcast); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the broadcast recorded in status from the console. A
// 404 means it is already gone and is treated as success. A system-managed
// object is never deleted.
func (r *UnifiWifiBroadcastReconciler) deleteUpstream(ctx context.Context, broadcast *unifiv1alpha1.UnifiWifiBroadcast) error {
	if broadcast.Status.WifiBroadcastID == "" {
		return nil
	}
	apiClient, siteID, err := r.resolveCleanupClient(ctx, broadcast)
	if err != nil {
		return err
	}

	existing, err := apiClient.GetWifiBroadcast(ctx, siteID, broadcast.Status.WifiBroadcastID)
	if err != nil {
		if errors.Is(err, unifi.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get upstream wifi broadcast %q for cleanup: %w", broadcast.Status.WifiBroadcastID, err)
	}
	// Mirror the reconcile path: resolve the effective origin from the console
	// value or the recorded status, and treat an empty origin as the operator's
	// own. Only a non-empty non-USER_DEFINED origin is system-managed and skipped,
	// so an operator-created broadcast whose console omits metadata.origin is
	// still deleted rather than leaked.
	origin := firstNonEmpty(existing.Metadata.Origin, broadcast.Status.Origin)
	if origin != "" && origin != wifiBroadcastOriginUserDefined {
		return nil
	}

	if err := apiClient.DeleteWifiBroadcast(ctx, siteID, broadcast.Status.WifiBroadcastID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream wifi broadcast %q: %w", broadcast.Status.WifiBroadcastID, err)
	}
	return nil
}

// resolveCleanupClient resolves the console and the upstream site UUID used to
// delete a broadcast. Design D2 mandates the status-first order: it prefers the
// site whose status.siteID matches the broadcast's recorded status.siteID, so
// teardown does not depend on the live reference chain. Only when no
// status.siteID is recorded (or no site matches it) does it fall back to the live
// chain (spec.networkRef -> UnifiNetwork -> spec.siteRef -> UnifiSite), which
// tolerates an object that is being deleted so the recursive site -> network ->
// broadcast drain keeps working. When neither resolves it returns an error so
// the finalizer blocks rather than silently orphaning upstream state.
func (r *UnifiWifiBroadcastReconciler) resolveCleanupClient(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
) (unifi.Client, string, error) {
	namespace := broadcast.Namespace

	if broadcast.Status.SiteID != "" {
		var sites unifiv1alpha1.UnifiSiteList
		if err := r.List(ctx, &sites, client.InNamespace(namespace)); err != nil {
			return nil, "", fmt.Errorf("list sites for cleanup: %w", err)
		}
		for i := range sites.Items {
			if sites.Items[i].Status.SiteID != broadcast.Status.SiteID {
				continue
			}
			apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, namespace, sites.Items[i].Spec.ControllerRef)
			if err != nil {
				return nil, "", fmt.Errorf("resolve controller for cleanup: %w", err)
			}
			return apiClient, broadcast.Status.SiteID, nil
		}
	}

	network := &unifiv1alpha1.UnifiNetwork{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: broadcast.Spec.NetworkRef.Name}, network); err == nil {
		site := &unifiv1alpha1.UnifiSite{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: namespace, Name: network.Spec.SiteRef.Name}, site); err == nil {
			// Use the live site's own status.siteID. The status-first pass above
			// already failed to match a site, so a non-empty recorded status.siteID
			// here is stale: pairing it with the live controller would clean up on
			// the wrong console/site. Fail closed instead so the finalizer blocks.
			if broadcast.Status.SiteID != "" && broadcast.Status.SiteID != site.Status.SiteID {
				return nil, "", fmt.Errorf(
					"recorded site %q does not match the referenced site %q for wifi broadcast %q cleanup",
					broadcast.Status.SiteID, site.Status.SiteID, broadcast.Name)
			}
			if site.Status.SiteID != "" {
				apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, namespace, site.Spec.ControllerRef)
				if err != nil {
					return nil, "", fmt.Errorf("resolve controller for cleanup: %w", err)
				}
				return apiClient, site.Status.SiteID, nil
			}
		}
	}

	return nil, "", fmt.Errorf("resolve controller for wifi broadcast %q cleanup: reference chain unresolved", broadcast.Name)
}

// resolveReferencedNetworkIDs resolves every UnifiNetwork referenced from the
// spec (preshared-key networks and mDNS bridging networks) to its upstream
// network UUID. A reference that does not resolve to a provisioned network
// fails closed and is retried. A reference to a network on a different site than
// the broadcast's own network (broadcast's spec.networkRef) fails closed
// terminally with CrossSiteReference, so a foreign-site network UUID is never
// written into the broadcast body (mirrors the firewall-zone controller's
// cross-site guard). The returned stop result is non-nil when the reconcile
// must finish without a write.
func (r *UnifiWifiBroadcastReconciler) resolveReferencedNetworkIDs(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	network *unifiv1alpha1.UnifiNetwork,
	networkID, siteID string,
) (map[string]string, *ctrl.Result, error) {
	names := referencedNetworkNames(broadcast.Spec)
	if len(names) == 0 {
		return nil, nil, nil
	}
	ids := make(map[string]string, len(names))
	for _, name := range names {
		referenced := &unifiv1alpha1.UnifiNetwork{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: broadcast.Namespace, Name: name}, referenced); err != nil {
			if apierrors.IsNotFound(err) {
				result, err := r.requeueStatus(ctx, broadcast, reasonDependencyNotReady,
					fmt.Sprintf("referenced UnifiNetwork %q not found", name))
				return nil, &result, err
			}
			return nil, nil, fmt.Errorf("get referenced UnifiNetwork %q: %w", name, err)
		}
		if referenced.Spec.SiteRef.Name != network.Spec.SiteRef.Name {
			result, err := r.updateStatus(ctx, broadcast, false, reasonCrossSiteReference,
				fmt.Sprintf("referenced UnifiNetwork %q belongs to UnifiSite %q, not %q; no upstream broadcast was modified",
					name, referenced.Spec.SiteRef.Name, network.Spec.SiteRef.Name), nil, &networkID, &siteID, nil)
			return nil, &result, err
		}
		if referenced.Status.NetworkID == "" {
			result, err := r.requeueStatus(ctx, broadcast, reasonDependencyNotReady,
				fmt.Sprintf("referenced UnifiNetwork %q has not reported its upstream network ID yet", name))
			return nil, &result, err
		}
		ids[name] = referenced.Status.NetworkID
	}
	return ids, nil, nil
}

// handleUpstreamError classifies a failed broadcast write. Certificate, auth,
// and bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiWifiBroadcastReconciler) handleUpstreamError(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, broadcast, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil, nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, broadcast, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil, nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, broadcast, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil, nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, broadcast, false, reasonInvalidSpec, specErr.Error(), nil, nil, nil, nil)
	}
	log.Error(err, "transient error reconciling wifi broadcast")
	return ctrl.Result{}, fmt.Errorf("reconcile wifi broadcast: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency failure a later edit can fix without changing this object's
// generation.
func (r *UnifiWifiBroadcastReconciler) requeueStatus(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, broadcast, false, reason, message, nil, nil, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = wifiBroadcastDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil identifier pointer leaves the recorded value
// untouched, preserving the last-known identifiers on failing paths.
func (r *UnifiWifiBroadcastReconciler) updateStatus(
	ctx context.Context,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	ready bool,
	reason, message string,
	wifiBroadcastID, networkID, siteID, origin *string,
) (ctrl.Result, error) {
	message = clampMessage(message)
	changed := false

	if broadcast.Status.ObservedGeneration != broadcast.Generation {
		broadcast.Status.ObservedGeneration = broadcast.Generation
		changed = true
	}
	if broadcast.Status.Summary != message {
		broadcast.Status.Summary = message
		changed = true
	}
	if wifiBroadcastID != nil && broadcast.Status.WifiBroadcastID != *wifiBroadcastID {
		broadcast.Status.WifiBroadcastID = *wifiBroadcastID
		changed = true
	}
	if networkID != nil && broadcast.Status.NetworkID != *networkID {
		broadcast.Status.NetworkID = *networkID
		changed = true
	}
	if siteID != nil && broadcast.Status.SiteID != *siteID {
		broadcast.Status.SiteID = *siteID
		changed = true
	}
	if origin != nil && broadcast.Status.Origin != *origin {
		broadcast.Status.Origin = *origin
		changed = true
	}
	if setReadyCondition(&broadcast.Status.Conditions, broadcast.Generation, ready, reason, message) {
		changed = true
	}

	if !changed {
		return ctrl.Result{}, nil
	}
	if err := r.Status().Update(ctx, broadcast); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status: %w", err)
	}
	return ctrl.Result{}, nil
}

// unsupportedSecurityReason reports the fail-closed reason for a security
// configuration the operator cannot provision yet. Any configuration that
// references a UnifiRadiusProfile fails closed, because that kind has no Go type
// and the enterprise variants always carry such a reference (design D4).
func unsupportedSecurityReason(spec unifiv1alpha1.WifiSecurityConfiguration) (reason, message string) {
	if securityUsesRadiusProfile(spec) {
		return reasonRadiusProfileUnsupported,
			"security configuration references a UnifiRadiusProfile, which is not implemented yet; no upstream broadcast was modified"
	}
	return "", ""
}

// securityUsesRadiusProfile reports whether the selected security variant
// carries a RADIUS configuration. The enterprise variants require one; the
// personal and open variants carry one only when explicitly authored.
func securityUsesRadiusProfile(spec unifiv1alpha1.WifiSecurityConfiguration) bool {
	switch {
	case spec.Open != nil:
		return spec.Open.RadiusConfiguration != nil
	case spec.WPA2Personal != nil:
		return spec.WPA2Personal.RadiusConfiguration != nil
	case spec.WPA2WPA3Personal != nil:
		return spec.WPA2WPA3Personal.RadiusConfiguration != nil
	case spec.WPA3Personal != nil:
		return spec.WPA3Personal.RadiusConfiguration != nil
	case spec.WPA2Enterprise != nil, spec.WPA2WPA3Enterprise != nil, spec.WPA3Enterprise != nil:
		return true
	default:
		return false
	}
}

// convertSecurityConfiguration maps the declarative security variant onto the
// upstream security body, resolving every passphrase Secret through
// resolvePassphrase. The caller has already failed closed on any variant that
// references a UnifiRadiusProfile.
func convertSecurityConfiguration(
	spec unifiv1alpha1.WifiSecurityConfiguration,
	networkIDsByRef map[string]string,
	resolvePassphrase func(*corev1.SecretKeySelector) (string, error),
) (*unifi.WifiSecurityConfiguration, error) {
	out := &unifi.WifiSecurityConfiguration{Type: spec.Type}
	switch {
	case spec.Open != nil:
		if spec.Open.Encryption != nil {
			out.Encryption = *spec.Open.Encryption
		}
	case spec.WPA2Personal != nil:
		v := spec.WPA2Personal
		passphrase, err := resolveOptionalPassphrase(v.Passphrase, resolvePassphrase)
		if err != nil {
			return nil, err
		}
		presharedKeys, err := convertPresharedKeys(v.PresharedKeys, networkIDsByRef, resolvePassphrase)
		if err != nil {
			return nil, err
		}
		out.Passphrase = passphrase
		out.PresharedKeys = presharedKeys
		if v.PmfMode != nil {
			out.PmfMode = *v.PmfMode
		}
		out.FastRoamingEnabled = v.FastRoamingEnabled
		out.GroupRekeyIntervalSeconds = v.GroupRekeyIntervalSeconds
	case spec.WPA2WPA3Personal != nil:
		v := spec.WPA2WPA3Personal
		passphrase, err := resolvePassphrase(&v.Passphrase)
		if err != nil {
			return nil, err
		}
		out.Passphrase = passphrase
		out.PmfMode = v.PmfMode
		out.SaeConfiguration = &unifi.WifiSAEConfiguration{
			AnticloggingThresholdSeconds: v.SaeConfiguration.AnticloggingThresholdSeconds,
			SyncTimeSeconds:              v.SaeConfiguration.SyncTimeSeconds,
		}
		wpa3FastRoaming := v.Wpa3FastRoamingEnabled
		out.Wpa3FastRoamingEnabled = &wpa3FastRoaming
		out.FastRoamingEnabled = v.FastRoamingEnabled
		out.GroupRekeyIntervalSeconds = v.GroupRekeyIntervalSeconds
	case spec.WPA3Personal != nil:
		v := spec.WPA3Personal
		passphrase, err := resolvePassphrase(&v.Passphrase)
		if err != nil {
			return nil, err
		}
		out.Passphrase = passphrase
		out.SaeConfiguration = &unifi.WifiSAEConfiguration{
			AnticloggingThresholdSeconds: v.SaeConfiguration.AnticloggingThresholdSeconds,
			SyncTimeSeconds:              v.SaeConfiguration.SyncTimeSeconds,
		}
		out.FastRoamingEnabled = v.FastRoamingEnabled
		out.GroupRekeyIntervalSeconds = v.GroupRekeyIntervalSeconds
	default:
		return nil, &unifi.InvalidSpecError{Field: "securityConfiguration", Reason: "unsupported security variant"}
	}
	return out, nil
}

// resolveOptionalPassphrase resolves a passphrase Secret reference, treating an
// unset selector as no passphrase.
func resolveOptionalPassphrase(
	selector *corev1.SecretKeySelector,
	resolvePassphrase func(*corev1.SecretKeySelector) (string, error),
) (string, error) {
	if selector == nil {
		return "", nil
	}
	return resolvePassphrase(selector)
}

// convertPresharedKeys maps the per-network preshared keys, resolving each
// passphrase Secret and each referenced network's upstream UUID.
func convertPresharedKeys(
	keys []unifiv1alpha1.WifiPresharedKey,
	networkIDsByRef map[string]string,
	resolvePassphrase func(*corev1.SecretKeySelector) (string, error),
) ([]unifi.WifiPresharedKey, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	out := make([]unifi.WifiPresharedKey, 0, len(keys))
	for _, key := range keys {
		passphrase, err := resolvePassphrase(&key.Passphrase)
		if err != nil {
			return nil, err
		}
		out = append(out, unifi.WifiPresharedKey{
			Network:    unifi.WifiNetworkReference{Type: wifiBroadcastNetworkReferenceSpecific, NetworkID: networkIDsByRef[key.Network.Name]},
			Passphrase: passphrase,
		})
	}
	return out, nil
}

// buildWifiBroadcastRequest maps the declarative spec onto the upstream
// create/update body. The security configuration is prebuilt (its Secrets
// already resolved) and the device-tag and network identifiers are the resolved
// upstream UUIDs.
func buildWifiBroadcastRequest(
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	networkID string,
	security *unifi.WifiSecurityConfiguration,
	tagIDs map[string]string,
	networkIDsByRef map[string]string,
) (unifi.WifiBroadcastRequest, error) {
	spec := broadcast.Spec
	request := unifi.WifiBroadcastRequest{
		Type:                                spec.Type,
		Name:                                spec.Name,
		Enabled:                             boolDefaultTrue(spec.Enabled),
		HideName:                            spec.HideName,
		ClientIsolationEnabled:              spec.ClientIsolationEnabled,
		MulticastToUnicastConversionEnabled: spec.MulticastToUnicastConversionEnabled,
		UapsdEnabled:                        spec.UapsdEnabled,
		Channel2gLockedTo6:                  boolValue(spec.Channel2gLockedTo6),
		DtimPeriod2gLockedTo3:               boolValue(spec.DtimPeriod2gLockedTo3),
		Network:                             &unifi.WifiNetworkReference{Type: wifiBroadcastNetworkReferenceSpecific, NetworkID: networkID},
		SecurityConfiguration:               security,
		BroadcastingDeviceFilter:            deviceTagFilter(spec.DeviceTags, tagIDs),
		BasicDataRateKbpsByFrequencyGHz:     convertBasicDataRates(spec.BasicDataRateKbpsByFrequencyGHz),
		BlackoutScheduleConfiguration:       convertBlackout(spec.BlackoutScheduleConfiguration),
		ClientFilteringPolicy:               convertClientFiltering(spec.ClientFilteringPolicy),
		MDNSProxyConfiguration:              convertMDNS(spec.MDNSProxyConfiguration, networkIDsByRef, tagIDs),
		MulticastFilteringPolicy:            convertMulticast(spec.MulticastFilteringPolicy),
	}
	if spec.Standard != nil {
		frequencies, err := mapBroadcastingFrequencies(spec.Standard.BroadcastingFrequenciesGHz)
		if err != nil {
			return unifi.WifiBroadcastRequest{}, err
		}
		advertiseDeviceName := spec.Standard.AdvertiseDeviceName
		arpProxyEnabled := spec.Standard.ArpProxyEnabled
		bssTransitionEnabled := spec.Standard.BssTransitionEnabled
		request.AdvertiseDeviceName = &advertiseDeviceName
		request.ArpProxyEnabled = &arpProxyEnabled
		request.BssTransitionEnabled = &bssTransitionEnabled
		request.BroadcastingFrequenciesGHz = frequencies
		request.BandSteeringEnabled = spec.Standard.BandSteeringEnabled
		request.MloEnabled = spec.Standard.MloEnabled
		request.DNSAssistanceConfiguration = convertDNS(spec.Standard.DNSAssistanceConfiguration)
		request.DtimPeriodByFrequencyGHzOverride = convertDtim(spec.Standard.DtimPeriodByFrequencyGHzOverride)
		request.HandoffSuggestionsConfiguration = convertHandoff(spec.Standard.HandoffSuggestionsConfiguration)
		request.HotspotConfiguration = convertHotspot(spec.Standard.HotspotConfiguration)
	}
	return request, nil
}

// mapBroadcastingFrequencies maps the CR's exact string frequency enum onto the
// numeric upstream wire values. The CRD enum makes an unknown value impossible,
// but it is rejected defensively as a terminal spec error.
func mapBroadcastingFrequencies(values []unifiv1alpha1.WifiBroadcastingFrequencyGHz) ([]float64, error) {
	out := make([]float64, 0, len(values))
	for _, value := range values {
		switch string(value) {
		case "2.4":
			out = append(out, 2.4)
		case "5":
			out = append(out, 5)
		case "6":
			out = append(out, 6)
		default:
			return nil, &unifi.InvalidSpecError{
				Field:  "broadcastingFrequenciesGHz",
				Reason: fmt.Sprintf("%q is not a known frequency", value),
			}
		}
	}
	return out, nil
}

// referencedDeviceTagNames returns every device-tag name the spec references
// (the broadcast scope and each mDNS policy device filter), de-duplicated.
func referencedDeviceTagNames(spec unifiv1alpha1.UnifiWifiBroadcastSpec) []string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, selector := range spec.DeviceTags {
		add(selector.Name)
	}
	if spec.MDNSProxyConfiguration != nil {
		for _, policy := range spec.MDNSProxyConfiguration.Policies {
			for _, selector := range policy.DeviceTags {
				add(selector.Name)
			}
		}
	}
	return names
}

// referencedNetworkNames returns every UnifiNetwork name the spec references
// (preshared-key networks and mDNS bridging networks), de-duplicated.
func referencedNetworkNames(spec unifiv1alpha1.UnifiWifiBroadcastSpec) []string {
	seen := make(map[string]struct{})
	var names []string
	add := func(name string) {
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	for _, key := range presharedKeysOf(spec.SecurityConfiguration) {
		add(key.Network.Name)
	}
	if spec.MDNSProxyConfiguration != nil {
		for _, policy := range spec.MDNSProxyConfiguration.Policies {
			for _, ref := range policy.BridgingNetworkRefs {
				add(ref.Name)
			}
		}
	}
	return names
}

// presharedKeysOf returns the preshared keys of the selected security variant.
// Only WPA2_PERSONAL models them.
func presharedKeysOf(spec unifiv1alpha1.WifiSecurityConfiguration) []unifiv1alpha1.WifiPresharedKey {
	if spec.WPA2Personal != nil {
		return spec.WPA2Personal.PresharedKeys
	}
	return nil
}

// resolveDeviceTagIDs resolves device-tag names against the read-only
// device-tags list. A name that does not exist is DeviceTagNotFound; a name
// that does not resolve to exactly one device is DeviceTagAmbiguous. Both fail
// closed with no upstream mutation.
func resolveDeviceTagIDs(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	names []string,
) (map[string]string, string, string, error) {
	if len(names) == 0 {
		return nil, "", "", nil
	}
	tags, err := apiClient.ListDeviceTags(ctx, siteID)
	if err != nil {
		return nil, "", "", err
	}
	ids := make(map[string]string, len(names))
	for _, name := range names {
		tag := findDeviceTagByName(tags, name)
		if tag == nil {
			return nil, reasonDeviceTagNotFound, fmt.Sprintf(
				"device tag %q does not exist on the site; no upstream broadcast was modified", name), nil
		}
		if len(tag.DeviceIDs) != 1 {
			return nil, reasonDeviceTagAmbiguous, fmt.Sprintf(
				"device tag %q resolves to %d devices; exactly one is required; no upstream broadcast was modified",
				name, len(tag.DeviceIDs)), nil
		}
		ids[name] = tag.ID
	}
	return ids, "", "", nil
}

// deviceTagFilter builds the DEVICE_TAGS broadcasting filter for the given
// selectors, or nil when none are set.
func deviceTagFilter(selectors []unifiv1alpha1.DeviceTagSelector, tagIDs map[string]string) *unifi.WifiBroadcastingDeviceFilter {
	if len(selectors) == 0 {
		return nil
	}
	ids := make([]string, 0, len(selectors))
	for _, selector := range selectors {
		if id, ok := tagIDs[selector.Name]; ok {
			ids = append(ids, id)
		}
	}
	return &unifi.WifiBroadcastingDeviceFilter{Type: wifiBroadcastDeviceFilterDeviceTags, DeviceTagIDs: ids}
}

// convertBasicDataRates maps the basic data-rate configuration.
func convertBasicDataRates(in *unifiv1alpha1.WifiBasicDataRateConfiguration) *unifi.WifiBasicDataRateConfiguration {
	if in == nil {
		return nil
	}
	return &unifi.WifiBasicDataRateConfiguration{GHz2_4: in.GHz2_4, GHz5: in.GHz5}
}

// convertDtim maps the DTIM period override.
func convertDtim(in *unifiv1alpha1.WifiDtimPeriodConfiguration) *unifi.WifiDtimPeriodConfiguration {
	if in == nil {
		return nil
	}
	return &unifi.WifiDtimPeriodConfiguration{GHz2_4: in.GHz2_4, GHz5: in.GHz5, GHz6: in.GHz6}
}

// convertBlackout maps the blackout schedule.
func convertBlackout(in *unifiv1alpha1.WifiBlackoutScheduleConfiguration) *unifi.WifiBlackoutScheduleConfiguration {
	if in == nil {
		return nil
	}
	days := make([]unifi.WifiBlackoutScheduleDay, 0, len(in.Days))
	for _, day := range in.Days {
		out := unifi.WifiBlackoutScheduleDay{Day: day.Day, Type: day.Type}
		for _, window := range day.TimeRanges {
			out.TimeRanges = append(out.TimeRanges, unifi.WifiBlackoutTimeRange{StartTime: window.StartTime, EndTime: window.EndTime})
		}
		days = append(days, out)
	}
	return &unifi.WifiBlackoutScheduleConfiguration{Days: days}
}

// convertClientFiltering maps the client filtering policy.
func convertClientFiltering(in *unifiv1alpha1.WifiClientFilteringPolicy) *unifi.WifiClientFilteringPolicy {
	if in == nil {
		return nil
	}
	return &unifi.WifiClientFilteringPolicy{Action: in.Action, MacAddressFilter: in.MacAddressFilter}
}

// convertMulticast maps the multicast filtering policy.
func convertMulticast(in *unifiv1alpha1.WifiMulticastFilteringPolicy) *unifi.WifiMulticastFilteringPolicy {
	if in == nil {
		return nil
	}
	return &unifi.WifiMulticastFilteringPolicy{Action: in.Action, SourceMacAddressFilter: in.SourceMacAddressFilter}
}

// convertDNS maps the DNS assistance configuration.
func convertDNS(in *unifiv1alpha1.WifiDNSAssistanceConfiguration) *unifi.WifiDNSAssistanceConfiguration {
	if in == nil {
		return nil
	}
	return &unifi.WifiDNSAssistanceConfiguration{Mode: in.Mode, Servers: in.Servers}
}

// convertHandoff maps the handoff suggestions configuration.
func convertHandoff(in *unifiv1alpha1.WifiHandoffSuggestionsConfiguration) *unifi.WifiHandoffSuggestionsConfiguration {
	if in == nil {
		return nil
	}
	return &unifi.WifiHandoffSuggestionsConfiguration{
		Band5GHzRssiThreshold: in.Band5GHzRssiThreshold,
		Band6GHzRssiThreshold: in.Band6GHzRssiThreshold,
	}
}

// convertHotspot maps the hotspot configuration.
func convertHotspot(in *unifiv1alpha1.WifiHotspotConfiguration) *unifi.WifiHotspotConfiguration {
	if in == nil {
		return nil
	}
	return &unifi.WifiHotspotConfiguration{Type: in.Type}
}

// convertMDNS maps the mDNS proxy configuration, resolving each bridging
// network reference and each policy device-tag selector to its upstream UUID.
func convertMDNS(
	in *unifiv1alpha1.WifiMDNSProxyConfiguration,
	networkIDsByRef map[string]string,
	tagIDs map[string]string,
) *unifi.WifiMDNSProxyConfiguration {
	if in == nil {
		return nil
	}
	out := &unifi.WifiMDNSProxyConfiguration{Mode: in.Mode}
	for _, policy := range in.Policies {
		converted := unifi.WifiMDNSProxyPolicy{
			Action:       policy.Action,
			DeviceFilter: deviceTagFilter(policy.DeviceTags, tagIDs),
		}
		for _, ref := range policy.BridgingNetworkRefs {
			converted.BridgingNetworkIDs = append(converted.BridgingNetworkIDs, networkIDsByRef[ref.Name])
		}
		for _, service := range policy.ServiceFilter {
			converted.ServiceFilter = append(converted.ServiceFilter, convertMDNSService(service))
		}
		out.Policies = append(out.Policies, converted)
	}
	return out
}

// convertMDNSService flattens the discriminated CR mDNS service onto the flat
// upstream shape.
func convertMDNSService(in unifiv1alpha1.WifiMDNSService) unifi.WifiMDNSService {
	out := unifi.WifiMDNSService{Type: in.Type}
	switch {
	case in.Custom != nil:
		out.Name = in.Custom.Name
		out.TypeDomain = in.Custom.TypeDomain
	case in.Predefined != nil:
		out.Name = in.Predefined.Name
	}
	return out
}

// boolDefaultTrue dereferences an optional bool defaulting to true.
func boolDefaultTrue(value *bool) bool {
	return value == nil || *value
}

// findWifiBroadcastByName returns the overview broadcast matching name, or nil.
func findWifiBroadcastByName(broadcasts []unifi.WifiBroadcastOverview, name string) *unifi.WifiBroadcastOverview {
	for i := range broadcasts {
		if broadcasts[i].Name == name {
			return &broadcasts[i]
		}
	}
	return nil
}

// resolveExistingBroadcast returns the upstream broadcast this CR manages, or
// nil when none exists. It resolves status.wifiBroadcastID first so a spec.name
// rename updates the same upstream object instead of orphaning it, then falls
// back to a name match for adoption. The detail endpoint is always read because
// the list is an overview only (design D7); a 404 is treated as absent.
func resolveExistingBroadcast(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	broadcast *unifiv1alpha1.UnifiWifiBroadcast,
	overviews []unifi.WifiBroadcastOverview,
) (*unifi.WifiBroadcast, error) {
	if broadcast.Status.WifiBroadcastID != "" {
		existing, err := apiClient.GetWifiBroadcast(ctx, siteID, broadcast.Status.WifiBroadcastID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the name match: the recorded object is gone.
		default:
			return nil, err
		}
	}

	match := findWifiBroadcastByName(overviews, broadcast.Spec.Name)
	if match == nil {
		return nil, nil
	}
	detail, err := apiClient.GetWifiBroadcast(ctx, siteID, match.ID)
	switch {
	case err == nil:
		return &detail, nil
	case errors.Is(err, unifi.ErrNotFound):
		return nil, nil
	default:
		return nil, err
	}
}

// systemBroadcastMessage explains why an upstream broadcast is read-only,
// without echoing credential material.
func systemBroadcastMessage(existing unifi.WifiBroadcast, origin string) string {
	if origin == "" {
		origin = broadcastOriginUnknown
	}
	return fmt.Sprintf("upstream wifi broadcast %q has metadata.origin %s, not %s; refusing to overwrite a system-managed object",
		existing.Name, origin, wifiBroadcastOriginUserDefined)
}

// broadcastMatchesRequest reports whether the observed upstream broadcast
// already matches the desired write, so the reconciler can skip the PUT and stay
// idempotent (design D7). Only authored fields are compared; observed fields
// (id, metadata) are ignored. A desired optional field that is unset is ignored
// so an omitted optional field is never treated as drift.
func broadcastMatchesRequest(existing unifi.WifiBroadcast, req unifi.WifiBroadcastRequest) bool {
	observed := existing.WifiBroadcastRequest
	if observed.Type != req.Type ||
		observed.Name != req.Name ||
		observed.Enabled != req.Enabled ||
		observed.HideName != req.HideName ||
		observed.ClientIsolationEnabled != req.ClientIsolationEnabled ||
		observed.MulticastToUnicastConversionEnabled != req.MulticastToUnicastConversionEnabled ||
		observed.UapsdEnabled != req.UapsdEnabled ||
		observed.Channel2gLockedTo6 != req.Channel2gLockedTo6 ||
		observed.DtimPeriod2gLockedTo3 != req.DtimPeriod2gLockedTo3 {
		return false
	}
	if !optionalBoolMatches(observed.AdvertiseDeviceName, req.AdvertiseDeviceName) ||
		!optionalBoolMatches(observed.ArpProxyEnabled, req.ArpProxyEnabled) ||
		!optionalBoolMatches(observed.BssTransitionEnabled, req.BssTransitionEnabled) ||
		!optionalBoolMatches(observed.BandSteeringEnabled, req.BandSteeringEnabled) ||
		!optionalBoolMatches(observed.MloEnabled, req.MloEnabled) {
		return false
	}
	if !floatSlicesMatch(observed.BroadcastingFrequenciesGHz, req.BroadcastingFrequenciesGHz) {
		return false
	}
	if !wifiNetworkReferenceMatches(observed.Network, req.Network) {
		return false
	}
	if !wifiSecurityMatches(observed.SecurityConfiguration, req.SecurityConfiguration) {
		return false
	}
	if !wifiDeviceFilterMatches(observed.BroadcastingDeviceFilter, req.BroadcastingDeviceFilter) {
		return false
	}
	if !wifiClientFilteringMatches(observed.ClientFilteringPolicy, req.ClientFilteringPolicy) {
		return false
	}
	if !wifiMulticastMatches(observed.MulticastFilteringPolicy, req.MulticastFilteringPolicy) {
		return false
	}
	if !wifiDNSMatches(observed.DNSAssistanceConfiguration, req.DNSAssistanceConfiguration) {
		return false
	}
	if !wifiMDNSMatches(observed.MDNSProxyConfiguration, req.MDNSProxyConfiguration) {
		return false
	}
	return wifiOptionalDeepEqual(observed.BasicDataRateKbpsByFrequencyGHz, req.BasicDataRateKbpsByFrequencyGHz) &&
		wifiOptionalDeepEqual(observed.BlackoutScheduleConfiguration, req.BlackoutScheduleConfiguration) &&
		wifiOptionalDeepEqual(observed.DtimPeriodByFrequencyGHzOverride, req.DtimPeriodByFrequencyGHzOverride) &&
		wifiOptionalDeepEqual(observed.HandoffSuggestionsConfiguration, req.HandoffSuggestionsConfiguration) &&
		wifiOptionalDeepEqual(observed.HotspotConfiguration, req.HotspotConfiguration)
}

// wifiOptionalDeepEqual compares two optional values: an unset desired value is
// optional and ignored, an unset observed value does not match a set desired
// value, and otherwise the values are compared structurally.
func wifiOptionalDeepEqual[T any](observed, req *T) bool {
	if req == nil {
		return true
	}
	if observed == nil {
		return false
	}
	return reflect.DeepEqual(*observed, *req)
}

// wifiNetworkReferenceMatches compares the network reference. An unset desired
// reference is ignored.
func wifiNetworkReferenceMatches(observed, req *unifi.WifiNetworkReference) bool {
	if req == nil {
		return true
	}
	return observed != nil && observed.Type == req.Type && observed.NetworkID == req.NetworkID
}

// wifiDeviceFilterMatches compares the broadcasting device filter, ignoring
// collection order. A desired-absent filter means "broadcast on all AP-capable
// devices" (design D5), so it matches only an observed-absent filter: a non-nil
// observed filter is drift and makes the reconciler issue a clearing PUT when
// spec.deviceTags is removed. A desired-present filter must match the observed
// type and tag set.
func wifiDeviceFilterMatches(observed, req *unifi.WifiBroadcastingDeviceFilter) bool {
	if req == nil {
		return observed == nil
	}
	if observed == nil || observed.Type != req.Type {
		return false
	}
	return stringSlicesMatch(observed.DeviceTagIDs, req.DeviceTagIDs) &&
		optionalStringSlicesMatch(req.DeviceIDs, observed.DeviceIDs)
}

// wifiClientFilteringMatches compares the client filtering policy, ignoring MAC
// order. An unset desired policy is ignored.
func wifiClientFilteringMatches(observed, req *unifi.WifiClientFilteringPolicy) bool {
	if req == nil {
		return true
	}
	return observed != nil && observed.Action == req.Action &&
		optionalStringSlicesMatch(req.MacAddressFilter, observed.MacAddressFilter)
}

// wifiMulticastMatches compares the multicast filtering policy, ignoring MAC
// order. An unset desired policy is ignored.
func wifiMulticastMatches(observed, req *unifi.WifiMulticastFilteringPolicy) bool {
	if req == nil {
		return true
	}
	return observed != nil && observed.Action == req.Action &&
		optionalStringSlicesMatch(req.SourceMacAddressFilter, observed.SourceMacAddressFilter)
}

// wifiDNSMatches compares the DNS assistance configuration, ignoring server
// order. An unset desired configuration is ignored.
func wifiDNSMatches(observed, req *unifi.WifiDNSAssistanceConfiguration) bool {
	if req == nil {
		return true
	}
	return observed != nil && observed.Mode == req.Mode &&
		optionalStringSlicesMatch(req.Servers, observed.Servers)
}

// wifiMDNSMatches compares the mDNS proxy configuration, ignoring bridging
// network order. An unset desired configuration is ignored.
func wifiMDNSMatches(observed, req *unifi.WifiMDNSProxyConfiguration) bool {
	if req == nil {
		return true
	}
	if observed == nil || observed.Mode != req.Mode {
		return false
	}
	if len(req.Policies) == 0 {
		return true
	}
	if len(observed.Policies) != len(req.Policies) {
		return false
	}
	for i := range req.Policies {
		if !wifiMDNSPolicyMatches(observed.Policies[i], req.Policies[i]) {
			return false
		}
	}
	return true
}

// wifiMDNSPolicyMatches compares one mDNS proxy policy.
func wifiMDNSPolicyMatches(observed, req unifi.WifiMDNSProxyPolicy) bool {
	if observed.Action != req.Action {
		return false
	}
	if !wifiDeviceFilterMatches(observed.DeviceFilter, req.DeviceFilter) {
		return false
	}
	if !optionalStringSlicesMatch(req.BridgingNetworkIDs, observed.BridgingNetworkIDs) {
		return false
	}
	return reflect.DeepEqual(observed.ServiceFilter, req.ServiceFilter)
}

// wifiSecurityMatches compares the security configuration. Only the selected
// variant's fields are present, and an unset desired optional field is ignored.
func wifiSecurityMatches(observed, req *unifi.WifiSecurityConfiguration) bool {
	if req == nil {
		return true
	}
	if observed == nil || observed.Type != req.Type {
		return false
	}
	if !optionalStringMatches(observed.Encryption, req.Encryption) ||
		!optionalPassphraseMatches(observed.Passphrase, req.Passphrase) ||
		!optionalStringMatches(observed.PmfMode, req.PmfMode) ||
		!optionalStringMatches(observed.SecurityMode, req.SecurityMode) {
		return false
	}
	if !optionalBoolMatches(observed.FastRoamingEnabled, req.FastRoamingEnabled) ||
		!optionalBoolMatches(observed.CoaEnabled, req.CoaEnabled) ||
		!optionalBoolMatches(observed.Wpa3FastRoamingEnabled, req.Wpa3FastRoamingEnabled) {
		return false
	}
	if !optionalInt32Matches(observed.GroupRekeyIntervalSeconds, req.GroupRekeyIntervalSeconds) {
		return false
	}
	if !wifiOptionalDeepEqual(observed.SaeConfiguration, req.SaeConfiguration) ||
		!wifiOptionalDeepEqual(observed.RadiusConfiguration, req.RadiusConfiguration) {
		return false
	}
	return wifiPresharedKeysMatch(observed.PresharedKeys, req.PresharedKeys)
}

// wifiPresharedKeysMatch compares per-network preshared keys. An unset desired
// list is ignored; otherwise the lists must match element-wise.
func wifiPresharedKeysMatch(observed, req []unifi.WifiPresharedKey) bool {
	if len(req) == 0 {
		return true
	}
	if len(observed) != len(req) {
		return false
	}
	for i := range req {
		if !optionalPassphraseMatches(observed[i].Passphrase, req[i].Passphrase) ||
			observed[i].Network.Type != req[i].Network.Type ||
			observed[i].Network.NetworkID != req[i].Network.NetworkID {
			return false
		}
	}
	return true
}

// optionalStringMatches compares an optional string. An unset desired value
// (empty) is ignored.
func optionalStringMatches(observed, req string) bool {
	return req == "" || observed == req
}

// optionalPassphraseMatches compares a passphrase. An unset desired value
// (empty) is ignored, like every other optional field in broadcastMatchesRequest:
// a CR that omits the passphrase must not loop PUTting against an upstream value
// it does not manage. An empty observed value is also treated as matching,
// because the console does not return a recoverable passphrase: otherwise a
// personal broadcast would be re-PUT on every reconcile (design D7 idempotency).
// A non-empty observed value that differs from a non-empty desired value is real
// drift (rotation) and does not match.
func optionalPassphraseMatches(observed, req string) bool {
	return req == "" || observed == "" || observed == req
}

// optionalInt32Matches compares an optional int32. An unset desired value is
// ignored.
func optionalInt32Matches(observed, req *int32) bool {
	if req == nil {
		return true
	}
	return observed != nil && *observed == *req
}

// floatSlicesMatch reports whether two float slices hold the same values,
// treating nil and empty as equal and ignoring order.
func floatSlicesMatch(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	as := append([]float64(nil), a...)
	bs := append([]float64(nil), b...)
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes and the first deletion. The
// controller watches its UnifiNetwork, UnifiSite, and UnifiController
// dependencies so a status-only change there (a late-provisioned network, a
// late-adopted site, or a detected application version) retriggers
// reconciliation.
func (r *UnifiWifiBroadcastReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiWifiBroadcast{}, builder.WithPredicates(wifiBroadcastPredicate())).
		Watches(&unifiv1alpha1.UnifiNetwork{},
			handler.EnqueueRequestsFromMapFunc(r.broadcastsForNetwork),
			builder.WithPredicates(networkDependencyPredicate())).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.broadcastsInNamespace)).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.broadcastsInNamespace)).
		Named("unifiwifibroadcast").
		Complete(r)
}

// broadcastsForNetwork enqueues the UnifiWifiBroadcasts that reference the
// changed UnifiNetwork via spec.networkRef. Only they need to re-reconcile when
// the network reports its upstream network ID or is deleted.
func (r *UnifiWifiBroadcastReconciler) broadcastsForNetwork(ctx context.Context, obj client.Object) []reconcile.Request {
	network, ok := obj.(*unifiv1alpha1.UnifiNetwork)
	if !ok {
		return nil
	}
	var broadcasts unifiv1alpha1.UnifiWifiBroadcastList
	if err := r.List(ctx, &broadcasts, client.InNamespace(network.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list wifi broadcasts for network watch", "namespace", network.Namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(broadcasts.Items))
	for i := range broadcasts.Items {
		item := &broadcasts.Items[i]
		if item.Spec.NetworkRef.Name != network.Name {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// broadcastsInNamespace enqueues every UnifiWifiBroadcast in the changed
// object's namespace. A dependency (UnifiSite or UnifiController) may be
// referenced transitively by any broadcast, so its status-only change
// retriggers all of them.
func (r *UnifiWifiBroadcastReconciler) broadcastsInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	var broadcasts unifiv1alpha1.UnifiWifiBroadcastList
	if err := r.List(ctx, &broadcasts, client.InNamespace(obj.GetNamespace())); err != nil {
		logf.FromContext(ctx).Error(err, "list wifi broadcasts for dependency watch", "namespace", obj.GetNamespace())
		return nil
	}
	requests := make([]reconcile.Request, 0, len(broadcasts.Items))
	for i := range broadcasts.Items {
		item := &broadcasts.Items[i]
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// wifiBroadcastPredicate triggers reconciles on generation changes and when a
// deletionTimestamp is first set. GenerationChangedPredicate drops metadata-only
// updates, but the finalizer cleanup runs on exactly such an update, so deletion
// detection is added alongside it.
func wifiBroadcastPredicate() predicate.Predicate {
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
