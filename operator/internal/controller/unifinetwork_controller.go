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
	"time"

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
	// networkDependencyRequeueAfter bounds how long the network waits before
	// retrying when its site or the resolved controller is not yet ready. Those
	// changes do not alter this object's generation, so the periodic requeue is
	// what lets a late-appearing dependency self-heal.
	networkDependencyRequeueAfter = 30 * time.Second

	// Network management discriminator values, matching the UnifiNetwork CRD
	// enum and the Integration v1 API.
	networkManagementGateway   = "GATEWAY"
	networkManagementSwitch    = "SWITCH"
	networkManagementUnmanaged = "UNMANAGED"

	// ipv6InterfacePrefixDelegation selects the IPv6 variant whose WAN interface
	// is referenced by a WANSelector the client cannot yet resolve.
	ipv6InterfacePrefixDelegation = "PREFIX_DELEGATION"

	// networkOriginUserDefined is the upstream metadata.origin of a network the
	// operator may manage. Every other origin (SYSTEM_DEFINED, DERIVED,
	// ORCHESTRATED) is system-managed and fails closed (design D11).
	networkOriginUserDefined = "USER_DEFINED"
)

// UnifiNetworkReconciler reconciles a UnifiNetwork object. It resolves the
// network's UnifiSite, connects through the site's controller, and creates,
// updates, or deletes the matching upstream network. The upstream identifier and
// the observed firewall zone are recorded in status only; spec never carries a
// UniFi internal identifier.
type UnifiNetworkReconciler struct {
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

// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unifinetworks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unifi.supporterino.de,resources=unififirewallzones,verbs=get;list;watch

// Reconcile resolves the network's site and controller, applies the
// declarative network to the console, and reports the result in status. It is
// idempotent: re-running it on an unchanged object performs no writes. It fails
// closed (Ready=False, no upstream mutation) on an unresolved site, a version
// below the networks minimum, a SWITCH-managed network below the Official API
// minimum, or a device-tag selector that names a missing tag (DeviceTagNotFound)
// or does not resolve to exactly one device (DeviceTagAmbiguous), and adds/removes
// its finalizer symmetrically around the upstream network's lifecycle.
func (r *UnifiNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	network := &unifiv1alpha1.UnifiNetwork{}
	if err := r.Get(ctx, req.NamespacedName, network); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !network.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, network)
	}

	site, stop, err := r.reconcileSite(ctx, network)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stop != nil {
		return *stop, nil
	}

	if err := r.ensureOwnership(ctx, network, site); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileUpstream(ctx, network, site)
}

// reconcileSite resolves spec.siteRef to a same-namespace UnifiSite that has
// adopted an upstream site. An absent, deleting, or not-yet-adopted site fails
// closed: a non-nil result means the reconcile has finished and the caller must
// stop, while a nil result means the returned site is usable.
func (r *UnifiNetworkReconciler) reconcileSite(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
) (*unifiv1alpha1.UnifiSite, *ctrl.Result, error) {
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: network.Namespace, Name: network.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		if apierrors.IsNotFound(err) {
			result, err := r.requeueStatus(ctx, network, reasonSiteRefNotFound,
				fmt.Sprintf("UnifiSite %q not found", network.Spec.SiteRef.Name))
			return nil, &result, err
		}
		return nil, nil, fmt.Errorf("get UnifiSite %q: %w", network.Spec.SiteRef.Name, err)
	}
	if !site.DeletionTimestamp.IsZero() {
		result, err := r.requeueStatus(ctx, network, reasonSiteRefNotFound,
			fmt.Sprintf("UnifiSite %q is being deleted", site.Name))
		return nil, &result, err
	}
	if site.Status.SiteID == "" {
		result, err := r.requeueStatus(ctx, network, reasonSiteNotAdopted,
			fmt.Sprintf("UnifiSite %q has not adopted an upstream site yet (status.siteID is empty)", site.Name))
		return nil, &result, err
	}
	return site, nil, nil
}

// ensureOwnership sets the controller ownerReference to the immediate parent
// UnifiSite and adds the network finalizer, in a single update when either is
// missing. The site's drain finalizer relies on the ownerReference.
func (r *UnifiNetworkReconciler) ensureOwnership(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
	site *unifiv1alpha1.UnifiSite,
) error {
	changed := false
	if !metav1.IsControlledBy(network, site) {
		if err := controllerutil.SetControllerReference(site, network, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference to UnifiSite %q: %w", site.Name, err)
		}
		changed = true
	}
	if !controllerutil.ContainsFinalizer(network, unifiv1alpha1.UnifiNetworkFinalizer) {
		controllerutil.AddFinalizer(network, unifiv1alpha1.UnifiNetworkFinalizer)
		changed = true
	}
	if !changed {
		return nil
	}
	if err := r.Update(ctx, network); err != nil {
		return fmt.Errorf("update metadata: %w", err)
	}
	return nil
}

// reconcileUpstream resolves the console through the site's controllerRef, gates
// on the networks capability, resolves a SWITCH-managed network's device-tag
// selector, and creates or updates the upstream network.
func (r *UnifiNetworkReconciler) reconcileUpstream(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
	site *unifiv1alpha1.UnifiSite,
) (ctrl.Result, error) {
	resolver := NewConnectionResolver(r.Client, r.APIReader, r.NewClient)
	controller, err := resolver.ResolveController(ctx, network.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, network, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller: %w", err)
	}
	if controller.Status.ApplicationVersion == "" {
		return r.requeueStatus(ctx, network, reasonDependencyNotReady,
			fmt.Sprintf("UnifiController %q has not reported its application version yet", controller.Name))
	}
	if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityNetworks); err != nil {
		return r.updateStatus(ctx, network, false, reasonVersionUnsupported, err.Error(), nil, nil)
	}

	switch network.Spec.Management {
	case networkManagementSwitch:
		// The device-tags list is part of the Official API surface, whose
		// minimum sits above the networks minimum. Fail closed on a console
		// between the two rather than attempting the tag list (design D5/D7).
		if err := CheckCapability(controller.Status.ApplicationVersion, CapabilityOfficialAPI); err != nil {
			return r.updateStatus(ctx, network, false, reasonVersionUnsupported, err.Error(), nil, nil)
		}
	case networkManagementGateway, networkManagementUnmanaged:
		// Handled below.
	default:
		return r.updateStatus(ctx, network, false, reasonInvalidSpec,
			fmt.Sprintf("unsupported management %q", network.Spec.Management), nil, nil)
	}

	if isPrefixDelegation(network) {
		return r.updateStatus(ctx, network, false, reasonWANLookupUnsupported,
			"IPv6 prefix delegation requires a WAN interface lookup that is not yet implemented; no upstream network was modified", nil, nil)
	}

	apiClient, err := resolver.Resolve(ctx, network.Namespace, site.Spec.ControllerRef)
	if err != nil {
		var resolveErr *ResolveError
		if errors.As(err, &resolveErr) {
			return r.requeueStatus(ctx, network, resolveErr.Reason, resolveErr.Message)
		}
		return ctrl.Result{}, fmt.Errorf("resolve controller client: %w", err)
	}

	var deviceID string
	if network.Spec.Management == networkManagementSwitch {
		if network.Spec.Switch == nil {
			return r.updateStatus(ctx, network, false, reasonInvalidSpec,
				"management SWITCH requires the switch fields", nil, nil)
		}
		tags, err := apiClient.ListDeviceTags(ctx, site.Status.SiteID)
		if err != nil {
			return r.handleUpstreamError(ctx, network, err)
		}
		reason, message := "", ""
		deviceID, reason, message = resolveSwitchDevice(network, tags)
		if reason != "" {
			return r.updateStatus(ctx, network, false, reason, message, nil, nil)
		}
	}

	networks, err := apiClient.ListNetworks(ctx, site.Status.SiteID)
	if err != nil {
		return r.handleUpstreamError(ctx, network, err)
	}

	request := buildNetworkRequest(network, deviceID)
	existing, err := resolveExistingNetwork(ctx, apiClient, site.Status.SiteID, network, networks)
	if err != nil {
		return r.handleUpstreamError(ctx, network, err)
	}

	// Fail closed on a system/derived/orchestrated object: the operator only
	// owns USER_DEFINED networks (design D11).
	if existing != nil && existing.Metadata.Origin != networkOriginUserDefined {
		return r.updateStatus(ctx, network, false, reasonSystemObjectReadOnly,
			systemObjectMessage(*existing), nil, nil)
	}

	var (
		result unifi.Network
		action string
	)
	switch {
	case existing == nil:
		result, err = apiClient.CreateNetwork(ctx, site.Status.SiteID, request)
		action = "created"
	case networkMatchesRequest(*existing, request):
		// The upstream object already matches the desired state; skip the PUT so
		// re-reconciling an unchanged object performs no write.
		networkID := firstNonEmpty(existing.ID, network.Status.NetworkID)
		zoneID := firstNonEmpty(existing.ZoneID, network.Status.ZoneID)
		summary := fmt.Sprintf("upstream network %q is up to date", network.Spec.Name)
		return r.updateStatus(ctx, network, true, reasonReconciled, summary, &networkID, &zoneID)
	default:
		result, err = apiClient.UpdateNetwork(ctx, site.Status.SiteID, existing.ID, request)
		action = "updated"
	}
	if err != nil {
		return r.handleUpstreamError(ctx, network, err)
	}

	// Keep the last-known identifiers when a response omits them, so a
	// transiently incomplete body cannot erase a correlation ID.
	networkID := firstNonEmpty(result.ID, network.Status.NetworkID)
	zoneID := firstNonEmpty(result.ZoneID, network.Status.ZoneID)
	if existing != nil {
		networkID = firstNonEmpty(networkID, existing.ID)
		zoneID = firstNonEmpty(zoneID, existing.ZoneID)
	}

	summary := fmt.Sprintf("%s upstream network %q", action, network.Spec.Name)
	return r.updateStatus(ctx, network, true, reasonReconciled, summary, &networkID, &zoneID)
}

// reconcileDelete deletes the upstream network recorded in status and removes
// the finalizer. The site still resolves because the site's own finalizer drains
// its children before the site (and its controller chain) disappears. When
// status.networkID is empty there is no upstream network to delete.
func (r *UnifiNetworkReconciler) reconcileDelete(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(network, unifiv1alpha1.UnifiNetworkFinalizer) {
		return ctrl.Result{}, nil
	}
	if err := r.deleteUpstream(ctx, network); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(network, unifiv1alpha1.UnifiNetworkFinalizer)
	if err := r.Update(ctx, network); err != nil {
		return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteUpstream removes the network recorded in status from the console. A 404
// means it is already gone and is treated as success.
func (r *UnifiNetworkReconciler) deleteUpstream(ctx context.Context, network *unifiv1alpha1.UnifiNetwork) error {
	if network.Status.NetworkID == "" {
		return nil
	}
	site := &unifiv1alpha1.UnifiSite{}
	key := types.NamespacedName{Namespace: network.Namespace, Name: network.Spec.SiteRef.Name}
	if err := r.Get(ctx, key, site); err != nil {
		return fmt.Errorf("get UnifiSite %q for cleanup: %w", network.Spec.SiteRef.Name, err)
	}
	if site.Status.SiteID == "" {
		// No upstream site was ever adopted, so there is no upstream network.
		return nil
	}
	apiClient, err := NewConnectionResolver(r.Client, r.APIReader, r.NewClient).Resolve(ctx, network.Namespace, site.Spec.ControllerRef)
	if err != nil {
		return fmt.Errorf("resolve controller for cleanup: %w", err)
	}

	// Never delete a system-managed object: the operator only owns USER_DEFINED
	// networks (design D11). A 404 means the object is already gone.
	existing, err := apiClient.GetNetwork(ctx, site.Status.SiteID, network.Status.NetworkID)
	if err != nil {
		if errors.Is(err, unifi.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("get upstream network %q for cleanup: %w", network.Status.NetworkID, err)
	}
	if existing.Metadata.Origin != networkOriginUserDefined {
		return nil
	}

	if err := apiClient.DeleteNetwork(ctx, site.Status.SiteID, network.Status.NetworkID); err != nil && !errors.Is(err, unifi.ErrNotFound) {
		return fmt.Errorf("delete upstream network %q: %w", network.Status.NetworkID, err)
	}
	return nil
}

// handleUpstreamError classifies a failed network write. Certificate, auth, and
// bad-input failures are terminal and fail closed; a transient failure is
// returned so controller-runtime backs off.
func (r *UnifiNetworkReconciler) handleUpstreamError(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
	err error,
) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if isCertificateError(err) {
		return r.updateStatus(ctx, network, false, reasonCertificateError,
			"TLS certificate verification failed; set spec.insecureSkipVerify only for a trusted self-signed console", nil, nil)
	}
	var apiErr *unifi.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusUnauthorized || apiErr.StatusCode == http.StatusForbidden:
			return r.updateStatus(ctx, network, false, reasonAuthenticationFailed,
				"console rejected the API key; verify the controller Secret", nil, nil)
		case !apiErr.Retryable():
			return r.updateStatus(ctx, network, false, reasonInvalidSpec,
				"console rejected the request: "+apiErr.Error(), nil, nil)
		}
	}
	var specErr *unifi.InvalidSpecError
	if errors.As(err, &specErr) {
		return r.updateStatus(ctx, network, false, reasonInvalidSpec, specErr.Error(), nil, nil)
	}
	log.Error(err, "transient error reconciling network")
	return ctrl.Result{}, fmt.Errorf("reconcile network: %w", err)
}

// requeueStatus writes status and re-checks after a bounded delay for a
// dependency failure a later edit can fix without changing this object's
// generation.
func (r *UnifiNetworkReconciler) requeueStatus(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
	reason, message string,
) (ctrl.Result, error) {
	result, err := r.updateStatus(ctx, network, false, reason, message, nil, nil)
	if err != nil {
		return result, err
	}
	result.RequeueAfter = networkDependencyRequeueAfter
	return result, nil
}

// updateStatus writes status only when something changed, so an unchanged
// reconcile is a no-op. A nil networkID or zoneID leaves the recorded value
// untouched, preserving the last-known identifiers on failing paths.
func (r *UnifiNetworkReconciler) updateStatus(
	ctx context.Context,
	network *unifiv1alpha1.UnifiNetwork,
	ready bool,
	reason, message string,
	networkID, zoneID *string,
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
	if networkID != nil && network.Status.NetworkID != *networkID {
		network.Status.NetworkID = *networkID
		changed = true
	}
	if zoneID != nil && network.Status.ZoneID != *zoneID {
		network.Status.ZoneID = *zoneID
		changed = true
	}
	if setReadyCondition(&network.Status.Conditions, network.Generation, ready, reason, message) {
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

// buildNetworkRequest maps the declarative spec onto the upstream create/update
// body. deviceID is the UUID resolved from a SWITCH-managed network's device-tag
// selector; it is ignored for other management variants. The request
// deliberately has no zoneId: UnifiFirewallZone owns network membership and the
// controller omits zoneId on writes (design D5).
func buildNetworkRequest(network *unifiv1alpha1.UnifiNetwork, deviceID string) unifi.NetworkRequest {
	spec := network.Spec
	enabled := true
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	request := unifi.NetworkRequest{
		Management:   spec.Management,
		Name:         spec.Name,
		Enabled:      enabled,
		VLANID:       spec.VLANID,
		DHCPGuarding: convertDHCPGuarding(spec.DHCPGuarding),
	}
	if spec.Gateway != nil {
		request.Gateway = &unifi.GatewayNetworkRequest{
			CellularBackupEnabled: spec.Gateway.CellularBackupEnabled,
			InternetAccessEnabled: spec.Gateway.InternetAccessEnabled,
			IsolationEnabled:      spec.Gateway.IsolationEnabled,
			IPv4Configuration:     convertIPv4(spec.Gateway.IPv4Configuration),
			IPv6Configuration:     convertIPv6(spec.Gateway.IPv6Configuration),
			MDNSForwardingEnabled: spec.Gateway.MDNSForwardingEnabled,
		}
	}
	if spec.Switch != nil {
		request.Switch = &unifi.SwitchNetworkRequest{
			CellularBackupEnabled: spec.Switch.CellularBackupEnabled,
			IsolationEnabled:      spec.Switch.IsolationEnabled,
			IPv4Configuration:     convertSwitchIPv4(spec.Switch.IPv4Configuration),
			DeviceID:              deviceID,
		}
	}
	return request
}

// isPrefixDelegation reports whether a gateway network selects the IPv6
// prefix-delegation variant, whose WAN interface reference cannot be resolved
// until a WAN lookup lands in the client.
func isPrefixDelegation(network *unifiv1alpha1.UnifiNetwork) bool {
	return network.Spec.Gateway != nil &&
		network.Spec.Gateway.IPv6Configuration != nil &&
		network.Spec.Gateway.IPv6Configuration.InterfaceType == ipv6InterfacePrefixDelegation
}

// resolveSwitchDevice resolves a SWITCH-managed network's device-tag selector
// against the read-only device-tags list. It returns the single member device
// UUID, or a non-empty reason and message when the named tag is absent or does
// not resolve to exactly one device. The resolved device's hardware type is not
// verified (design Non-Goals): the binding accepts any single-device tag and
// lets the console reject an unusable one.
func resolveSwitchDevice(network *unifiv1alpha1.UnifiNetwork, tags []unifi.DeviceTag) (deviceID, reason, message string) {
	tagName := network.Spec.Switch.DeviceTag.Name
	tag := findDeviceTagByName(tags, tagName)
	if tag == nil {
		return "", reasonDeviceTagNotFound, fmt.Sprintf(
			"device tag %q does not exist on the site; no upstream network was modified", tagName)
	}
	if len(tag.DeviceIDs) != 1 {
		return "", reasonDeviceTagAmbiguous, fmt.Sprintf(
			"device tag %q resolves to %d devices; exactly one is required; no upstream network was modified",
			tagName, len(tag.DeviceIDs))
	}
	return tag.DeviceIDs[0], "", ""
}

// convertSwitchIPv4 maps the switch IPv4 configuration.
func convertSwitchIPv4(in unifiv1alpha1.SwitchManagedIPv4Configuration) unifi.IPv4Configuration {
	return unifi.IPv4Configuration{
		AutoScaleEnabled:        in.AutoScaleEnabled,
		HostIPAddress:           in.HostIPAddress,
		PrefixLength:            in.PrefixLength,
		AdditionalHostIPSubnets: in.AdditionalHostIPSubnets,
	}
}

// convertDHCPGuarding maps the CR DHCP guarding configuration.
func convertDHCPGuarding(in *unifiv1alpha1.DHCPGuarding) *unifi.DHCPGuarding {
	if in == nil {
		return nil
	}
	return &unifi.DHCPGuarding{TrustedDHCPServerIPAddresses: in.TrustedDHCPServerIPAddresses}
}

// convertIPv4 maps the gateway IPv4 configuration.
func convertIPv4(in unifiv1alpha1.GatewayManagedIPv4Configuration) unifi.IPv4Configuration {
	return unifi.IPv4Configuration{
		AutoScaleEnabled:        in.AutoScaleEnabled,
		HostIPAddress:           in.HostIPAddress,
		PrefixLength:            in.PrefixLength,
		AdditionalHostIPSubnets: in.AdditionalHostIPSubnets,
	}
}

// convertIPv6 maps the IPv6 configuration. Prefix delegation is rejected before
// this is called; the client has no WAN interface lookup.
func convertIPv6(in *unifiv1alpha1.IPv6Configuration) *unifi.IPv6Configuration {
	if in == nil {
		return nil
	}
	out := &unifi.IPv6Configuration{
		InterfaceType:                in.InterfaceType,
		ClientAddressAssignment:      unifi.IPv6ClientAddressAssignment{SLAACEnabled: in.ClientAddressAssignment.SLAACEnabled},
		AdditionalHostIPSubnets:      in.AdditionalHostIPSubnets,
		DNSServerIPAddressesOverride: in.DNSServerIPAddressesOverride,
		HostIPAddress:                in.HostIPAddress,
		PrefixLength:                 in.PrefixLength,
	}
	if in.RouterAdvertisement != nil {
		out.RouterAdvertisement = &unifi.RouterAdvertisement{Priority: in.RouterAdvertisement.Priority}
	}
	return out
}

// findNetworkByName returns the upstream network matching name, or nil.
func findNetworkByName(networks []unifi.Network, name string) *unifi.Network {
	for i := range networks {
		if networks[i].Name == name {
			return &networks[i]
		}
	}
	return nil
}

// findDeviceTagByName returns the device tag matching name, or nil.
func findDeviceTagByName(tags []unifi.DeviceTag, name string) *unifi.DeviceTag {
	for i := range tags {
		if tags[i].Name == name {
			return &tags[i]
		}
	}
	return nil
}

// resolveExistingNetwork returns the upstream network this CR manages, or nil
// when none exists. It resolves status.networkID first so a spec.name rename
// updates the same upstream object instead of orphaning it, then falls back to
// a name match for adoption. A 404 while resolving status.networkID is treated
// as absent so a stale ID cannot block reconciliation.
func resolveExistingNetwork(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	network *unifiv1alpha1.UnifiNetwork,
	networks []unifi.Network,
) (*unifi.Network, error) {
	if network.Status.NetworkID != "" {
		existing, err := apiClient.GetNetwork(ctx, siteID, network.Status.NetworkID)
		switch {
		case err == nil:
			return &existing, nil
		case errors.Is(err, unifi.ErrNotFound):
			// Fall through to the name match: the recorded object is gone.
		default:
			return nil, err
		}
	}

	match := findNetworkByName(networks, network.Spec.Name)
	if match == nil {
		return nil, nil
	}
	// Fetch the detail so the comparison sees the variant fields the list
	// overview omits.
	detail, err := apiClient.GetNetwork(ctx, siteID, match.ID)
	switch {
	case err == nil:
		return &detail, nil
	case errors.Is(err, unifi.ErrNotFound):
		return nil, nil
	default:
		return nil, err
	}
}

// systemObjectMessage explains why an upstream network is read-only, without
// echoing credential material.
func systemObjectMessage(existing unifi.Network) string {
	origin := existing.Metadata.Origin
	if origin == "" {
		origin = "unknown"
	}
	return fmt.Sprintf("upstream network %q has metadata.origin %s, not %s; refusing to overwrite a system-managed object",
		existing.Name, origin, networkOriginUserDefined)
}

// networkMatchesRequest reports whether the observed upstream network already
// matches the desired write, so the reconciler can skip the PUT and stay
// idempotent. Only authored fields are compared; observed fields (id, default,
// metadata, zoneId) are ignored. deviceId is authored for a SWITCH-managed
// network (resolved from the device-tag selector) and is compared. Nil and
// empty collections are normalized, and slice order is insignificant.
func networkMatchesRequest(existing unifi.Network, req unifi.NetworkRequest) bool {
	if existing.Management != req.Management ||
		existing.Name != req.Name ||
		existing.Enabled != req.Enabled ||
		existing.VLANID != req.VLANID {
		return false
	}
	if !dhcpGuardingMatches(existing.DHCPGuarding, req.DHCPGuarding) {
		return false
	}
	switch req.Management {
	case networkManagementGateway:
		if req.Gateway == nil {
			return false
		}
		return gatewayMatches(existing, *req.Gateway)
	case networkManagementSwitch:
		if req.Switch == nil {
			return false
		}
		return switchMatches(existing, *req.Switch)
	case networkManagementUnmanaged:
		// The CR authors only the common fields (already compared above); any
		// observed variant field is not desired state and is ignored.
		return true
	default:
		return false
	}
}

// dhcpGuardingMatches compares DHCP guarding against the CR contract "omit to
// disable the feature". A desired configuration that is unset (nil) or carries
// no trusted addresses means DHCP guarding is disabled, so it matches only an
// observed configuration that is also absent or empty. A non-empty observed set
// is drift: the reconciler issues a PUT that omits the field, disabling the
// feature upstream. A desired non-empty set matches only the same addresses,
// ignoring order.
func dhcpGuardingMatches(existing *unifi.DHCPGuarding, req *unifi.DHCPGuarding) bool {
	desired := trustedDHCPAddresses(req)
	if len(desired) == 0 {
		return len(trustedDHCPAddresses(existing)) == 0
	}
	return stringSlicesMatch(trustedDHCPAddresses(existing), desired)
}

// trustedDHCPAddresses returns the trusted DHCP server addresses of a guarding
// configuration, treating nil as empty.
func trustedDHCPAddresses(guarding *unifi.DHCPGuarding) []string {
	if guarding == nil {
		return nil
	}
	return guarding.TrustedDHCPServerIPAddresses
}

// gatewayMatches compares the GATEWAY variant, normalizing an absent observed
// boolean to false.
func gatewayMatches(existing unifi.Network, req unifi.GatewayNetworkRequest) bool {
	if boolValue(existing.CellularBackupEnabled) != req.CellularBackupEnabled ||
		boolValue(existing.InternetAccessEnabled) != req.InternetAccessEnabled ||
		boolValue(existing.IsolationEnabled) != req.IsolationEnabled {
		return false
	}
	if !optionalBoolMatches(existing.MDNSForwardingEnabled, req.MDNSForwardingEnabled) {
		return false
	}
	if existing.IPv4Configuration == nil || !ipv4Matches(*existing.IPv4Configuration, req.IPv4Configuration) {
		return false
	}
	return ipv6Matches(existing.IPv6Configuration, req.IPv6Configuration)
}

// switchMatches compares the SWITCH variant, including the resolved device ID
// the reconciler supplies from the device-tag selector.
func switchMatches(existing unifi.Network, req unifi.SwitchNetworkRequest) bool {
	if boolValue(existing.CellularBackupEnabled) != req.CellularBackupEnabled ||
		boolValue(existing.IsolationEnabled) != req.IsolationEnabled ||
		existing.DeviceID != req.DeviceID {
		return false
	}
	return existing.IPv4Configuration != nil && ipv4Matches(*existing.IPv4Configuration, req.IPv4Configuration)
}

// ipv4Matches compares the shared IPv4 configuration. The authored fields are
// compared; an unset additional-subnets collection is optional and ignored.
func ipv4Matches(existing unifi.IPv4Configuration, req unifi.IPv4Configuration) bool {
	return existing.AutoScaleEnabled == req.AutoScaleEnabled &&
		existing.HostIPAddress == req.HostIPAddress &&
		existing.PrefixLength == req.PrefixLength &&
		optionalStringSlicesMatch(req.AdditionalHostIPSubnets, existing.AdditionalHostIPSubnets)
}

// ipv6Matches compares the IPv6 configuration against the desired one. The CR
// contract is "omit to leave IPv6 unconfigured": a desired configuration that is
// unset (nil) matches only an observed network with no IPv6 configuration, so a
// configured observed value is drift and the reconciler issues a PUT that omits
// the field. Authored fields are compared; nested optional collections and
// router advertisement that the CR leaves unset are ignored, and the
// observed-only prefix-delegation WAN interface ID is never compared.
func ipv6Matches(existing, req *unifi.IPv6Configuration) bool {
	if req == nil {
		return existing == nil
	}
	if existing == nil {
		return false
	}
	return existing.InterfaceType == req.InterfaceType &&
		existing.ClientAddressAssignment.SLAACEnabled == req.ClientAddressAssignment.SLAACEnabled &&
		existing.HostIPAddress == req.HostIPAddress &&
		existing.PrefixLength == req.PrefixLength &&
		optionalStringSlicesMatch(req.AdditionalHostIPSubnets, existing.AdditionalHostIPSubnets) &&
		optionalStringSlicesMatch(req.DNSServerIPAddressesOverride, existing.DNSServerIPAddressesOverride) &&
		routerAdvertisementMatches(existing.RouterAdvertisement, req.RouterAdvertisement)
}

// boolValue dereferences an optional bool, treating nil as false.
func boolValue(value *bool) bool {
	return value != nil && *value
}

// optionalBoolMatches compares an observed optional bool with the desired value.
// A desired value that is unset (nil) is optional: the comparison ignores the
// observed value so an omitted optional field is never treated as drift. An
// observed nil is compared as false because the console omits a disabled field.
func optionalBoolMatches(existing, req *bool) bool {
	if req == nil {
		return true
	}
	return boolValue(existing) == *req
}

// routerAdvertisementMatches compares router advertisement. A desired router
// advertisement that is unset (nil) is optional and ignored; otherwise the
// authored priority is compared.
func routerAdvertisementMatches(existing, req *unifi.RouterAdvertisement) bool {
	if req == nil {
		return true
	}
	if existing == nil {
		return false
	}
	return existing.Priority == req.Priority
}

// optionalStringSlicesMatch compares an observed string slice with the desired
// slice. A desired slice that is unset or empty is optional: the comparison
// ignores the observed value so an omitted optional collection is never treated
// as drift. A desired non-empty slice must equal the observed set, ignoring
// order.
func optionalStringSlicesMatch(desired, observed []string) bool {
	if len(desired) == 0 {
		return true
	}
	return stringSlicesMatch(observed, desired)
}

// stringSlicesMatch reports whether two string slices hold the same elements,
// treating nil and empty as equal and ignoring order.
func stringSlicesMatch(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	slices.Sort(as)
	slices.Sort(bs)
	return slices.Equal(as, bs)
}

// firstNonEmpty returns the first non-empty value, or an empty string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// SetupWithManager sets up the controller with the Manager. Status-only writes
// do not change the object's generation, so the predicate drops the resulting
// no-op reconcile while still handling spec changes and the first deletion.
// The controller also watches its UnifiSite and UnifiController dependencies so
// a status-only change there (for example a late-adopted siteID or a detected
// application version) retriggers reconciliation. It watches UnifiFirewallZone
// so a membership write re-reconciles the networks the zone claims, refreshing
// UnifiNetwork.status.zoneID (design D2). Only the networks the changed zone
// lists in spec.networkRefs are enqueued, so the zone's own status writes
// (conflict marking, dependency requeues) do not bounce every network in the
// namespace.
func (r *UnifiNetworkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&unifiv1alpha1.UnifiNetwork{}, builder.WithPredicates(networkPredicate())).
		Watches(&unifiv1alpha1.UnifiController{}, handler.EnqueueRequestsFromMapFunc(r.networksInNamespace)).
		Watches(&unifiv1alpha1.UnifiSite{}, handler.EnqueueRequestsFromMapFunc(r.networksForSite)).
		Watches(&unifiv1alpha1.UnifiFirewallZone{}, handler.EnqueueRequestsFromMapFunc(r.networksForZone)).
		Named("unifinetwork").
		Complete(r)
}

// networksInNamespace enqueues every UnifiNetwork in the changed object's
// namespace. A dependency (UnifiController or UnifiSite) may be referenced by
// any network, so the dependency's change retriggers all of them.
func (r *UnifiNetworkReconciler) networksInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listNetworkRequests(ctx, obj.GetNamespace(), "")
}

// networksForZone enqueues only the UnifiNetworks a changed UnifiFirewallZone
// lists in spec.networkRefs. A zone writes membership for exactly those
// networks, so only they need to re-reconcile to refresh status.zoneID after
// the write; the rest of the namespace is left untouched. This keeps the
// membership-refresh trigger (design D2) without the namespace-wide churn of
// mapping every network on each zone status write.
func (r *UnifiNetworkReconciler) networksForZone(ctx context.Context, obj client.Object) []reconcile.Request {
	zone, ok := obj.(*unifiv1alpha1.UnifiFirewallZone)
	if !ok || len(zone.Spec.NetworkRefs) == 0 {
		return nil
	}
	var networks unifiv1alpha1.UnifiNetworkList
	if err := r.List(ctx, &networks, client.InNamespace(zone.Namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list networks for firewall zone watch", "namespace", zone.Namespace)
		return nil
	}
	claimed := make(map[string]struct{}, len(zone.Spec.NetworkRefs))
	for _, ref := range zone.Spec.NetworkRefs {
		claimed[ref.Name] = struct{}{}
	}
	requests := make([]reconcile.Request, 0, len(zone.Spec.NetworkRefs))
	for i := range networks.Items {
		item := &networks.Items[i]
		if _, ok := claimed[item.Name]; !ok {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// networksForSite enqueues the UnifiNetworks that reference the changed
// UnifiSite via spec.siteRef.
func (r *UnifiNetworkReconciler) networksForSite(ctx context.Context, obj client.Object) []reconcile.Request {
	return r.listNetworkRequests(ctx, obj.GetNamespace(), obj.GetName())
}

// listNetworkRequests lists UnifiNetworks in namespace and optionally filters
// them to those whose spec.siteRef.name matches siteName.
func (r *UnifiNetworkReconciler) listNetworkRequests(ctx context.Context, namespace, siteName string) []reconcile.Request {
	var networks unifiv1alpha1.UnifiNetworkList
	if err := r.List(ctx, &networks, client.InNamespace(namespace)); err != nil {
		logf.FromContext(ctx).Error(err, "list networks for dependency watch", "namespace", namespace)
		return nil
	}
	requests := make([]reconcile.Request, 0, len(networks.Items))
	for i := range networks.Items {
		item := &networks.Items[i]
		if siteName != "" && item.Spec.SiteRef.Name != siteName {
			continue
		}
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: item.Namespace, Name: item.Name},
		})
	}
	return requests
}

// networkPredicate triggers reconciles on generation changes and when a
// deletionTimestamp is first set. GenerationChangedPredicate drops
// metadata-only updates, but the finalizer cleanup runs on exactly such an
// update, so deletion detection is added alongside it.
func networkPredicate() predicate.Predicate {
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
