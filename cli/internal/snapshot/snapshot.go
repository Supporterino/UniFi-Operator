// Package snapshot projects UniFi controller objects into Custom Resources that a user
// can `kubectl apply` and have the operator adopt.
package snapshot

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Supporterino/UniFi-Operator/cli/internal/emit"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

const (
	// Group is the API group of the emitted Custom Resources.
	Group = "unifi.supporterino.de"
	// Version is the API version of the emitted Custom Resources.
	Version = "v1alpha1"

	// KindUnifiController is the kind for a projected UniFi controller connection.
	KindUnifiController = "UnifiController"
	// KindUnifiSite is the kind for a projected UniFi site.
	KindUnifiSite = "UnifiSite"
	// KindUnifiNetwork is the kind for a projected UniFi network.
	KindUnifiNetwork = "UnifiNetwork"
	// KindUnifiFirewallZone is the kind for a projected UniFi firewall zone.
	KindUnifiFirewallZone = "UnifiFirewallZone"
	// KindUnifiWifiBroadcast is the kind for a projected UniFi WiFi broadcast.
	KindUnifiWifiBroadcast = "UnifiWifiBroadcast"

	// Broadcast type discriminator values accepted by UnifiWifiBroadcast.spec.type.
	WifiBroadcastTypeStandard     = "STANDARD"
	WifiBroadcastTypeIotOptimized = "IOT_OPTIMIZED"

	// Broadcast security discriminator values accepted by
	// UnifiWifiBroadcast.spec.securityConfiguration.type.
	WifiSecurityOpen               = "OPEN"
	WifiSecurityWPA2Personal       = "WPA2_PERSONAL"
	WifiSecurityWPA2WPA3Personal   = "WPA2_WPA3_PERSONAL"
	WifiSecurityWPA3Personal       = "WPA3_PERSONAL"
	WifiSecurityWPA2Enterprise     = "WPA2_ENTERPRISE"
	WifiSecurityWPA2WPA3Enterprise = "WPA2_WPA3_ENTERPRISE"
	WifiSecurityWPA3Enterprise     = "WPA3_ENTERPRISE"

	// Management discriminator values accepted by UnifiNetwork.spec.management.
	ManagementGateway   = "GATEWAY"
	ManagementSwitch    = "SWITCH"
	ManagementUnmanaged = "UNMANAGED"

	// AnnotationOriginalName records the upstream object name on a resource whose
	// metadata.name was disambiguated to avoid a collision.
	AnnotationOriginalName = "unifi.supporterino.de/original-name"
	// AnnotationUnresolvedDeviceTag records that a switch-managed network's upstream
	// device binding could not be resolved to exactly one device tag.
	AnnotationUnresolvedDeviceTag = "unifi.supporterino.de/unresolved-device-tag"
	// AnnotationUnresolvedNetworkRef records that a firewall zone referenced an upstream
	// network that was not emitted, so spec.networkRefs is incomplete.
	AnnotationUnresolvedNetworkRef = "unifi.supporterino.de/unresolved-network-ref"
	// AnnotationUnrepresentedIPv6 records that part of a gateway network's IPv6
	// configuration could not be represented in spec.
	AnnotationUnrepresentedIPv6 = "unifi.supporterino.de/unrepresented-ipv6"
	// AnnotationUnresolvedWifiSecret records that a WiFi broadcast's personal
	// passphrase could not be recovered from the console, so spec carries a
	// placeholder Secret reference the user must complete.
	AnnotationUnresolvedWifiSecret = "unifi.supporterino.de/unresolved-wifi-secret"
	// AnnotationUnresolvedRadiusProfile records that an enterprise WiFi broadcast
	// references a RADIUS profile UUID the CLI cannot represent, so spec carries a
	// placeholder UnifiRadiusProfile reference the user must complete.
	AnnotationUnresolvedRadiusProfile = "unifi.supporterino.de/unresolved-radius-profile"
	// AnnotationUnrepresentedWifiSettings records that an upstream WiFi broadcast
	// carries settings the reduced CLI projection (design D8) does not model, so
	// they are not in spec. The annotation names them so they are not silently
	// dropped (cli/AGENTS.md, docs/crd-conventions.md Rule 6).
	AnnotationUnrepresentedWifiSettings = "unifi.supporterino.de/unrepresented-wifi-settings"
	// AnnotationControllerTemplate marks an emitted UnifiController as a template the
	// user must complete with an API-key Secret.
	AnnotationControllerTemplate = "unifi.supporterino.de/controller-template"
	// AnnotationInsecureControllerURL marks an emitted UnifiController whose spec.url is
	// not https. The CRD requires an https URL, so the user must update it before applying.
	AnnotationInsecureControllerURL = "unifi.supporterino.de/insecure-controller-url"

	// DefaultControllerRef is the default UnifiController name emitted sites reference
	// and the default name of the emitted UnifiController template.
	DefaultControllerRef = "unifi-controller"
	// DefaultSecretName and DefaultSecretKey are the placeholder Secret coordinates in
	// the emitted UnifiController template. They carry no credential value.
	DefaultSecretName = "unifi-api-key"
	DefaultSecretKey  = "api-key"

	// unresolvedDeviceTagName is the deterministic placeholder deviceTag.name emitted
	// for a switch-managed network whose upstream device binding is not uniquely
	// resolvable to a device tag.
	unresolvedDeviceTagName = "unresolved-device-tag"

	// unresolvedWifiSecretName and unresolvedWifiSecretKey are the deterministic
	// placeholder Secret coordinates emitted for a WiFi personal passphrase, which
	// cannot be recovered from the console. They carry no credential value.
	unresolvedWifiSecretName = "unresolved-wifi-passphrase"
	unresolvedWifiSecretKey  = "passphrase"

	// unresolvedRadiusProfileName is the deterministic placeholder UnifiRadiusProfile
	// name emitted for an enterprise broadcast whose upstream RADIUS profile UUID
	// cannot be represented. It carries no upstream identifier.
	unresolvedRadiusProfileName = "unresolved-radius-profile"

	// wifiSecretWarning is the reason recorded on AnnotationUnresolvedWifiSecret.
	wifiSecretWarning = "the console does not expose the WiFi passphrase; spec.securityConfiguration references a placeholder Secret that must be set before applying"
	// wifiRadiusWarning is the reason recorded on AnnotationUnresolvedRadiusProfile.
	wifiRadiusWarning = "the console RADIUS profile UUID cannot be represented; spec.securityConfiguration references a placeholder UnifiRadiusProfile that must be set before applying"

	minVLAN = 1
	maxVLAN = 4009

	minIPv4Prefix = 8
	maxIPv4Prefix = 30

	minIPv6Prefix = 64
	maxIPv6Prefix = 127
)

// CoreRef is a reference to another Custom Resource by Kubernetes name, same namespace.
type CoreRef struct {
	Name string `json:"name"`
}

// DeviceTagSelector selects an existing, read-only upstream device tag by name. It
// mirrors the operator's DeviceTagSelector; the opaque tag and device UUIDs never reach
// spec.
type DeviceTagSelector struct {
	Name string `json:"name"`
}

// SecretKeySelector identifies a same-namespace Secret key. It carries no value.
type SecretKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// UnifiControllerSpec is the desired state emitted for a UniFi controller connection.
// InsecureSkipVerify is intentionally left unset so TLS verification stays on by default.
type UnifiControllerSpec struct {
	URL       string             `json:"url"`
	SecretRef *SecretKeySelector `json:"secretRef"`
}

// UnifiSiteSpec is the desired state emitted for a UniFi site. The upstream site UUID is
// never emitted; the controller adopts the site by InternalReference and records the UUID
// in status.
type UnifiSiteSpec struct {
	ControllerRef     CoreRef `json:"controllerRef"`
	InternalReference string  `json:"internalReference"`
}

// DHCPGuarding holds the trusted DHCP server addresses for a network.
type DHCPGuarding struct {
	TrustedDHCPServerIPAddresses []string `json:"trustedDhcpServerIpAddresses"`
}

// IPv4Configuration is the IPv4 configuration shared by the gateway and switch variants.
type IPv4Configuration struct {
	AutoScaleEnabled        bool     `json:"autoScaleEnabled"`
	HostIPAddress           string   `json:"hostIpAddress"`
	PrefixLength            int32    `json:"prefixLength"`
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`
}

// IPv6ClientAddressAssignment configures IPv6 client addressing.
type IPv6ClientAddressAssignment struct {
	SLAACEnabled bool `json:"slaacEnabled"`
}

// RouterAdvertisement configures IPv6 router advertisement.
type RouterAdvertisement struct {
	Priority string `json:"priority"`
}

// IPv6Configuration is the emitted IPv6 configuration. Prefix-delegation IPv6 references
// the WAN by opaque id upstream and is therefore never emitted (see AnnotationUnrepresentedIPv6).
type IPv6Configuration struct {
	InterfaceType                string                      `json:"interfaceType"`
	ClientAddressAssignment      IPv6ClientAddressAssignment `json:"clientAddressAssignment"`
	AdditionalHostIPSubnets      []string                    `json:"additionalHostIpSubnets,omitempty"`
	DNSServerIPAddressesOverride []string                    `json:"dnsServerIpAddressesOverride,omitempty"`
	RouterAdvertisement          *RouterAdvertisement        `json:"routerAdvertisement,omitempty"`
	HostIPAddress                string                      `json:"hostIpAddress,omitempty"`
	PrefixLength                 int32                       `json:"prefixLength,omitempty"`
}

// GatewayNetworkOptions is the emitted GATEWAY variant.
type GatewayNetworkOptions struct {
	CellularBackupEnabled bool               `json:"cellularBackupEnabled"`
	InternetAccessEnabled bool               `json:"internetAccessEnabled"`
	IsolationEnabled      bool               `json:"isolationEnabled"`
	IPv4Configuration     IPv4Configuration  `json:"ipv4Configuration"`
	IPv6Configuration     *IPv6Configuration `json:"ipv6Configuration,omitempty"`
	MDNSForwardingEnabled *bool              `json:"mdnsForwardingEnabled,omitempty"`
}

// SwitchNetworkOptions is the emitted SWITCH variant. DeviceTag is required by the CRD.
// It names the read-only upstream device tag that manages the network; when the upstream
// device binding cannot be uniquely resolved to a tag it is emitted as a deterministic
// placeholder and the resource is annotated.
type SwitchNetworkOptions struct {
	CellularBackupEnabled bool              `json:"cellularBackupEnabled"`
	IsolationEnabled      bool              `json:"isolationEnabled"`
	IPv4Configuration     IPv4Configuration `json:"ipv4Configuration"`
	DeviceTag             DeviceTagSelector `json:"deviceTag"`
}

// UnifiNetworkSpec is the desired state emitted for a UniFi network. The field names and
// types mirror the operator's UnifiNetworkSpec (api/v1alpha1) so emitted resources
// validate against the CRD. It carries no upstream id, zoneId, or deviceId: per
// docs/crd-conventions.md opaque identifiers belong in status, which the operator owns.
type UnifiNetworkSpec struct {
	SiteRef      CoreRef                `json:"siteRef"`
	Management   string                 `json:"management"`
	Name         string                 `json:"name"`
	Enabled      *bool                  `json:"enabled,omitempty"`
	VLANID       int32                  `json:"vlanId"`
	DHCPGuarding *DHCPGuarding          `json:"dhcpGuarding,omitempty"`
	Gateway      *GatewayNetworkOptions `json:"gateway,omitempty"`
	Switch       *SwitchNetworkOptions  `json:"switch,omitempty"`
}

// UnifiFirewallZoneSpec is the desired state emitted for a UniFi firewall zone. It
// mirrors the operator's UnifiFirewallZoneSpec: member networks are referenced by the
// Kubernetes name of the UnifiNetwork CR (never the upstream zone or network UUID) and
// the upstream zone UUID is left to status.
type UnifiFirewallZoneSpec struct {
	SiteRef     CoreRef   `json:"siteRef"`
	Name        string    `json:"name"`
	NetworkRefs []CoreRef `json:"networkRefs"`
}

// IotOptimizedWifiOptions is the IOT_OPTIMIZED variant marker. The v10.4.57 union adds
// no IOT-only properties, so the CRD requires it as an empty object.
type IotOptimizedWifiOptions struct{}

// StandardWifiOptions is the emitted STANDARD broadcast variant. BroadcastingFrequenciesGHz
// uses the CRD's exact string enum ("2.4", "5", "6"), not the numeric upstream form.
type StandardWifiOptions struct {
	AdvertiseDeviceName        bool     `json:"advertiseDeviceName"`
	ArpProxyEnabled            bool     `json:"arpProxyEnabled"`
	BssTransitionEnabled       bool     `json:"bssTransitionEnabled"`
	BroadcastingFrequenciesGHz []string `json:"broadcastingFrequenciesGHz"`
}

// WifiOpenSecurityConfiguration is the emitted OPEN security variant.
type WifiOpenSecurityConfiguration struct {
	Encryption *string `json:"encryption,omitempty"`
}

// WifiSAEConfiguration configures SAE (personal WPA3 variants).
type WifiSAEConfiguration struct {
	AnticloggingThresholdSeconds int32 `json:"anticloggingThresholdSeconds"`
	SyncTimeSeconds              int32 `json:"syncTimeSeconds"`
}

// WifiPresharedKey is an emitted per-network preshared key. The passphrase is a
// placeholder Secret reference; the value is never recoverable from the console.
type WifiPresharedKey struct {
	Network    CoreRef           `json:"network"`
	Passphrase SecretKeySelector `json:"passphrase"`
}

// WifiWPA2PersonalSecurityConfiguration is the emitted WPA2_PERSONAL security variant.
type WifiWPA2PersonalSecurityConfiguration struct {
	Passphrase                *SecretKeySelector `json:"passphrase,omitempty"`
	PresharedKeys             []WifiPresharedKey `json:"presharedKeys,omitempty"`
	PmfMode                   *string            `json:"pmfMode,omitempty"`
	FastRoamingEnabled        *bool              `json:"fastRoamingEnabled,omitempty"`
	GroupRekeyIntervalSeconds *int32             `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiWPA2WPA3PersonalSecurityConfiguration is the emitted WPA2_WPA3_PERSONAL variant.
type WifiWPA2WPA3PersonalSecurityConfiguration struct {
	Passphrase                SecretKeySelector    `json:"passphrase"`
	PmfMode                   string               `json:"pmfMode"`
	SaeConfiguration          WifiSAEConfiguration `json:"saeConfiguration"`
	Wpa3FastRoamingEnabled    bool                 `json:"wpa3FastRoamingEnabled"`
	FastRoamingEnabled        *bool                `json:"fastRoamingEnabled,omitempty"`
	GroupRekeyIntervalSeconds *int32               `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiWPA3PersonalSecurityConfiguration is the emitted WPA3_PERSONAL security variant.
type WifiWPA3PersonalSecurityConfiguration struct {
	Passphrase                SecretKeySelector    `json:"passphrase"`
	SaeConfiguration          WifiSAEConfiguration `json:"saeConfiguration"`
	FastRoamingEnabled        *bool                `json:"fastRoamingEnabled,omitempty"`
	GroupRekeyIntervalSeconds *int32               `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiRadiusNasID is the emitted RADIUS NAS-Identifier configuration (DERIVED or
// USER_DEFINED).
type WifiRadiusNasID struct {
	Type   string `json:"type"`
	Source string `json:"source,omitempty"`
	Value  string `json:"value,omitempty"`
}

// WifiRadiusMacAuthenticationConfiguration is the emitted RADIUS MAC authentication
// configuration.
type WifiRadiusMacAuthenticationConfiguration struct {
	MacAddressFormat string `json:"macAddressFormat"`
}

// WifiEnterpriseRadiusConfiguration is the emitted enterprise RADIUS configuration. The
// upstream profile UUID is replaced by radiusProfileRef, a reference to a UnifiRadiusProfile
// that the operator resolves (and currently fails closed on until the kind is implemented).
type WifiEnterpriseRadiusConfiguration struct {
	NasID                          WifiRadiusNasID                           `json:"nasId"`
	RadiusProfileRef               CoreRef                                   `json:"radiusProfileRef"`
	MACAuthenticationConfiguration *WifiRadiusMacAuthenticationConfiguration `json:"macAuthenticationConfiguration,omitempty"`
}

// WifiWPA2EnterpriseSecurityConfiguration is the emitted WPA2_ENTERPRISE security variant.
type WifiWPA2EnterpriseSecurityConfiguration struct {
	CoaEnabled                bool                              `json:"coaEnabled"`
	RadiusConfiguration       WifiEnterpriseRadiusConfiguration `json:"radiusConfiguration"`
	PmfMode                   *string                           `json:"pmfMode,omitempty"`
	FastRoamingEnabled        *bool                             `json:"fastRoamingEnabled,omitempty"`
	GroupRekeyIntervalSeconds *int32                            `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiWPA2WPA3EnterpriseSecurityConfiguration is the emitted WPA2_WPA3_ENTERPRISE variant.
type WifiWPA2WPA3EnterpriseSecurityConfiguration struct {
	CoaEnabled                bool                              `json:"coaEnabled"`
	PmfMode                   string                            `json:"pmfMode"`
	RadiusConfiguration       WifiEnterpriseRadiusConfiguration `json:"radiusConfiguration"`
	Wpa3FastRoamingEnabled    bool                              `json:"wpa3FastRoamingEnabled"`
	FastRoamingEnabled        *bool                             `json:"fastRoamingEnabled,omitempty"`
	GroupRekeyIntervalSeconds *int32                            `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiWPA3EnterpriseSecurityConfiguration is the emitted WPA3_ENTERPRISE security variant.
type WifiWPA3EnterpriseSecurityConfiguration struct {
	CoaEnabled                bool                              `json:"coaEnabled"`
	SecurityMode              string                            `json:"securityMode"`
	RadiusConfiguration       WifiEnterpriseRadiusConfiguration `json:"radiusConfiguration"`
	FastRoamingEnabled        *bool                             `json:"fastRoamingEnabled,omitempty"`
	GroupRekeyIntervalSeconds *int32                            `json:"groupRekeyIntervalSeconds,omitempty"`
}

// WifiSecurityConfiguration is the emitted broadcast security union. The discriminator
// selects exactly one variant sub-object.
type WifiSecurityConfiguration struct {
	Type               string                                       `json:"type"`
	Open               *WifiOpenSecurityConfiguration               `json:"open,omitempty"`
	WPA2Personal       *WifiWPA2PersonalSecurityConfiguration       `json:"wpa2Personal,omitempty"`
	WPA2WPA3Personal   *WifiWPA2WPA3PersonalSecurityConfiguration   `json:"wpa2Wpa3Personal,omitempty"`
	WPA3Personal       *WifiWPA3PersonalSecurityConfiguration       `json:"wpa3Personal,omitempty"`
	WPA2Enterprise     *WifiWPA2EnterpriseSecurityConfiguration     `json:"wpa2Enterprise,omitempty"`
	WPA2WPA3Enterprise *WifiWPA2WPA3EnterpriseSecurityConfiguration `json:"wpa2Wpa3Enterprise,omitempty"`
	WPA3Enterprise     *WifiWPA3EnterpriseSecurityConfiguration     `json:"wpa3Enterprise,omitempty"`
}

// WifiBroadcastStatus is the observed state recorded on an emitted UnifiWifiBroadcast. The
// upstream broadcast UUID belongs in status only (docs/crd-conventions.md).
type WifiBroadcastStatus struct {
	WifiBroadcastID string `json:"wifiBroadcastID,omitempty"`
}

// UnifiWifiBroadcastSpec is the desired state emitted for a UniFi WiFi broadcast. It
// mirrors the operator's UnifiWifiBroadcastSpec so emitted resources validate against the
// CRD. It carries no upstream id or network UUID: the upstream network is referenced by
// the emitted UnifiNetwork name and the broadcast id is recorded in status only.
type UnifiWifiBroadcastSpec struct {
	NetworkRef                          CoreRef                   `json:"networkRef"`
	Type                                string                    `json:"type"`
	Name                                string                    `json:"name"`
	Enabled                             *bool                     `json:"enabled,omitempty"`
	HideName                            bool                      `json:"hideName"`
	ClientIsolationEnabled              bool                      `json:"clientIsolationEnabled"`
	MulticastToUnicastConversionEnabled bool                      `json:"multicastToUnicastConversionEnabled"`
	UapsdEnabled                        bool                      `json:"uapsdEnabled"`
	Channel2gLockedTo6                  *bool                     `json:"channel2gLockedTo6,omitempty"`
	DtimPeriod2gLockedTo3               *bool                     `json:"dtimPeriod2gLockedTo3,omitempty"`
	SecurityConfiguration               WifiSecurityConfiguration `json:"securityConfiguration"`
	DeviceTags                          []DeviceTagSelector       `json:"deviceTags,omitempty"`
	Standard                            *StandardWifiOptions      `json:"standard,omitempty"`
	IotOptimized                        *IotOptimizedWifiOptions  `json:"iotOptimized,omitempty"`
}

// Controller emits a UnifiController template: the console URL plus a placeholder
// same-namespace secretRef the user must complete with a Secret holding their API key.
// It never reads the console and carries no credential. It returns a human-readable
// warning describing the required completion.
//
// The CRD requires spec.url to be https so the API key never travels in cleartext. A
// non-https controller URL is still emitted (the low-level client accepts http for local
// testing) but is annotated with AnnotationInsecureControllerURL and warned about, so the
// user switches to https before applying rather than having the API server reject it.
func Controller(url, name string) (emit.Resource, []string) {
	annotations := map[string]string{
		AnnotationControllerTemplate: "replace spec.secretRef with a same-namespace Secret holding your UniFi API key",
	}
	warnings := []string{fmt.Sprintf(
		"UnifiController %q is a template: set spec.secretRef to a same-namespace Secret holding your UniFi API key before applying",
		name)}
	if !strings.HasPrefix(url, "https://") {
		annotations[AnnotationInsecureControllerURL] = "true"
		warnings = append(warnings, fmt.Sprintf(
			"UnifiController %q: spec.url is not https://, but the operator requires an HTTPS controller URL; update spec.url to https:// before applying (annotation %s=true)",
			name, AnnotationInsecureControllerURL))
	}
	resource := emit.Resource{
		APIVersion: Group + "/" + Version,
		Kind:       KindUnifiController,
		Metadata: emit.Metadata{
			Name:        name,
			Annotations: annotations,
		},
		Spec: UnifiControllerSpec{
			URL:       url,
			SecretRef: &SecretKeySelector{Name: DefaultSecretName, Key: DefaultSecretKey},
		},
	}
	return resource, warnings
}

// Sites emits a UnifiSite for each upstream site and returns the deterministic
// metadata.name chosen for every upstream internalReference. Site names are derived with
// DNSSafeName; collisions get a stable numeric suffix. The returned name map is what
// Networks must use as siteRef.name so site and network references agree.
func Sites(controllerRef string, sites []unifi.Site) ([]emit.Resource, map[string]string, []string) {
	resources := make([]emit.Resource, 0, len(sites))
	names := make(map[string]string, len(sites))
	var warnings []string
	alloc := newNameAllocator()

	for _, site := range sites {
		base := DNSSafeName(site.InternalReference)
		name, collided := alloc.allocate(base)
		names[site.InternalReference] = name

		metadata := emit.Metadata{Name: name}
		if collided {
			metadata.Annotations = map[string]string{AnnotationOriginalName: site.InternalReference}
			warnings = append(warnings, fmt.Sprintf(
				"site %q: projected name %q is already in use; emitting as %q",
				site.InternalReference, base, name))
		}

		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiSite,
			Metadata:   metadata,
			Spec: UnifiSiteSpec{
				ControllerRef:     CoreRef{Name: controllerRef},
				InternalReference: site.InternalReference,
			},
		})
	}
	return resources, names, warnings
}

// Networks projects each UniFi network into a UnifiNetwork Custom Resource. siteName is
// the metadata.name of the target UnifiSite as returned by Sites, so siteRef.name always
// matches the emitted site. Colliding projected names get a stable numeric suffix and an
// AnnotationOriginalName annotation rather than overwriting one another.
//
// deviceTags is the read-only device-tag list of the site, used to reverse-map a
// switch-managed network's observed deviceId to a tag name. The returned map is the
// upstream-network-id -> emitted-name map (including any collision suffix); callers that
// reference networks by name, such as FirewallZones, must use it rather than re-deriving
// names, which can disagree with the emitted resources once a collision is disambiguated.
func Networks(siteName string, networks []unifi.Network, deviceTags []unifi.DeviceTag) ([]emit.Resource, map[string]string, []string) {
	resources := make([]emit.Resource, 0, len(networks))
	names := make(map[string]string, len(networks))
	var warnings []string
	alloc := newNameAllocator()

	for _, network := range networks {
		spec, annotations, networkWarnings, ok := buildNetworkSpec(siteName, network, deviceTags)
		if !ok {
			warnings = append(warnings, networkWarnings...)
			continue
		}

		base := DNSSafeName(network.Name)
		name, collided := alloc.allocate(base)
		names[network.ID] = name
		metadata := emit.Metadata{Name: name}
		if collided {
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[AnnotationOriginalName] = network.Name
			networkWarnings = append(networkWarnings, fmt.Sprintf(
				"network %q: projected name %q is already in use; emitting as %q",
				network.Name, base, name))
		}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}

		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiNetwork,
			Metadata:   metadata,
			Spec:       spec,
		})
		warnings = append(warnings, networkWarnings...)
	}
	return resources, names, warnings
}

// buildNetworkSpec maps one upstream network into a spec valid against the CRD. It
// returns ok=false (with a warning and no resource) when a required variant field is
// missing or cannot be represented faithfully, so the CLI never emits an invalid CR.
func buildNetworkSpec(siteName string, network unifi.Network, deviceTags []unifi.DeviceTag) (UnifiNetworkSpec, map[string]string, []string, bool) {
	if network.VLANID < minVLAN || network.VLANID > maxVLAN {
		return UnifiNetworkSpec{}, nil, []string{fmt.Sprintf(
			"network %q: upstream vlanId %d is outside the CRD range %d..%d; skipping",
			network.Name, network.VLANID, minVLAN, maxVLAN)}, false
	}

	spec := UnifiNetworkSpec{
		SiteRef:    CoreRef{Name: siteName},
		Management: network.Management,
		Name:       network.Name,
		Enabled:    network.Enabled,
		VLANID:     network.VLANID,
	}
	if network.DHCPGuarding != nil && len(network.DHCPGuarding.TrustedDHCPServerIPAddresses) > 0 {
		spec.DHCPGuarding = &DHCPGuarding{
			TrustedDHCPServerIPAddresses: network.DHCPGuarding.TrustedDHCPServerIPAddresses,
		}
	}

	var annotations map[string]string
	var warnings []string

	switch network.Management {
	case ManagementGateway:
		gateway, gatewayWarnings, ok := gatewayOptions(network, &annotations)
		if !ok {
			return UnifiNetworkSpec{}, nil, gatewayWarnings, false
		}
		spec.Gateway = gateway
		warnings = append(warnings, gatewayWarnings...)
	case ManagementSwitch:
		switchOptions, switchWarnings, ok := switchNetworkOptions(network, deviceTags, &annotations)
		if !ok {
			return UnifiNetworkSpec{}, nil, switchWarnings, false
		}
		spec.Switch = switchOptions
		warnings = append(warnings, switchWarnings...)
	case ManagementUnmanaged:
		// Only the common fields are representable.
	default:
		return UnifiNetworkSpec{}, nil, []string{fmt.Sprintf(
			"network %q: unknown management %q; skipping", network.Name, network.Management)}, false
	}

	return spec, annotations, warnings, true
}

// gatewayOptions maps the observed gateway union into the emitted GATEWAY variant.
func gatewayOptions(network unifi.Network, annotations *map[string]string) (*GatewayNetworkOptions, []string, bool) {
	if network.CellularBackupEnabled == nil || network.InternetAccessEnabled == nil || network.IsolationEnabled == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: gateway configuration is missing from the controller response; skipping", network.Name)}, false
	}
	if network.IPv4Configuration == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: gateway ipv4Configuration is missing from the controller response; skipping", network.Name)}, false
	}
	ipv4, err := mapIPv4(network.IPv4Configuration)
	if err != nil {
		return nil, []string{fmt.Sprintf("network %q: %v; skipping", network.Name, err)}, false
	}

	gateway := &GatewayNetworkOptions{
		CellularBackupEnabled: *network.CellularBackupEnabled,
		InternetAccessEnabled: *network.InternetAccessEnabled,
		IsolationEnabled:      *network.IsolationEnabled,
		IPv4Configuration:     *ipv4,
		MDNSForwardingEnabled: network.MDNSForwardingEnabled,
	}

	var warnings []string
	if network.IPv6Configuration != nil {
		ipv6, reason, ok := mapIPv6(network.IPv6Configuration)
		if ok {
			gateway.IPv6Configuration = ipv6
		} else {
			setAnnotation(annotations, AnnotationUnrepresentedIPv6, reason)
			warnings = append(warnings, fmt.Sprintf("network %q: %s", network.Name, reason))
		}
	}
	return gateway, warnings, true
}

// switchNetworkOptions maps the observed switch union into the emitted SWITCH variant.
// The upstream device binding is an opaque deviceId; when it belongs to exactly one
// device tag that tag's name becomes the emitted selector. When it belongs to no tag or
// more than one tag the selector would be unresolvable, so a deterministic placeholder is
// emitted and the gap is recorded in an annotation and a warning. It returns ok=false
// when a required switch field is missing or invalid so no invalid CR is emitted.
func switchNetworkOptions(network unifi.Network, deviceTags []unifi.DeviceTag, annotations *map[string]string) (*SwitchNetworkOptions, []string, bool) {
	if network.CellularBackupEnabled == nil || network.IsolationEnabled == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: switch configuration is missing from the controller response; skipping", network.Name)}, false
	}
	if network.IPv4Configuration == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: switch ipv4Configuration is missing from the controller response; skipping", network.Name)}, false
	}
	ipv4, err := mapIPv4(network.IPv4Configuration)
	if err != nil {
		return nil, []string{fmt.Sprintf("network %q: %v; skipping", network.Name, err)}, false
	}

	options := &SwitchNetworkOptions{
		CellularBackupEnabled: *network.CellularBackupEnabled,
		IsolationEnabled:      *network.IsolationEnabled,
		IPv4Configuration:     *ipv4,
	}
	if tagName, ok := deviceTagNameFor(deviceTags, network.DeviceID); ok {
		options.DeviceTag = DeviceTagSelector{Name: tagName}
		return options, nil, true
	}

	reason := "upstream device binding is not uniquely resolvable to a device tag; spec.switch.deviceTag.name is a placeholder and must be set to the managing device tag"
	setAnnotation(annotations, AnnotationUnresolvedDeviceTag, reason)
	options.DeviceTag = DeviceTagSelector{Name: unresolvedDeviceTagName}

	return options, []string{fmt.Sprintf("network %q: %s", network.Name, reason)}, true
}

// deviceTagNameFor returns the name of the single device tag that contains deviceID and
// resolves to exactly one device. It returns ok=false when deviceID is empty, belongs to
// no tag or to more than one tag, or the matched tag has more than one member, since the
// operator requires the selector to name a single-device tag. The caller keeps the
// annotated placeholder instead of emitting a selector the operator cannot resolve.
func deviceTagNameFor(deviceTags []unifi.DeviceTag, deviceID string) (string, bool) {
	if deviceID == "" {
		return "", false
	}
	name := ""
	matches := 0
	for _, tag := range deviceTags {
		if !containsString(tag.DeviceIDs, deviceID) {
			continue
		}
		if len(tag.DeviceIDs) != 1 {
			return "", false
		}
		name = tag.Name
		matches++
	}
	if matches != 1 || name == "" {
		return "", false
	}
	return name, true
}

// containsString reports whether values contains want.
func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// FirewallZones projects each upstream firewall zone into a UnifiFirewallZone. siteName
// is the metadata.name of the target UnifiSite. networkNames is the upstream-network-id ->
// emitted-name map returned by Networks; each zone's member networks are resolved through
// it so networkRefs always name the UnifiNetwork resources this snapshot emitted,
// including any collision suffix. A member network that was not emitted is omitted from
// spec.networkRefs and recorded in an annotation and a warning rather than re-derived from
// its upstream name, which could disagree with the emitted CR.
func FirewallZones(siteName string, networkNames map[string]string, zones []unifi.FirewallZone) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(zones))
	var warnings []string
	alloc := newNameAllocator()

	for _, zone := range zones {
		refs, unresolved := firewallZoneNetworkRefs(zone, networkNames)

		var annotations map[string]string
		if unresolved > 0 {
			setAnnotation(&annotations, AnnotationUnresolvedNetworkRef,
				"member networks missing from the snapshot were omitted from spec.networkRefs; set them to the intended UnifiNetwork names")
			warnings = append(warnings, fmt.Sprintf(
				"firewall zone %q: %d member network(s) were not emitted; spec.networkRefs may be incomplete",
				zone.Name, unresolved))
		}

		base := DNSSafeName(zone.Name)
		name, collided := alloc.allocate(base)
		if collided {
			setAnnotation(&annotations, AnnotationOriginalName, zone.Name)
			warnings = append(warnings, fmt.Sprintf(
				"firewall zone %q: projected name %q is already in use; emitting as %q",
				zone.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiFirewallZone,
			Metadata:   metadata,
			Spec: UnifiFirewallZoneSpec{
				SiteRef:     CoreRef{Name: siteName},
				Name:        zone.Name,
				NetworkRefs: refs,
			},
		})
	}
	return resources, warnings
}

// firewallZoneNetworkRefs resolves a zone's upstream member-network ids to the emitted
// UnifiNetwork names. It returns the number of ids that were not among the emitted
// networks so the caller can record the gap.
func firewallZoneNetworkRefs(zone unifi.FirewallZone, networkNames map[string]string) ([]CoreRef, int) {
	refs := make([]CoreRef, 0, len(zone.NetworkIDs))
	unresolved := 0
	for _, id := range zone.NetworkIDs {
		name, ok := networkNames[id]
		if !ok {
			unresolved++
			continue
		}
		refs = append(refs, CoreRef{Name: name})
	}
	return refs, unresolved
}

// WifiBroadcasts projects each upstream WiFi broadcast into a UnifiWifiBroadcast.
// networkNames is the upstream-network-id -> emitted-name map returned by Networks; each
// broadcast's spec.networkRef is resolved through it so the reference names a UnifiNetwork
// resource this snapshot emitted. deviceTags is the site's read-only device-tag list, used
// to reverse-map a DEVICE_TAGS scope to tag names. A broadcast whose network cannot be
// resolved, whose type/variant is unrepresentable, or whose personal passphrase is not
// recoverable is emitted with a placeholder (passphrase) or skipped with a warning rather
// than silently dropped. The upstream broadcast id is recorded in status only.
func WifiBroadcasts(networkNames map[string]string, deviceTags []unifi.DeviceTag, broadcasts []unifi.WifiBroadcast) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(broadcasts))
	var warnings []string
	alloc := newNameAllocator()

	for _, broadcast := range broadcasts {
		spec, annotations, broadcastWarnings, ok := buildWifiBroadcastSpec(networkNames, deviceTags, broadcast)
		if !ok {
			warnings = append(warnings, broadcastWarnings...)
			continue
		}

		base := DNSSafeName(broadcast.Name)
		name, collided := alloc.allocate(base)
		if collided {
			setAnnotation(&annotations, AnnotationOriginalName, broadcast.Name)
			broadcastWarnings = append(broadcastWarnings, fmt.Sprintf(
				"wifi broadcast %q: projected name %q is already in use; emitting as %q",
				broadcast.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiWifiBroadcast,
			Metadata:   metadata,
			Spec:       spec,
			Status:     WifiBroadcastStatus{WifiBroadcastID: broadcast.ID},
		})
		warnings = append(warnings, broadcastWarnings...)
	}
	return resources, warnings
}

// buildWifiBroadcastSpec maps one upstream broadcast detail into a spec valid against the
// CRD. It returns ok=false (with a warning and no resource) when a required field is
// missing or cannot be represented faithfully, so the CLI never emits an invalid CR.
func buildWifiBroadcastSpec(networkNames map[string]string, deviceTags []unifi.DeviceTag, broadcast unifi.WifiBroadcast) (UnifiWifiBroadcastSpec, map[string]string, []string, bool) {
	networkName, ok := broadcastNetworkName(networkNames, broadcast.Network)
	if !ok {
		return UnifiWifiBroadcastSpec{}, nil, []string{fmt.Sprintf(
			"wifi broadcast %q: upstream network could not be resolved to an emitted UnifiNetwork; skipping", broadcast.Name)}, false
	}

	security, usedPassphrase, usedRadius, err := wifiSecurityConfiguration(broadcast.SecurityConfiguration, networkNames)
	if err != nil {
		return UnifiWifiBroadcastSpec{}, nil, []string{fmt.Sprintf(
			"wifi broadcast %q: %v; skipping", broadcast.Name, err)}, false
	}

	enabled := broadcast.Enabled
	channel2g := broadcast.Channel2gLockedTo6
	dtim2g := broadcast.DtimPeriod2gLockedTo3
	spec := UnifiWifiBroadcastSpec{
		NetworkRef:                          CoreRef{Name: networkName},
		Type:                                broadcast.Type,
		Name:                                broadcast.Name,
		Enabled:                             &enabled,
		HideName:                            broadcast.HideName,
		ClientIsolationEnabled:              broadcast.ClientIsolationEnabled,
		MulticastToUnicastConversionEnabled: broadcast.MulticastToUnicastConversionEnabled,
		UapsdEnabled:                        broadcast.UapsdEnabled,
		Channel2gLockedTo6:                  &channel2g,
		DtimPeriod2gLockedTo3:               &dtim2g,
		SecurityConfiguration:               security,
	}

	var annotations map[string]string
	var warnings []string

	switch broadcast.Type {
	case WifiBroadcastTypeStandard:
		standard, standardWarnings, ok := standardWifiOptions(broadcast)
		if !ok {
			return UnifiWifiBroadcastSpec{}, nil, standardWarnings, false
		}
		spec.Standard = standard
		warnings = append(warnings, standardWarnings...)
	case WifiBroadcastTypeIotOptimized:
		spec.IotOptimized = &IotOptimizedWifiOptions{}
	default:
		return UnifiWifiBroadcastSpec{}, nil, []string{fmt.Sprintf(
			"wifi broadcast %q: unknown type %q; skipping", broadcast.Name, broadcast.Type)}, false
	}

	selectors, deviceAnnotations, deviceWarnings, ok := wifiDeviceTags(broadcast.BroadcastingDeviceFilter, deviceTags)
	if !ok {
		return UnifiWifiBroadcastSpec{}, nil, deviceWarnings, false
	}
	spec.DeviceTags = selectors
	for key, value := range deviceAnnotations {
		setAnnotation(&annotations, key, value)
	}
	warnings = append(warnings, deviceWarnings...)

	if usedPassphrase {
		setAnnotation(&annotations, AnnotationUnresolvedWifiSecret, wifiSecretWarning)
		warnings = append(warnings, fmt.Sprintf(
			"wifi broadcast %q: the passphrase is not recoverable from the console; set the placeholder Secret %q before applying (annotation %s)",
			broadcast.Name, unresolvedWifiSecretName, AnnotationUnresolvedWifiSecret))
	}
	if usedRadius {
		setAnnotation(&annotations, AnnotationUnresolvedRadiusProfile, wifiRadiusWarning)
		warnings = append(warnings, fmt.Sprintf(
			"wifi broadcast %q: the RADIUS profile UUID is not representable; set the placeholder UnifiRadiusProfile %q before applying (annotation %s)",
			broadcast.Name, unresolvedRadiusProfileName, AnnotationUnresolvedRadiusProfile))
	}

	// The CLI projects a reduced set of settings (design D8); record and warn about
	// any upstream setting that is not represented rather than dropping it silently.
	if unrepresented := broadcast.UnrepresentedSettings(); len(unrepresented) > 0 {
		setAnnotation(&annotations, AnnotationUnrepresentedWifiSettings,
			"settings not represented in spec: "+strings.Join(unrepresented, ", "))
		warnings = append(warnings, fmt.Sprintf(
			"wifi broadcast %q: settings not represented in spec (%s); set them manually if needed (annotation %s)",
			broadcast.Name, strings.Join(unrepresented, ", "), AnnotationUnrepresentedWifiSettings))
	}
	return spec, annotations, warnings, true
}

// broadcastNetworkName resolves the upstream network reference to an emitted UnifiNetwork
// name. Only the SPECIFIC variant maps to a name; the NATIVE variant and an unknown
// network id cannot be represented.
func broadcastNetworkName(networkNames map[string]string, ref *unifi.WifiNetworkReference) (string, bool) {
	if ref == nil || ref.Type != "SPECIFIC" || ref.NetworkID == "" {
		return "", false
	}
	name, ok := networkNames[ref.NetworkID]
	return name, ok
}

// standardWifiOptions maps the observed STANDARD variant. It returns ok=false when a
// required STANDARD field is missing or a frequency is outside the CRD enum.
func standardWifiOptions(broadcast unifi.WifiBroadcast) (*StandardWifiOptions, []string, bool) {
	if broadcast.AdvertiseDeviceName == nil || broadcast.ArpProxyEnabled == nil || broadcast.BssTransitionEnabled == nil {
		return nil, []string{fmt.Sprintf(
			"wifi broadcast %q: STANDARD variant fields are missing from the controller response; skipping", broadcast.Name)}, false
	}
	frequencies, err := frequencyStrings(broadcast.BroadcastingFrequenciesGHz)
	if err != nil {
		return nil, []string{fmt.Sprintf("wifi broadcast %q: %v; skipping", broadcast.Name, err)}, false
	}
	return &StandardWifiOptions{
		AdvertiseDeviceName:        *broadcast.AdvertiseDeviceName,
		ArpProxyEnabled:            *broadcast.ArpProxyEnabled,
		BssTransitionEnabled:       *broadcast.BssTransitionEnabled,
		BroadcastingFrequenciesGHz: frequencies,
	}, nil, true
}

// frequencyStrings maps the numeric upstream frequency bands onto the CRD's exact string
// enum. It returns an error for a band the CRD does not accept.
func frequencyStrings(in []float64) ([]string, error) {
	if len(in) == 0 {
		return nil, errors.New("broadcastingFrequenciesGHz is empty")
	}
	out := make([]string, 0, len(in))
	for _, frequency := range in {
		switch frequency {
		case 2.4:
			out = append(out, "2.4")
		case 5:
			out = append(out, "5")
		case 6:
			out = append(out, "6")
		default:
			return nil, fmt.Errorf("frequency %g is not one of 2.4, 5, 6", frequency)
		}
	}
	return out, nil
}

// wifiDeviceTags maps the upstream broadcasting device filter to name-based selectors. A
// DEVICE_TAGS filter is reverse-mapped to tag names via the read-only device-tag list; an
// unresolved tag, or an unrepresentable DEVICES (raw device UUID) filter, is recorded in
// an annotation and a warning. It returns ok=false only for an internal error; an
// unresolved filter still yields a valid resource.
func wifiDeviceTags(filter *unifi.WifiBroadcastingDeviceFilter, deviceTags []unifi.DeviceTag) ([]DeviceTagSelector, map[string]string, []string, bool) {
	if filter == nil {
		return nil, nil, nil, true
	}
	switch filter.Type {
	case "DEVICE_TAGS":
		selectors := make([]DeviceTagSelector, 0, len(filter.DeviceTagIDs))
		unresolved := 0
		for _, id := range filter.DeviceTagIDs {
			name, ok := deviceTagNameForID(deviceTags, id)
			if !ok {
				unresolved++
				continue
			}
			selectors = append(selectors, DeviceTagSelector{Name: name})
		}
		if unresolved == 0 {
			return selectors, nil, nil, true
		}
		annotations := map[string]string{AnnotationUnresolvedDeviceTag: "device scope is incomplete: some upstream device tags were not emitted as names; review spec.deviceTags"}
		warnings := []string{fmt.Sprintf(
			"%d device tag(s) in the device scope were not resolved to emitted names; spec.deviceTags may be incomplete", unresolved)}
		return selectors, annotations, warnings, true
	case "DEVICES":
		annotations := map[string]string{AnnotationUnresolvedDeviceTag: "device scope uses raw device IDs, which cannot be represented; spec.deviceTags was omitted and the broadcast applies to all AP-capable devices"}
		warnings := []string{"device scope uses raw device IDs which cannot be represented; spec.deviceTags was omitted"}
		return nil, annotations, warnings, true
	default:
		return nil, nil, nil, true
	}
}

// deviceTagNameForID returns the name of the device tag with the given UUID when it
// resolves to exactly one device. The operator requires a single-device tag, so a
// multi-device tag is treated as unresolvable and kept out of the emitted selector.
func deviceTagNameForID(deviceTags []unifi.DeviceTag, tagID string) (string, bool) {
	for _, tag := range deviceTags {
		if tag.ID != tagID {
			continue
		}
		if len(tag.DeviceIDs) != 1 {
			return "", false
		}
		return tag.Name, true
	}
	return "", false
}

// wifiSecurityConfiguration maps the observed security variant onto the emitted union.
// Personal passphrases are replaced with a placeholder Secret reference (the console does
// not expose a recoverable value); usedPassphrase reports whether one was emitted. An
// enterprise variant's RADIUS profile UUID is replaced with a placeholder UnifiRadiusProfile
// reference; usedRadius reports whether one was emitted. The returned error means the
// broadcast cannot be represented and should be skipped.
func wifiSecurityConfiguration(in *unifi.WifiSecurityConfiguration, networkNames map[string]string) (WifiSecurityConfiguration, bool, bool, error) {
	if in == nil {
		return WifiSecurityConfiguration{}, false, false, errors.New("securityConfiguration is missing from the controller response")
	}
	placeholder := SecretKeySelector{Name: unresolvedWifiSecretName, Key: unresolvedWifiSecretKey}

	switch in.Type {
	case WifiSecurityOpen:
		out := WifiSecurityConfiguration{Type: in.Type, Open: &WifiOpenSecurityConfiguration{}}
		if in.Encryption != "" {
			encryption := in.Encryption
			out.Open.Encryption = &encryption
		}
		return out, false, false, nil
	case WifiSecurityWPA2Personal:
		variant := &WifiWPA2PersonalSecurityConfiguration{
			FastRoamingEnabled:        in.FastRoamingEnabled,
			GroupRekeyIntervalSeconds: in.GroupRekeyIntervalSeconds,
		}
		// The console cannot return a recoverable passphrase, so always emit the
		// placeholder (even when the detail omits it) and annotate: a WPA2_PERSONAL
		// broadcast must still tell the user to supply the passphrase.
		passphrase := placeholder
		variant.Passphrase = &passphrase
		if in.PmfMode != "" {
			pmfMode := in.PmfMode
			variant.PmfMode = &pmfMode
		}
		preshared, err := wifiPresharedKeys(in.PresharedKeys, networkNames, placeholder)
		if err != nil {
			return WifiSecurityConfiguration{}, false, false, err
		}
		if len(preshared) > 0 {
			variant.PresharedKeys = preshared
		}
		return WifiSecurityConfiguration{Type: in.Type, WPA2Personal: variant}, true, false, nil
	case WifiSecurityWPA2WPA3Personal:
		if in.SaeConfiguration == nil {
			return WifiSecurityConfiguration{}, false, false, errors.New("WPA2_WPA3_PERSONAL security is missing saeConfiguration")
		}
		return WifiSecurityConfiguration{
			Type: in.Type,
			WPA2WPA3Personal: &WifiWPA2WPA3PersonalSecurityConfiguration{
				Passphrase:                placeholder,
				PmfMode:                   in.PmfMode,
				SaeConfiguration:          wifiSAE(in.SaeConfiguration),
				Wpa3FastRoamingEnabled:    in.Wpa3FastRoamingEnabled != nil && *in.Wpa3FastRoamingEnabled,
				FastRoamingEnabled:        in.FastRoamingEnabled,
				GroupRekeyIntervalSeconds: in.GroupRekeyIntervalSeconds,
			},
		}, true, false, nil
	case WifiSecurityWPA3Personal:
		if in.SaeConfiguration == nil {
			return WifiSecurityConfiguration{}, false, false, errors.New("WPA3_PERSONAL security is missing saeConfiguration")
		}
		return WifiSecurityConfiguration{
			Type: in.Type,
			WPA3Personal: &WifiWPA3PersonalSecurityConfiguration{
				Passphrase:                placeholder,
				SaeConfiguration:          wifiSAE(in.SaeConfiguration),
				FastRoamingEnabled:        in.FastRoamingEnabled,
				GroupRekeyIntervalSeconds: in.GroupRekeyIntervalSeconds,
			},
		}, true, false, nil
	case WifiSecurityWPA2Enterprise:
		variant := &WifiWPA2EnterpriseSecurityConfiguration{
			CoaEnabled:                in.CoaEnabled != nil && *in.CoaEnabled,
			RadiusConfiguration:       enterpriseRadiusConfiguration(in),
			FastRoamingEnabled:        in.FastRoamingEnabled,
			GroupRekeyIntervalSeconds: in.GroupRekeyIntervalSeconds,
		}
		if in.PmfMode != "" {
			pmfMode := in.PmfMode
			variant.PmfMode = &pmfMode
		}
		return WifiSecurityConfiguration{Type: in.Type, WPA2Enterprise: variant}, false, true, nil
	case WifiSecurityWPA2WPA3Enterprise:
		return WifiSecurityConfiguration{
			Type: in.Type,
			WPA2WPA3Enterprise: &WifiWPA2WPA3EnterpriseSecurityConfiguration{
				CoaEnabled:                in.CoaEnabled != nil && *in.CoaEnabled,
				PmfMode:                   in.PmfMode,
				RadiusConfiguration:       enterpriseRadiusConfiguration(in),
				Wpa3FastRoamingEnabled:    in.Wpa3FastRoamingEnabled != nil && *in.Wpa3FastRoamingEnabled,
				FastRoamingEnabled:        in.FastRoamingEnabled,
				GroupRekeyIntervalSeconds: in.GroupRekeyIntervalSeconds,
			},
		}, false, true, nil
	case WifiSecurityWPA3Enterprise:
		return WifiSecurityConfiguration{
			Type: in.Type,
			WPA3Enterprise: &WifiWPA3EnterpriseSecurityConfiguration{
				CoaEnabled:                in.CoaEnabled != nil && *in.CoaEnabled,
				SecurityMode:              in.SecurityMode,
				RadiusConfiguration:       enterpriseRadiusConfiguration(in),
				FastRoamingEnabled:        in.FastRoamingEnabled,
				GroupRekeyIntervalSeconds: in.GroupRekeyIntervalSeconds,
			},
		}, false, true, nil
	default:
		return WifiSecurityConfiguration{}, false, false, fmt.Errorf("unknown security type %q", in.Type)
	}
}

// enterpriseRadiusConfiguration maps the observed enterprise RADIUS configuration, replacing
// the opaque profile UUID with a placeholder UnifiRadiusProfile reference.
func enterpriseRadiusConfiguration(in *unifi.WifiSecurityConfiguration) WifiEnterpriseRadiusConfiguration {
	out := WifiEnterpriseRadiusConfiguration{
		RadiusProfileRef: CoreRef{Name: unresolvedRadiusProfileName},
	}
	if in.RadiusConfiguration == nil {
		return out
	}
	out.NasID = WifiRadiusNasID{
		Type:   in.RadiusConfiguration.NasID.Type,
		Source: in.RadiusConfiguration.NasID.Source,
		Value:  in.RadiusConfiguration.NasID.Value,
	}
	if mac := in.RadiusConfiguration.MACAuthenticationConfiguration; mac != nil {
		out.MACAuthenticationConfiguration = &WifiRadiusMacAuthenticationConfiguration{MacAddressFormat: mac.MacAddressFormat}
	}
	return out
}

// wifiPresharedKeys maps per-network preshared keys, resolving each network to an emitted
// UnifiNetwork name and replacing the passphrase with the placeholder.
func wifiPresharedKeys(in []unifi.WifiPresharedKey, networkNames map[string]string, placeholder SecretKeySelector) ([]WifiPresharedKey, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]WifiPresharedKey, 0, len(in))
	for _, key := range in {
		ref := key.Network
		name, ok := broadcastNetworkName(networkNames, &ref)
		if !ok {
			return nil, errors.New("a preshared-key network could not be resolved to an emitted UnifiNetwork")
		}
		out = append(out, WifiPresharedKey{Network: CoreRef{Name: name}, Passphrase: placeholder})
	}
	return out, nil
}

// wifiSAE maps the observed SAE configuration.
func wifiSAE(in *unifi.WifiSAEConfiguration) WifiSAEConfiguration {
	return WifiSAEConfiguration{
		AnticloggingThresholdSeconds: in.AnticloggingThresholdSeconds,
		SyncTimeSeconds:              in.SyncTimeSeconds,
	}
}

// mapIPv4 validates and maps the observed IPv4 configuration.
func mapIPv4(in *unifi.IPv4Configuration) (*IPv4Configuration, error) {
	if in.HostIPAddress == "" || in.PrefixLength < minIPv4Prefix || in.PrefixLength > maxIPv4Prefix {
		return nil, fmt.Errorf(
			"ipv4Configuration is incomplete or out of range (hostIpAddress=%q prefixLength=%d)",
			in.HostIPAddress, in.PrefixLength)
	}
	return &IPv4Configuration{
		AutoScaleEnabled:        in.AutoScaleEnabled,
		HostIPAddress:           in.HostIPAddress,
		PrefixLength:            in.PrefixLength,
		AdditionalHostIPSubnets: in.AdditionalHostIPSubnets,
	}, nil
}

// mapIPv6 maps the observed IPv6 configuration. Prefix-delegation IPv6 references the WAN
// by opaque id, which the CR models by name; it cannot be represented without a WAN
// lookup, so it returns ok=false with a human-readable reason.
func mapIPv6(in *unifi.IPv6Configuration) (*IPv6Configuration, string, bool) {
	switch in.InterfaceType {
	case "STATIC":
		if in.HostIPAddress == "" || in.PrefixLength < minIPv6Prefix || in.PrefixLength > maxIPv6Prefix {
			return nil, "IPv6 static configuration is incomplete or out of range; ipv6Configuration was omitted", false
		}
		out := &IPv6Configuration{
			InterfaceType:                in.InterfaceType,
			ClientAddressAssignment:      IPv6ClientAddressAssignment{SLAACEnabled: in.ClientAddressAssignment.SLAACEnabled},
			AdditionalHostIPSubnets:      in.AdditionalHostIPSubnets,
			DNSServerIPAddressesOverride: in.DNSServerIPAddressesOverride,
			HostIPAddress:                in.HostIPAddress,
			PrefixLength:                 in.PrefixLength,
		}
		if in.RouterAdvertisement != nil {
			out.RouterAdvertisement = &RouterAdvertisement{Priority: in.RouterAdvertisement.Priority}
		}
		return out, "", true
	case "PREFIX_DELEGATION":
		return nil, "IPv6 prefix delegation selects the WAN by opaque id, which cannot be represented as a WAN name; ipv6Configuration was omitted", false
	default:
		return nil, fmt.Sprintf("unknown IPv6 interface type %q; ipv6Configuration was omitted", in.InterfaceType), false
	}
}

// setAnnotation records a non-empty annotation on the map, allocating it on first use.
func setAnnotation(annotations *map[string]string, key, value string) {
	if *annotations == nil {
		*annotations = map[string]string{}
	}
	(*annotations)[key] = value
}

// DNSSafeName converts a UniFi object name into a DNS-1123 subdomain-safe resource name.
// Non-alphanumeric runs collapse to a single dash; an empty result falls back to
// "network".
func DNSSafeName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case b.Len() > 0 && !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "network"
	}
	return out
}

// nameAllocator assigns deterministic, unique DNS-1123 names. Collisions get a stable
// numeric suffix; the suffix is assigned in input order, so the result is deterministic.
type nameAllocator struct {
	used map[string]bool
	next map[string]int
}

func newNameAllocator() *nameAllocator {
	return &nameAllocator{used: map[string]bool{}, next: map[string]int{}}
}

// allocate returns the name to use for base and whether base was already taken.
func (a *nameAllocator) allocate(base string) (string, bool) {
	if !a.used[base] {
		a.used[base] = true
		a.next[base] = 1
		return base, false
	}
	next := a.next[base]
	if next == 0 {
		next = 1
	}
	for {
		next++
		name := fmt.Sprintf("%s-%d", base, next)
		if !a.used[name] {
			a.used[name] = true
			a.next[base] = next
			return name, true
		}
	}
}
