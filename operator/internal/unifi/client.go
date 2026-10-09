// Package unifi provides the typed UniFi controller client consumed by the
// operator's reconcilers.
//
// The package speaks the UniFi Network Integration v1 API (base path
// /proxy/network/integration/v1) documented in docs/unifi-api.md, frozen to the
// controller's OpenAPI document at spec v10.4.57. It is deliberately
// Kubernetes-agnostic: it returns plain Go structs, takes a context.Context on
// every call, and never imports Kubernetes types.
//
// Upstream correlation identifiers (UUIDs) are returned to callers, which keep
// them out of a CR's spec and record them in status only (see
// docs/crd-conventions.md). The API key is carried in the X-API-Key header and
// is never logged, put in an error, or written to status.
package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// ErrNotFound is returned when the controller has no object for the requested
// identifier. An *APIError with a 404 status matches ErrNotFound.
var ErrNotFound = errors.New("unifi: object not found")

// InvalidSpecError reports a caller-supplied value that cannot be used to build
// a safe controller request, such as an empty, traversing, or separator-bearing
// path segment, or an internally inconsistent request body. It is terminal:
// callers should surface a failing condition (reason InvalidSpec) rather than
// retry.
type InvalidSpecError struct {
	// Field names the offending input, for example "siteID".
	Field string
	// Reason describes why the value is unusable.
	Reason string
}

// Error implements the error interface.
func (e *InvalidSpecError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// Info is the response of GET /v1/info. It is the reachability probe and the
// source of the detected application version that gates capability
// availability.
type Info struct {
	// ApplicationVersion is the detected UniFi Network application version.
	ApplicationVersion string `json:"applicationVersion"`
}

// Site is a site as returned by GET /v1/sites.
type Site struct {
	// ID is the site UUID used in every site-scoped path.
	ID string `json:"id"`
	// InternalReference is the site's internal unique name used by older APIs.
	InternalReference string `json:"internalReference"`
	// Name is the human-readable site name.
	Name string `json:"name"`
}

// NetworkMetadata is the observed metadata of a network. Only the origin is
// modeled; it is observed, never authored.
type NetworkMetadata struct {
	// Origin is the entity origin (USER_DEFINED, SYSTEM_DEFINED, DERIVED, or
	// ORCHESTRATED).
	Origin string `json:"origin"`
}

// DHCPGuarding holds the trusted DHCP server addresses for a network.
type DHCPGuarding struct {
	// TrustedDHCPServerIPAddresses is the list of trusted DHCP server IP
	// addresses.
	TrustedDHCPServerIPAddresses []string `json:"trustedDhcpServerIpAddresses"`
}

// IPv4Configuration is the IPv4 configuration of a network, shared by the
// gateway- and switch-managed variants. The upstream optional `dhcpConfiguration`
// union and WAN-specific `natOutboundIpAddressConfiguration` are deliberately
// unmodeled.
type IPv4Configuration struct {
	AutoScaleEnabled        bool     `json:"autoScaleEnabled"`
	HostIPAddress           string   `json:"hostIpAddress"`
	PrefixLength            int32    `json:"prefixLength"`
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`
}

// IPv6ClientAddressAssignment configures IPv6 client addressing. Only SLAAC is
// modeled; the upstream optional DHCPv6 configuration is deliberately unmodeled.
type IPv6ClientAddressAssignment struct {
	SLAACEnabled bool `json:"slaacEnabled"`
}

// RouterAdvertisement configures IPv6 router advertisement.
type RouterAdvertisement struct {
	Priority string `json:"priority"`
}

// IPv6Configuration is the interface-type-discriminated IPv6 configuration.
type IPv6Configuration struct {
	InterfaceType                string                      `json:"interfaceType"`
	ClientAddressAssignment      IPv6ClientAddressAssignment `json:"clientAddressAssignment"`
	AdditionalHostIPSubnets      []string                    `json:"additionalHostIpSubnets,omitempty"`
	DNSServerIPAddressesOverride []string                    `json:"dnsServerIpAddressesOverride,omitempty"`
	RouterAdvertisement          *RouterAdvertisement        `json:"routerAdvertisement,omitempty"`

	// PrefixDelegationWANInterfaceID is set for interfaceType PREFIX_DELEGATION.
	PrefixDelegationWANInterfaceID string `json:"prefixDelegationWanInterfaceId,omitempty"`
	// HostIPAddress and PrefixLength are set for interfaceType STATIC.
	HostIPAddress string `json:"hostIpAddress,omitempty"`
	PrefixLength  int32  `json:"prefixLength,omitempty"`
}

// Network is a UniFi network as returned by the controller. It is a flat plain
// struct: the union-specific fields are populated only for the matching
// management variant. Callers must keep ID out of a CR's spec and record it in
// status only.
type Network struct {
	ID           string          `json:"id"`
	Management   string          `json:"management"`
	Name         string          `json:"name"`
	Enabled      bool            `json:"enabled"`
	VLANID       int32           `json:"vlanId"`
	Default      bool            `json:"default"`
	Metadata     NetworkMetadata `json:"metadata"`
	DHCPGuarding *DHCPGuarding   `json:"dhcpGuarding,omitempty"`

	// ZoneID is populated for management GATEWAY; DeviceID for management
	// SWITCH. Both are observed upstream UUIDs.
	ZoneID   string `json:"zoneId,omitempty"`
	DeviceID string `json:"deviceId,omitempty"`

	// Union-observed fields. Pointer types distinguish an absent field from an
	// explicit false.
	CellularBackupEnabled *bool              `json:"cellularBackupEnabled,omitempty"`
	InternetAccessEnabled *bool              `json:"internetAccessEnabled,omitempty"`
	IsolationEnabled      *bool              `json:"isolationEnabled,omitempty"`
	MDNSForwardingEnabled *bool              `json:"mdnsForwardingEnabled,omitempty"`
	IPv4Configuration     *IPv4Configuration `json:"ipv4Configuration,omitempty"`
	IPv6Configuration     *IPv6Configuration `json:"ipv6Configuration,omitempty"`
}

// GatewayNetworkRequest carries the GATEWAY-variant fields of a create/update
// body. The fields are the upstream-required set for management GATEWAY.
type GatewayNetworkRequest struct {
	CellularBackupEnabled bool               `json:"cellularBackupEnabled"`
	InternetAccessEnabled bool               `json:"internetAccessEnabled"`
	IsolationEnabled      bool               `json:"isolationEnabled"`
	IPv4Configuration     IPv4Configuration  `json:"ipv4Configuration"`
	IPv6Configuration     *IPv6Configuration `json:"ipv6Configuration,omitempty"`
	MDNSForwardingEnabled *bool              `json:"mdnsForwardingEnabled,omitempty"`
}

// SwitchNetworkRequest carries the SWITCH-variant fields of a create/update
// body. DeviceID is the resolved upstream device UUID; the CR keeps the opaque
// ID out of spec and resolves it from a typed device selector.
type SwitchNetworkRequest struct {
	CellularBackupEnabled bool              `json:"cellularBackupEnabled"`
	IsolationEnabled      bool              `json:"isolationEnabled"`
	IPv4Configuration     IPv4Configuration `json:"ipv4Configuration"`
	DeviceID              string            `json:"deviceId"`
}

// NetworkRequest is the management-discriminated create/update network body.
// Exactly one variant matching Management must be set; UNMANAGED has no variant.
//
// The request deliberately has no zoneId field: UnifiFirewallZone owns network
// membership and the controller omits zoneId on writes (design D5).
type NetworkRequest struct {
	Management   string        `json:"management"`
	Name         string        `json:"name"`
	Enabled      bool          `json:"enabled"`
	VLANID       int32         `json:"vlanId"`
	DHCPGuarding *DHCPGuarding `json:"dhcpGuarding,omitempty"`

	// Gateway is set for management GATEWAY and marshaled flat onto the body.
	Gateway *GatewayNetworkRequest `json:"-"`
	// Switch is set for management SWITCH and marshaled flat onto the body.
	Switch *SwitchNetworkRequest `json:"-"`
}

// Management discriminator values accepted by NetworkRequest.
const (
	managementField     = "management"
	managementGateway   = "GATEWAY"
	managementSwitch    = "SWITCH"
	managementUnmanaged = "UNMANAGED"
)

// MarshalJSON flattens the common fields and the selected variant onto a single
// JSON object, matching the upstream discriminated body. Marshaling is only
// meaningful after validate; a nil variant is omitted rather than panicking.
func (r NetworkRequest) MarshalJSON() ([]byte, error) {
	body := map[string]any{
		managementField: r.Management,
		"name":          r.Name,
		"enabled":       r.Enabled,
		"vlanId":        r.VLANID,
	}
	if r.DHCPGuarding != nil {
		body["dhcpGuarding"] = r.DHCPGuarding
	}

	switch r.Management {
	case managementGateway:
		if g := r.Gateway; g != nil {
			body["cellularBackupEnabled"] = g.CellularBackupEnabled
			body["internetAccessEnabled"] = g.InternetAccessEnabled
			body["isolationEnabled"] = g.IsolationEnabled
			body["ipv4Configuration"] = g.IPv4Configuration
			if g.IPv6Configuration != nil {
				body["ipv6Configuration"] = g.IPv6Configuration
			}
			if g.MDNSForwardingEnabled != nil {
				body["mdnsForwardingEnabled"] = *g.MDNSForwardingEnabled
			}
		}
	case managementSwitch:
		if s := r.Switch; s != nil {
			body["cellularBackupEnabled"] = s.CellularBackupEnabled
			body["isolationEnabled"] = s.IsolationEnabled
			body["ipv4Configuration"] = s.IPv4Configuration
			body["deviceId"] = s.DeviceID
		}
	}
	return json.Marshal(body)
}

// Client is the subset of the UniFi Integration v1 API the operator consumes.
// It is defined here so reconcilers can depend on the behavior rather than the
// concrete HTTPClient.
type Client interface {
	// GetInfo returns the application info used as the reachability probe and
	// version-gating input.
	GetInfo(ctx context.Context) (Info, error)
	// ListSites returns every site managed by the console.
	ListSites(ctx context.Context) ([]Site, error)
	// ListNetworks returns every network on the given site.
	ListNetworks(ctx context.Context, siteID string) ([]Network, error)
	// GetNetwork returns the network with the given UUID on the given site.
	GetNetwork(ctx context.Context, siteID, networkID string) (Network, error)
	// CreateNetwork creates a network on the given site.
	CreateNetwork(ctx context.Context, siteID string, req NetworkRequest) (Network, error)
	// UpdateNetwork replaces the network with the given UUID on the given site.
	UpdateNetwork(ctx context.Context, siteID, networkID string, req NetworkRequest) (Network, error)
	// DeleteNetwork deletes the network with the given UUID on the given site.
	DeleteNetwork(ctx context.Context, siteID, networkID string) error
}

// APIError describes a non-successful response from the UniFi controller. The
// statusName, code, message, timestamp, requestPath, and requestID fields are
// decoded from the controller's flat Error Message body; they never contain
// credentials.
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// StatusName is the controller-provided status name (for example
	// "UNAUTHORIZED").
	StatusName string
	// Code is the controller-provided machine-readable error code.
	Code string
	// Message is the controller-provided error text.
	Message string
	// Timestamp is the controller-provided error timestamp.
	Timestamp string
	// RequestPath is the request path the controller reported.
	RequestPath string
	// RequestID is the controller-provided request ID, for server-side
	// correlation.
	RequestID string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Message != "" {
		return http.StatusText(e.StatusCode) + ": " + e.Message
	}
	return http.StatusText(e.StatusCode)
}

// Is reports whether the error matches a sentinel, so a 404 response matches
// ErrNotFound while remaining a typed *APIError.
func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// Retryable reports whether the failure is transient and the caller should back
// off and retry rather than surface a terminal condition. Client errors (4xx,
// except 429) are terminal.
func (e *APIError) Retryable() bool {
	return e.StatusCode >= http.StatusInternalServerError || e.StatusCode == http.StatusTooManyRequests
}

// validate reports whether the request carries a variant consistent with its
// Management discriminator.
func (r NetworkRequest) validate() error {
	switch r.Management {
	case managementGateway:
		if r.Gateway == nil || r.Switch != nil {
			return &InvalidSpecError{Field: managementField, Reason: "management GATEWAY requires only the gateway fields"}
		}
	case managementSwitch:
		if r.Switch == nil || r.Gateway != nil {
			return &InvalidSpecError{Field: managementField, Reason: "management SWITCH requires only the switch fields"}
		}
	case managementUnmanaged:
		if r.Gateway != nil || r.Switch != nil {
			return &InvalidSpecError{Field: managementField, Reason: "management UNMANAGED accepts no variant fields"}
		}
	default:
		return &InvalidSpecError{Field: managementField, Reason: fmt.Sprintf("%q is not a known management type", r.Management)}
	}
	return nil
}
