package unifi

import (
	"context"
	"fmt"
)

// DnsPolicyOriginUserDefined is the upstream metadata.origin of a user-defined
// record; other origins are adopted read-only.
const DnsPolicyOriginUserDefined = "USER_DEFINED"

// DnsPolicy is a DNS policy record as returned by the controller. The upstream
// union is flattened on the wire: only the fields for Type are set. ID and
// Metadata are observed and must never reach a CR's spec.
//
// TTLSeconds, Priority, Port, and Weight are pointers so a legitimate zero value
// is distinguished from an absent one.
type DnsPolicy struct {
	// ID is the record UUID. It is observed upstream.
	ID string `json:"id"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

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

// ListDnsPolicies returns every DNS policy on the given site. The list response
// carries the full flattened variant fields, so no detail call is needed to
// project a policy into a Custom Resource.
func (c *Client) ListDnsPolicies(ctx context.Context, siteID string) ([]DnsPolicy, error) {
	endpoint, err := c.endpoint("sites", siteID, "dns", "policies")
	if err != nil {
		return nil, err
	}
	policies, err := listAll[DnsPolicy](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list dns policies: %w", err)
	}
	return policies, nil
}
