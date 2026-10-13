package unifi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// Firewall policy discriminators used to build and interpret upstream bodies.
const (
	// firewallTrafficFilterIPAddress and firewallTrafficFilterPort are the
	// source/destination traffic-filter discriminators the operator authors.
	firewallTrafficFilterIPAddress = "IP_ADDRESS"
	firewallTrafficFilterPort      = "PORT"

	// firewallFilterTrafficMatchingList is the IP/port filter discriminator for
	// a filter backed by a traffic matching list.
	firewallFilterTrafficMatchingList = "TRAFFIC_MATCHING_LIST"
)

// FirewallPolicyAction is the action of a firewall policy. AllowReturnTraffic
// is set only for type ALLOW.
type FirewallPolicyAction struct {
	// Type is the action discriminator (ALLOW, BLOCK, or REJECT).
	Type string `json:"type"`
	// AllowReturnTraffic creates the mirrored return-traffic policy; set only
	// for type ALLOW.
	AllowReturnTraffic *bool `json:"allowReturnTraffic,omitempty"`
}

// FirewallPolicyIPProtocolScope is the IP protocol scope of a firewall policy.
type FirewallPolicyIPProtocolScope struct {
	// IPVersion is the IP-version discriminator (IPV4, IPV4_AND_IPV6, or IPV6).
	IPVersion string `json:"ipVersion"`
}

// FirewallPolicyIPAddressFilter is the IP-address traffic filter. The operator
// authors the TRAFFIC_MATCHING_LIST variant referencing an upstream list UUID.
type FirewallPolicyIPAddressFilter struct {
	// Type is the filter discriminator (IP_ADDRESSES or TRAFFIC_MATCHING_LIST).
	Type string `json:"type"`
	// TrafficMatchingListID is the upstream list UUID, set for
	// TRAFFIC_MATCHING_LIST.
	TrafficMatchingListID string `json:"trafficMatchingListId,omitempty"`
	// MatchOpposite matches everything except the listed values.
	MatchOpposite bool `json:"matchOpposite"`
}

// FirewallPolicyPortFilter is the port traffic filter. The operator authors the
// TRAFFIC_MATCHING_LIST variant referencing an upstream list UUID.
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
// The operator authors only the IP_ADDRESS and PORT variants, each backed by a
// traffic matching list.
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

// FirewallPolicyRequest is the create/update firewall-policy body. It carries
// only the authored fields; the policy id, index, and metadata are
// response-only and are never sent upstream. Upstream identifiers (zone UUIDs,
// traffic-matching-list UUIDs) are resolved by the reconciler before the body is
// built.
type FirewallPolicyRequest struct {
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

// FirewallPolicy is a firewall policy as returned by the controller. ID, Index,
// and Metadata are observed and must never reach a CR's spec.
type FirewallPolicy struct {
	// ID is the policy UUID. It is observed upstream.
	ID string `json:"id"`
	// Index is the upstream policy order index.
	Index int32 `json:"index"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

	FirewallPolicyRequest
}

// ListFirewallPolicies returns every firewall policy on the given site.
func (c *HTTPClient) ListFirewallPolicies(ctx context.Context, siteID string) ([]FirewallPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies")
	if err != nil {
		return nil, err
	}
	return listAll[FirewallPolicy](ctx, c, endpoint)
}

// GetFirewallPolicy returns the firewall policy with the given UUID on the
// given site.
func (c *HTTPClient) GetFirewallPolicy(ctx context.Context, siteID, policyID string) (FirewallPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies", policyID)
	if err != nil {
		return FirewallPolicy{}, err
	}
	var policy FirewallPolicy
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &policy); err != nil {
		return FirewallPolicy{}, err
	}
	return policy, nil
}

// CreateFirewallPolicy creates a firewall policy on the given site.
func (c *HTTPClient) CreateFirewallPolicy(ctx context.Context, siteID string, req FirewallPolicyRequest) (FirewallPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies")
	if err != nil {
		return FirewallPolicy{}, err
	}
	var policy FirewallPolicy
	if err := c.do(ctx, http.MethodPost, endpoint, req, &policy); err != nil {
		return FirewallPolicy{}, err
	}
	return policy, nil
}

// UpdateFirewallPolicy replaces the firewall policy with the given UUID on the
// given site.
func (c *HTTPClient) UpdateFirewallPolicy(ctx context.Context, siteID, policyID string, req FirewallPolicyRequest) (FirewallPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies", policyID)
	if err != nil {
		return FirewallPolicy{}, err
	}
	var policy FirewallPolicy
	if err := c.do(ctx, http.MethodPut, endpoint, req, &policy); err != nil {
		return FirewallPolicy{}, err
	}
	return policy, nil
}

// DeleteFirewallPolicy deletes the firewall policy with the given UUID on the
// given site.
func (c *HTTPClient) DeleteFirewallPolicy(ctx context.Context, siteID, policyID string) error {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies", policyID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}

// FirewallPolicyOrderingIDs is the before/after-system-defined partition of the
// user-defined firewall policy ordering for a zone pair.
type FirewallPolicyOrderingIDs struct {
	// BeforeSystemDefined lists the policy UUIDs ordered before the
	// system-defined policies.
	BeforeSystemDefined []string `json:"beforeSystemDefined"`
	// AfterSystemDefined lists the policy UUIDs ordered after the system-defined
	// policies.
	AfterSystemDefined []string `json:"afterSystemDefined"`
}

// FirewallPolicyOrdering is the user-defined firewall policy ordering for a
// source/destination zone pair. It is both the GET response and the PUT body.
type FirewallPolicyOrdering struct {
	// OrderedFirewallPolicyIDs is the before/after-system-defined partition.
	OrderedFirewallPolicyIDs FirewallPolicyOrderingIDs `json:"orderedFirewallPolicyIds"`
}

// GetFirewallPolicyOrdering returns the user-defined firewall policy ordering
// for the given source/destination zone pair.
func (c *HTTPClient) GetFirewallPolicyOrdering(ctx context.Context, siteID, sourceZoneID, destinationZoneID string) (FirewallPolicyOrdering, error) {
	endpoint, err := c.firewallPolicyOrderingEndpoint(siteID, sourceZoneID, destinationZoneID)
	if err != nil {
		return FirewallPolicyOrdering{}, err
	}
	var ordering FirewallPolicyOrdering
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &ordering); err != nil {
		return FirewallPolicyOrdering{}, err
	}
	return ordering, nil
}

// UpdateFirewallPolicyOrdering replaces the user-defined firewall policy
// ordering for the given source/destination zone pair.
func (c *HTTPClient) UpdateFirewallPolicyOrdering(ctx context.Context, siteID, sourceZoneID, destinationZoneID string, req FirewallPolicyOrdering) (FirewallPolicyOrdering, error) {
	endpoint, err := c.firewallPolicyOrderingEndpoint(siteID, sourceZoneID, destinationZoneID)
	if err != nil {
		return FirewallPolicyOrdering{}, err
	}
	var ordering FirewallPolicyOrdering
	if err := c.do(ctx, http.MethodPut, endpoint, req, &ordering); err != nil {
		return FirewallPolicyOrdering{}, err
	}
	return ordering, nil
}

// firewallPolicyOrderingEndpoint builds the ordering URL for a zone pair,
// carrying the required source/destination zone query parameters.
func (c *HTTPClient) firewallPolicyOrderingEndpoint(siteID, sourceZoneID, destinationZoneID string) (string, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "policies", "ordering")
	if err != nil {
		return "", err
	}
	return withQueryParams(endpoint, map[string]string{
		"sourceFirewallZoneId":      sourceZoneID,
		"destinationFirewallZoneId": destinationZoneID,
	})
}

// withQueryParams sets the given query parameters on an endpoint URL.
func withQueryParams(endpoint string, params map[string]string) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	query := u.Query()
	for key, value := range params {
		query.Set(key, value)
	}
	u.RawQuery = query.Encode()
	return u.String(), nil
}
