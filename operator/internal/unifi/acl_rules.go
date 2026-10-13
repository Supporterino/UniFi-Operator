package unifi

import (
	"context"
	"net/http"
)

// AclRuleEndpoint is a source or destination traffic filter. The upstream union
// is flattened on the wire: only the fields for Type are set.
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

// AclRuleDeviceFilter is the upstream enforcing-device filter. The operator
// authors the DEVICES variant carrying the resolved device UUIDs.
type AclRuleDeviceFilter struct {
	// Type is the filter discriminator (DEVICES).
	Type string `json:"type"`
	// DeviceIDs are the upstream device UUIDs.
	DeviceIDs []string `json:"deviceIds"`
}

// AclRuleRequest is the create/update ACL-rule body. It carries only the
// authored fields; the rule id, index, and metadata are response-only.
type AclRuleRequest struct {
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

// AclRule is an ACL rule as returned by the controller. ID, Index, and Metadata
// are observed and must never reach a CR's spec.
type AclRule struct {
	// ID is the rule UUID. It is observed upstream.
	ID string `json:"id"`
	// Index is the upstream rule order index.
	Index int32 `json:"index"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

	AclRuleRequest
}

// AclRuleOrdering is the flat, site-global ACL-rule ordering. It is both the GET
// response and the PUT body.
type AclRuleOrdering struct {
	// OrderedAclRuleIDs is the ordered list of ACL-rule UUIDs.
	OrderedAclRuleIDs []string `json:"orderedAclRuleIds"`
}

// ListAclRules returns every ACL rule on the given site.
func (c *HTTPClient) ListAclRules(ctx context.Context, siteID string) ([]AclRule, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules")
	if err != nil {
		return nil, err
	}
	return listAll[AclRule](ctx, c, endpoint)
}

// GetAclRule returns the ACL rule with the given UUID on the given site.
func (c *HTTPClient) GetAclRule(ctx context.Context, siteID, ruleID string) (AclRule, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules", ruleID)
	if err != nil {
		return AclRule{}, err
	}
	var rule AclRule
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &rule); err != nil {
		return AclRule{}, err
	}
	return rule, nil
}

// CreateAclRule creates an ACL rule on the given site.
func (c *HTTPClient) CreateAclRule(ctx context.Context, siteID string, req AclRuleRequest) (AclRule, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules")
	if err != nil {
		return AclRule{}, err
	}
	var rule AclRule
	if err := c.do(ctx, http.MethodPost, endpoint, req, &rule); err != nil {
		return AclRule{}, err
	}
	return rule, nil
}

// UpdateAclRule replaces the ACL rule with the given UUID on the given site.
func (c *HTTPClient) UpdateAclRule(ctx context.Context, siteID, ruleID string, req AclRuleRequest) (AclRule, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules", ruleID)
	if err != nil {
		return AclRule{}, err
	}
	var rule AclRule
	if err := c.do(ctx, http.MethodPut, endpoint, req, &rule); err != nil {
		return AclRule{}, err
	}
	return rule, nil
}

// DeleteAclRule deletes the ACL rule with the given UUID on the given site.
func (c *HTTPClient) DeleteAclRule(ctx context.Context, siteID, ruleID string) error {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules", ruleID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}

// GetAclRuleOrdering returns the site-global ACL-rule ordering.
func (c *HTTPClient) GetAclRuleOrdering(ctx context.Context, siteID string) (AclRuleOrdering, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules", "ordering")
	if err != nil {
		return AclRuleOrdering{}, err
	}
	var ordering AclRuleOrdering
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &ordering); err != nil {
		return AclRuleOrdering{}, err
	}
	return ordering, nil
}

// UpdateAclRuleOrdering replaces the site-global ACL-rule ordering.
func (c *HTTPClient) UpdateAclRuleOrdering(ctx context.Context, siteID string, req AclRuleOrdering) (AclRuleOrdering, error) {
	endpoint, err := c.endpoint("sites", siteID, "acl-rules", "ordering")
	if err != nil {
		return AclRuleOrdering{}, err
	}
	var ordering AclRuleOrdering
	if err := c.do(ctx, http.MethodPut, endpoint, req, &ordering); err != nil {
		return AclRuleOrdering{}, err
	}
	return ordering, nil
}
