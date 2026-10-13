package unifi

import "context"

// RadiusProfile is an existing RADIUS authentication profile as returned by
// GET /v1/sites/{siteId}/radius/profiles. The frozen v10.4.57 surface exposes
// RADIUS profiles read-only and only as an overview: the schema is
// {id, metadata, name}, so the profile's servers, ports, and shared secret are
// neither modeled nor observable here, and ID is an upstream UUID that must be
// recorded in a Custom Resource's status only. Metadata is observed, never
// authored.
type RadiusProfile struct {
	// ID is the profile UUID. It is observed upstream and must not reach a spec.
	ID string `json:"id"`
	// Name is the human-readable profile name a selector matches on.
	Name string `json:"name"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
}

// ListRadiusProfiles returns every RADIUS authentication profile on the given
// site. It is the read-only resolution source for adopting an existing profile
// by name; the frozen surface exposes no create, update, or delete endpoint, so
// there is deliberately no write method.
func (c *HTTPClient) ListRadiusProfiles(ctx context.Context, siteID string) ([]RadiusProfile, error) {
	endpoint, err := c.endpoint("sites", siteID, "radius", "profiles")
	if err != nil {
		return nil, err
	}
	return listAll[RadiusProfile](ctx, c, endpoint)
}
