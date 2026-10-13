package unifi

import "context"

// VpnServer is a VPN server overview as returned by the controller. ID,
// Type, Enabled, and Metadata are observed and must never reach a CR's spec.
// The read-only surface exposes no credential.
type VpnServer struct {
	// ID is the server UUID. It is observed upstream.
	ID string `json:"id"`
	// Name is the human-readable server name.
	Name string `json:"name"`
	// Type is the server type (WIREGUARD, OPENVPN, L2TP, PPTP, or UID).
	Type string `json:"type"`
	// Enabled controls whether the server is active.
	Enabled bool `json:"enabled"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
}

// SiteToSiteTunnel is a site-to-site VPN tunnel overview as returned by the
// controller. ID, Type, and Metadata are observed and must never reach a CR's
// spec. The read-only surface exposes no credential.
type SiteToSiteTunnel struct {
	// ID is the tunnel UUID. It is observed upstream.
	ID string `json:"id"`
	// Name is the human-readable tunnel name.
	Name string `json:"name"`
	// Type is the tunnel type (IPSEC, OPENVPN, or WIREGUARD).
	Type string `json:"type"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
}

// ListVpnServers returns every VPN server on the given site.
func (c *HTTPClient) ListVpnServers(ctx context.Context, siteID string) ([]VpnServer, error) {
	endpoint, err := c.endpoint("sites", siteID, "vpn", "servers")
	if err != nil {
		return nil, err
	}
	return listAll[VpnServer](ctx, c, endpoint)
}

// ListSiteToSiteTunnels returns every site-to-site VPN tunnel on the given site.
func (c *HTTPClient) ListSiteToSiteTunnels(ctx context.Context, siteID string) ([]SiteToSiteTunnel, error) {
	endpoint, err := c.endpoint("sites", siteID, "vpn", "site-to-site-tunnels")
	if err != nil {
		return nil, err
	}
	return listAll[SiteToSiteTunnel](ctx, c, endpoint)
}
