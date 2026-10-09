package unifi

import (
	"context"
	"encoding/json"
	"net/http"
)

// FirewallZone is a firewall zone as returned by the controller's Integration
// v1 API. ID is an upstream UUID and must be recorded in a Custom Resource's
// status only. Metadata is observed, never authored; only a USER_DEFINED zone
// may be created, updated, or deleted.
type FirewallZone struct {
	// ID is the zone UUID. It is observed upstream and must not reach a spec.
	ID string `json:"id"`
	// Name is the human-readable firewall-zone name.
	Name string `json:"name"`
	// NetworkIDs is the UUIDs of the member networks. It is observed; membership
	// is authored through FirewallZoneRequest by the zone's single writer.
	NetworkIDs []string `json:"networkIds"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
}

// FirewallZoneRequest is the create/update firewall-zone body. A write carries
// only the authored fields {name, networkIds}; metadata.origin is response-only
// and is never sent upstream.
type FirewallZoneRequest struct {
	// Name is the human-readable firewall-zone name.
	Name string `json:"name"`
	// NetworkIDs is the upstream UUIDs of the member networks. It may be empty,
	// which clears all membership.
	NetworkIDs []string `json:"networkIds"`
}

// MarshalJSON always emits networkIds as a JSON array, never null, because the
// upstream create/update body requires the field to be present; a nil request
// slice therefore serializes as an empty list.
func (r FirewallZoneRequest) MarshalJSON() ([]byte, error) {
	networkIDs := r.NetworkIDs
	if networkIDs == nil {
		networkIDs = []string{}
	}
	type requestBody struct {
		Name       string   `json:"name"`
		NetworkIDs []string `json:"networkIds"`
	}
	return json.Marshal(requestBody{Name: r.Name, NetworkIDs: networkIDs})
}

// ListZones returns every firewall zone on the given site.
func (c *HTTPClient) ListZones(ctx context.Context, siteID string) ([]FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones")
	if err != nil {
		return nil, err
	}
	return listAll[FirewallZone](ctx, c, endpoint)
}

// GetZone returns the firewall zone with the given UUID on the given site.
func (c *HTTPClient) GetZone(ctx context.Context, siteID, zoneID string) (FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones", zoneID)
	if err != nil {
		return FirewallZone{}, err
	}
	var zone FirewallZone
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &zone); err != nil {
		return FirewallZone{}, err
	}
	return zone, nil
}

// CreateZone creates a custom firewall zone on the given site.
func (c *HTTPClient) CreateZone(ctx context.Context, siteID string, req FirewallZoneRequest) (FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones")
	if err != nil {
		return FirewallZone{}, err
	}
	var zone FirewallZone
	if err := c.do(ctx, http.MethodPost, endpoint, req, &zone); err != nil {
		return FirewallZone{}, err
	}
	return zone, nil
}

// UpdateZone replaces the firewall zone with the given UUID on the given site.
func (c *HTTPClient) UpdateZone(ctx context.Context, siteID, zoneID string, req FirewallZoneRequest) (FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones", zoneID)
	if err != nil {
		return FirewallZone{}, err
	}
	var zone FirewallZone
	if err := c.do(ctx, http.MethodPut, endpoint, req, &zone); err != nil {
		return FirewallZone{}, err
	}
	return zone, nil
}

// DeleteZone deletes the custom firewall zone with the given UUID on the given
// site.
func (c *HTTPClient) DeleteZone(ctx context.Context, siteID, zoneID string) error {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones", zoneID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}
