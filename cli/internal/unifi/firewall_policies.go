package unifi

import (
	"context"
	"fmt"
)

// Firewall policy discriminators and constants used to interpret upstream bodies.
const (
	// FirewallPolicyOriginUserDefined is the upstream metadata.origin of a
	// user-defined policy; other origins are system-managed.
	FirewallPolicyOriginUserDefined = "USER_DEFINED"

	// FirewallPolicyActionAllow is the action type that carries allowReturnTraffic.
	FirewallPolicyActionAllow = "ALLOW"

	// FirewallPolicyTrafficFilterIPAddress and FirewallPolicyTrafficFilterPort are
	// the source/destination traffic-filter discriminators.
	FirewallPolicyTrafficFilterIPAddress = "IP_ADDRESS"
	FirewallPolicyTrafficFilterPort      = "PORT"

	// FirewallFilterTrafficMatchingList is the IP/port filter variant backed by a
	// traffic matching list UUID. Any other variant is not modeled by the CRD.
	FirewallFilterTrafficMatchingList = "TRAFFIC_MATCHING_LIST"
)

// FirewallPolicyAction is the action of a firewall policy. AllowReturnTraffic is
// set only for type ALLOW.
type FirewallPolicyAction struct {
	// Type is the action discriminator (ALLOW, BLOCK, or REJECT).
	Type string `json:"type"`
	// AllowReturnTraffic creates the mirrored return-traffic policy; set only for
	// type ALLOW.
	AllowReturnTraffic *bool `json:"allowReturnTraffic,omitempty"`
}

// FirewallPolicyIPProtocolScope is the IP protocol scope of a firewall policy.
type FirewallPolicyIPProtocolScope struct {
	// IPVersion is the IP-version discriminator (IPV4, IPV4_AND_IPV6, or IPV6).
	IPVersion string `json:"ipVersion"`
}

// FirewallPolicyIPAddressFilter is the IP-address traffic filter. The
// TrafficMatchingList variant references an upstream list UUID.
type FirewallPolicyIPAddressFilter struct {
	// Type is the filter discriminator (IP_ADDRESSES or TRAFFIC_MATCHING_LIST).
	Type string `json:"type"`
	// TrafficMatchingListID is the upstream list UUID, set for
	// TRAFFIC_MATCHING_LIST.
	TrafficMatchingListID string `json:"trafficMatchingListId,omitempty"`
	// MatchOpposite matches everything except the listed values.
	MatchOpposite bool `json:"matchOpposite"`
}

// FirewallPolicyPortFilter is the port traffic filter. The TrafficMatchingList
// variant references an upstream list UUID.
type FirewallPolicyPortFilter struct {
	// Type is the filter discriminator (PORTS or TRAFFIC_MATCHING_LIST).
	Type string `json:"type"`
	// TrafficMatchingListID is the upstream list UUID, set for
	// TRAFFIC_MATCHING_LIST.
	TrafficMatchingListID string `json:"trafficMatchingListId,omitempty"`
	// MatchOpposite matches everything except the listed values.
	MatchOpposite bool `json:"matchOpposite"`
}

// FirewallPolicyTrafficFilter is the source/destination traffic-filter union.
type FirewallPolicyTrafficFilter struct {
	// Type is the filter discriminator (IP_ADDRESS or PORT).
	Type string `json:"type"`
	// IPAddressFilter is set for type IP_ADDRESS.
	IPAddressFilter *FirewallPolicyIPAddressFilter `json:"ipAddressFilter,omitempty"`
	// PortFilter is set for type PORT.
	PortFilter *FirewallPolicyPortFilter `json:"portFilter,omitempty"`
}

// FirewallPolicyZone is a firewall policy source or destination: an upstream
// zone UUID plus an optional traffic filter.
type FirewallPolicyZone struct {
	// ZoneID is the upstream zone UUID.
	ZoneID string `json:"zoneId"`
	// TrafficFilter optionally narrows the zone's traffic.
	TrafficFilter *FirewallPolicyTrafficFilter `json:"trafficFilter,omitempty"`
}

// FirewallPolicy is a firewall policy as returned by the controller. ID, Index,
// and Metadata are observed and must never reach a CR's spec.
type FirewallPolicy struct {
	// ID is the policy UUID. It is observed upstream.
	ID string `json:"id"`
	// Index is the upstream policy order index.
	Index int32 `json:"index"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

	// Name is the human-readable policy name.
	Name string `json:"name"`
	// Enabled controls whether the policy is active.
	Enabled bool `json:"enabled"`
	// LoggingEnabled generates syslog entries when traffic is matched.
	LoggingEnabled bool `json:"loggingEnabled"`
	// Description is an optional description.
	Description string `json:"description,omitempty"`
	// Action is the action applied to matched traffic.
	Action FirewallPolicyAction `json:"action"`
	// IPProtocolScope is the IP protocol scope.
	IPProtocolScope FirewallPolicyIPProtocolScope `json:"ipProtocolScope"`
	// Source is the originating zone.
	Source FirewallPolicyZone `json:"source"`
	// Destination is the target zone.
	Destination FirewallPolicyZone `json:"destination"`
}

// ListFirewallPolicies returns every firewall policy on the given site. The list
// response carries the full policy shape (action, scope, source, and destination),
// so no detail call is needed to project a policy into a Custom Resource.
func (c *Client) ListFirewallPolicies(ctx context.Context, siteID string) ([]FirewallPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies")
	if err != nil {
		return nil, err
	}
	policies, err := listAll[FirewallPolicy](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list firewall policies: %w", err)
	}
	return policies, nil
}
