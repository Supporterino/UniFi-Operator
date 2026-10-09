package unifi

import (
	"context"
	"fmt"
)

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
// side of the zone surface; the CLI does not create, update, or delete zones.
func (c *Client) ListZones(ctx context.Context, siteID string) ([]FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones")
	if err != nil {
		return nil, err
	}
	zones, err := listAll[FirewallZone](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list firewall zones: %w", err)
	}
	return zones, nil
}
