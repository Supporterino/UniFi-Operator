package unifi

import (
	"context"
	"fmt"
)

// VpnServer is a VPN server overview as returned by
// GET /v1/sites/{siteId}/vpn/servers. ID, Type, Enabled, and Metadata are
// observed and must never reach a CR's spec. The read-only surface exposes no
// credential.
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

// SiteToSiteTunnel is a site-to-site VPN tunnel overview as returned by
// GET /v1/sites/{siteId}/vpn/site-to-site-tunnels. ID, Type, and Metadata are
// observed and must never reach a CR's spec. The read-only surface exposes no
// credential.
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

// ListVpnServers returns every VPN server on the given site. The frozen surface
// exposes VPN servers read-only; there is deliberately no write method.
func (c *Client) ListVpnServers(ctx context.Context, siteID string) ([]VpnServer, error) {
	endpoint, err := c.endpoint("sites", siteID, "vpn", "servers")
	if err != nil {
		return nil, err
	}
	servers, err := listAll[VpnServer](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list vpn servers: %w", err)
	}
	return servers, nil
}

// ListSiteToSiteTunnels returns every site-to-site VPN tunnel on the given site.
// The frozen surface exposes tunnels read-only; there is deliberately no write
// method.
func (c *Client) ListSiteToSiteTunnels(ctx context.Context, siteID string) ([]SiteToSiteTunnel, error) {
	endpoint, err := c.endpoint("sites", siteID, "vpn", "site-to-site-tunnels")
	if err != nil {
		return nil, err
	}
	tunnels, err := listAll[SiteToSiteTunnel](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list site-to-site tunnels: %w", err)
	}
	return tunnels, nil
}
