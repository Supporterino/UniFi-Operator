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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// resolveAclEndpoint maps a declarative ACL endpoint onto the upstream filter,
// resolving every network reference (NETWORKS variant) to its upstream UUID. It
// reuses selectorResolution so the reconciler can fail closed identically to the
// firewall resolvers.
func resolveAclEndpoint(
	ctx context.Context,
	reader client.Reader,
	namespace, siteName string,
	endpoint unifiv1alpha1.AclRuleEndpoint,
) (unifi.AclRuleEndpoint, selectorResolution) {
	out := unifi.AclRuleEndpoint{Type: endpoint.Type}
	switch endpoint.Type {
	case "IP_ADDRESSES_OR_SUBNETS":
		if endpoint.IPAddressesOrSubnets == nil {
			return unifi.AclRuleEndpoint{}, missingAclVariant(endpoint.Type)
		}
		out.IPAddressesOrSubnets = endpoint.IPAddressesOrSubnets.Addresses
		out.PortFilter = aclPorts(endpoint.IPAddressesOrSubnets.Ports)
	case "NETWORKS":
		if endpoint.Networks == nil {
			return unifi.AclRuleEndpoint{}, missingAclVariant(endpoint.Type)
		}
		ids := make([]string, 0, len(endpoint.Networks.NetworkRefs))
		seen := make(map[string]struct{}, len(endpoint.Networks.NetworkRefs))
		for _, ref := range endpoint.Networks.NetworkRefs {
			if _, ok := seen[ref.Name]; ok {
				continue
			}
			seen[ref.Name] = struct{}{}
			res := resolveAclNetworkRef(ctx, reader, namespace, siteName, ref)
			if res.stopped() {
				return unifi.AclRuleEndpoint{}, res
			}
			ids = append(ids, res.value)
		}
		out.NetworkIDs = ids
		out.PortFilter = aclPorts(endpoint.Networks.Ports)
	case "PORTS":
		if endpoint.Ports == nil {
			return unifi.AclRuleEndpoint{}, missingAclVariant(endpoint.Type)
		}
		out.PortFilter = aclPorts(endpoint.Ports.Ports)
	case "MAC_ADDRESSES":
		if endpoint.MacAddresses == nil {
			return unifi.AclRuleEndpoint{}, missingAclVariant(endpoint.Type)
		}
		out.MacAddresses = endpoint.MacAddresses.MacAddresses
		out.PrefixLength = endpoint.MacAddresses.PrefixLength
	default:
		return unifi.AclRuleEndpoint{}, selectorResolution{
			reason:  reasonInvalidSpec,
			message: fmt.Sprintf("%q is not a known ACL endpoint type", endpoint.Type),
		}
	}
	return out, selectorResolution{}
}

// missingAclVariant reports the terminal spec error for an endpoint whose type
// requires a variant sub-object that is not set.
func missingAclVariant(endpointType string) selectorResolution {
	return selectorResolution{
		reason:  reasonInvalidSpec,
		message: fmt.Sprintf("ACL endpoint type %s is missing its variant value", endpointType),
	}
}

// resolveAclNetworkRef resolves a UnifiNetwork reference to its upstream network
// UUID, failing closed when it is absent, deleting, cross-site, or not yet
// provisioned.
func resolveAclNetworkRef(
	ctx context.Context,
	reader client.Reader,
	namespace, siteName string,
	ref unifiv1alpha1.CoreRef,
) selectorResolution {
	network := &unifiv1alpha1.UnifiNetwork{}
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	if err := reader.Get(ctx, key, network); err != nil {
		if apierrors.IsNotFound(err) {
			return selectorResolution{reason: reasonNetworkRefNotFound, message: fmt.Sprintf("UnifiNetwork %q not found", ref.Name), retry: true}
		}
		return selectorResolution{err: fmt.Errorf("get UnifiNetwork %q: %w", ref.Name, err)}
	}
	if !network.DeletionTimestamp.IsZero() {
		return selectorResolution{reason: reasonNetworkRefNotFound, message: fmt.Sprintf("UnifiNetwork %q is being deleted", network.Name), retry: true}
	}
	if network.Spec.SiteRef.Name != siteName {
		return selectorResolution{reason: reasonCrossSiteReference, message: fmt.Sprintf("UnifiNetwork %q belongs to UnifiSite %q, not %q; no upstream object was modified",
			ref.Name, network.Spec.SiteRef.Name, siteName)}
	}
	if network.Status.NetworkID == "" {
		return selectorResolution{reason: reasonDependencyNotReady, message: fmt.Sprintf("UnifiNetwork %q has not reported its upstream network ID yet", ref.Name), retry: true}
	}
	return selectorResolution{value: network.Status.NetworkID}
}

// aclPorts converts the CR's typed ports to the upstream int32 slice, returning
// nil for an empty set so an omitted port filter stays off the request body.
func aclPorts(ports []unifiv1alpha1.AclPort) []int32 {
	if len(ports) == 0 {
		return nil
	}
	out := make([]int32, 0, len(ports))
	for _, port := range ports {
		out = append(out, int32(port))
	}
	return out
}

// resolveEnforcingDeviceIDs resolves the enforcing-device tag selectors against
// the read-only device-tags list, returning the union of member device UUIDs. An
// unknown tag fails closed (DeviceTagNotFound); a selector set that resolves to
// no device at all fails closed (InvalidSpec), because an empty enforcing-device
// filter is not a valid upstream body.
func resolveEnforcingDeviceIDs(
	ctx context.Context,
	apiClient unifi.Client,
	siteID string,
	tags []unifiv1alpha1.DeviceTagSelector,
) (deviceIDs []string, reason, message string, err error) {
	if len(tags) == 0 {
		return nil, "", "", nil
	}
	deviceTags, err := apiClient.ListDeviceTags(ctx, siteID)
	if err != nil {
		return nil, "", "", err
	}
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	for _, selector := range tags {
		tag := findDeviceTagByName(deviceTags, selector.Name)
		if tag == nil {
			return nil, reasonDeviceTagNotFound, fmt.Sprintf(
				"device tag %q does not exist on the site; no upstream ACL rule was modified", selector.Name), nil
		}
		for _, id := range tag.DeviceIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, reasonInvalidSpec, "the enforcing-device tags resolve to no devices; no upstream ACL rule was modified", nil
	}
	return ids, "", "", nil
}
