package unifi

import (
	"context"
	"net/http"
)

// WifiNetworkReference is the upstream `Wifi network reference` union. The
// operator writes the SPECIFIC variant carrying the resolved network UUID;
// NATIVE is never authored. NetworkID is an opaque upstream UUID and stays out
// of a Custom Resource's spec.
type WifiNetworkReference struct {
	// Type is the network-reference discriminator (NATIVE or SPECIFIC).
	Type string `json:"type"`
	// NetworkID is the upstream network UUID. It is set for type SPECIFIC.
	NetworkID string `json:"networkId,omitempty"`
}

// WifiBroadcastingDeviceFilter is the upstream `Broadcasting device filter`
// union. The operator writes the DEVICE_TAGS variant carrying the resolved tag
// UUIDs; the raw DEVICES variant is modeled for decoding only and is never
// authored.
type WifiBroadcastingDeviceFilter struct {
	// Type is the filter discriminator (DEVICES or DEVICE_TAGS).
	Type string `json:"type"`
	// DeviceTagIDs is the upstream device-tag UUIDs, set for type DEVICE_TAGS.
	DeviceTagIDs []string `json:"deviceTagIds,omitempty"`
	// DeviceIDs is the upstream device UUIDs, set for type DEVICES.
	DeviceIDs []string `json:"deviceIds,omitempty"`
}

// WifiBasicDataRateConfiguration is the minimum basic data rate per frequency
// band in kbps. The JSON keys are the exact upstream band keys 2.4 and 5.
type WifiBasicDataRateConfiguration struct {
	// GHz2_4 is the 2.4 GHz minimum basic data rate in kbps.
	GHz2_4 int32 `json:"2.4"`
	// GHz5 is the 5 GHz minimum basic data rate in kbps.
	GHz5 int32 `json:"5"`
}

// WifiDtimPeriodConfiguration is the DTIM period per frequency band. The JSON
// keys are the exact upstream band keys 2.4, 5, and 6.
type WifiDtimPeriodConfiguration struct {
	// GHz2_4 is the 2.4 GHz DTIM period.
	GHz2_4 int32 `json:"2.4"`
	// GHz5 is the 5 GHz DTIM period.
	GHz5 int32 `json:"5"`
	// GHz6 is the 6 GHz DTIM period.
	GHz6 int32 `json:"6"`
}

// WifiBlackoutScheduleConfiguration is the blackout schedule: one entry per
// day of week.
type WifiBlackoutScheduleConfiguration struct {
	// Days lists the blackout windows per day.
	Days []WifiBlackoutScheduleDay `json:"days"`
}

// WifiBlackoutScheduleDay is the blackout window for a single day.
type WifiBlackoutScheduleDay struct {
	// Day is the day of week (SUN..SAT).
	Day string `json:"day"`
	// Type is the blackout discriminator (ALL_DAY or TIME_RANGE).
	Type string `json:"type"`
	// TimeRanges lists the blackout windows for type TIME_RANGE.
	TimeRanges []WifiBlackoutTimeRange `json:"timeRanges,omitempty"`
}

// WifiBlackoutTimeRange is a start/end blackout window in 24-hour HH:mm format.
type WifiBlackoutTimeRange struct {
	// StartTime is the window start in HH:mm format.
	StartTime string `json:"startTime"`
	// EndTime is the window end in HH:mm format.
	EndTime string `json:"endTime"`
}

// WifiClientFilteringPolicy allow- or block-lists client MAC addresses.
type WifiClientFilteringPolicy struct {
	// Action is the filtering policy action (ALLOW or BLOCK).
	Action string `json:"action"`
	// MacAddressFilter is the list of client MAC addresses.
	MacAddressFilter []string `json:"macAddressFilter"`
}

// WifiMDNSProxyConfiguration configures mDNS forwarding for a broadcast.
type WifiMDNSProxyConfiguration struct {
	// Mode is the mDNS discriminator (AUTO or CUSTOM).
	Mode string `json:"mode"`
	// Policies lists the mDNS proxy policies, set for mode CUSTOM.
	Policies []WifiMDNSProxyPolicy `json:"policies,omitempty"`
}

// WifiMDNSProxyPolicy is an mDNS proxy policy (ALLOW or BLOCK).
type WifiMDNSProxyPolicy struct {
	// Action is the proxy policy action (ALLOW or BLOCK).
	Action string `json:"action"`
	// DeviceFilter scopes the policy to matching devices.
	DeviceFilter *WifiBroadcastingDeviceFilter `json:"deviceFilter,omitempty"`
	// BridgingNetworkIDs lists the network UUIDs whose mDNS traffic is bridged;
	// only valid for action ALLOW.
	BridgingNetworkIDs []string `json:"bridgingNetworkIds,omitempty"`
	// ServiceFilter lists the mDNS services the policy applies to; only valid
	// for action ALLOW.
	ServiceFilter []WifiMDNSService `json:"serviceFilter,omitempty"`
}

// WifiMDNSService is an mDNS service (CUSTOM or PREDEFINED). Name carries the
// built-in identifier for PREDEFINED and the service name for CUSTOM;
// TypeDomain is set only for CUSTOM.
type WifiMDNSService struct {
	// Type is the service discriminator (CUSTOM or PREDEFINED).
	Type string `json:"type"`
	// Name is the service name or built-in identifier.
	Name string `json:"name"`
	// TypeDomain is the mDNS service type domain, set for type CUSTOM.
	TypeDomain string `json:"typeDomain,omitempty"`
}

// WifiMulticastFilteringPolicy is a multicast filtering policy (ALLOW or
// BLOCK).
type WifiMulticastFilteringPolicy struct {
	// Action is the filtering policy action (ALLOW or BLOCK).
	Action string `json:"action"`
	// SourceMacAddressFilter lists the allowed multicast source MAC addresses;
	// only valid for action ALLOW.
	SourceMacAddressFilter []string `json:"sourceMacAddressFilter,omitempty"`
}

// WifiDNSAssistanceConfiguration is the DNS assistance configuration (AUTO or
// MANUAL). Servers is set only for mode MANUAL.
type WifiDNSAssistanceConfiguration struct {
	// Mode is the DNS assistance discriminator (AUTO or MANUAL).
	Mode string `json:"mode"`
	// Servers lists up to two failover DNS servers; only valid for mode MANUAL.
	Servers []string `json:"servers,omitempty"`
}

// WifiHandoffSuggestionsConfiguration suggests low-signal clients to roam to a
// better access point. The thresholds are in dBm.
type WifiHandoffSuggestionsConfiguration struct {
	// Band5GHzRssiThreshold is the 5 GHz RSSI threshold in dBm.
	Band5GHzRssiThreshold *int32 `json:"band5GHzRssiThreshold,omitempty"`
	// Band6GHzRssiThreshold is the 6 GHz RSSI threshold in dBm.
	Band6GHzRssiThreshold *int32 `json:"band6GHzRssiThreshold,omitempty"`
}

// WifiHotspotConfiguration configures a broadcast hotspot (CAPTIVE_PORTAL or
// PASSPOINT).
type WifiHotspotConfiguration struct {
	// Type is the hotspot discriminator (CAPTIVE_PORTAL or PASSPOINT).
	Type string `json:"type"`
}

// WifiSAEConfiguration configures SAE (Simultaneous Authentication of Equals).
type WifiSAEConfiguration struct {
	// AnticloggingThresholdSeconds is the anti-clogging threshold.
	AnticloggingThresholdSeconds int32 `json:"anticloggingThresholdSeconds"`
	// SyncTimeSeconds is the SAE sync time.
	SyncTimeSeconds int32 `json:"syncTimeSeconds"`
}

// WifiPresharedKey is a per-network preshared key. Passphrase is inline in the
// upstream wire format; the CR carries it as a Secret reference and the
// reconciler resolves it at request time. It is never logged.
type WifiPresharedKey struct {
	// Network is the upstream network the preshared key applies to.
	Network WifiNetworkReference `json:"network"`
	// Passphrase is the inline preshared key. Treat as sensitive; never log.
	Passphrase string `json:"passphrase"`
}

// WifiRadiusNasID is the NAS-Identifier configuration (DERIVED or
// USER_DEFINED). Source is set for DERIVED; Value for USER_DEFINED.
type WifiRadiusNasID struct {
	// Type is the NAS-ID discriminator (DERIVED or USER_DEFINED).
	Type string `json:"type"`
	// Source is the derived NAS-ID source, set for type DERIVED.
	Source string `json:"source,omitempty"`
	// Value is the user-defined NAS-Identifier, set for type USER_DEFINED.
	Value string `json:"value,omitempty"`
}

// WifiRadiusMacAuthenticationConfiguration configures RADIUS MAC
// authentication.
type WifiRadiusMacAuthenticationConfiguration struct {
	// MacAddressFormat is the MAC address format presented to RADIUS.
	MacAddressFormat string `json:"macAddressFormat"`
}

// WifiRadiusConfiguration is the RADIUS configuration shared by the
// enterprise and non-enterprise security variants. ProfileID is the opaque
// upstream RADIUS profile UUID; the CR exposes it as a UnifiRadiusProfile
// reference and the reconciler resolves it at request time.
type WifiRadiusConfiguration struct {
	// ProfileID is the upstream RADIUS profile UUID.
	ProfileID string `json:"profileId"`
	// NasID configures the RADIUS NAS-Identifier.
	NasID WifiRadiusNasID `json:"nasId"`
	// MACAuthenticationConfiguration configures RADIUS MAC authentication;
	// required for the non-enterprise variants and optional for enterprise.
	MACAuthenticationConfiguration *WifiRadiusMacAuthenticationConfiguration `json:"macAuthenticationConfiguration,omitempty"`
}

// WifiSecurityConfiguration is the flat security configuration. The Type field
// selects which of the optional variant fields are meaningful; a single flat
// struct mirrors the upstream flattened security union. Passphrase and
// PresharedKeys hold inline upstream secrets and must never be logged.
type WifiSecurityConfiguration struct {
	// Type is the security discriminator (OPEN, WPA2_PERSONAL,
	// WPA2_WPA3_PERSONAL, WPA3_PERSONAL, WPA2_ENTERPRISE,
	// WPA2_WPA3_ENTERPRISE, or WPA3_ENTERPRISE).
	Type string `json:"type"`

	// Encryption is the open-security encryption mode (OPEN variant).
	Encryption string `json:"encryption,omitempty"`
	// Passphrase is the inline personal passphrase. Treat as sensitive; never
	// log.
	Passphrase string `json:"passphrase,omitempty"`
	// PresharedKeys lists per-network preshared keys (personal variants).
	PresharedKeys []WifiPresharedKey `json:"presharedKeys,omitempty"`
	// PmfMode is the Protected Management Frames mode.
	PmfMode string `json:"pmfMode,omitempty"`
	// SaeConfiguration configures SAE (WPA2_WPA3_PERSONAL/WPA3_PERSONAL).
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
	// RadiusConfiguration configures RADIUS for the enterprise and
	// non-enterprise variants.
	RadiusConfiguration *WifiRadiusConfiguration `json:"radiusConfiguration,omitempty"`
}

// WifiBroadcastRequest is the flat `Create or update Wifi broadcast` body. The
// Type discriminator selects the STANDARD or IOT_OPTIMIZED variant; the
// STANDARD-only fields are optional here so an IOT_OPTIMIZED body omits them.
// The body is flat (there is no nested variant object), matching the upstream
// allOf union. It intentionally carries upstream identifiers (network UUIDs,
// device-tag UUIDs, RADIUS profile UUID) and inline passphrases: the reconciler
// resolves references and reads Secrets before building it, and never logs it.
type WifiBroadcastRequest struct {
	// Type is the broadcast discriminator (STANDARD or IOT_OPTIMIZED).
	Type string `json:"type"`
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

	// Network references the upstream network (SPECIFIC variant).
	Network *WifiNetworkReference `json:"network,omitempty"`
	// SecurityConfiguration is the broadcast security variant.
	SecurityConfiguration *WifiSecurityConfiguration `json:"securityConfiguration,omitempty"`
	// BroadcastingDeviceFilter scopes the broadcast to a device set
	// (DEVICE_TAGS variant). Omitting it broadcasts on all AP-capable devices.
	BroadcastingDeviceFilter *WifiBroadcastingDeviceFilter `json:"broadcastingDeviceFilter,omitempty"`

	// BasicDataRateKbpsByFrequencyGHz sets the minimum basic data rates.
	BasicDataRateKbpsByFrequencyGHz *WifiBasicDataRateConfiguration `json:"basicDataRateKbpsByFrequencyGHz,omitempty"`
	// BlackoutScheduleConfiguration disables the broadcast on a schedule.
	BlackoutScheduleConfiguration *WifiBlackoutScheduleConfiguration `json:"blackoutScheduleConfiguration,omitempty"`
	// ClientFilteringPolicy allow- or block-lists client MAC addresses.
	ClientFilteringPolicy *WifiClientFilteringPolicy `json:"clientFilteringPolicy,omitempty"`
	// MDNSProxyConfiguration configures mDNS forwarding.
	MDNSProxyConfiguration *WifiMDNSProxyConfiguration `json:"mdnsProxyConfiguration,omitempty"`
	// MulticastFilteringPolicy configures multicast filtering.
	MulticastFilteringPolicy *WifiMulticastFilteringPolicy `json:"multicastFilteringPolicy,omitempty"`

	// The following fields are only valid for type STANDARD.
	//
	// AdvertiseDeviceName advertises the device name in beacon frames.
	AdvertiseDeviceName *bool `json:"advertiseDeviceName,omitempty"`
	// ArpProxyEnabled enables ARP proxying.
	ArpProxyEnabled *bool `json:"arpProxyEnabled,omitempty"`
	// BssTransitionEnabled enables BSS transition management.
	BssTransitionEnabled *bool `json:"bssTransitionEnabled,omitempty"`
	// BroadcastingFrequenciesGHz lists the frequency bands (2.4, 5, 6) in GHz.
	BroadcastingFrequenciesGHz []float64 `json:"broadcastingFrequenciesGHz,omitempty"`
	// BandSteeringEnabled enables band steering.
	BandSteeringEnabled *bool `json:"bandSteeringEnabled,omitempty"`
	// MloEnabled enables Multi-Link Operation.
	MloEnabled *bool `json:"mloEnabled,omitempty"`
	// DNSAssistanceConfiguration configures DNS assistance.
	DNSAssistanceConfiguration *WifiDNSAssistanceConfiguration `json:"dnsAssistanceConfiguration,omitempty"`
	// DtimPeriodByFrequencyGHzOverride overrides the DTIM period per band.
	DtimPeriodByFrequencyGHzOverride *WifiDtimPeriodConfiguration `json:"dtimPeriodByFrequencyGHzOverride,omitempty"`
	// HandoffSuggestionsConfiguration suggests low-signal clients to roam.
	HandoffSuggestionsConfiguration *WifiHandoffSuggestionsConfiguration `json:"handoffSuggestionsConfiguration,omitempty"`
	// HotspotConfiguration configures a hotspot.
	HotspotConfiguration *WifiHotspotConfiguration `json:"hotspotConfiguration,omitempty"`
}

// WifiBroadcastOverview is a WiFi broadcast as returned by the list endpoint
// GET /v1/sites/{siteId}/wifi/broadcasts. The list is an overview only: the
// variant configuration is returned by the detail endpoint (GetWifiBroadcast).
// ID and Metadata are observed and must never reach a CR's spec.
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

	// The following fields are only returned for type STANDARD.
	//
	// BroadcastingFrequenciesGHz lists the enabled frequency bands in GHz.
	BroadcastingFrequenciesGHz []float64 `json:"broadcastingFrequenciesGHz,omitempty"`
	// HotspotConfiguration is the broadcast hotspot configuration.
	HotspotConfiguration *WifiHotspotConfiguration `json:"hotspotConfiguration,omitempty"`
}

// WifiBroadcast is a WiFi broadcast as returned by the detail endpoint
// GET /v1/sites/{siteId}/wifi/broadcasts/{broadcastId}. It carries the full
// writable surface plus the observed ID and Metadata, which must never reach a
// CR's spec.
type WifiBroadcast struct {
	// ID is the broadcast UUID. It is observed upstream.
	ID string `json:"id"`
	// Metadata is the observed entity metadata (origin).
	Metadata NetworkMetadata `json:"metadata"`

	WifiBroadcastRequest
}

// ListWifiBroadcasts returns every WiFi broadcast (overview) on the given site.
func (c *HTTPClient) ListWifiBroadcasts(ctx context.Context, siteID string) ([]WifiBroadcastOverview, error) {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts")
	if err != nil {
		return nil, err
	}
	return listAll[WifiBroadcastOverview](ctx, c, endpoint)
}

// GetWifiBroadcast returns the WiFi broadcast with the given UUID on the given
// site. The detail response carries the full variant configuration.
func (c *HTTPClient) GetWifiBroadcast(ctx context.Context, siteID, broadcastID string) (WifiBroadcast, error) {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts", broadcastID)
	if err != nil {
		return WifiBroadcast{}, err
	}
	var broadcast WifiBroadcast
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &broadcast); err != nil {
		return WifiBroadcast{}, err
	}
	return broadcast, nil
}

// CreateWifiBroadcast creates a WiFi broadcast on the given site.
func (c *HTTPClient) CreateWifiBroadcast(ctx context.Context, siteID string, req WifiBroadcastRequest) (WifiBroadcast, error) {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts")
	if err != nil {
		return WifiBroadcast{}, err
	}
	var broadcast WifiBroadcast
	if err := c.do(ctx, http.MethodPost, endpoint, req, &broadcast); err != nil {
		return WifiBroadcast{}, err
	}
	return broadcast, nil
}

// UpdateWifiBroadcast replaces the WiFi broadcast with the given UUID on the
// given site.
func (c *HTTPClient) UpdateWifiBroadcast(ctx context.Context, siteID, broadcastID string, req WifiBroadcastRequest) (WifiBroadcast, error) {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts", broadcastID)
	if err != nil {
		return WifiBroadcast{}, err
	}
	var broadcast WifiBroadcast
	if err := c.do(ctx, http.MethodPut, endpoint, req, &broadcast); err != nil {
		return WifiBroadcast{}, err
	}
	return broadcast, nil
}

// DeleteWifiBroadcast deletes the WiFi broadcast with the given UUID on the
// given site.
func (c *HTTPClient) DeleteWifiBroadcast(ctx context.Context, siteID, broadcastID string) error {
	endpoint, err := c.endpoint("sites", siteID, "wifi", "broadcasts", broadcastID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}
