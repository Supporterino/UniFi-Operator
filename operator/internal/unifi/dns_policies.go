package unifi

import (
	"context"
	"net/http"
)

// DnsPolicyRequest is the create/update DNS-policy body. The upstream union is
// flattened on the wire: only the fields for Type are set, so a single struct
// carries every variant. The record id and metadata are response-only and are
// never sent upstream.
//
// TTLSeconds, Priority, Port, and Weight are pointers so that a legitimate zero
// value is serialized rather than omitted.
type DnsPolicyRequest struct {
	// Type is the DNS-policy discriminator.
	Type string `json:"type"`
	// Domain is the domain the record applies to.
	Domain string `json:"domain"`
	// Enabled controls whether the record is active.
	Enabled bool `json:"enabled"`

	// IPv4Address is set for type A_RECORD.
	IPv4Address string `json:"ipv4Address,omitempty"`
	// IPv6Address is set for type AAAA_RECORD.
	IPv6Address string `json:"ipv6Address,omitempty"`
	// TTLSeconds is set for types A_RECORD, AAAA_RECORD, and CNAME_RECORD.
	TTLSeconds *int32 `json:"ttlSeconds,omitempty"`
	// TargetDomain is set for type CNAME_RECORD.
	TargetDomain string `json:"targetDomain,omitempty"`
	// MailServerDomain is set for type MX_RECORD.
	MailServerDomain string `json:"mailServerDomain,omitempty"`
	// Priority is set for types MX_RECORD and SRV_RECORD.
	Priority *int32 `json:"priority,omitempty"`
	// Port is set for type SRV_RECORD.
	Port *int32 `json:"port,omitempty"`
	// Protocol is set for type SRV_RECORD.
	Protocol string `json:"protocol,omitempty"`
	// ServerDomain is set for type SRV_RECORD.
	ServerDomain string `json:"serverDomain,omitempty"`
	// Service is set for type SRV_RECORD.
	Service string `json:"service,omitempty"`
	// Weight is set for type SRV_RECORD.
	Weight *int32 `json:"weight,omitempty"`
	// Text is set for type TXT_RECORD.
	Text string `json:"text,omitempty"`
	// IPAddress is set for type FORWARD_DOMAIN.
	IPAddress string `json:"ipAddress,omitempty"`
}

// DnsPolicy is a DNS policy record as returned by the controller. ID and
// Metadata are observed and must never reach a CR's spec.
type DnsPolicy struct {
	// ID is the record UUID. It is observed upstream.
	ID string `json:"id"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

	DnsPolicyRequest
}

// ListDnsPolicies returns every DNS policy on the given site.
func (c *HTTPClient) ListDnsPolicies(ctx context.Context, siteID string) ([]DnsPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "dns", "policies")
	if err != nil {
		return nil, err
	}
	return listAll[DnsPolicy](ctx, c, endpoint)
}

// GetDnsPolicy returns the DNS policy with the given UUID on the given site.
func (c *HTTPClient) GetDnsPolicy(ctx context.Context, siteID, policyID string) (DnsPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "dns", "policies", policyID)
	if err != nil {
		return DnsPolicy{}, err
	}
	var policy DnsPolicy
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &policy); err != nil {
		return DnsPolicy{}, err
	}
	return policy, nil
}

// CreateDnsPolicy creates a DNS policy on the given site.
func (c *HTTPClient) CreateDnsPolicy(ctx context.Context, siteID string, req DnsPolicyRequest) (DnsPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "dns", "policies")
	if err != nil {
		return DnsPolicy{}, err
	}
	var policy DnsPolicy
	if err := c.do(ctx, http.MethodPost, endpoint, req, &policy); err != nil {
		return DnsPolicy{}, err
	}
	return policy, nil
}

// UpdateDnsPolicy replaces the DNS policy with the given UUID on the given site.
func (c *HTTPClient) UpdateDnsPolicy(ctx context.Context, siteID, policyID string, req DnsPolicyRequest) (DnsPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "dns", "policies", policyID)
	if err != nil {
		return DnsPolicy{}, err
	}
	var policy DnsPolicy
	if err := c.do(ctx, http.MethodPut, endpoint, req, &policy); err != nil {
		return DnsPolicy{}, err
	}
	return policy, nil
}

// DeleteDnsPolicy deletes the DNS policy with the given UUID on the given site.
func (c *HTTPClient) DeleteDnsPolicy(ctx context.Context, siteID, policyID string) error {
	endpoint, err := c.endpoint("sites", siteID, "dns", "policies", policyID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}
