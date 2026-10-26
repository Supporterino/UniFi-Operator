package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
)

// zbfNotConfiguredCode is the upstream machine-readable error code a console
// returns when Zone-Based Firewall is not configured. A 400 carrying this code
// on the firewall-zones endpoints means the subsystem is empty, not that the
// request was bad (design D3).
const zbfNotConfiguredCode = "api.firewall.zone-based-firewall-not-configured"

// IsZoneBasedFirewallUnconfigured reports whether err is the controller's
// zone-based-firewall-not-configured response. The code is returned by both the
// firewall-zones and the firewall-policies endpoints, so reconcilers use this to
// report an absent upstream subsystem instead of a bad spec. It matches the
// machine-readable code, never the message text.
func IsZoneBasedFirewallUnconfigured(err error) bool {
	return zoneBasedFirewallUnconfigured(err)
}

// zoneBasedFirewallUnconfigured is the implementation of
// IsZoneBasedFirewallUnconfigured.
func zoneBasedFirewallUnconfigured(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Code == zbfNotConfiguredCode
}

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

// ListZones returns every firewall zone on the given site and reports whether
// the console's Zone-Based Firewall subsystem is configured. A console with
// Zone-Based Firewall unconfigured answers the endpoint with a
// zone-based-firewall-not-configured 400, which maps to an empty set rather than
// an error so reconciliation reports no zones instead of failing; zbfConfigured
// is then false, which lets a caller distinguish "the subsystem is absent" from
// "the site has no zones". Every other outcome — an empty 200, another error, or
// an unbuildable endpoint — leaves it true, so only the machine-readable code
// signals an absent subsystem.
func (c *HTTPClient) ListZones(ctx context.Context, siteID string) (zones []FirewallZone, zbfConfigured bool, err error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones")
	if err != nil {
		return nil, true, err
	}
	zones, err = listAll[FirewallZone](ctx, c, endpoint)
	if zoneBasedFirewallUnconfigured(err) {
		return []FirewallZone{}, false, nil
	}
	return zones, true, err
}

// GetZone returns the firewall zone with the given UUID on the given site. A
// console with Zone-Based Firewall unconfigured has no zones, so the
// zone-based-firewall-not-configured 400 maps to ErrNotFound: callers already
// treat a missing zone as absent, and a zero FirewallZone would otherwise be
// mistaken for a real, empty zone.
//
// This mapping deliberately extends to the delete path: deleteUpstream treats
// ErrNotFound as "already gone" and removes the finalizer without an upstream
// delete. That is the intended outcome, not an accidental side effect — with
// Zone-Based Firewall off there is no upstream zone to delete, so a stuck
// Terminating CR would be worse than a finalizer removed for a zone that cannot
// exist.
func (c *HTTPClient) GetZone(ctx context.Context, siteID, zoneID string) (FirewallZone, error) {
	endpoint, err := c.endpoint("sites", siteID, "firewall", "zones", zoneID)
	if err != nil {
		return FirewallZone{}, err
	}
	var zone FirewallZone
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &zone); err != nil {
		if zoneBasedFirewallUnconfigured(err) {
			return FirewallZone{}, ErrNotFound
		}
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
