package unifi

import (
	"context"
	"net/http"
)

// LagMember is an observed LAG member: a device UUID and its member port
// indexes. Both are upstream identifiers and must be recorded in a Custom
// Resource's status only.
type LagMember struct {
	// DeviceID is the UUID of the member device.
	DeviceID string `json:"deviceId"`
	// PortIdxs are the member port indexes.
	PortIdxs []int32 `json:"portIdxs"`
}

// Lag is a LAG as returned by the controller. The upstream `LAG details` union
// is flattened: Type selects the variant, and a variant may carry a
// McLagDomainID (MULTI_CHASSIS) or SwitchStackID (SWITCH_STACK). The frozen
// v10.4.57 `LAG details` schema exposes no name, so a LAG is matched by its type
// and members. ID and Metadata are observed and must never reach a CR's spec.
type Lag struct {
	// ID is the LAG UUID. It is observed upstream.
	ID string `json:"id"`
	// Type is the LAG type (LOCAL, MULTI_CHASSIS, or SWITCH_STACK).
	Type string `json:"type"`
	// Metadata is the observed entity metadata.
	Metadata NetworkMetadata `json:"metadata"`
	// Members are the LAG members.
	Members []LagMember `json:"members"`

	// McLagDomainID is set for type MULTI_CHASSIS.
	McLagDomainID string `json:"mcLagDomainId,omitempty"`
	// SwitchStackID is set for type SWITCH_STACK.
	SwitchStackID string `json:"switchStackId,omitempty"`
}

// McLagPeer is an observed MC-LAG domain peer.
type McLagPeer struct {
	// DeviceID is the UUID of the peer device.
	DeviceID string `json:"deviceId"`
	// LinkPortIdxs are the peer link port indexes.
	LinkPortIdxs []int32 `json:"linkPortIdxs"`
	// Role is the peer role (TOP or BOTTOM).
	Role string `json:"role"`
}

// McLagLocal is an observed local LAG within an MC-LAG domain.
type McLagLocal struct {
	// ID is the local LAG UUID. It is observed upstream.
	ID string `json:"id"`
	// Metadata is the observed entity metadata.
	Metadata NetworkMetadata `json:"metadata"`
	// Members are the local LAG members.
	Members []LagMember `json:"members"`
}

// McLagDomain is an MC-LAG domain as returned by the controller.
type McLagDomain struct {
	// ID is the domain UUID. It is observed upstream.
	ID string `json:"id"`
	// Name is the human-readable domain name.
	Name string `json:"name"`
	// Metadata is the observed entity metadata.
	Metadata NetworkMetadata `json:"metadata"`
	// Peers are the domain peers.
	Peers []McLagPeer `json:"peers"`
	// Lags are the domain's local LAGs.
	Lags []McLagLocal `json:"lags"`
}

// SwitchStackMember is an observed switch-stack member.
type SwitchStackMember struct {
	// DeviceID is the UUID of the member device.
	DeviceID string `json:"deviceId"`
}

// SwitchStackLag is an observed LAG within a switch stack.
type SwitchStackLag struct {
	// ID is the local LAG UUID. It is observed upstream.
	ID string `json:"id"`
	// Metadata is the observed entity metadata.
	Metadata NetworkMetadata `json:"metadata"`
	// Members are the local LAG members.
	Members []LagMember `json:"members"`
}

// SwitchStack is a switch stack as returned by the controller.
type SwitchStack struct {
	// ID is the stack UUID. It is observed upstream.
	ID string `json:"id"`
	// Name is the human-readable stack name.
	Name string `json:"name"`
	// Metadata is the observed entity metadata.
	Metadata NetworkMetadata `json:"metadata"`
	// Members are the member devices.
	Members []SwitchStackMember `json:"members"`
	// Lags are the stack's local LAGs.
	Lags []SwitchStackLag `json:"lags"`
}

// ListLags returns every LAG on the given site.
func (c *HTTPClient) ListLags(ctx context.Context, siteID string) ([]Lag, error) {
	endpoint, err := c.endpoint("sites", siteID, "switching", "lags")
	if err != nil {
		return nil, err
	}
	return listAll[Lag](ctx, c, endpoint)
}

// GetLag returns the LAG with the given UUID on the given site.
func (c *HTTPClient) GetLag(ctx context.Context, siteID, lagID string) (Lag, error) {
	endpoint, err := c.endpoint("sites", siteID, "switching", "lags", lagID)
	if err != nil {
		return Lag{}, err
	}
	var lag Lag
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &lag); err != nil {
		return Lag{}, err
	}
	return lag, nil
}

// ListMcLagDomains returns every MC-LAG domain on the given site.
func (c *HTTPClient) ListMcLagDomains(ctx context.Context, siteID string) ([]McLagDomain, error) {
	endpoint, err := c.endpoint("sites", siteID, "switching", "mc-lag-domains")
	if err != nil {
		return nil, err
	}
	return listAll[McLagDomain](ctx, c, endpoint)
}

// GetMcLagDomain returns the MC-LAG domain with the given UUID on the given
// site.
func (c *HTTPClient) GetMcLagDomain(ctx context.Context, siteID, domainID string) (McLagDomain, error) {
	endpoint, err := c.endpoint("sites", siteID, "switching", "mc-lag-domains", domainID)
	if err != nil {
		return McLagDomain{}, err
	}
	var domain McLagDomain
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &domain); err != nil {
		return McLagDomain{}, err
	}
	return domain, nil
}

// ListSwitchStacks returns every switch stack on the given site.
func (c *HTTPClient) ListSwitchStacks(ctx context.Context, siteID string) ([]SwitchStack, error) {
	endpoint, err := c.endpoint("sites", siteID, "switching", "switch-stacks")
	if err != nil {
		return nil, err
	}
	return listAll[SwitchStack](ctx, c, endpoint)
}

// GetSwitchStack returns the switch stack with the given UUID on the given site.
func (c *HTTPClient) GetSwitchStack(ctx context.Context, siteID, stackID string) (SwitchStack, error) {
	endpoint, err := c.endpoint("sites", siteID, "switching", "switch-stacks", stackID)
	if err != nil {
		return SwitchStack{}, err
	}
	var stack SwitchStack
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &stack); err != nil {
		return SwitchStack{}, err
	}
	return stack, nil
}
