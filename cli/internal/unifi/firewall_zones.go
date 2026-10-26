package unifi

import (
	"context"
	"errors"
	"fmt"
)

// zbfNotConfiguredCode is the upstream machine-readable error code a console returns
// when Zone-Based Firewall is not configured. It is returned by more than one firewall
// endpoint (observed on the zones and policies reads), not by the zones endpoint alone, so
// a 400 carrying it means the subsystem is empty rather than that the request was bad.
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

// ListZones returns every firewall zone on the given site and whether the console has
// Zone-Based Firewall configured. It is the read-only side of the zone surface; the CLI
// does not create, update, or delete zones. A console with Zone-Based Firewall unconfigured
// answers the endpoint with a zone-based-firewall-not-configured 400, which maps to an empty
// set rather than an error so `snapshot firewall-zones` succeeds with zero resources.
//
// zbfConfigured is false only for that machine-readable code; every other outcome, including
// an empty 200 page, leaves it true (design D1). It is not inferred from an empty zone list,
// because an empty page is not a contract guarantee that the subsystem is absent.
func (c *Client) ListZones(ctx context.Context, siteID string) ([]FirewallZone, bool, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones")
	if err != nil {
		return nil, true, err
	}
	zones, err := listAll[FirewallZone](ctx, c, endpoint)
	if zoneBasedFirewallUnconfigured(err) {
		return []FirewallZone{}, false, nil
	}
	if err != nil {
		return nil, true, fmt.Errorf("list firewall zones: %w", err)
	}
	return zones, true, nil
}
