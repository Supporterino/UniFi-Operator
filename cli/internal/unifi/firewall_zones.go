package unifi

import (
	"context"
	"errors"
	"fmt"
)

// zbfNotConfiguredCode is the upstream machine-readable error code a console returns
// when Zone-Based Firewall is not configured. A 400 carrying this code on the
// firewall-zones endpoint means the subsystem is empty, not that the request was bad
// (design D3).
const zbfNotConfiguredCode = "api.firewall.zone-based-firewall-not-configured"

// zoneBasedFirewallUnconfigured reports whether err is the controller's
// zone-based-firewall-not-configured response. It matches the machine-readable code,
// never the message text.
func zoneBasedFirewallUnconfigured(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == zbfNotConfiguredCode
}

// FirewallZone is a firewall zone as returned by the controller's Integration
// v1 API. ID and NetworkIDs are upstream UUIDs and must never reach an emitted
// Custom Resource's spec; Metadata is observed, never authored.
type FirewallZone struct {
	// ID is the zone UUID. It is observed upstream and must not reach a spec.
	ID string `json:"id"`
	// Name is the human-readable firewall-zone name.
	Name string `json:"name"`
	// NetworkIDs is the UUIDs of the member networks.
	NetworkIDs []string `json:"networkIds"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
}

// ListZones returns every firewall zone on the given site. It is the read-only
// side of the zone surface; the CLI does not create, update, or delete zones. A
// console with Zone-Based Firewall unconfigured answers the endpoint with a
// zone-based-firewall-not-configured 400, which maps to an empty set rather than
// an error so `snapshot firewall-zones` succeeds with zero resources.
func (c *Client) ListZones(ctx context.Context, siteID string) ([]FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones")
	if err != nil {
		return nil, err
	}
	zones, err := listAll[FirewallZone](ctx, c, endpoint)
	if zoneBasedFirewallUnconfigured(err) {
		return []FirewallZone{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("list firewall zones: %w", err)
	}
	return zones, nil
}
