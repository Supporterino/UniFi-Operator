package unifi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// WifiNetworkReference is the upstream `Wifi network reference` union. The
// operator writes the SPECIFIC variant carrying the resolved network UUID;
// NATIVE is never authored. NetworkID is an opaque upstream UUID and must never
// reach an emitted Custom Resource's spec.
type WifiNetworkReference struct {
	// Type is the network-reference discriminator (NATIVE or SPECIFIC).
	Type string `json:"type"`
	// NetworkID is the upstream network UUID. It is set for type SPECIFIC.
	NetworkID string `json:"networkId,omitempty"`
}

// WifiBroadcastingDeviceFilter is the upstream `Broadcasting device filter`
// union. The operator writes the DEVICE_TAGS variant; the raw DEVICES variant
// carries opaque device UUIDs and cannot be represented in a Custom Resource.
type WifiBroadcastingDeviceFilter struct {
	// Type is the filter discriminator (DEVICES or DEVICE_TAGS).
	Type string `json:"type"`
	// DeviceTagIDs is the upstream device-tag UUIDs, set for type DEVICE_TAGS.
	DeviceTagIDs []string `json:"deviceTagIds,omitempty"`
	// DeviceIDs is the upstream device UUIDs, set for type DEVICES.
	DeviceIDs []string `json:"deviceIds,omitempty"`
}

// WifiSAEConfiguration configures SAE (Simultaneous Authentication of Equals).
type WifiSAEConfiguration struct {
	// AnticloggingThresholdSeconds is the anti-clogging threshold.
	AnticloggingThresholdSeconds int32 `json:"anticloggingThresholdSeconds"`
	// SyncTimeSeconds is the SAE sync time.
	SyncTimeSeconds int32 `json:"syncTimeSeconds"`
}

// WifiPresharedKey is a per-network preshared key. Passphrase is inline in the
// upstream wire format; an emitted Custom Resource carries a Secret reference
// instead, never the value.
type WifiPresharedKey struct {
	// Network is the upstream network the preshared key applies to.
	Network WifiNetworkReference `json:"network"`
	// Passphrase is the inline preshared key. It is a secret and is never
	// emitted.
	Passphrase string `json:"passphrase"`
}

// WifiRadiusNasID is the upstream RADIUS NAS-Identifier configuration.
type WifiRadiusNasID struct {
	// Type is the NAS-ID discriminator (DERIVED or USER_DEFINED).
	Type string `json:"type"`
	// Source is the derived NAS-ID source, set for type DERIVED.
	Source string `json:"source,omitempty"`
	// Value is the user-defined NAS-Identifier, set for type USER_DEFINED.
	Value string `json:"value,omitempty"`
}

// WifiRadiusMacAuthenticationConfiguration configures RADIUS MAC authentication.
type WifiRadiusMacAuthenticationConfiguration struct {
	// MacAddressFormat is the MAC address format presented to RADIUS.
	MacAddressFormat string `json:"macAddressFormat"`
}

// WifiRadiusConfiguration is the upstream RADIUS configuration. ProfileID is the
// opaque profile UUID and must never be emitted; the CLI replaces it with a
// placeholder reference. An OPEN or personal variant may also carry one (the
// CLI does not model that case and surfaces it via UnrepresentedSettings).
type WifiRadiusConfiguration struct {
	NasID                          WifiRadiusNasID                           `json:"nasId"`
	ProfileID                      string                                    `json:"profileId,omitempty"`
	MACAuthenticationConfiguration *WifiRadiusMacAuthenticationConfiguration `json:"macAuthenticationConfiguration,omitempty"`
}

// WifiSecurityConfiguration is the flat security configuration. The Type field
// selects which optional variant fields are meaningful. Passphrase and
// PresharedKeys hold inline upstream secrets and must never be emitted.
type WifiSecurityConfiguration struct {
	// Type is the security discriminator.
	Type string `json:"type"`
	// Encryption is the open-security encryption mode (OPEN variant).
	Encryption string `json:"encryption,omitempty"`
	// Passphrase is the inline personal passphrase. It is a secret.
	Passphrase string `json:"passphrase,omitempty"`
	// PresharedKeys lists per-network preshared keys (personal variants).
	PresharedKeys []WifiPresharedKey `json:"presharedKeys,omitempty"`
	// PmfMode is the Protected Management Frames mode.
	PmfMode string `json:"pmfMode,omitempty"`
	// SaeConfiguration configures SAE (personal WPA3 variants).
	SaeConfiguration *WifiSAEConfiguration `json:"saeConfiguration,omitempty"`
	// FastRoamingEnabled enables fast roaming (802.11r).
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`
	// GroupRekeyIntervalSeconds is the group rekey interval.
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`
	// CoaEnabled enables Change of Authorization (enterprise variants).
	CoaEnabled *bool `json:"coaEnabled,omitempty"`
	// SecurityMode is the WPA3 enterprise security mode.
	SecurityMode string `json:"securityMode,omitempty"`
	// Wpa3FastRoamingEnabled enables WPA3 fast roaming.
	Wpa3FastRoamingEnabled *bool `json:"wpa3FastRoamingEnabled,omitempty"`
	// RadiusConfiguration is the upstream RADIUS configuration. Enterprise variants
	// always carry one (mapped to a placeholder profile reference); an OPEN or
	// personal variant may also carry one, which the CLI does not model and surfaces
	// via UnrepresentedSettings rather than silently dropping.
	RadiusConfiguration *WifiRadiusConfiguration `json:"radiusConfiguration,omitempty"`
}

// WifiBroadcastOverview is a WiFi broadcast as returned by the list endpoint
// GET /v1/sites/{siteId}/wifi/broadcasts. The list is an overview only; the
// full variant configuration is returned by GetWifiBroadcast.
type WifiBroadcastOverview struct {
	// ID is the broadcast UUID.
	ID string `json:"id"`
	// Name is the human-readable broadcast name.
	Name string `json:"name"`
	// Enabled controls whether the broadcast is enabled.
	Enabled bool `json:"enabled"`
	// Type is the broadcast discriminator (STANDARD or IOT_OPTIMIZED).
	Type string `json:"type"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
	// Network references the upstream network.
	Network *WifiNetworkReference `json:"network,omitempty"`
	// SecurityConfiguration is the broadcast security variant.
	SecurityConfiguration *WifiSecurityConfiguration `json:"securityConfiguration,omitempty"`
	// BroadcastingDeviceFilter scopes the broadcast to a device set.
	BroadcastingDeviceFilter *WifiBroadcastingDeviceFilter `json:"broadcastingDeviceFilter,omitempty"`
	// BroadcastingFrequenciesGHz lists the enabled frequency bands in GHz
	// (STANDARD overview).
	BroadcastingFrequenciesGHz []float64 `json:"broadcastingFrequenciesGHz,omitempty"`
}

// WifiBroadcast is a WiFi broadcast as returned by the detail endpoint
// GET /v1/sites/{siteId}/wifi/broadcasts/{broadcastId}. It carries the full
// writable surface plus the observed ID and Metadata.
type WifiBroadcast struct {
	// ID is the broadcast UUID. It is observed upstream and must not reach a
	// spec.
	ID string `json:"id"`
	// Name is the human-readable broadcast (SSID) name.
	Name string `json:"name"`
	// Enabled controls whether the broadcast is enabled.
	Enabled bool `json:"enabled"`
	// HideName controls whether the SSID is hidden.
	HideName bool `json:"hideName"`
	// ClientIsolationEnabled isolates clients from each other.
	ClientIsolationEnabled bool `json:"clientIsolationEnabled"`
	// MulticastToUnicastConversionEnabled converts multicast to unicast.
	MulticastToUnicastConversionEnabled bool `json:"multicastToUnicastConversionEnabled"`
	// UapsdEnabled enables Unscheduled Automatic Power Save Delivery.
	UapsdEnabled bool `json:"uapsdEnabled"`
	// Channel2gLockedTo6 locks the 2.4 GHz channel to 6.
	Channel2gLockedTo6 bool `json:"channel2gLockedTo6"`
	// DtimPeriod2gLockedTo3 locks the 2.4 GHz DTIM period to 3.
	DtimPeriod2gLockedTo3 bool `json:"dtimPeriod2gLockedTo3"`
	// Type is the broadcast discriminator (STANDARD or IOT_OPTIMIZED).
	Type string `json:"type"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`
	// Network references the upstream network. NetworkID is an opaque UUID.
	Network *WifiNetworkReference `json:"network,omitempty"`
	// SecurityConfiguration is the broadcast security variant.
	SecurityConfiguration *WifiSecurityConfiguration `json:"securityConfiguration,omitempty"`
	// BroadcastingDeviceFilter scopes the broadcast to a device set.
	BroadcastingDeviceFilter *WifiBroadcastingDeviceFilter `json:"broadcastingDeviceFilter,omitempty"`

	// The following fields are only returned for type STANDARD.
	AdvertiseDeviceName        *bool     `json:"advertiseDeviceName,omitempty"`
	ArpProxyEnabled            *bool     `json:"arpProxyEnabled,omitempty"`
	BssTransitionEnabled       *bool     `json:"bssTransitionEnabled,omitempty"`
	BroadcastingFrequenciesGHz []float64 `json:"broadcastingFrequenciesGHz,omitempty"`

	// Unmodeled upstream detail fields. They are decoded only so the snapshot can
	// annotate and warn about them (design D8 reduces the emitted set); they are
	// never projected into a Custom Resource spec.
	BasicDataRateKbpsByFrequencyGHz  json.RawMessage `json:"basicDataRateKbpsByFrequencyGHz,omitempty"`
	BlackoutScheduleConfiguration    json.RawMessage `json:"blackoutScheduleConfiguration,omitempty"`
	ClientFilteringPolicy            json.RawMessage `json:"clientFilteringPolicy,omitempty"`
	MDNSProxyConfiguration           json.RawMessage `json:"mdnsProxyConfiguration,omitempty"`
	MulticastFilteringPolicy         json.RawMessage `json:"multicastFilteringPolicy,omitempty"`
	DNSAssistanceConfiguration       json.RawMessage `json:"dnsAssistanceConfiguration,omitempty"`
	DtimPeriodByFrequencyGHzOverride json.RawMessage `json:"dtimPeriodByFrequencyGHzOverride,omitempty"`
	HandoffSuggestionsConfiguration  json.RawMessage `json:"handoffSuggestionsConfiguration,omitempty"`
	HotspotConfiguration             json.RawMessage `json:"hotspotConfiguration,omitempty"`
	BandSteeringEnabled              json.RawMessage `json:"bandSteeringEnabled,omitempty"`
	MloEnabled                       json.RawMessage `json:"mloEnabled,omitempty"`
}

// UnrepresentedSettings returns the names of the upstream detail fields the CLI
// deliberately does not model (design D8's reduced emitted set) that are present
// on this broadcast. The snapshot annotates and warns about them so a setting is
// never silently dropped (cli/AGENTS.md, docs/crd-conventions.md Rule 6).
func (b WifiBroadcast) UnrepresentedSettings() []string {
	candidates := []struct {
		name string
		raw  json.RawMessage
	}{
		{"basicDataRateKbpsByFrequencyGHz", b.BasicDataRateKbpsByFrequencyGHz},
		{"blackoutScheduleConfiguration", b.BlackoutScheduleConfiguration},
		{"clientFilteringPolicy", b.ClientFilteringPolicy},
		{"mdnsProxyConfiguration", b.MDNSProxyConfiguration},
		{"multicastFilteringPolicy", b.MulticastFilteringPolicy},
		{"dnsAssistanceConfiguration", b.DNSAssistanceConfiguration},
		{"dtimPeriodByFrequencyGHzOverride", b.DtimPeriodByFrequencyGHzOverride},
		{"handoffSuggestionsConfiguration", b.HandoffSuggestionsConfiguration},
		{"hotspotConfiguration", b.HotspotConfiguration},
		{"bandSteeringEnabled", b.BandSteeringEnabled},
		{"mloEnabled", b.MloEnabled},
	}
	var names []string
	for _, candidate := range candidates {
		if rawMessagePresent(candidate.raw) {
			names = append(names, candidate.name)
		}
	}
	// The security union's RADIUS configuration is nested. Enterprise variants map
	// it to a placeholder profile reference (reported separately); an OPEN or
	// personal broadcast carrying one is reduced without it, so it is surfaced here.
	if b.SecurityConfiguration != nil &&
		b.SecurityConfiguration.RadiusConfiguration != nil &&
		!isEnterpriseSecurity(b.SecurityConfiguration.Type) {
		names = append(names, "securityConfiguration.radiusConfiguration")
	}
	return names
}

// isEnterpriseSecurity reports whether the security discriminator is one of the
// enterprise variants, whose RADIUS configuration is mapped rather than dropped.
func isEnterpriseSecurity(securityType string) bool {
	switch securityType {
	case "WPA2_ENTERPRISE", "WPA2_WPA3_ENTERPRISE", "WPA3_ENTERPRISE":
		return true
	default:
		return false
	}
}

// rawMessagePresent reports whether a decoded JSON field was present and not an
// explicit null.
func rawMessagePresent(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// ListWifiBroadcasts returns every WiFi broadcast overview on the given site.
// The list is an overview only; the variant configuration is read per broadcast
// with GetWifiBroadcast.
func (c *Client) ListWifiBroadcasts(ctx context.Context, siteID string) ([]WifiBroadcastOverview, error) {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts")
	if err != nil {
		return nil, err
	}
	broadcasts, err := listAll[WifiBroadcastOverview](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list wifi broadcasts: %w", err)
	}
	return broadcasts, nil
}

// GetWifiBroadcast returns the details of one WiFi broadcast on the given site.
// The list endpoint (ListWifiBroadcasts) returns only overview fields, so the
// variant configuration is only available through this detail endpoint. The
// returned WifiBroadcast carries the upstream id and network UUID that must not
// reach an emitted spec.
func (c *Client) GetWifiBroadcast(ctx context.Context, siteID, broadcastID string) (WifiBroadcast, error) {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts", broadcastID)
	if err != nil {
		return WifiBroadcast{}, err
	}
	var broadcast WifiBroadcast
	if err := c.do(ctx, http.MethodGet, endpoint, &broadcast); err != nil {
		return WifiBroadcast{}, fmt.Errorf("get wifi broadcast: %w", err)
	}
	return broadcast, nil
}
