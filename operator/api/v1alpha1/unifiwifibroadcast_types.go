/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnifiWifiBroadcastFinalizer blocks removal of a UnifiWifiBroadcast until its
// upstream broadcast has been deleted. The broadcast reconciler adds it before
// creating the upstream broadcast and removes it after the upstream broadcast
// is gone.
const UnifiWifiBroadcastFinalizer = "unifi.supporterino.de/wifi-broadcast-finalizer"

// +kubebuilder:validation:XValidation:rule="self.type == 'STANDARD' ? has(self.standard) : !has(self.standard)",message="standard fields require type STANDARD"
// +kubebuilder:validation:XValidation:rule="self.type == 'IOT_OPTIMIZED' ? has(self.iotOptimized) : !has(self.iotOptimized)",message="iotOptimized fields require type IOT_OPTIMIZED"

// UnifiWifiBroadcastSpec defines the desired state of UnifiWifiBroadcast. It
// mirrors the discriminated `Wifi broadcast create or update` union of the
// UniFi Integration v1 API (spec v10.4.57). Raw upstream identifiers are
// replaced by Kubernetes references: the network UUID by networkRef, the
// device-tag filter by deviceTags, and every passphrase by a Secret reference.
// The upstream NATIVE network variant and the raw DEVICES broadcast filter are
// deliberately not exposed.
type UnifiWifiBroadcastSpec struct {
	// NetworkRef references the UnifiNetwork that owns this broadcast. The
	// reference resolves in the same namespace and determines both the
	// ownerReference and the upstream network. It is immutable: re-pointing a
	// broadcast to another network is delete-and-recreate.
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="networkRef is immutable"
	NetworkRef CoreRef `json:"networkRef"`

	// Type is the broadcast union discriminator (STANDARD or IOT_OPTIMIZED).
	// +kubebuilder:validation:Enum=STANDARD;IOT_OPTIMIZED
	Type string `json:"type"`

	// Name is the human-readable broadcast (SSID) name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Enabled controls whether the broadcast is enabled.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// HideName controls whether the SSID is hidden from scans.
	HideName bool `json:"hideName"`

	// ClientIsolationEnabled isolates clients on this broadcast from each other.
	ClientIsolationEnabled bool `json:"clientIsolationEnabled"`

	// MulticastToUnicastConversionEnabled converts multicast traffic to unicast.
	MulticastToUnicastConversionEnabled bool `json:"multicastToUnicastConversionEnabled"`

	// UapsdEnabled enables Unscheduled Automatic Power Save Delivery.
	UapsdEnabled bool `json:"uapsdEnabled"`

	// Channel2gLockedTo6 locks the 2.4 GHz radio channel to 6 on all
	// broadcasting devices.
	// +kubebuilder:default=false
	// +optional
	Channel2gLockedTo6 *bool `json:"channel2gLockedTo6,omitempty"`

	// DtimPeriod2gLockedTo3 locks the DTIM period to 3 for the 2.4 GHz radio.
	// +kubebuilder:default=false
	// +optional
	DtimPeriod2gLockedTo3 *bool `json:"dtimPeriod2gLockedTo3,omitempty"`

	// SecurityConfiguration is the security variant for this broadcast.
	SecurityConfiguration WifiSecurityConfiguration `json:"securityConfiguration"`

	// DeviceTags scopes the broadcast to the devices carrying the named tags.
	// Omitting it broadcasts on all AP-capable devices. The raw upstream
	// DEVICES filter is not exposed.
	// +optional
	// +kubebuilder:validation:MinItems=1
	DeviceTags []DeviceTagSelector `json:"deviceTags,omitempty"`

	// BasicDataRateKbpsByFrequencyGHz sets the minimum basic data rates per
	// frequency band in kbps.
	// +optional
	BasicDataRateKbpsByFrequencyGHz *WifiBasicDataRateConfiguration `json:"basicDataRateKbpsByFrequencyGHz,omitempty"`

	// BlackoutScheduleConfiguration disables the broadcast during scheduled
	// periods.
	// +optional
	BlackoutScheduleConfiguration *WifiBlackoutScheduleConfiguration `json:"blackoutScheduleConfiguration,omitempty"`

	// ClientFilteringPolicy allow- or block-lists client MAC addresses.
	// +optional
	ClientFilteringPolicy *WifiClientFilteringPolicy `json:"clientFilteringPolicy,omitempty"`

	// MDNSProxyConfiguration configures mDNS traffic forwarding.
	// +optional
	MDNSProxyConfiguration *WifiMDNSProxyConfiguration `json:"mdnsProxyConfiguration,omitempty"`

	// MulticastFilteringPolicy configures multicast filtering.
	// +optional
	MulticastFilteringPolicy *WifiMulticastFilteringPolicy `json:"multicastFilteringPolicy,omitempty"`

	// Standard holds the STANDARD variant fields. It is required for type
	// STANDARD and forbidden otherwise (CEL).
	// +optional
	Standard *StandardWifiOptions `json:"standard,omitempty"`

	// IotOptimized holds the IOT_OPTIMIZED variant fields. It is required for
	// type IOT_OPTIMIZED and forbidden otherwise (CEL). The v10.4.57 union
	// defines no IOT-only properties, so the variant is an empty marker object.
	// +optional
	IotOptimized *IotOptimizedWifiOptions `json:"iotOptimized,omitempty"`
}

// StandardWifiOptions holds the fields the STANDARD broadcast variant defines
// on top of the common broadcast shape.
type StandardWifiOptions struct {
	// AdvertiseDeviceName advertises the device name in beacon frames.
	AdvertiseDeviceName bool `json:"advertiseDeviceName"`

	// ArpProxyEnabled enables ARP proxying for this broadcast.
	ArpProxyEnabled bool `json:"arpProxyEnabled"`

	// BssTransitionEnabled enables BSS transition management (802.11v).
	BssTransitionEnabled bool `json:"bssTransitionEnabled"`

	// BroadcastingFrequenciesGHz lists the frequency bands (2.4, 5, 6) the
	// broadcast is enabled on.
	// +kubebuilder:validation:MinItems=1
	BroadcastingFrequenciesGHz []WifiBroadcastingFrequencyGHz `json:"broadcastingFrequenciesGHz"`

	// BandSteeringEnabled enables band steering. Omit to use the site default.
	// +optional
	BandSteeringEnabled *bool `json:"bandSteeringEnabled,omitempty"`

	// MloEnabled enables Multi-Link Operation. Omit to use the site default.
	// +optional
	MloEnabled *bool `json:"mloEnabled,omitempty"`

	// DNSAssistanceConfiguration configures DNS assistance. Omit to disable.
	// +optional
	DNSAssistanceConfiguration *WifiDNSAssistanceConfiguration `json:"dnsAssistanceConfiguration,omitempty"`

	// DtimPeriodByFrequencyGHzOverride overrides the DTIM period per band.
	// +optional
	DtimPeriodByFrequencyGHzOverride *WifiDtimPeriodConfiguration `json:"dtimPeriodByFrequencyGHzOverride,omitempty"`

	// HandoffSuggestionsConfiguration suggests low-signal clients to roam to a
	// better access point. Omit to disable.
	// +optional
	HandoffSuggestionsConfiguration *WifiHandoffSuggestionsConfiguration `json:"handoffSuggestionsConfiguration,omitempty"`

	// HotspotConfiguration configures a hotspot (captive portal or Passpoint).
	// +optional
	HotspotConfiguration *WifiHotspotConfiguration `json:"hotspotConfiguration,omitempty"`
}

// WifiBroadcastingFrequencyGHz is a WiFi broadcasting frequency in GHz. The
// frozen v10.4.57 union only allows 2.4, 5, and 6 GHz. It is modeled as an
// exact string enum because controller-gen rejects floating-point CRD fields
// (and the repo deliberately does not enable crd:allowDangerousTypes); the
// reconciler maps the value to the numeric upstream representation.
// +kubebuilder:validation:Enum={"2.4","5","6"}
type WifiBroadcastingFrequencyGHz string

// IotOptimizedWifiOptions holds the fields the IOT_OPTIMIZED broadcast variant
// defines on top of the common broadcast shape. The v10.4.57 union adds no
// IOT-only properties (the variant is the base shape with IoT-tuned required
// fields), so the struct is intentionally empty and exists as the
// discriminator variant.
type IotOptimizedWifiOptions struct{}

// WifiBasicDataRateConfiguration sets the minimum basic data rate per
// frequency band in kbps. The frozen v10.4.57 union models only the 2.4 GHz and
// 5 GHz bands.
type WifiBasicDataRateConfiguration struct {
	// GHz2_4 is the 2.4 GHz minimum basic data rate in kbps.
	// +kubebuilder:validation:Enum=1000;2000;5500;6000;9000;11000;12000;24000
	GHz2_4 int32 `json:"2.4"`

	// GHz5 is the 5 GHz minimum basic data rate in kbps.
	// +kubebuilder:validation:Enum=6000;9000;12000;24000
	GHz5 int32 `json:"5"`
}

// WifiDtimPeriodConfiguration sets the DTIM period per frequency band. All
// three bands are required when the override is set.
type WifiDtimPeriodConfiguration struct {
	// GHz2_4 is the 2.4 GHz DTIM period (1..255).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	GHz2_4 int32 `json:"2.4"`

	// GHz5 is the 5 GHz DTIM period (1..255).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	GHz5 int32 `json:"5"`

	// GHz6 is the 6 GHz DTIM period (1..255).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=255
	GHz6 int32 `json:"6"`
}

// WifiBlackoutScheduleConfiguration disables a broadcast during scheduled
// periods. At least one day entry is required.
type WifiBlackoutScheduleConfiguration struct {
	// Days lists the blackout windows per day.
	// +kubebuilder:validation:MinItems=1
	Days []WifiBlackoutScheduleDay `json:"days"`
}

// +kubebuilder:validation:XValidation:rule="self.type == 'TIME_RANGE' ? has(self.timeRanges) : !has(self.timeRanges)",message="timeRanges require type TIME_RANGE"

// WifiBlackoutScheduleDay is the discriminated blackout window for a single
// day (ALL_DAY or TIME_RANGE).
type WifiBlackoutScheduleDay struct {
	// Day is the day of week the window applies to.
	// +kubebuilder:validation:Enum=SUN;MON;TUE;WED;THU;FRI;SAT
	Day string `json:"day"`

	// Type is the blackout discriminator (ALL_DAY or TIME_RANGE).
	// +kubebuilder:validation:Enum=ALL_DAY;TIME_RANGE
	Type string `json:"type"`

	// TimeRanges lists the blackout windows. Required for type TIME_RANGE.
	// +optional
	// +kubebuilder:validation:MinItems=1
	TimeRanges []WifiBlackoutTimeRange `json:"timeRanges,omitempty"`
}

// WifiBlackoutTimeRange is a start/end blackout window in 24-hour HH:mm format.
type WifiBlackoutTimeRange struct {
	// StartTime is the window start in HH:mm format.
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	StartTime string `json:"startTime"`

	// EndTime is the window end in HH:mm format.
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	EndTime string `json:"endTime"`
}

// WifiClientFilteringPolicy allow- or block-lists client MAC addresses for a
// broadcast.
type WifiClientFilteringPolicy struct {
	// Action is the filtering policy action (ALLOW or BLOCK).
	// +kubebuilder:validation:Enum=ALLOW;BLOCK
	Action string `json:"action"`

	// MacAddressFilter is the list of client MAC addresses the policy applies
	// to. It may be empty.
	// +kubebuilder:validation:MaxItems=512
	MacAddressFilter []string `json:"macAddressFilter"`
}

// +kubebuilder:validation:XValidation:rule="self.mode == 'CUSTOM' ? has(self.policies) : !has(self.policies)",message="policies require mode CUSTOM"

// WifiMDNSProxyConfiguration configures mDNS forwarding for a broadcast.
type WifiMDNSProxyConfiguration struct {
	// Mode is the mDNS discriminator (AUTO or CUSTOM).
	// +kubebuilder:validation:Enum=AUTO;CUSTOM
	Mode string `json:"mode"`

	// Policies lists the mDNS proxy policies. Required for mode CUSTOM.
	// +optional
	// +kubebuilder:validation:MinItems=1
	Policies []WifiMDNSProxyPolicy `json:"policies,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.action == 'ALLOW' ? true : (!has(self.bridgingNetworkIds) && !has(self.serviceFilter))",message="bridgingNetworkIds and serviceFilter require action ALLOW"

// WifiMDNSProxyPolicy is the discriminated mDNS proxy policy (ALLOW or BLOCK).
// The bridging network and service filter are only valid for ALLOW.
type WifiMDNSProxyPolicy struct {
	// Action is the proxy policy action (ALLOW or BLOCK).
	// +kubebuilder:validation:Enum=ALLOW;BLOCK
	Action string `json:"action"`

	// DeviceTags scopes the policy to devices carrying the named tags. Omitting
	// it applies the policy to all AP-capable devices.
	// +optional
	// +kubebuilder:validation:MinItems=1
	DeviceTags []DeviceTagSelector `json:"deviceTags,omitempty"`

	// BridgingNetworkRefs lists the UnifiNetwork resources whose mDNS traffic
	// is bridged. Only valid for action ALLOW.
	// +optional
	// +kubebuilder:validation:MinItems=1
	BridgingNetworkRefs []CoreRef `json:"bridgingNetworkIds,omitempty"`

	// ServiceFilter lists the mDNS services the policy applies to. Only valid
	// for action ALLOW.
	// +optional
	// +kubebuilder:validation:MinItems=1
	ServiceFilter []WifiMDNSService `json:"serviceFilter,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.type == 'CUSTOM' ? has(self.custom) : !has(self.custom)",message="custom fields require type CUSTOM"
// +kubebuilder:validation:XValidation:rule="self.type == 'PREDEFINED' ? has(self.predefined) : !has(self.predefined)",message="predefined fields require type PREDEFINED"

// WifiMDNSService is the discriminated mDNS service (CUSTOM or PREDEFINED).
type WifiMDNSService struct {
	// Type is the service discriminator (CUSTOM or PREDEFINED).
	// +kubebuilder:validation:Enum=CUSTOM;PREDEFINED
	Type string `json:"type"`

	// Custom is the user-defined service. Required for type CUSTOM.
	// +optional
	Custom *WifiMDNSCustomService `json:"custom,omitempty"`

	// Predefined is the built-in service. Required for type PREDEFINED.
	// +optional
	Predefined *WifiMDNSPredefinedService `json:"predefined,omitempty"`
}

// WifiMDNSCustomService is a user-defined mDNS service.
type WifiMDNSCustomService struct {
	// Name is the mDNS service name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// TypeDomain is the mDNS service type domain (for example _http._tcp).
	// +kubebuilder:validation:MinLength=1
	TypeDomain string `json:"typeDomain"`
}

// WifiMDNSPredefinedService is a built-in mDNS service.
type WifiMDNSPredefinedService struct {
	// Name is the built-in service identifier.
	// +kubebuilder:validation:Enum=AMAZON_DEVICES;ANDROID_TV_REMOTE;APPLE_AIR_DROP;APPLE_AIR_PLAY;APPLE_FILE_SHARING;APPLE_ICHAT;APPLE_ITUNES;AQARA;BOSE;DNS_SERVICE_DISCOVERY;FTP_SERVERS;GOOGLE_CHROMECAST;HOMEKIT;MATTER_NETWORK;PHILIPS_HUE;PRINTERS;ROKU;SCANNERS;SONOS;SPOTIFY_CONNECT;SSH_SERVERS;TIME_CAPSULE;WEB_SERVERS;WINDOWS_FILE_SHARING_SAMBA
	Name string `json:"name"`
}

// +kubebuilder:validation:XValidation:rule="self.action == 'ALLOW' ? true : !has(self.sourceMacAddressFilter)",message="sourceMacAddressFilter requires action ALLOW"

// WifiMulticastFilteringPolicy is the discriminated multicast filtering policy
// (ALLOW or BLOCK). The source MAC filter is only valid for ALLOW.
type WifiMulticastFilteringPolicy struct {
	// Action is the filtering policy action (ALLOW or BLOCK).
	// +kubebuilder:validation:Enum=ALLOW;BLOCK
	Action string `json:"action"`

	// SourceMacAddressFilter lists the allowed multicast source MAC addresses.
	// Only valid for action ALLOW.
	// +optional
	// +kubebuilder:validation:MaxItems=256
	SourceMacAddressFilter []string `json:"sourceMacAddressFilter,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.mode == 'MANUAL' ? true : !has(self.servers)",message="servers require mode MANUAL"

// WifiDNSAssistanceConfiguration is the discriminated DNS assistance
// configuration (AUTO or MANUAL). The failover servers are only valid for
// MANUAL.
type WifiDNSAssistanceConfiguration struct {
	// Mode is the DNS assistance discriminator (AUTO or MANUAL).
	// +kubebuilder:validation:Enum=AUTO;MANUAL
	Mode string `json:"mode"`

	// Servers lists up to two failover DNS servers. Only valid for mode MANUAL.
	// +optional
	// +kubebuilder:validation:MaxItems=2
	Servers []string `json:"servers,omitempty"`
}

// WifiHandoffSuggestionsConfiguration suggests low-signal clients to roam to a
// better access point. Both thresholds are in dBm and independent.
type WifiHandoffSuggestionsConfiguration struct {
	// Band5GHzRssiThreshold is the 5 GHz RSSI threshold in dBm.
	// +optional
	// +kubebuilder:validation:Minimum=-80
	// +kubebuilder:validation:Maximum=-60
	Band5GHzRssiThreshold *int32 `json:"band5GHzRssiThreshold,omitempty"`

	// Band6GHzRssiThreshold is the 6 GHz RSSI threshold in dBm.
	// +optional
	// +kubebuilder:validation:Minimum=-90
	// +kubebuilder:validation:Maximum=-70
	Band6GHzRssiThreshold *int32 `json:"band6GHzRssiThreshold,omitempty"`
}

// WifiHotspotConfiguration configures a broadcast hotspot. The frozen v10.4.57
// detail union carries no additional fields for either variant.
type WifiHotspotConfiguration struct {
	// Type is the hotspot discriminator (CAPTIVE_PORTAL or PASSPOINT).
	// +kubebuilder:validation:Enum=CAPTIVE_PORTAL;PASSPOINT
	Type string `json:"type"`
}

// +kubebuilder:validation:XValidation:rule="self.type == 'OPEN' ? has(self.open) : !has(self.open)",message="open fields require type OPEN"
// +kubebuilder:validation:XValidation:rule="self.type == 'WPA2_PERSONAL' ? has(self.wpa2Personal) : !has(self.wpa2Personal)",message="wpa2Personal fields require type WPA2_PERSONAL"
// +kubebuilder:validation:XValidation:rule="self.type == 'WPA2_WPA3_PERSONAL' ? has(self.wpa2Wpa3Personal) : !has(self.wpa2Wpa3Personal)",message="wpa2Wpa3Personal fields require type WPA2_WPA3_PERSONAL"
// +kubebuilder:validation:XValidation:rule="self.type == 'WPA3_PERSONAL' ? has(self.wpa3Personal) : !has(self.wpa3Personal)",message="wpa3Personal fields require type WPA3_PERSONAL"
// +kubebuilder:validation:XValidation:rule="self.type == 'WPA2_ENTERPRISE' ? has(self.wpa2Enterprise) : !has(self.wpa2Enterprise)",message="wpa2Enterprise fields require type WPA2_ENTERPRISE"
// +kubebuilder:validation:XValidation:rule="self.type == 'WPA2_WPA3_ENTERPRISE' ? has(self.wpa2Wpa3Enterprise) : !has(self.wpa2Wpa3Enterprise)",message="wpa2Wpa3Enterprise fields require type WPA2_WPA3_ENTERPRISE"
// +kubebuilder:validation:XValidation:rule="self.type == 'WPA3_ENTERPRISE' ? has(self.wpa3Enterprise) : !has(self.wpa3Enterprise)",message="wpa3Enterprise fields require type WPA3_ENTERPRISE"

// WifiSecurityConfiguration is the seven-variant broadcast security union. The
// discriminator selects exactly one variant sub-object; personal passphrases
// are Secret references and enterprise RADIUS profiles are referenced by
// Kubernetes name, never by opaque UUID.
type WifiSecurityConfiguration struct {
	// Type is the security discriminator.
	// +kubebuilder:validation:Enum=OPEN;WPA2_PERSONAL;WPA2_WPA3_PERSONAL;WPA3_PERSONAL;WPA2_ENTERPRISE;WPA2_WPA3_ENTERPRISE;WPA3_ENTERPRISE
	Type string `json:"type"`

	// Open is the OPEN variant. Required for type OPEN.
	// +optional
	Open *WifiOpenSecurityConfiguration `json:"open,omitempty"`

	// WPA2Personal is the WPA2_PERSONAL variant. Required for type
	// WPA2_PERSONAL.
	// +optional
	WPA2Personal *WifiWPA2PersonalSecurityConfiguration `json:"wpa2Personal,omitempty"`

	// WPA2WPA3Personal is the WPA2_WPA3_PERSONAL variant. Required for type
	// WPA2_WPA3_PERSONAL.
	// +optional
	WPA2WPA3Personal *WifiWPA2WPA3PersonalSecurityConfiguration `json:"wpa2Wpa3Personal,omitempty"`

	// WPA3Personal is the WPA3_PERSONAL variant. Required for type
	// WPA3_PERSONAL.
	// +optional
	WPA3Personal *WifiWPA3PersonalSecurityConfiguration `json:"wpa3Personal,omitempty"`

	// WPA2Enterprise is the WPA2_ENTERPRISE variant. Required for type
	// WPA2_ENTERPRISE.
	// +optional
	WPA2Enterprise *WifiWPA2EnterpriseSecurityConfiguration `json:"wpa2Enterprise,omitempty"`

	// WPA2WPA3Enterprise is the WPA2_WPA3_ENTERPRISE variant. Required for type
	// WPA2_WPA3_ENTERPRISE.
	// +optional
	WPA2WPA3Enterprise *WifiWPA2WPA3EnterpriseSecurityConfiguration `json:"wpa2Wpa3Enterprise,omitempty"`

	// WPA3Enterprise is the WPA3_ENTERPRISE variant. Required for type
	// WPA3_ENTERPRISE.
	// +optional
	WPA3Enterprise *WifiWPA3EnterpriseSecurityConfiguration `json:"wpa3Enterprise,omitempty"`
}

// WifiOpenSecurityConfiguration is the OPEN security variant.
type WifiOpenSecurityConfiguration struct {
	// Encryption is the open-security encryption mode. Omit for plain open.
	// +optional
	// +kubebuilder:validation:Enum=ENHANCED_OPEN;ENHANCED_OPEN_WITH_TRANSITION
	Encryption *string `json:"encryption,omitempty"`

	// RadiusConfiguration configures RADIUS for this variant.
	// +optional
	RadiusConfiguration *WifiNonEnterpriseRadiusConfiguration `json:"radiusConfiguration,omitempty"`
}

// WifiWPA2PersonalSecurityConfiguration is the WPA2_PERSONAL security variant.
type WifiWPA2PersonalSecurityConfiguration struct {
	// Passphrase references the Secret key holding the network passphrase.
	// +optional
	Passphrase *corev1.SecretKeySelector `json:"passphrase,omitempty"`

	// PresharedKeys lists per-network preshared keys.
	// +optional
	// +kubebuilder:validation:MinItems=1
	PresharedKeys []WifiPresharedKey `json:"presharedKeys,omitempty"`

	// PmfMode is the Protected Management Frames mode.
	// +optional
	// +kubebuilder:validation:Enum=REQUIRED;OPTIONAL
	PmfMode *string `json:"pmfMode,omitempty"`

	// FastRoamingEnabled enables fast roaming (802.11r).
	// +optional
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`

	// GroupRekeyIntervalSeconds is the group rekey interval. Omit to disable.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`

	// RadiusConfiguration configures RADIUS for this variant.
	// +optional
	RadiusConfiguration *WifiNonEnterpriseRadiusConfiguration `json:"radiusConfiguration,omitempty"`
}

// WifiWPA2WPA3PersonalSecurityConfiguration is the WPA2_WPA3_PERSONAL
// security variant.
type WifiWPA2WPA3PersonalSecurityConfiguration struct {
	// Passphrase references the Secret key holding the network passphrase.
	Passphrase corev1.SecretKeySelector `json:"passphrase"`

	// PmfMode is the Protected Management Frames mode.
	// +kubebuilder:validation:Enum=REQUIRED;OPTIONAL
	PmfMode string `json:"pmfMode"`

	// SaeConfiguration configures SAE (WPA3) authentication.
	SaeConfiguration WifiSAEConfiguration `json:"saeConfiguration"`

	// Wpa3FastRoamingEnabled enables WPA3 fast roaming.
	Wpa3FastRoamingEnabled bool `json:"wpa3FastRoamingEnabled"`

	// FastRoamingEnabled enables fast roaming (802.11r).
	// +optional
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`

	// GroupRekeyIntervalSeconds is the group rekey interval. Omit to disable.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`

	// RadiusConfiguration configures RADIUS for this variant.
	// +optional
	RadiusConfiguration *WifiNonEnterpriseRadiusConfiguration `json:"radiusConfiguration,omitempty"`
}

// WifiWPA3PersonalSecurityConfiguration is the WPA3_PERSONAL security variant.
type WifiWPA3PersonalSecurityConfiguration struct {
	// Passphrase references the Secret key holding the network passphrase.
	Passphrase corev1.SecretKeySelector `json:"passphrase"`

	// SaeConfiguration configures SAE (WPA3) authentication.
	SaeConfiguration WifiSAEConfiguration `json:"saeConfiguration"`

	// FastRoamingEnabled enables fast roaming (802.11r).
	// +optional
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`

	// GroupRekeyIntervalSeconds is the group rekey interval. Omit to disable.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`

	// RadiusConfiguration configures RADIUS for this variant.
	// +optional
	RadiusConfiguration *WifiNonEnterpriseRadiusConfiguration `json:"radiusConfiguration,omitempty"`
}

// WifiWPA2EnterpriseSecurityConfiguration is the WPA2_ENTERPRISE security
// variant.
type WifiWPA2EnterpriseSecurityConfiguration struct {
	// CoaEnabled enables Change of Authorization (CoA).
	CoaEnabled bool `json:"coaEnabled"`

	// RadiusConfiguration configures the enterprise RADIUS profile.
	RadiusConfiguration WifiEnterpriseRadiusConfiguration `json:"radiusConfiguration"`

	// PmfMode is the Protected Management Frames mode.
	// +optional
	// +kubebuilder:validation:Enum=REQUIRED;OPTIONAL
	PmfMode *string `json:"pmfMode,omitempty"`

	// FastRoamingEnabled enables fast roaming (802.11r).
	// +optional
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`

	// GroupRekeyIntervalSeconds is the group rekey interval. Omit to disable.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiWPA2WPA3EnterpriseSecurityConfiguration is the WPA2_WPA3_ENTERPRISE
// security variant.
type WifiWPA2WPA3EnterpriseSecurityConfiguration struct {
	// CoaEnabled enables Change of Authorization (CoA).
	CoaEnabled bool `json:"coaEnabled"`

	// PmfMode is the Protected Management Frames mode.
	// +kubebuilder:validation:Enum=REQUIRED;OPTIONAL
	PmfMode string `json:"pmfMode"`

	// RadiusConfiguration configures the enterprise RADIUS profile.
	RadiusConfiguration WifiEnterpriseRadiusConfiguration `json:"radiusConfiguration"`

	// Wpa3FastRoamingEnabled enables WPA3 fast roaming.
	Wpa3FastRoamingEnabled bool `json:"wpa3FastRoamingEnabled"`

	// FastRoamingEnabled enables fast roaming (802.11r).
	// +optional
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`

	// GroupRekeyIntervalSeconds is the group rekey interval. Omit to disable.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiWPA3EnterpriseSecurityConfiguration is the WPA3_ENTERPRISE security
// variant.
type WifiWPA3EnterpriseSecurityConfiguration struct {
	// CoaEnabled enables Change of Authorization (CoA).
	CoaEnabled bool `json:"coaEnabled"`

	// SecurityMode is the WPA3 enterprise security mode.
	// +kubebuilder:validation:Enum=DEFAULT;HIGH_SECURITY_192_BIT
	SecurityMode string `json:"securityMode"`

	// RadiusConfiguration configures the enterprise RADIUS profile.
	RadiusConfiguration WifiEnterpriseRadiusConfiguration `json:"radiusConfiguration"`

	// FastRoamingEnabled enables fast roaming (802.11r).
	// +optional
	FastRoamingEnabled *bool `json:"fastRoamingEnabled,omitempty"`

	// GroupRekeyIntervalSeconds is the group rekey interval. Omit to disable.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=86400
	GroupRekeyIntervalSeconds *int32 `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiSAEConfiguration configures SAE (Simultaneous Authentication of Equals).
type WifiSAEConfiguration struct {
	// AnticloggingThresholdSeconds is the anti-clogging threshold (1..60).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	AnticloggingThresholdSeconds int32 `json:"anticloggingThresholdSeconds"`

	// SyncTimeSeconds is the SAE sync time (1..60).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=60
	SyncTimeSeconds int32 `json:"syncTimeSeconds"`
}

// WifiPresharedKey is a per-network preshared key. The passphrase is a Secret
// reference and the network is a UnifiNetwork reference.
type WifiPresharedKey struct {
	// Network references the UnifiNetwork the preshared key applies to.
	Network CoreRef `json:"network"`

	// Passphrase references the Secret key holding the preshared key.
	Passphrase corev1.SecretKeySelector `json:"passphrase"`
}

// WifiNonEnterpriseRadiusConfiguration is the RADIUS configuration used by the
// non-enterprise security variants. The upstream profileId is a
// radiusProfileRef to a UnifiRadiusProfile.
type WifiNonEnterpriseRadiusConfiguration struct {
	// MACAuthenticationConfiguration configures RADIUS MAC authentication.
	MACAuthenticationConfiguration WifiRadiusMacAuthenticationConfiguration `json:"macAuthenticationConfiguration"`

	// NasID configures the RADIUS NAS-Identifier.
	NasID WifiRadiusNasID `json:"nasId"`

	// RadiusProfileRef references the UnifiRadiusProfile to authenticate
	// against.
	RadiusProfileRef CoreRef `json:"radiusProfileRef"`
}

// WifiEnterpriseRadiusConfiguration is the RADIUS configuration used by the
// enterprise security variants. The upstream profileId is a radiusProfileRef
// to a UnifiRadiusProfile.
type WifiEnterpriseRadiusConfiguration struct {
	// NasID configures the RADIUS NAS-Identifier.
	NasID WifiRadiusNasID `json:"nasId"`

	// RadiusProfileRef references the UnifiRadiusProfile to authenticate
	// against.
	RadiusProfileRef CoreRef `json:"radiusProfileRef"`

	// MACAuthenticationConfiguration configures RADIUS MAC authentication.
	// +optional
	MACAuthenticationConfiguration *WifiRadiusMacAuthenticationConfiguration `json:"macAuthenticationConfiguration,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="self.type == 'DERIVED' ? has(self.source) : !has(self.source)",message="source requires type DERIVED"
// +kubebuilder:validation:XValidation:rule="self.type == 'USER_DEFINED' ? has(self.value) : !has(self.value)",message="value requires type USER_DEFINED"

// WifiRadiusNasID is the discriminated RADIUS NAS-Identifier configuration
// (DERIVED or USER_DEFINED).
type WifiRadiusNasID struct {
	// Type is the NAS-ID discriminator (DERIVED or USER_DEFINED).
	// +kubebuilder:validation:Enum=DERIVED;USER_DEFINED
	Type string `json:"type"`

	// Source is the derived NAS-ID source. Required for type DERIVED.
	// +optional
	// +kubebuilder:validation:Enum=DEVICE_MAC_ADDRESS;DEVICE_NAME;SITE_NAME;BSSID
	Source string `json:"source,omitempty"`

	// Value is the user-defined NAS-Identifier. Required for type
	// USER_DEFINED.
	// +optional
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value,omitempty"`
}

// WifiRadiusMacAuthenticationConfiguration configures RADIUS MAC
// authentication.
type WifiRadiusMacAuthenticationConfiguration struct {
	// MacAddressFormat is the MAC address format presented to the RADIUS
	// server.
	// +kubebuilder:validation:Enum=UPPERCASE_NOT_SEPARATED;UPPERCASE_DASH_SEPARATED;UPPERCASE_COLON_SEPARATED;LOWERCASE_NOT_SEPARATED;LOWERCASE_COLON_SEPARATED;LOWERCASE_DASH_SEPARATED
	MacAddressFormat string `json:"macAddressFormat"`
}

// UnifiWifiBroadcastStatus defines the observed state of UnifiWifiBroadcast.
type UnifiWifiBroadcastStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiWifiBroadcast resource.
	//
	// The primary readiness signal is the "Ready" condition.
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// summary is a human-readable summary of the last reconciliation.
	// +optional
	Summary string `json:"summary,omitempty"`

	// wifiBroadcastID is the UUID of the upstream WiFi broadcast. It is
	// recorded in status only, never in spec.
	// +optional
	WifiBroadcastID string `json:"wifiBroadcastID,omitempty"`

	// networkID is the UUID of the upstream network the broadcast belongs to.
	// It is resolved transitively through spec.networkRef and recorded in
	// status only.
	// +optional
	NetworkID string `json:"networkID,omitempty"`

	// siteID is the UUID of the upstream site the broadcast belongs to. It is
	// resolved transitively through the referenced network's siteRef and
	// recorded in status only so teardown does not depend on the live
	// reference chain.
	// +optional
	SiteID string `json:"siteID,omitempty"`

	// origin is the upstream broadcast origin (USER_DEFINED, DERIVED, or
	// ORCHESTRATED). Only USER_DEFINED broadcasts are written; any other origin
	// is adopted read-only and reported here.
	// +optional
	Origin string `json:"origin,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Network",type=string,JSONPath=`.spec.networkRef.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiWifiBroadcast is the Schema for the unifiwifibroadcasts API
type UnifiWifiBroadcast struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiWifiBroadcast
	// +required
	Spec UnifiWifiBroadcastSpec `json:"spec"`

	// status defines the observed state of UnifiWifiBroadcast
	// +optional
	Status UnifiWifiBroadcastStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiWifiBroadcastList contains a list of UnifiWifiBroadcast
type UnifiWifiBroadcastList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiWifiBroadcast `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiWifiBroadcast{}, &UnifiWifiBroadcastList{})
}
