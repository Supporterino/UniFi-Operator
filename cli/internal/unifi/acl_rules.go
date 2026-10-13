package unifi

import (
	"context"
	"fmt"
)

// ACL rule discriminators and constants used to interpret upstream bodies.
const (
	// AclRuleOriginUserDefined is the upstream metadata.origin of a user-defined
	// rule; other origins are adopted read-only.
	AclRuleOriginUserDefined = "USER_DEFINED"

	// AclDeviceFilterDevices is the only upstream enforcing-device filter variant.
	AclDeviceFilterDevices = "DEVICES"

	// AclEndpointIPAddressesOrSubnets, AclEndpointNetworks, AclEndpointPorts, and
	// AclEndpointMacAddresses are the source/destination filter discriminators.
	AclEndpointIPAddressesOrSubnets = "IP_ADDRESSES_OR_SUBNETS"
	AclEndpointNetworks             = "NETWORKS"
	AclEndpointPorts                = "PORTS"
	AclEndpointMacAddresses         = "MAC_ADDRESSES"
)

// AclRuleEndpoint is a source or destination traffic filter as returned by the
// controller. The upstream union is flattened on the wire: only the fields for
// Type are set.
type AclRuleEndpoint struct {
	// Type is the endpoint discriminator.
	Type string `json:"type"`
	// IPAddressesOrSubnets is set for type IP_ADDRESSES_OR_SUBNETS.
	IPAddressesOrSubnets []string `json:"ipAddressesOrSubnets,omitempty"`
	// NetworkIDs is set for type NETWORKS.
	NetworkIDs []string `json:"networkIds,omitempty"`
	// PortFilter is set for type PORTS and optionally for the address/network
	// variants.
	PortFilter []int32 `json:"portFilter,omitempty"`
	// MacAddresses is set for type MAC_ADDRESSES.
	MacAddresses []string `json:"macAddresses,omitempty"`
	// PrefixLength is set for type MAC_ADDRESSES.
	PrefixLength *int32 `json:"prefixLength,omitempty"`
}

// AclRuleDeviceFilter is the upstream enforcing-device filter. The DEVICES
// variant carries the upstream device UUIDs.
type AclRuleDeviceFilter struct {
	// Type is the filter discriminator (DEVICES).
	Type string `json:"type"`
	// DeviceIDs are the upstream device UUIDs.
	DeviceIDs []string `json:"deviceIds"`
}

// AclRule is an ACL rule as returned by the controller. ID, Index, and Metadata
// are observed and must never reach a CR's spec.
type AclRule struct {
	// ID is the rule UUID. It is observed upstream.
	ID string `json:"id"`
	// Index is the upstream rule order index.
	Index int32 `json:"index"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

	// Type is the rule discriminator (IPV4 or MAC).
	Type string `json:"type"`
	// Name is the rule name.
	Name string `json:"name"`
	// Enabled controls whether the rule is active.
	Enabled bool `json:"enabled"`
	// Description is an optional description.
	Description string `json:"description,omitempty"`
	// Action is the ACL action (ALLOW or BLOCK).
	Action string `json:"action"`
	// Source is the originating filter.
	Source AclRuleEndpoint `json:"sourceFilter"`
	// Destination is the target filter.
	Destination AclRuleEndpoint `json:"destinationFilter"`
	// ProtocolFilter restricts an IPV4 rule to specific protocols.
	ProtocolFilter []string `json:"protocolFilter,omitempty"`
	// NetworkIDFilter is the upstream network UUID a MAC rule applies to.
	NetworkIDFilter string `json:"networkIdFilter,omitempty"`
	// EnforcingDeviceFilter scopes enforcement to specific devices.
	EnforcingDeviceFilter *AclRuleDeviceFilter `json:"enforcingDeviceFilter,omitempty"`
}

// ListAclRules returns every ACL rule on the given site. The list response
// carries the full rule shape (type, action, source, and destination), so no
// detail call is needed to project a rule into a Custom Resource.
func (c *Client) ListAclRules(ctx context.Context, siteID string) ([]AclRule, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules")
	if err != nil {
		return nil, err
	}
	rules, err := listAll[AclRule](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list acl rules: %w", err)
	}
	return rules, nil
}
