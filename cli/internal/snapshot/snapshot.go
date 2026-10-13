// Package snapshot projects UniFi controller objects into Custom Resources that a user
// can `kubectl apply` and have the operator adopt.
package snapshot

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
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
	// KindUnifiRadiusProfile is the kind for a projected (adopted) UniFi RADIUS profile.
	KindUnifiRadiusProfile = "UnifiRadiusProfile"
	// KindUnifiTrafficMatchingList is the kind for a projected UniFi traffic matching list.
	KindUnifiTrafficMatchingList = "UnifiTrafficMatchingList"
	// KindUnifiFirewallPolicy is the kind for a projected UniFi firewall policy.
	KindUnifiFirewallPolicy = "UnifiFirewallPolicy"
	// KindUnifiAclRule is the kind for a projected UniFi ACL rule.
	KindUnifiAclRule = "UnifiAclRule"
	// KindUnifiDnsPolicy is the kind for a projected UniFi DNS policy.
	KindUnifiDnsPolicy = "UnifiDnsPolicy"
	// KindUnifiSwitchingLag is the kind for a projected (adopted) UniFi LAG.
	KindUnifiSwitchingLag = "UnifiSwitchingLag"
	// KindUnifiSwitchingMcLagDomain is the kind for a projected (adopted) UniFi MC-LAG domain.
	KindUnifiSwitchingMcLagDomain = "UnifiSwitchingMcLagDomain"
	// KindUnifiSwitchingSwitchStack is the kind for a projected (adopted) UniFi switch stack.
	KindUnifiSwitchingSwitchStack = "UnifiSwitchingSwitchStack"
	// KindUnifiVpnServer is the kind for a projected (adopted) UniFi VPN server.
	KindUnifiVpnServer = "UnifiVpnServer"
	// KindUnifiSiteToSiteTunnel is the kind for a projected (adopted) UniFi site-to-site tunnel.
	KindUnifiSiteToSiteTunnel = "UnifiSiteToSiteTunnel"

	// LAG type discriminators accepted by UnifiSwitchingLag.spec.type.
	switchingLagTypeLocal        = "LOCAL"
	switchingLagTypeMultiChassis = "MULTI_CHASSIS"
	switchingLagTypeSwitchStack  = "SWITCH_STACK"

	// aclRuleTypeIPv4 and aclRuleTypeMAC are the rule discriminators accepted by
	// UnifiAclRule.spec.type.
	aclRuleTypeIPv4 = "IPV4"
	aclRuleTypeMAC  = "MAC"

	// DNS-policy variant discriminators accepted by UnifiDnsPolicy.spec.type.
	dnsTypeARecord       = "A_RECORD"
	dnsTypeAaaaRecord    = "AAAA_RECORD"
	dnsTypeCnameRecord   = "CNAME_RECORD"
	dnsTypeMxRecord      = "MX_RECORD"
	dnsTypeSrvRecord     = "SRV_RECORD"
	dnsTypeTxtRecord     = "TXT_RECORD"
	dnsTypeForwardDomain = "FORWARD_DOMAIN"

	// DNS record TTL bounds declared by the CRD.
	dnsMinTTL        = 0
	dnsMaxAddressTTL = 86400
	dnsMaxCnameTTL   = 604800
	dnsMaxPriority   = 65535

	// firewallZoneOriginUserDefined is the upstream metadata.origin of a zone the
	// snapshot projects to a configurable UnifiFirewallZone; any other origin is a
	// system zone, targeted through the built-in-zone selector.
	firewallZoneOriginUserDefined = "USER_DEFINED"

	// TrafficMatchingTypeIPv4Addresses, TrafficMatchingTypeIPv6Addresses, and
	// TrafficMatchingTypePorts are the list-type discriminator values accepted by
	// UnifiTrafficMatchingList.spec.type.
	TrafficMatchingTypeIPv4Addresses = "IPV4_ADDRESSES"
	TrafficMatchingTypeIPv6Addresses = "IPV6_ADDRESSES"
	TrafficMatchingTypePorts         = "PORTS"

	// Item type discriminators accepted by UnifiTrafficMatchingList.spec.items[].type.
	TrafficMatchingItemIPAddress       = "IP_ADDRESS"
	TrafficMatchingItemIPAddressRange  = "IP_ADDRESS_RANGE"
	TrafficMatchingItemSubnet          = "SUBNET"
	TrafficMatchingItemPortNumber      = "PORT_NUMBER"
	TrafficMatchingItemPortNumberRange = "PORT_NUMBER_RANGE"

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

	// minPort and maxPort are the CRD's bounds on a traffic matching list's
	// port-number entries.
	minPort = 1
	maxPort = 65535

	// maxIPv4AddressLength is the CRD's MaxLength on a trusted DHCP server
	// address; a well-formed IPv4 address is at most "255.255.255.255".
	maxIPv4AddressLength = 15

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
	AdditionalHostIPSubnets []string `json:"additionalHostIPSubnets,omitempty"`

	// DHCPConfiguration is the DHCP configuration handed to clients on this network.
	// Omit to leave DHCP unconfigured.
	DHCPConfiguration *DHCPConfiguration `json:"dhcpConfiguration,omitempty"`
}

// DHCPConfiguration is the emitted DHCP configuration nested under IPv4Configuration. It
// mirrors the operator's UnifiNetworkSpec.dhcpConfiguration so an emitted network keeps
// its console DHCP settings when adopted. It is pure desired state and carries no upstream
// identifier.
type DHCPConfiguration struct {
	Mode                         string              `json:"mode"`
	IPAddressRange               *DHCPIPAddressRange `json:"ipAddressRange,omitempty"`
	DNSServerIPAddressesOverride []string            `json:"dnsServerIpAddressesOverride,omitempty"`
	LeaseTimeSeconds             int32               `json:"leaseTimeSeconds,omitempty"`
	DomainName                   string              `json:"domainName,omitempty"`
	PingConflictDetectionEnabled bool                `json:"pingConflictDetectionEnabled,omitempty"`
}

// DHCPIPAddressRange is the lease address range of an emitted DHCP configuration.
type DHCPIPAddressRange struct {
	Start string `json:"start"`
	Stop  string `json:"stop"`
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

// UnifiRadiusProfileSpec is the desired state emitted for an adopted UniFi RADIUS
// profile. It mirrors the operator's UnifiRadiusProfileSpec adoption shape: the CR
// references its UnifiSite by name and names the existing upstream profile to adopt.
// The upstream profile UUID is left to status (docs/crd-conventions.md).
type UnifiRadiusProfileSpec struct {
	SiteRef CoreRef `json:"siteRef"`
	Name    string  `json:"name"`
}

// RadiusProfileStatus records the adopted upstream RADIUS profile UUID on an emitted
// UnifiRadiusProfile. The UUID is an upstream correlation identifier, so it belongs in
// status only, never in spec.
type RadiusProfileStatus struct {
	RadiusProfileID string `json:"radiusProfileID,omitempty"`
}

// TrafficMatchingEntry is an emitted entry of a UnifiTrafficMatchingList. It mirrors the
// operator's TrafficMatchingEntry discriminated union: a Type discriminator plus exactly
// one variant sub-object (address, addressRange, port, or portRange). It carries no
// upstream identifier.
type TrafficMatchingEntry struct {
	Type         string                            `json:"type"`
	Address      *TrafficMatchingAddressEntry      `json:"address,omitempty"`
	AddressRange *TrafficMatchingAddressRangeEntry `json:"addressRange,omitempty"`
	Port         *TrafficMatchingPortEntry         `json:"port,omitempty"`
	PortRange    *TrafficMatchingPortRangeEntry    `json:"portRange,omitempty"`
}

// TrafficMatchingAddressEntry is a single IP address or CIDR subnet value.
type TrafficMatchingAddressEntry struct {
	Value string `json:"value"`
}

// TrafficMatchingAddressRangeEntry is an inclusive IP address range.
type TrafficMatchingAddressRangeEntry struct {
	Start string `json:"start"`
	Stop  string `json:"stop"`
}

// TrafficMatchingPortEntry is a single port number (1..65535).
type TrafficMatchingPortEntry struct {
	Value int32 `json:"value"`
}

// TrafficMatchingPortRangeEntry is an inclusive port range (1..65535).
type TrafficMatchingPortRangeEntry struct {
	Start int32 `json:"start"`
	Stop  int32 `json:"stop"`
}

// UnifiTrafficMatchingListSpec is the desired state emitted for a UniFi traffic
// matching list. It mirrors the operator's UnifiTrafficMatchingListSpec: the CR
// references its UnifiSite by name, names the list, and expresses its entries as typed
// values and ranges. It carries no upstream list identifier; the list UUID is left to
// status.
type UnifiTrafficMatchingListSpec struct {
	SiteRef CoreRef                `json:"siteRef"`
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Items   []TrafficMatchingEntry `json:"items"`
}

// TrafficMatchingListStatus records the upstream traffic matching list UUID on an
// emitted UnifiTrafficMatchingList. The UUID belongs in status only, never in spec.
type TrafficMatchingListStatus struct {
	ListID string `json:"listID,omitempty"`
}

// BuiltInFirewallZoneSelector selects a system-defined upstream firewall zone by
// name. System zones are read-only; no opaque zone UUID appears in spec.
type BuiltInFirewallZoneSelector struct {
	Name string `json:"name"`
}

// FirewallTrafficMatchingListFilter references a UnifiTrafficMatchingList by Kubernetes
// identity. The controller resolves the reference to the list's upstream UUID at
// reconcile time.
type FirewallTrafficMatchingListFilter struct {
	ListRef       CoreRef `json:"listRef"`
	MatchOpposite *bool   `json:"matchOpposite,omitempty"`
}

// FirewallTrafficFilter is the emitted typed traffic filter: a type discriminator plus
// the ipAddress or port variant, each backed by a UnifiTrafficMatchingList reference.
type FirewallTrafficFilter struct {
	Type      string                             `json:"type"`
	IPAddress *FirewallTrafficMatchingListFilter `json:"ipAddress,omitempty"`
	Port      *FirewallTrafficMatchingListFilter `json:"port,omitempty"`
}

// FirewallPolicyEndpoint is the emitted source or destination of a firewall policy: a
// configurable zone by Kubernetes identity (zoneRef) or a system zone by name
// (builtInZone), plus an optional typed traffic filter. Exactly one of zoneRef or
// builtInZone is set.
type FirewallPolicyEndpoint struct {
	ZoneRef     *CoreRef                     `json:"zoneRef,omitempty"`
	BuiltInZone *BuiltInFirewallZoneSelector `json:"builtInZone,omitempty"`
	Filter      *FirewallTrafficFilter       `json:"filter,omitempty"`
}

// FirewallPolicyAction is the emitted action. AllowReturnTraffic is set only for type
// ALLOW.
type FirewallPolicyAction struct {
	Type               string `json:"type"`
	AllowReturnTraffic *bool  `json:"allowReturnTraffic,omitempty"`
}

// FirewallIPProtocolScope is the emitted IP protocol scope.
type FirewallIPProtocolScope struct {
	IPVersion string `json:"ipVersion"`
}

// UnifiFirewallPolicySpec is the desired state emitted for a UniFi firewall policy. It
// mirrors the operator's UnifiFirewallPolicySpec: configurable zones are referenced by
// Kubernetes identity, system zones through a typed built-in-zone selector, and traffic
// filters through typed references to a UnifiTrafficMatchingList. It carries no upstream
// identifier; policy and zone UUIDs are left to status.
type UnifiFirewallPolicySpec struct {
	SiteRef         CoreRef                 `json:"siteRef"`
	Name            string                  `json:"name"`
	Enabled         *bool                   `json:"enabled,omitempty"`
	Description     *string                 `json:"description,omitempty"`
	LoggingEnabled  *bool                   `json:"loggingEnabled,omitempty"`
	Action          FirewallPolicyAction    `json:"action"`
	IPProtocolScope FirewallIPProtocolScope `json:"ipProtocolScope"`
	Source          FirewallPolicyEndpoint  `json:"source"`
	Destination     FirewallPolicyEndpoint  `json:"destination"`
}

// FirewallPolicyStatus records the upstream firewall policy UUID and the resolved source
// and destination zone UUIDs on an emitted UnifiFirewallPolicy. The UUIDs belong in
// status only, never in spec.
type FirewallPolicyStatus struct {
	PolicyID          string `json:"policyID,omitempty"`
	SourceZoneID      string `json:"sourceZoneID,omitempty"`
	DestinationZoneID string `json:"destinationZoneID,omitempty"`
	Origin            string `json:"origin,omitempty"`
}

// AclPort is a TCP/UDP port number (1..65535).
type AclPort int32

// AclIPAddressesEndpoint is an emitted IP-addresses-or-subnets filter.
type AclIPAddressesEndpoint struct {
	Addresses []string  `json:"addresses"`
	Ports     []AclPort `json:"ports,omitempty"`
}

// AclNetworksEndpoint is an emitted network-membership filter. Networks are referenced by
// Kubernetes identity; no opaque network UUID appears in spec.
type AclNetworksEndpoint struct {
	NetworkRefs []CoreRef `json:"networkRefs"`
	Ports       []AclPort `json:"ports,omitempty"`
}

// AclPortsEndpoint is an emitted port-set filter.
type AclPortsEndpoint struct {
	Ports []AclPort `json:"ports"`
}

// AclMacAddressesEndpoint is an emitted MAC-address filter.
type AclMacAddressesEndpoint struct {
	MacAddresses []string `json:"macAddresses"`
	PrefixLength *int32   `json:"prefixLength,omitempty"`
}

// AclRuleEndpoint is an emitted source or destination filter: a type discriminator plus
// exactly one variant sub-object.
type AclRuleEndpoint struct {
	Type                 string                   `json:"type"`
	IPAddressesOrSubnets *AclIPAddressesEndpoint  `json:"ipAddressesOrSubnets,omitempty"`
	Networks             *AclNetworksEndpoint     `json:"networks,omitempty"`
	Ports                *AclPortsEndpoint        `json:"ports,omitempty"`
	MacAddresses         *AclMacAddressesEndpoint `json:"macAddresses,omitempty"`
}

// UnifiAclRuleSpec is the desired state emitted for a UniFi ACL rule. It mirrors the
// operator's UnifiAclRuleSpec: typed source/destination filters, network membership by
// Kubernetes identity, and an enforcing-device selector by device-tag name. It carries no
// upstream identifier; the rule UUID is left to status.
type UnifiAclRuleSpec struct {
	SiteRef             CoreRef             `json:"siteRef"`
	Name                string              `json:"name"`
	Description         *string             `json:"description,omitempty"`
	Enabled             *bool               `json:"enabled,omitempty"`
	Action              string              `json:"action"`
	Type                string              `json:"type"`
	Source              AclRuleEndpoint     `json:"source"`
	Destination         AclRuleEndpoint     `json:"destination"`
	ProtocolFilter      []string            `json:"protocolFilter,omitempty"`
	NetworkRef          *CoreRef            `json:"networkRef,omitempty"`
	EnforcingDeviceTags []DeviceTagSelector `json:"enforcingDeviceTags,omitempty"`
}

// AclRuleStatus records the upstream ACL rule UUID and origin on an emitted UnifiAclRule.
// The UUID belongs in status only, never in spec.
type AclRuleStatus struct {
	RuleID string `json:"ruleID,omitempty"`
	Origin string `json:"origin,omitempty"`
}

// DnsARecord is the emitted A_RECORD variant.
type DnsARecord struct {
	IPv4Address string `json:"ipv4Address"`
	TTLSeconds  int32  `json:"ttlSeconds"`
}

// DnsAaaaRecord is the emitted AAAA_RECORD variant.
type DnsAaaaRecord struct {
	IPv6Address string `json:"ipv6Address"`
	TTLSeconds  int32  `json:"ttlSeconds"`
}

// DnsCnameRecord is the emitted CNAME_RECORD variant.
type DnsCnameRecord struct {
	TargetDomain string `json:"targetDomain"`
	TTLSeconds   int32  `json:"ttlSeconds"`
}

// DnsMxRecord is the emitted MX_RECORD variant.
type DnsMxRecord struct {
	MailServerDomain string `json:"mailServerDomain"`
	Priority         int32  `json:"priority"`
}

// DnsSrvRecord is the emitted SRV_RECORD variant.
type DnsSrvRecord struct {
	Port         int32  `json:"port"`
	Priority     int32  `json:"priority"`
	Protocol     string `json:"protocol"`
	ServerDomain string `json:"serverDomain"`
	Service      string `json:"service"`
	Weight       int32  `json:"weight"`
}

// DnsTxtRecord is the emitted TXT_RECORD variant.
type DnsTxtRecord struct {
	Text string `json:"text"`
}

// DnsForwardDomain is the emitted FORWARD_DOMAIN variant.
type DnsForwardDomain struct {
	IPAddress string `json:"ipAddress"`
}

// UnifiDnsPolicySpec is the desired state emitted for a UniFi DNS policy. It mirrors the
// operator's UnifiDnsPolicySpec: Type is the union discriminator and exactly one variant
// sub-object is emitted for the matching type. It carries no upstream identifier; the
// record UUID is left to status.
type UnifiDnsPolicySpec struct {
	SiteRef       CoreRef           `json:"siteRef"`
	Type          string            `json:"type"`
	Domain        string            `json:"domain"`
	Enabled       *bool             `json:"enabled,omitempty"`
	ARecord       *DnsARecord       `json:"aRecord,omitempty"`
	AaaaRecord    *DnsAaaaRecord    `json:"aaaaRecord,omitempty"`
	CnameRecord   *DnsCnameRecord   `json:"cnameRecord,omitempty"`
	MxRecord      *DnsMxRecord      `json:"mxRecord,omitempty"`
	SrvRecord     *DnsSrvRecord     `json:"srvRecord,omitempty"`
	TxtRecord     *DnsTxtRecord     `json:"txtRecord,omitempty"`
	ForwardDomain *DnsForwardDomain `json:"forwardDomain,omitempty"`
}

// DnsPolicyStatus records the upstream DNS policy UUID and origin on an emitted
// UnifiDnsPolicy. The UUID belongs in status only, never in spec.
type DnsPolicyStatus struct {
	PolicyID string `json:"policyID,omitempty"`
	Origin   string `json:"origin,omitempty"`
}

// SwitchingLagMemberSelector selects one member of a LAG by a read-only device-tag name
// and its member port indexes.
type SwitchingLagMemberSelector struct {
	DeviceTag DeviceTagSelector `json:"deviceTag"`
	PortIdxs  []int32           `json:"portIdxs"`
}

// SwitchingLagMember is an observed LAG member, recorded in status only.
type SwitchingLagMember struct {
	DeviceID string  `json:"deviceID,omitempty"`
	PortIdxs []int32 `json:"portIdxs,omitempty"`
}

// SwitchingMcLagPeer is an observed MC-LAG domain peer, recorded in status only.
type SwitchingMcLagPeer struct {
	DeviceID     string  `json:"deviceID,omitempty"`
	LinkPortIdxs []int32 `json:"linkPortIdxs,omitempty"`
	Role         string  `json:"role,omitempty"`
}

// SwitchingStackMember is an observed switch-stack member, recorded in status only.
type SwitchingStackMember struct {
	DeviceID string `json:"deviceID,omitempty"`
}

// UnifiSwitchingLagSpec is the desired state emitted for an adopted UniFi LAG. The
// upstream LAG has no name, so the CR adopts it by a typed composite selector: its type
// plus a member selector expressed through device-tag selectors and port indexes. No
// opaque UUID appears in spec.
type UnifiSwitchingLagSpec struct {
	SiteRef CoreRef                      `json:"siteRef"`
	Type    string                       `json:"type"`
	Members []SwitchingLagMemberSelector `json:"members"`
}

// UnifiSwitchingLagStatus records the adopted upstream LAG UUID and its observed
// configuration. The UUID belongs in status only, never in spec.
type UnifiSwitchingLagStatus struct {
	LagID   string               `json:"lagID,omitempty"`
	Type    string               `json:"type,omitempty"`
	Members []SwitchingLagMember `json:"members,omitempty"`
}

// UnifiSwitchingMcLagDomainSpec is the desired state emitted for an adopted UniFi MC-LAG
// domain: it references its site and names the existing upstream domain to adopt.
type UnifiSwitchingMcLagDomainSpec struct {
	SiteRef CoreRef `json:"siteRef"`
	Name    string  `json:"name"`
}

// UnifiSwitchingMcLagDomainStatus records the adopted upstream MC-LAG domain UUID and its
// observed members and peers. The UUID belongs in status only, never in spec.
type UnifiSwitchingMcLagDomainStatus struct {
	DomainID string               `json:"domainID,omitempty"`
	Members  []SwitchingLagMember `json:"members,omitempty"`
	Peers    []SwitchingMcLagPeer `json:"peers,omitempty"`
}

// UnifiSwitchingSwitchStackSpec is the desired state emitted for an adopted UniFi switch
// stack: it references its site and names the existing upstream stack to adopt.
type UnifiSwitchingSwitchStackSpec struct {
	SiteRef CoreRef `json:"siteRef"`
	Name    string  `json:"name"`
}

// UnifiSwitchingSwitchStackStatus records the adopted upstream switch-stack UUID and its
// observed members. The UUID belongs in status only, never in spec.
type UnifiSwitchingSwitchStackStatus struct {
	StackID string                 `json:"stackID,omitempty"`
	Members []SwitchingStackMember `json:"members,omitempty"`
}

// UnifiVpnServerSpec is the desired state emitted for an adopted UniFi VPN server: it
// references its site and names the existing upstream server to adopt. It authors no
// variant configuration and no credential.
type UnifiVpnServerSpec struct {
	SiteRef CoreRef `json:"siteRef"`
	Name    string  `json:"name"`
}

// VpnServerStatus records the adopted upstream VPN server UUID and its observed type and
// enabled state. The UUID belongs in status only, never in spec.
type VpnServerStatus struct {
	ServerID string `json:"serverID,omitempty"`
	Type     string `json:"type,omitempty"`
	Enabled  *bool  `json:"enabled,omitempty"`
}

// UnifiSiteToSiteTunnelSpec is the desired state emitted for an adopted UniFi site-to-site
// tunnel: it references its site and names the existing upstream tunnel to adopt. It
// authors no local/remote endpoints and no credential.
type UnifiSiteToSiteTunnelSpec struct {
	SiteRef CoreRef `json:"siteRef"`
	Name    string  `json:"name"`
}

// SiteToSiteTunnelStatus records the adopted upstream site-to-site tunnel UUID and its
// observed type. The UUID belongs in status only, never in spec.
type SiteToSiteTunnelStatus struct {
	TunnelID string `json:"tunnelID,omitempty"`
	Type     string `json:"type,omitempty"`
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
// CRD. It carries no upstream id or network UUID: a SPECIFIC broadcast references the
// emitted UnifiNetwork by name (spec.networkRef), a NATIVE broadcast sets
// spec.nativeNetwork plus a spec.siteRef naming the emitted UnifiSite, and the broadcast
// id is recorded in status only.
type UnifiWifiBroadcastSpec struct {
	NetworkRef                          *CoreRef                  `json:"networkRef,omitempty"`
	NativeNetwork                       *bool                     `json:"nativeNetwork,omitempty"`
	SiteRef                             *CoreRef                  `json:"siteRef,omitempty"`
	Type                                string                    `json:"type"`
	Name                                string                    `json:"name"`
	Enabled                             *bool                     `json:"enabled,omitempty"`
	HideName                            bool                      `json:"hideName"`
	ClientIsolationEnabled              bool                      `json:"clientIsolationEnabled"`
	MulticastToUnicastConversionEnabled bool                      `json:"multicastToUnicastConversionEnabled"`
	UapsdEnabled                        bool                      `json:"uapsdEnabled"`
	BandSteeringEnabled                 *bool                     `json:"bandSteeringEnabled,omitempty"`
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

	var annotations map[string]string
	var warnings []string

	if network.DHCPGuarding != nil && len(network.DHCPGuarding.TrustedDHCPServerIPAddresses) > 0 {
		// A live console can return junk entries such as ["192.168.2.1","",""], which
		// the tightened CRD schema now rejects. Keep only the addresses the schema
		// accepts and warn about the rest so the emitted CR stays valid.
		trusted, dropped := validTrustedDHCPServers(network.DHCPGuarding.TrustedDHCPServerIPAddresses)
		if len(trusted) > 0 {
			spec.DHCPGuarding = &DHCPGuarding{TrustedDHCPServerIPAddresses: trusted}
		}
		if dropped > 0 {
			warnings = append(warnings, fmt.Sprintf(
				"network %q: dropped %d invalid trusted DHCP server address(es) that the CRD dhcpGuarding schema rejects",
				network.Name, dropped))
		}
	}

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

// validTrustedDHCPServers keeps the entries that satisfy the CRD's tightened
// trustedDhcpServerIpAddresses schema — non-empty, well-formed IPv4 addresses of
// at most 15 characters — and returns the number it removed. A console can return
// junk entries such as ["192.168.2.1","",""], which the API server now rejects.
func validTrustedDHCPServers(in []string) ([]string, int) {
	out := make([]string, 0, len(in))
	dropped := 0
	for _, address := range in {
		if !validIPv4Address(address) {
			dropped++
			continue
		}
		out = append(out, address)
	}
	return out, dropped
}

// validIPv4Address reports whether address satisfies the CRD's
// trustedDhcpServerIpAddresses item schema: a string of at most 15 characters
// that parses as an IPv4 address. netip rejects IPv4-mapped IPv6 forms such as
// "::ffff:1.2.3.4" because Is4() is false for them, matching the CEL rule
// ip(self).family() == 4, and the length bound matches MaxLength=15.
func validIPv4Address(address string) bool {
	if len(address) > maxIPv4AddressLength {
		return false
	}
	parsed, err := netip.ParseAddr(address)
	return err == nil && parsed.Is4()
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
	names := firewallZoneCRNames(zones)

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
		name := names[zone.ID]
		if name != base {
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

// firewallZoneCRNames returns the emitted CR metadata.name for every upstream zone,
// keyed by zone UUID, using the same deterministic allocator sequence as FirewallZones
// so a firewall policy's zoneRef names exactly the UnifiFirewallZone the snapshot
// emitted, including any collision suffix.
func firewallZoneCRNames(zones []unifi.FirewallZone) map[string]string {
	names := make(map[string]string, len(zones))
	alloc := newNameAllocator()
	for _, zone := range zones {
		name, _ := alloc.allocate(DNSSafeName(zone.Name))
		names[zone.ID] = name
	}
	return names
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

// RadiusProfiles projects each upstream RADIUS profile into a UnifiRadiusProfile in
// the adoption shape: spec.siteRef names the emitted UnifiSite and spec.name names the
// existing upstream profile to adopt. The upstream profile UUID is recorded in status
// only, never in spec. siteName is the metadata.name of the target UnifiSite. Colliding
// projected names get a stable numeric suffix and an AnnotationOriginalName annotation
// rather than overwriting one another.
func RadiusProfiles(siteName string, profiles []unifi.RadiusProfile) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(profiles))
	var warnings []string
	alloc := newNameAllocator()

	for _, profile := range profiles {
		base := DNSSafeName(profile.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: profile.Name}
			warnings = append(warnings, fmt.Sprintf(
				"radius profile %q: projected name %q is already in use; emitting as %q",
				profile.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiRadiusProfile,
			Metadata:   metadata,
			Spec: UnifiRadiusProfileSpec{
				SiteRef: CoreRef{Name: siteName},
				Name:    profile.Name,
			},
			Status: RadiusProfileStatus{RadiusProfileID: profile.ID},
		})
	}
	return resources, warnings
}

// TrafficMatchingLists projects each upstream traffic matching list into a
// UnifiTrafficMatchingList. siteName is the metadata.name of the target UnifiSite.
// Each list's entries are mapped to typed values and ranges; the upstream list UUID is
// recorded in status only, never in spec. Colliding projected names get a stable numeric
// suffix and an AnnotationOriginalName annotation rather than overwriting one another.
//
// A list whose type is not one of IPV4_ADDRESSES/IPV6_ADDRESSES/PORTS, or that carries
// an entry which does not conform to its list type, cannot be represented faithfully and
// is skipped with a warning rather than emitting a partially projected resource
// (unifi-snapshot spec: unrepresentable objects are skipped with a warning).
func TrafficMatchingLists(siteName string, lists []unifi.TrafficMatchingList) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(lists))
	var warnings []string
	names := trafficMatchingListCRNames(lists)

	for _, list := range lists {
		items, reason, ok := trafficMatchingItems(list.Type, list.Items)
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"traffic matching list %q: %s; skipping", list.Name, reason))
			continue
		}

		base := DNSSafeName(list.Name)
		name := names[list.ID]

		var annotations map[string]string
		if name != base {
			annotations = map[string]string{AnnotationOriginalName: list.Name}
			warnings = append(warnings, fmt.Sprintf(
				"traffic matching list %q: projected name %q is already in use; emitting as %q",
				list.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiTrafficMatchingList,
			Metadata:   metadata,
			Spec: UnifiTrafficMatchingListSpec{
				SiteRef: CoreRef{Name: siteName},
				Name:    list.Name,
				Type:    list.Type,
				Items:   items,
			},
			Status: TrafficMatchingListStatus{ListID: list.ID},
		})
	}
	return resources, warnings
}

// trafficMatchingListCRNames returns the emitted CR metadata.name for every
// representable traffic matching list, keyed by upstream list UUID, using the same
// allocator sequence as TrafficMatchingLists. A list TrafficMatchingLists would skip
// (unknown type or a non-conforming entry) is absent, so a firewall policy filter that
// references it safe-skips rather than emitting an unresolvable reference.
func trafficMatchingListCRNames(lists []unifi.TrafficMatchingList) map[string]string {
	names := make(map[string]string, len(lists))
	alloc := newNameAllocator()
	for _, list := range lists {
		if _, _, ok := trafficMatchingItems(list.Type, list.Items); !ok {
			continue
		}
		name, _ := alloc.allocate(DNSSafeName(list.Name))
		names[list.ID] = name
	}
	return names
}

// trafficMatchingItems maps a list's upstream entries to the emitted typed entries. It
// returns ok=false with a human-readable reason when the list type is unknown, an entry
// does not conform to it, or a port value is not a valid number, so the caller skips the
// whole list rather than emit a partial projection.
func trafficMatchingItems(listType string, items []unifi.TrafficMatchingItem) ([]TrafficMatchingEntry, string, bool) {
	if !validTrafficMatchingType(listType) {
		return nil, fmt.Sprintf("unknown list type %q", listType), false
	}
	out := make([]TrafficMatchingEntry, 0, len(items))
	for _, item := range items {
		mapped, reason, ok := trafficMatchingItem(listType, item)
		if !ok {
			return nil, reason, false
		}
		out = append(out, mapped)
	}
	return out, "", true
}

// validTrafficMatchingType reports whether listType is a representable list discriminator.
func validTrafficMatchingType(listType string) bool {
	switch listType {
	case TrafficMatchingTypeIPv4Addresses, TrafficMatchingTypeIPv6Addresses, TrafficMatchingTypePorts:
		return true
	default:
		return false
	}
}

// trafficMatchingItem maps one upstream entry to the emitted discriminated entry,
// validating that the entry's type is valid for the list type and that its required
// fields are present (ports must parse as an integer in 1..65535).
func trafficMatchingItem(listType string, item unifi.TrafficMatchingItem) (TrafficMatchingEntry, string, bool) {
	switch item.Type {
	case TrafficMatchingItemIPAddress:
		if listType != TrafficMatchingTypeIPv4Addresses && listType != TrafficMatchingTypeIPv6Addresses {
			return TrafficMatchingEntry{}, itemTypeMismatch(item.Type, listType), false
		}
		if item.Value == "" {
			return TrafficMatchingEntry{}, "IP_ADDRESS entry is missing value", false
		}
		return TrafficMatchingEntry{Type: item.Type, Address: &TrafficMatchingAddressEntry{Value: item.Value}}, "", true
	case TrafficMatchingItemIPAddressRange:
		if listType != TrafficMatchingTypeIPv4Addresses {
			return TrafficMatchingEntry{}, itemTypeMismatch(item.Type, listType), false
		}
		if item.Start == "" || item.Stop == "" {
			return TrafficMatchingEntry{}, "IP_ADDRESS_RANGE entry is missing start or stop", false
		}
		return TrafficMatchingEntry{Type: item.Type, AddressRange: &TrafficMatchingAddressRangeEntry{Start: item.Start, Stop: item.Stop}}, "", true
	case TrafficMatchingItemSubnet:
		if listType != TrafficMatchingTypeIPv4Addresses && listType != TrafficMatchingTypeIPv6Addresses {
			return TrafficMatchingEntry{}, itemTypeMismatch(item.Type, listType), false
		}
		if item.Value == "" {
			return TrafficMatchingEntry{}, "SUBNET entry is missing value", false
		}
		return TrafficMatchingEntry{Type: item.Type, Address: &TrafficMatchingAddressEntry{Value: item.Value}}, "", true
	case TrafficMatchingItemPortNumber:
		if listType != TrafficMatchingTypePorts {
			return TrafficMatchingEntry{}, itemTypeMismatch(item.Type, listType), false
		}
		port, reason, ok := parsePort(item.Value, "PORT_NUMBER")
		if !ok {
			return TrafficMatchingEntry{}, reason, false
		}
		return TrafficMatchingEntry{Type: item.Type, Port: &TrafficMatchingPortEntry{Value: port}}, "", true
	case TrafficMatchingItemPortNumberRange:
		if listType != TrafficMatchingTypePorts {
			return TrafficMatchingEntry{}, itemTypeMismatch(item.Type, listType), false
		}
		start, reason, ok := parsePort(item.Start, "PORT_NUMBER_RANGE start")
		if !ok {
			return TrafficMatchingEntry{}, reason, false
		}
		stop, reason, ok := parsePort(item.Stop, "PORT_NUMBER_RANGE stop")
		if !ok {
			return TrafficMatchingEntry{}, reason, false
		}
		return TrafficMatchingEntry{Type: item.Type, PortRange: &TrafficMatchingPortRangeEntry{Start: start, Stop: stop}}, "", true
	default:
		return TrafficMatchingEntry{}, fmt.Sprintf("unknown item type %q", item.Type), false
	}
}

// parsePort parses an upstream port string into the CRD's 1..65535 integer range. It
// returns ok=false with a human-readable reason for a missing, non-numeric, or
// out-of-range value, so the caller skips the list rather than emit an invalid CR.
func parsePort(value, field string) (int32, string, bool) {
	if value == "" {
		return 0, fmt.Sprintf("%s is missing", field), false
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Sprintf("%s %q is not a number", field, value), false
	}
	if port < minPort || port > maxPort {
		return 0, fmt.Sprintf("%s %d is outside the CRD range %d..%d", field, port, minPort, maxPort), false
	}
	return int32(port), "", true
}

// itemTypeMismatch formats the reason an entry does not conform to its list type.
func itemTypeMismatch(itemType, listType string) string {
	return fmt.Sprintf("item type %q is not valid for list type %q", itemType, listType)
}

// FirewallPolicies projects each upstream firewall policy into a UnifiFirewallPolicy.
// siteName is the metadata.name of the target UnifiSite. zones and lists are the
// upstream site's firewall zones and traffic matching lists, used to resolve an opaque
// source/destination zone UUID to a configurable zone reference (the same UnifiFirewallZone
// name FirewallZones emits) or a built-in-zone selector, and a traffic filter's opaque
// list UUID to the same UnifiTrafficMatchingList name TrafficMatchingLists emits. No
// upstream UUID appears in spec; the policy and resolved zone UUIDs are recorded in
// status only. Colliding projected names get a stable numeric suffix and an
// AnnotationOriginalName annotation.
//
// A policy whose source or destination zone cannot be resolved, or whose traffic filter
// is not a TRAFFIC_MATCHING_LIST-backed IP_ADDRESS/PORT variant, cannot be represented
// faithfully and is skipped with a warning rather than emitting a partial CR
// (unifi-snapshot spec: unrepresentable objects are skipped with a warning).
func FirewallPolicies(siteName string, zones []unifi.FirewallZone, lists []unifi.TrafficMatchingList, policies []unifi.FirewallPolicy) ([]emit.Resource, []string) {
	zoneByID := make(map[string]unifi.FirewallZone, len(zones))
	for _, zone := range zones {
		zoneByID[zone.ID] = zone
	}
	zoneNames := firewallZoneCRNames(zones)
	listNames := trafficMatchingListCRNames(lists)

	resources := make([]emit.Resource, 0, len(policies))
	var warnings []string
	alloc := newNameAllocator()

	for _, policy := range policies {
		spec, reason, ok := buildFirewallPolicySpec(siteName, zoneByID, zoneNames, listNames, policy)
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"firewall policy %q: %s; skipping", policy.Name, reason))
			continue
		}

		base := DNSSafeName(policy.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: policy.Name}
			warnings = append(warnings, fmt.Sprintf(
				"firewall policy %q: projected name %q is already in use; emitting as %q",
				policy.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiFirewallPolicy,
			Metadata:   metadata,
			Spec:       spec,
			Status: FirewallPolicyStatus{
				PolicyID:          policy.ID,
				SourceZoneID:      policy.Source.ZoneID,
				DestinationZoneID: policy.Destination.ZoneID,
				Origin:            policy.Metadata.Origin,
			},
		})
	}
	return resources, warnings
}

// buildFirewallPolicySpec maps one upstream policy into a spec valid against the CRD. It
// returns ok=false (with a reason and no resource) when a zone reference or traffic
// filter cannot be represented faithfully, so the CLI never emits an invalid CR.
func buildFirewallPolicySpec(siteName string, zoneByID map[string]unifi.FirewallZone, zoneNames, listNames map[string]string, policy unifi.FirewallPolicy) (UnifiFirewallPolicySpec, string, bool) {
	source, reason, ok := firewallPolicyEndpoint(policy.Source, zoneByID, zoneNames, listNames)
	if !ok {
		return UnifiFirewallPolicySpec{}, fmt.Sprintf("source %s", reason), false
	}
	destination, reason, ok := firewallPolicyEndpoint(policy.Destination, zoneByID, zoneNames, listNames)
	if !ok {
		return UnifiFirewallPolicySpec{}, fmt.Sprintf("destination %s", reason), false
	}

	enabled := policy.Enabled
	loggingEnabled := policy.LoggingEnabled
	spec := UnifiFirewallPolicySpec{
		SiteRef:         CoreRef{Name: siteName},
		Name:            policy.Name,
		Enabled:         &enabled,
		LoggingEnabled:  &loggingEnabled,
		Action:          firewallPolicyAction(policy.Action),
		IPProtocolScope: FirewallIPProtocolScope{IPVersion: policy.IPProtocolScope.IPVersion},
		Source:          source,
		Destination:     destination,
	}
	if policy.Description != "" {
		description := policy.Description
		spec.Description = &description
	}
	return spec, "", true
}

// firewallPolicyAction maps the observed action. The CRD requires allowReturnTraffic for
// type ALLOW and forbids it otherwise, so a nil upstream value for ALLOW becomes false and
// it is never set for BLOCK/REJECT.
func firewallPolicyAction(in unifi.FirewallPolicyAction) FirewallPolicyAction {
	out := FirewallPolicyAction{Type: in.Type}
	if in.Type != unifi.FirewallPolicyActionAllow {
		return out
	}
	if in.AllowReturnTraffic != nil {
		out.AllowReturnTraffic = in.AllowReturnTraffic
		return out
	}
	allowReturnTraffic := false
	out.AllowReturnTraffic = &allowReturnTraffic
	return out
}

// firewallPolicyEndpoint maps an observed source/destination to a zone reference and an
// optional typed filter. A USER_DEFINED zone resolves to a configurable zoneRef naming the
// emitted UnifiFirewallZone; any other (system) zone resolves to a built-in-zone selector.
// It returns ok=false when the zone is absent from the list, a user-defined zone has no
// emitted name, or a filter cannot be represented.
func firewallPolicyEndpoint(endpoint unifi.FirewallPolicyZone, zoneByID map[string]unifi.FirewallZone, zoneNames, listNames map[string]string) (FirewallPolicyEndpoint, string, bool) {
	zone, ok := zoneByID[endpoint.ZoneID]
	if !ok {
		return FirewallPolicyEndpoint{}, "zone could not be resolved to an emitted firewall zone", false
	}

	out := FirewallPolicyEndpoint{}
	if zone.Metadata.Origin == firewallZoneOriginUserDefined {
		name, ok := zoneNames[zone.ID]
		if !ok {
			return FirewallPolicyEndpoint{}, fmt.Sprintf("user-defined zone %q has no emitted name", zone.Name), false
		}
		out.ZoneRef = &CoreRef{Name: name}
	} else {
		out.BuiltInZone = &BuiltInFirewallZoneSelector{Name: zone.Name}
	}

	if endpoint.TrafficFilter != nil {
		filter, reason, ok := firewallPolicyFilter(*endpoint.TrafficFilter, listNames)
		if !ok {
			return FirewallPolicyEndpoint{}, reason, false
		}
		out.Filter = filter
	}
	return out, "", true
}

// firewallPolicyFilter maps an observed traffic filter. Only the IP_ADDRESS and PORT
// variants backed by a TRAFFIC_MATCHING_LIST are modeled, so any other variant is
// unrepresentable and returns ok=false.
func firewallPolicyFilter(filter unifi.FirewallPolicyTrafficFilter, listNames map[string]string) (*FirewallTrafficFilter, string, bool) {
	matchOpposite := func(value bool) *bool { return &value }

	switch filter.Type {
	case unifi.FirewallPolicyTrafficFilterIPAddress:
		if filter.IPAddressFilter == nil {
			return nil, "IP_ADDRESS filter is missing its configuration", false
		}
		if filter.IPAddressFilter.Type != unifi.FirewallFilterTrafficMatchingList {
			return nil, fmt.Sprintf(
				"IP_ADDRESS filter variant %q is not representable (only TRAFFIC_MATCHING_LIST is modeled)",
				filter.IPAddressFilter.Type), false
		}
		ref, reason, ok := trafficMatchingListRef(filter.IPAddressFilter.TrafficMatchingListID, listNames)
		if !ok {
			return nil, reason, false
		}
		return &FirewallTrafficFilter{
			Type:      unifi.FirewallPolicyTrafficFilterIPAddress,
			IPAddress: &FirewallTrafficMatchingListFilter{ListRef: ref, MatchOpposite: matchOpposite(filter.IPAddressFilter.MatchOpposite)},
		}, "", true
	case unifi.FirewallPolicyTrafficFilterPort:
		if filter.PortFilter == nil {
			return nil, "PORT filter is missing its configuration", false
		}
		if filter.PortFilter.Type != unifi.FirewallFilterTrafficMatchingList {
			return nil, fmt.Sprintf(
				"PORT filter variant %q is not representable (only TRAFFIC_MATCHING_LIST is modeled)",
				filter.PortFilter.Type), false
		}
		ref, reason, ok := trafficMatchingListRef(filter.PortFilter.TrafficMatchingListID, listNames)
		if !ok {
			return nil, reason, false
		}
		return &FirewallTrafficFilter{
			Type: unifi.FirewallPolicyTrafficFilterPort,
			Port: &FirewallTrafficMatchingListFilter{ListRef: ref, MatchOpposite: matchOpposite(filter.PortFilter.MatchOpposite)},
		}, "", true
	default:
		return nil, fmt.Sprintf("traffic filter type %q is not representable", filter.Type), false
	}
}

// trafficMatchingListRef resolves an upstream traffic matching list UUID to the emitted
// UnifiTrafficMatchingList name.
func trafficMatchingListRef(listID string, listNames map[string]string) (CoreRef, string, bool) {
	if listID == "" {
		return CoreRef{}, "traffic filter does not name a matching list", false
	}
	name, ok := listNames[listID]
	if !ok {
		return CoreRef{}, "traffic filter references a matching list that was not emitted by this snapshot", false
	}
	return CoreRef{Name: name}, "", true
}

// AclRules projects each upstream ACL rule into a UnifiAclRule. siteName is the
// metadata.name of the target UnifiSite. networkNames is the upstream-network-id ->
// emitted-name map returned by Networks; a NETWORKS source/destination filter and a MAC
// rule's networkIdFilter are resolved through it so the references name the UnifiNetwork
// resources this snapshot emitted. deviceTags is the site's read-only device-tag list,
// used to resolve an enforcing-device UUID to the tag name(s) containing it. No upstream
// UUID appears in spec; the rule UUID and origin are recorded in status only. Colliding
// projected names get a stable numeric suffix and an AnnotationOriginalName annotation.
//
// A rule whose filters cannot be represented faithfully — an unknown type/action, a
// network reference that was not emitted, an unsupported endpoint shape, or an enforcing
// device that belongs to no device tag — is skipped with a warning rather than emitting a
// partial CR (unifi-snapshot spec: unrepresentable objects are skipped with a warning).
func AclRules(siteName string, networkNames map[string]string, deviceTags []unifi.DeviceTag, rules []unifi.AclRule) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(rules))
	var warnings []string
	alloc := newNameAllocator()

	for _, rule := range rules {
		spec, reason, ok := buildAclRuleSpec(siteName, networkNames, deviceTags, rule)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("acl rule %q: %s; skipping", rule.Name, reason))
			continue
		}

		base := DNSSafeName(rule.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: rule.Name}
			warnings = append(warnings, fmt.Sprintf(
				"acl rule %q: projected name %q is already in use; emitting as %q",
				rule.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiAclRule,
			Metadata:   metadata,
			Spec:       spec,
			Status:     AclRuleStatus{RuleID: rule.ID, Origin: rule.Metadata.Origin},
		})
	}
	return resources, warnings
}

// buildAclRuleSpec maps one upstream rule into a spec valid against the CRD. It returns
// ok=false (with a reason and no resource) when a field cannot be represented faithfully,
// so the CLI never emits an invalid CR.
func buildAclRuleSpec(siteName string, networkNames map[string]string, deviceTags []unifi.DeviceTag, rule unifi.AclRule) (UnifiAclRuleSpec, string, bool) {
	if !validAclType(rule.Type) {
		return UnifiAclRuleSpec{}, fmt.Sprintf("unknown rule type %q", rule.Type), false
	}
	if rule.Action != "ALLOW" && rule.Action != "BLOCK" {
		return UnifiAclRuleSpec{}, fmt.Sprintf("unknown action %q", rule.Action), false
	}

	source, reason, ok := aclEndpoint(rule.Source, networkNames)
	if !ok {
		return UnifiAclRuleSpec{}, "source " + reason, false
	}
	destination, reason, ok := aclEndpoint(rule.Destination, networkNames)
	if !ok {
		return UnifiAclRuleSpec{}, "destination " + reason, false
	}

	enabled := rule.Enabled
	spec := UnifiAclRuleSpec{
		SiteRef:     CoreRef{Name: siteName},
		Name:        rule.Name,
		Enabled:     &enabled,
		Action:      rule.Action,
		Type:        rule.Type,
		Source:      source,
		Destination: destination,
	}
	if rule.Description != "" {
		description := rule.Description
		spec.Description = &description
	}

	switch rule.Type {
	case aclRuleTypeIPv4:
		if source.Type == unifi.AclEndpointMacAddresses || destination.Type == unifi.AclEndpointMacAddresses {
			return UnifiAclRuleSpec{}, "IPV4 rule carries a MAC-address filter", false
		}
		if rule.NetworkIDFilter != "" {
			return UnifiAclRuleSpec{}, "IPV4 rule carries a networkIdFilter, which is only valid for MAC rules", false
		}
		protocols, reason, ok := aclProtocols(rule.ProtocolFilter)
		if !ok {
			return UnifiAclRuleSpec{}, reason, false
		}
		spec.ProtocolFilter = protocols
	case aclRuleTypeMAC:
		if source.Type != unifi.AclEndpointMacAddresses || destination.Type != unifi.AclEndpointMacAddresses {
			return UnifiAclRuleSpec{}, "MAC rule requires MAC_ADDRESSES source and destination filters", false
		}
		if len(rule.ProtocolFilter) > 0 {
			return UnifiAclRuleSpec{}, "MAC rule carries a protocolFilter, which is only valid for IPV4 rules", false
		}
		ref, reason, ok := aclNetworkRef(rule.NetworkIDFilter, networkNames)
		if !ok {
			return UnifiAclRuleSpec{}, reason, false
		}
		spec.NetworkRef = &ref
	}

	if rule.EnforcingDeviceFilter != nil {
		tags, reason, ok := aclEnforcingDeviceTags(*rule.EnforcingDeviceFilter, deviceTags)
		if !ok {
			return UnifiAclRuleSpec{}, reason, false
		}
		spec.EnforcingDeviceTags = tags
	}
	return spec, "", true
}

// validAclType reports whether ruleType is a representable rule discriminator.
func validAclType(ruleType string) bool {
	return ruleType == aclRuleTypeIPv4 || ruleType == aclRuleTypeMAC
}

// aclEndpoint maps an observed source/destination filter to the emitted discriminated
// union, validating that its required fields are present.
func aclEndpoint(endpoint unifi.AclRuleEndpoint, networkNames map[string]string) (AclRuleEndpoint, string, bool) {
	out := AclRuleEndpoint{Type: endpoint.Type}
	switch endpoint.Type {
	case unifi.AclEndpointIPAddressesOrSubnets:
		if len(endpoint.IPAddressesOrSubnets) == 0 {
			return AclRuleEndpoint{}, "IP_ADDRESSES_OR_SUBNETS filter has no addresses", false
		}
		ports, reason, ok := aclPorts(endpoint.PortFilter)
		if !ok {
			return AclRuleEndpoint{}, reason, false
		}
		out.IPAddressesOrSubnets = &AclIPAddressesEndpoint{Addresses: endpoint.IPAddressesOrSubnets, Ports: ports}
	case unifi.AclEndpointNetworks:
		refs, reason, ok := aclNetworkRefs(endpoint.NetworkIDs, networkNames)
		if !ok {
			return AclRuleEndpoint{}, reason, false
		}
		ports, reason, ok := aclPorts(endpoint.PortFilter)
		if !ok {
			return AclRuleEndpoint{}, reason, false
		}
		out.Networks = &AclNetworksEndpoint{NetworkRefs: refs, Ports: ports}
	case unifi.AclEndpointPorts:
		ports, reason, ok := aclPorts(endpoint.PortFilter)
		if !ok {
			return AclRuleEndpoint{}, reason, false
		}
		if len(ports) == 0 {
			return AclRuleEndpoint{}, "PORTS filter has no ports", false
		}
		out.Ports = &AclPortsEndpoint{Ports: ports}
	case unifi.AclEndpointMacAddresses:
		if len(endpoint.MacAddresses) == 0 {
			return AclRuleEndpoint{}, "MAC_ADDRESSES filter has no addresses", false
		}
		out.MacAddresses = &AclMacAddressesEndpoint{MacAddresses: endpoint.MacAddresses, PrefixLength: endpoint.PrefixLength}
	default:
		return AclRuleEndpoint{}, fmt.Sprintf("unknown endpoint type %q", endpoint.Type), false
	}
	return out, "", true
}

// aclPorts validates and maps an observed port list into the CRD's 1..65535 range.
func aclPorts(in []int32) ([]AclPort, string, bool) {
	if len(in) == 0 {
		return nil, "", true
	}
	out := make([]AclPort, 0, len(in))
	for _, port := range in {
		if port < minPort || port > maxPort {
			return nil, fmt.Sprintf("port %d is outside the CRD range %d..%d", port, minPort, maxPort), false
		}
		out = append(out, AclPort(port))
	}
	return out, "", true
}

// aclNetworkRefs resolves observed network UUIDs to the emitted UnifiNetwork names.
func aclNetworkRefs(ids []string, networkNames map[string]string) ([]CoreRef, string, bool) {
	if len(ids) == 0 {
		return nil, "NETWORKS filter has no networks", false
	}
	refs := make([]CoreRef, 0, len(ids))
	for _, id := range ids {
		name, ok := networkNames[id]
		if !ok {
			return nil, "NETWORKS filter references a network that was not emitted by this snapshot", false
		}
		refs = append(refs, CoreRef{Name: name})
	}
	return refs, "", true
}

// aclNetworkRef resolves a MAC rule's observed network UUID to the emitted UnifiNetwork
// name.
func aclNetworkRef(id string, networkNames map[string]string) (CoreRef, string, bool) {
	if id == "" {
		return CoreRef{}, "MAC rule is missing its networkIdFilter", false
	}
	name, ok := networkNames[id]
	if !ok {
		return CoreRef{}, "MAC rule references a network that was not emitted by this snapshot", false
	}
	return CoreRef{Name: name}, "", true
}

// aclProtocols validates the observed IPV4 protocol filter against the CRD enum.
func aclProtocols(in []string) ([]string, string, bool) {
	if len(in) == 0 {
		return nil, "", true
	}
	for _, protocol := range in {
		if protocol != "TCP" && protocol != "UDP" {
			return nil, fmt.Sprintf("unknown protocol filter %q", protocol), false
		}
	}
	return in, "", true
}

// aclEnforcingDeviceTags resolves an observed enforcing-device filter to name-based
// device-tag selectors, mapping each device UUID to the tag(s) containing it. It returns
// ok=false when the filter is not the DEVICES variant, has no devices, or a device belongs
// to no tag (an unrepresentable scope).
func aclEnforcingDeviceTags(filter unifi.AclRuleDeviceFilter, deviceTags []unifi.DeviceTag) ([]DeviceTagSelector, string, bool) {
	if filter.Type != unifi.AclDeviceFilterDevices {
		return nil, fmt.Sprintf("enforcing-device filter type %q is not representable", filter.Type), false
	}
	if len(filter.DeviceIDs) == 0 {
		return nil, "enforcing-device filter has no devices", false
	}

	names := make([]string, 0, len(filter.DeviceIDs))
	seen := make(map[string]bool, len(filter.DeviceIDs))
	for _, id := range filter.DeviceIDs {
		found := false
		for _, tag := range deviceTags {
			if !containsString(tag.DeviceIDs, id) {
				continue
			}
			found = true
			if !seen[tag.Name] {
				seen[tag.Name] = true
				names = append(names, tag.Name)
			}
		}
		if !found {
			return nil, "enforcing device belongs to no device tag", false
		}
	}

	selectors := make([]DeviceTagSelector, 0, len(names))
	for _, name := range names {
		selectors = append(selectors, DeviceTagSelector{Name: name})
	}
	return selectors, "", true
}

// DnsPolicies projects each upstream DNS policy into a UnifiDnsPolicy. siteName is the
// metadata.name of the target UnifiSite. The upstream record has no name, so the CR's
// metadata.name is derived deterministically from the record's domain and type; colliding
// projected names get a stable numeric suffix and an AnnotationOriginalName annotation
// recording the original domain and type. No upstream UUID appears in spec; the record
// UUID and origin are recorded in status only.
//
// A policy with an unknown type, a payload that does not match its declared type, or a
// variant field outside the CRD bounds is skipped with a warning rather than emitting a
// partial CR (unifi-snapshot spec: unrepresentable objects are skipped with a warning).
func DnsPolicies(siteName string, policies []unifi.DnsPolicy) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(policies))
	var warnings []string
	alloc := newNameAllocator()

	for _, policy := range policies {
		spec, reason, ok := buildDnsPolicySpec(siteName, policy)
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"dns policy %q: %s; skipping", dnsPolicyLogicalName(policy), reason))
			continue
		}

		base := DNSSafeName(dnsPolicyLogicalName(policy))
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: dnsPolicyLogicalName(policy)}
			warnings = append(warnings, fmt.Sprintf(
				"dns policy %q: projected name %q is already in use; emitting as %q",
				dnsPolicyLogicalName(policy), base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiDnsPolicy,
			Metadata:   metadata,
			Spec:       spec,
			Status:     DnsPolicyStatus{PolicyID: policy.ID, Origin: policy.Metadata.Origin},
		})
	}
	return resources, warnings
}

// dnsPolicyLogicalName is the human-readable identity of an upstream DNS record, used as
// the basis for its projected metadata.name (the upstream record has no name).
func dnsPolicyLogicalName(policy unifi.DnsPolicy) string {
	if policy.Domain == "" {
		return policy.Type
	}
	return policy.Domain + " " + policy.Type
}

// buildDnsPolicySpec maps one upstream policy into a spec valid against the CRD. It
// returns ok=false (with a reason and no resource) when the type is unknown, the payload
// carries fields of another variant, a required field is missing, or a bounded field is
// out of range, so the CLI never emits an invalid CR.
func buildDnsPolicySpec(siteName string, policy unifi.DnsPolicy) (UnifiDnsPolicySpec, string, bool) {
	if policy.Domain == "" {
		return UnifiDnsPolicySpec{}, "record has no domain", false
	}
	if _, ok := dnsVariantAllowed[policy.Type]; !ok {
		return UnifiDnsPolicySpec{}, fmt.Sprintf("unknown type %q", policy.Type), false
	}
	if field, foreign := dnsForeignField(policy); foreign {
		return UnifiDnsPolicySpec{}, fmt.Sprintf(
			"payload carries %q which does not match type %q", field, policy.Type), false
	}

	enabled := policy.Enabled
	spec := UnifiDnsPolicySpec{
		SiteRef: CoreRef{Name: siteName},
		Type:    policy.Type,
		Domain:  policy.Domain,
		Enabled: &enabled,
	}

	switch policy.Type {
	case dnsTypeARecord:
		if policy.IPv4Address == "" {
			return UnifiDnsPolicySpec{}, "A_RECORD is missing ipv4Address", false
		}
		ttl, reason, ok := dnsBound(policy.TTLSeconds, "ttlSeconds", dnsMinTTL, dnsMaxAddressTTL)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		spec.ARecord = &DnsARecord{IPv4Address: policy.IPv4Address, TTLSeconds: ttl}
	case dnsTypeAaaaRecord:
		if policy.IPv6Address == "" {
			return UnifiDnsPolicySpec{}, "AAAA_RECORD is missing ipv6Address", false
		}
		ttl, reason, ok := dnsBound(policy.TTLSeconds, "ttlSeconds", dnsMinTTL, dnsMaxAddressTTL)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		spec.AaaaRecord = &DnsAaaaRecord{IPv6Address: policy.IPv6Address, TTLSeconds: ttl}
	case dnsTypeCnameRecord:
		if policy.TargetDomain == "" {
			return UnifiDnsPolicySpec{}, "CNAME_RECORD is missing targetDomain", false
		}
		ttl, reason, ok := dnsBound(policy.TTLSeconds, "ttlSeconds", dnsMinTTL, dnsMaxCnameTTL)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		spec.CnameRecord = &DnsCnameRecord{TargetDomain: policy.TargetDomain, TTLSeconds: ttl}
	case dnsTypeMxRecord:
		if policy.MailServerDomain == "" {
			return UnifiDnsPolicySpec{}, "MX_RECORD is missing mailServerDomain", false
		}
		priority, reason, ok := dnsBound(policy.Priority, "priority", dnsMinTTL, dnsMaxPriority)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		spec.MxRecord = &DnsMxRecord{MailServerDomain: policy.MailServerDomain, Priority: priority}
	case dnsTypeSrvRecord:
		if policy.Protocol == "" || policy.ServerDomain == "" || policy.Service == "" {
			return UnifiDnsPolicySpec{}, "SRV_RECORD is missing protocol, serverDomain, or service", false
		}
		port, reason, ok := dnsBound(policy.Port, "port", dnsMinTTL, dnsMaxPriority)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		priority, reason, ok := dnsBound(policy.Priority, "priority", dnsMinTTL, dnsMaxPriority)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		weight, reason, ok := dnsBound(policy.Weight, "weight", dnsMinTTL, dnsMaxPriority)
		if !ok {
			return UnifiDnsPolicySpec{}, reason, false
		}
		spec.SrvRecord = &DnsSrvRecord{
			Port:         port,
			Priority:     priority,
			Protocol:     policy.Protocol,
			ServerDomain: policy.ServerDomain,
			Service:      policy.Service,
			Weight:       weight,
		}
	case dnsTypeTxtRecord:
		if policy.Text == "" {
			return UnifiDnsPolicySpec{}, "TXT_RECORD is missing text", false
		}
		spec.TxtRecord = &DnsTxtRecord{Text: policy.Text}
	case dnsTypeForwardDomain:
		if policy.IPAddress == "" {
			return UnifiDnsPolicySpec{}, "FORWARD_DOMAIN is missing ipAddress", false
		}
		spec.ForwardDomain = &DnsForwardDomain{IPAddress: policy.IPAddress}
	default:
		return UnifiDnsPolicySpec{}, fmt.Sprintf("unknown type %q", policy.Type), false
	}
	return spec, "", true
}

// dnsVariantFields lists every flattened variant field in a fixed order so the foreign-
// field reason is deterministic.
var dnsVariantFields = []string{
	"ipv4Address", "ipv6Address", "ttlSeconds", "targetDomain", "mailServerDomain",
	"priority", "port", "protocol", "serverDomain", "service", "weight", "text", "ipAddress",
}

// dnsVariantAllowed maps each DNS-policy type to the set of variant fields it may carry.
var dnsVariantAllowed = map[string]map[string]bool{
	dnsTypeARecord:       set("ipv4Address", "ttlSeconds"),
	dnsTypeAaaaRecord:    set("ipv6Address", "ttlSeconds"),
	dnsTypeCnameRecord:   set("targetDomain", "ttlSeconds"),
	dnsTypeMxRecord:      set("mailServerDomain", "priority"),
	dnsTypeSrvRecord:     set("port", "priority", "protocol", "serverDomain", "service", "weight"),
	dnsTypeTxtRecord:     set("text"),
	dnsTypeForwardDomain: set("ipAddress"),
}

// set builds a string set from the given values.
func set(values ...string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

// dnsForeignField reports the first variant field a payload carries that is not valid for
// its declared type. It assumes the type is known (the caller checks that first).
func dnsForeignField(policy unifi.DnsPolicy) (string, bool) {
	allowed := dnsVariantAllowed[policy.Type]
	for _, field := range dnsVariantFields {
		if dnsFieldSet(policy, field) && !allowed[field] {
			return field, true
		}
	}
	return "", false
}

// dnsFieldSet reports whether the named flat variant field is set on the payload.
func dnsFieldSet(policy unifi.DnsPolicy, field string) bool {
	switch field {
	case "ipv4Address":
		return policy.IPv4Address != ""
	case "ipv6Address":
		return policy.IPv6Address != ""
	case "ttlSeconds":
		return policy.TTLSeconds != nil
	case "targetDomain":
		return policy.TargetDomain != ""
	case "mailServerDomain":
		return policy.MailServerDomain != ""
	case "priority":
		return policy.Priority != nil
	case "port":
		return policy.Port != nil
	case "protocol":
		return policy.Protocol != ""
	case "serverDomain":
		return policy.ServerDomain != ""
	case "service":
		return policy.Service != ""
	case "weight":
		return policy.Weight != nil
	case "text":
		return policy.Text != ""
	case "ipAddress":
		return policy.IPAddress != ""
	default:
		return false
	}
}

// dnsBound validates a required bounded integer variant field. A nil pointer means the
// field was absent from the response.
func dnsBound(value *int32, field string, min, max int32) (int32, string, bool) {
	if value == nil {
		return 0, field + " is missing", false
	}
	if *value < min || *value > max {
		return 0, fmt.Sprintf("%s %d is outside the CRD range %d..%d", field, *value, min, max), false
	}
	return *value, "", true
}

// SwitchingLags projects each upstream LAG into an adopted UnifiSwitchingLag. siteName is
// the metadata.name of the target UnifiSite. deviceTags is the site's read-only device-tag
// list, used to express each member's device as a name-based device-tag selector so no
// opaque device UUID reaches spec. Because the upstream LAG has no name, the CR's
// metadata.name is derived deterministically from the LAG's type and members; colliding
// projected names get a stable numeric suffix and an AnnotationOriginalName annotation. The
// upstream LAG UUID and its observed members are recorded in status only.
//
// A LAG with an unknown type, no members, a member with no port indexes, or a member
// device that belongs to no device tag or more than one (an ambiguous selector) is skipped
// with a warning rather than emitting a partial CR.
func SwitchingLags(siteName string, deviceTags []unifi.DeviceTag, lags []unifi.Lag) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(lags))
	var warnings []string
	alloc := newNameAllocator()

	for _, lag := range lags {
		spec, reason, ok := buildSwitchingLagSpec(siteName, deviceTags, lag)
		if !ok {
			warnings = append(warnings, fmt.Sprintf(
				"switching lag (%s): %s; skipping", switchingLagDescriptor(lag), reason))
			continue
		}

		logical := switchingLagLogicalName(spec)
		base := DNSSafeName(logical)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: logical}
			warnings = append(warnings, fmt.Sprintf(
				"switching lag %s: projected name %q is already in use; emitting as %q",
				logical, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiSwitchingLag,
			Metadata:   metadata,
			Spec:       spec,
			Status: UnifiSwitchingLagStatus{
				LagID:   lag.ID,
				Type:    lag.Type,
				Members: switchingLagMembers(lag.Members),
			},
		})
	}
	return resources, warnings
}

// buildSwitchingLagSpec maps one upstream LAG into a spec valid against the CRD, resolving
// each member device UUID to a device-tag selector.
func buildSwitchingLagSpec(siteName string, deviceTags []unifi.DeviceTag, lag unifi.Lag) (UnifiSwitchingLagSpec, string, bool) {
	if !validSwitchingLagType(lag.Type) {
		return UnifiSwitchingLagSpec{}, fmt.Sprintf("unknown type %q", lag.Type), false
	}
	if len(lag.Members) == 0 {
		return UnifiSwitchingLagSpec{}, "LAG has no members", false
	}

	members := make([]SwitchingLagMemberSelector, 0, len(lag.Members))
	for _, member := range lag.Members {
		tagName, ok := lagMemberDeviceTagName(deviceTags, member.DeviceID)
		if !ok {
			return UnifiSwitchingLagSpec{}, "a member device does not belong to exactly one device tag", false
		}
		if len(member.PortIdxs) == 0 {
			return UnifiSwitchingLagSpec{}, "a member has no port indexes", false
		}
		members = append(members, SwitchingLagMemberSelector{
			DeviceTag: DeviceTagSelector{Name: tagName},
			PortIdxs:  member.PortIdxs,
		})
	}

	return UnifiSwitchingLagSpec{
		SiteRef: CoreRef{Name: siteName},
		Type:    lag.Type,
		Members: members,
	}, "", true
}

// switchingLagMembers maps observed LAG members for the status subresource. The device IDs
// are upstream identifiers and are recorded in status only.
func switchingLagMembers(members []unifi.LagMember) []SwitchingLagMember {
	out := make([]SwitchingLagMember, 0, len(members))
	for _, member := range members {
		out = append(out, SwitchingLagMember{DeviceID: member.DeviceID, PortIdxs: member.PortIdxs})
	}
	return out
}

// switchingLagLogicalName is the human-readable identity of an upstream LAG (which has no
// name), used as the basis for its projected metadata.name. It is derived from the resolved
// device-tag selectors and port indexes so no opaque device UUID appears in the name.
func switchingLagLogicalName(spec UnifiSwitchingLagSpec) string {
	parts := make([]string, 0, len(spec.Members)+1)
	parts = append(parts, strings.ToLower(spec.Type))
	for _, member := range spec.Members {
		parts = append(parts, fmt.Sprintf("%s:%s", member.DeviceTag.Name, int32List(member.PortIdxs)))
	}
	return strings.Join(parts, " ")
}

// switchingLagDescriptor renders a skipped LAG for a warning without leaking an opaque
// device UUID.
func switchingLagDescriptor(lag unifi.Lag) string {
	return fmt.Sprintf("%s LAG with %d member(s)", lag.Type, len(lag.Members))
}

// int32List renders port indexes as a comma-separated list for a deterministic logical
// name.
func int32List(values []int32) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatInt(int64(value), 10))
	}
	return strings.Join(parts, ",")
}

// validSwitchingLagType reports whether lagType is a representable LAG discriminator.
func validSwitchingLagType(lagType string) bool {
	switch lagType {
	case switchingLagTypeLocal, switchingLagTypeMultiChassis, switchingLagTypeSwitchStack:
		return true
	default:
		return false
	}
}

// lagMemberDeviceTagName returns the name of the single device tag that contains deviceID.
// It returns ok=false when deviceID is empty, belongs to no tag, or belongs to more than
// one tag (an ambiguous selector the operator could not resolve).
func lagMemberDeviceTagName(deviceTags []unifi.DeviceTag, deviceID string) (string, bool) {
	if deviceID == "" {
		return "", false
	}
	name := ""
	matches := 0
	for _, tag := range deviceTags {
		if !containsString(tag.DeviceIDs, deviceID) {
			continue
		}
		name = tag.Name
		matches++
	}
	if matches != 1 || name == "" {
		return "", false
	}
	return name, true
}

// SwitchingMcLagDomains projects each upstream MC-LAG domain into an adopted
// UnifiSwitchingMcLagDomain: spec.siteRef names the emitted UnifiSite and spec.name names
// the existing upstream domain to adopt. The upstream UUID and observed members and peers
// are recorded in status only. Colliding projected names get a stable numeric suffix and an
// AnnotationOriginalName annotation. A domain without a name cannot be adopted by name and
// is skipped with a warning.
func SwitchingMcLagDomains(siteName string, domains []unifi.McLagDomain) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(domains))
	var warnings []string
	alloc := newNameAllocator()

	for _, domain := range domains {
		if domain.Name == "" {
			warnings = append(warnings, "switching MC-LAG domain: upstream domain has no name; skipping")
			continue
		}

		base := DNSSafeName(domain.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: domain.Name}
			warnings = append(warnings, fmt.Sprintf(
				"switching MC-LAG domain %q: projected name %q is already in use; emitting as %q",
				domain.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiSwitchingMcLagDomain,
			Metadata:   metadata,
			Spec: UnifiSwitchingMcLagDomainSpec{
				SiteRef: CoreRef{Name: siteName},
				Name:    domain.Name,
			},
			Status: UnifiSwitchingMcLagDomainStatus{
				DomainID: domain.ID,
				Members:  mcLagDomainMembers(domain.Lags),
				Peers:    mcLagDomainPeers(domain.Peers),
			},
		})
	}
	return resources, warnings
}

// mcLagDomainMembers flattens the members of an MC-LAG domain's local LAGs for the status
// subresource. The device IDs are upstream identifiers recorded in status only.
func mcLagDomainMembers(lags []unifi.McLagLocal) []SwitchingLagMember {
	out := make([]SwitchingLagMember, 0)
	for _, lag := range lags {
		out = append(out, switchingLagMembers(lag.Members)...)
	}
	return out
}

// mcLagDomainPeers maps observed MC-LAG domain peers for the status subresource.
func mcLagDomainPeers(peers []unifi.McLagPeer) []SwitchingMcLagPeer {
	out := make([]SwitchingMcLagPeer, 0, len(peers))
	for _, peer := range peers {
		out = append(out, SwitchingMcLagPeer{
			DeviceID:     peer.DeviceID,
			LinkPortIdxs: peer.LinkPortIdxs,
			Role:         peer.Role,
		})
	}
	return out
}

// SwitchingSwitchStacks projects each upstream switch stack into an adopted
// UnifiSwitchingSwitchStack: spec.siteRef names the emitted UnifiSite and spec.name names
// the existing upstream stack to adopt. The upstream UUID and observed members are recorded
// in status only. Colliding projected names get a stable numeric suffix and an
// AnnotationOriginalName annotation. A stack without a name cannot be adopted by name and is
// skipped with a warning.
func SwitchingSwitchStacks(siteName string, stacks []unifi.SwitchStack) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(stacks))
	var warnings []string
	alloc := newNameAllocator()

	for _, stack := range stacks {
		if stack.Name == "" {
			warnings = append(warnings, "switching switch stack: upstream stack has no name; skipping")
			continue
		}

		base := DNSSafeName(stack.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: stack.Name}
			warnings = append(warnings, fmt.Sprintf(
				"switching switch stack %q: projected name %q is already in use; emitting as %q",
				stack.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiSwitchingSwitchStack,
			Metadata:   metadata,
			Spec: UnifiSwitchingSwitchStackSpec{
				SiteRef: CoreRef{Name: siteName},
				Name:    stack.Name,
			},
			Status: UnifiSwitchingSwitchStackStatus{
				StackID: stack.ID,
				Members: switchingStackMembers(stack.Members),
			},
		})
	}
	return resources, warnings
}

// switchingStackMembers maps observed switch-stack members for the status subresource.
func switchingStackMembers(members []unifi.SwitchStackMember) []SwitchingStackMember {
	out := make([]SwitchingStackMember, 0, len(members))
	for _, member := range members {
		out = append(out, SwitchingStackMember{DeviceID: member.DeviceID})
	}
	return out
}

// VpnServers projects each upstream VPN server into an adopted UnifiVpnServer: spec.siteRef
// names the emitted UnifiSite and spec.name names the existing upstream server to adopt.
// The upstream UUID and its observed type and enabled state are recorded in status only,
// never in spec; no credential is authored. Colliding projected names get a stable numeric
// suffix and an AnnotationOriginalName annotation. A server without a name cannot be
// adopted by name and is skipped with a warning.
func VpnServers(siteName string, servers []unifi.VpnServer) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(servers))
	var warnings []string
	alloc := newNameAllocator()

	for _, server := range servers {
		if server.Name == "" {
			warnings = append(warnings, fmt.Sprintf(
				"vpn server (type %q): upstream server has no name; skipping", server.Type))
			continue
		}

		base := DNSSafeName(server.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: server.Name}
			warnings = append(warnings, fmt.Sprintf(
				"vpn server %q: projected name %q is already in use; emitting as %q",
				server.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		enabled := server.Enabled
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiVpnServer,
			Metadata:   metadata,
			Spec: UnifiVpnServerSpec{
				SiteRef: CoreRef{Name: siteName},
				Name:    server.Name,
			},
			Status: VpnServerStatus{ServerID: server.ID, Type: server.Type, Enabled: &enabled},
		})
	}
	return resources, warnings
}

// SiteToSiteTunnels projects each upstream site-to-site VPN tunnel into an adopted
// UnifiSiteToSiteTunnel: spec.siteRef names the emitted UnifiSite and spec.name names the
// existing upstream tunnel to adopt. The upstream UUID and its observed type are recorded in
// status only, never in spec; no credential is authored. Colliding projected names get a
// stable numeric suffix and an AnnotationOriginalName annotation. A tunnel without a name
// cannot be adopted by name and is skipped with a warning.
func SiteToSiteTunnels(siteName string, tunnels []unifi.SiteToSiteTunnel) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(tunnels))
	var warnings []string
	alloc := newNameAllocator()

	for _, tunnel := range tunnels {
		if tunnel.Name == "" {
			warnings = append(warnings, fmt.Sprintf(
				"site-to-site tunnel (type %q): upstream tunnel has no name; skipping", tunnel.Type))
			continue
		}

		base := DNSSafeName(tunnel.Name)
		name, collided := alloc.allocate(base)

		var annotations map[string]string
		if collided {
			annotations = map[string]string{AnnotationOriginalName: tunnel.Name}
			warnings = append(warnings, fmt.Sprintf(
				"site-to-site tunnel %q: projected name %q is already in use; emitting as %q",
				tunnel.Name, base, name))
		}

		metadata := emit.Metadata{Name: name}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiSiteToSiteTunnel,
			Metadata:   metadata,
			Spec: UnifiSiteToSiteTunnelSpec{
				SiteRef: CoreRef{Name: siteName},
				Name:    tunnel.Name,
			},
			Status: SiteToSiteTunnelStatus{TunnelID: tunnel.ID, Type: tunnel.Type},
		})
	}
	return resources, warnings
}

// WifiBroadcasts projects each upstream WiFi broadcast into a UnifiWifiBroadcast.
// networkNames is the upstream-network-id -> emitted-name map returned by Networks; a
// SPECIFIC broadcast's spec.networkRef is resolved through it so the reference names a
// UnifiNetwork resource this snapshot emitted. siteName is the emitted UnifiSite name for
// the snapshot's site, used as spec.siteRef for a NATIVE-bound broadcast (which carries no
// network and resolves its site directly). deviceTags is the site's read-only device-tag
// list, used to reverse-map a DEVICE_TAGS scope to tag names. A broadcast whose SPECIFIC
// network cannot be resolved, whose type/variant is unrepresentable, or whose personal
// passphrase is not recoverable is emitted with a placeholder (passphrase) or skipped with
// a warning rather than silently dropped. The upstream broadcast id is recorded in status
// only.
func WifiBroadcasts(networkNames map[string]string, siteName string, deviceTags []unifi.DeviceTag, broadcasts []unifi.WifiBroadcast) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(broadcasts))
	var warnings []string
	alloc := newNameAllocator()

	for _, broadcast := range broadcasts {
		spec, annotations, broadcastWarnings, ok := buildWifiBroadcastSpec(networkNames, siteName, deviceTags, broadcast)
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
func buildWifiBroadcastSpec(networkNames map[string]string, siteName string, deviceTags []unifi.DeviceTag, broadcast unifi.WifiBroadcast) (UnifiWifiBroadcastSpec, map[string]string, []string, bool) {
	var networkRef *CoreRef
	var nativeNetwork *bool
	var siteRef *CoreRef
	switch kind, networkName := broadcastNetworkName(networkNames, broadcast.Network); kind {
	case wifiBindingSpecific:
		networkRef = &CoreRef{Name: networkName}
	case wifiBindingNative:
		if siteName == "" {
			return UnifiWifiBroadcastSpec{}, nil, []string{fmt.Sprintf(
				"wifi broadcast %q: upstream site could not be resolved to an emitted UnifiSite; skipping", broadcast.Name)}, false
		}
		native := true
		nativeNetwork = &native
		siteRef = &CoreRef{Name: siteName}
	default:
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
		NetworkRef:                          networkRef,
		NativeNetwork:                       nativeNetwork,
		SiteRef:                             siteRef,
		Type:                                broadcast.Type,
		Name:                                broadcast.Name,
		Enabled:                             &enabled,
		HideName:                            broadcast.HideName,
		ClientIsolationEnabled:              broadcast.ClientIsolationEnabled,
		MulticastToUnicastConversionEnabled: broadcast.MulticastToUnicastConversionEnabled,
		UapsdEnabled:                        broadcast.UapsdEnabled,
		BandSteeringEnabled:                 broadcast.BandSteeringEnabled,
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

// wifiNetworkBindingKind discriminates a broadcast's resolved upstream network
// reference. The SPECIFIC variant maps to an emitted UnifiNetwork name; the NATIVE
// variant carries no network identifier and is resolved to a site instead.
type wifiNetworkBindingKind int

const (
	// wifiBindingNone is an absent or unrepresentable reference that maps to no
	// emitted resource.
	wifiBindingNone wifiNetworkBindingKind = iota
	// wifiBindingSpecific is a reference to a UnifiNetwork by upstream UUID.
	wifiBindingSpecific
	// wifiBindingNative is the upstream NATIVE variant, which carries no network
	// identifier.
	wifiBindingNative
)

// broadcastNetworkName resolves the upstream network reference to an emitted UnifiNetwork
// name. It returns the binding kind so the caller can distinguish a SPECIFIC reference
// (mapped to a name), a NATIVE reference (no network; resolved to a site), and an
// unresolvable reference. Only a SPECIFIC binding yields a name.
func broadcastNetworkName(networkNames map[string]string, ref *unifi.WifiNetworkReference) (wifiNetworkBindingKind, string) {
	if ref == nil {
		return wifiBindingNone, ""
	}
	switch ref.Type {
	case "NATIVE":
		return wifiBindingNative, ""
	case "SPECIFIC":
		if ref.NetworkID == "" {
			return wifiBindingNone, ""
		}
		name, ok := networkNames[ref.NetworkID]
		if !ok {
			return wifiBindingNone, ""
		}
		return wifiBindingSpecific, name
	default:
		return wifiBindingNone, ""
	}
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
		kind, name := broadcastNetworkName(networkNames, &ref)
		if kind != wifiBindingSpecific {
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

// mapIPv4 validates and maps the observed IPv4 configuration, including the nested DHCP
// configuration when the console provides one. An unrepresentable DHCP block (empty mode)
// returns an error so the whole network skips by design; see mapDHCPConfiguration.
func mapIPv4(in *unifi.IPv4Configuration) (*IPv4Configuration, error) {
	if in.HostIPAddress == "" || in.PrefixLength < minIPv4Prefix || in.PrefixLength > maxIPv4Prefix {
		return nil, fmt.Errorf(
			"ipv4Configuration is incomplete or out of range (hostIpAddress=%q prefixLength=%d)",
			in.HostIPAddress, in.PrefixLength)
	}
	out := &IPv4Configuration{
		AutoScaleEnabled:        in.AutoScaleEnabled,
		HostIPAddress:           in.HostIPAddress,
		PrefixLength:            in.PrefixLength,
		AdditionalHostIPSubnets: in.AdditionalHostIPSubnets,
	}
	if in.DHCPConfiguration != nil {
		dhcp, err := mapDHCPConfiguration(in.DHCPConfiguration)
		if err != nil {
			return nil, err
		}
		out.DHCPConfiguration = dhcp
	}
	return out, nil
}

// mapDHCPConfiguration maps the observed DHCP configuration. The CRD requires a non-empty
// mode whenever the block is present, so a console value without one cannot be represented
// and the caller skips the network rather than emit an invalid CR. That skip is by design:
// omitting the block would emit a partially projected resource, which the unifi-snapshot
// spec forbids, so the network is skipped with a warning rather than annotated.
func mapDHCPConfiguration(in *unifi.DHCPConfiguration) (*DHCPConfiguration, error) {
	if in.Mode == "" {
		return nil, errors.New("ipv4Configuration.dhcpConfiguration.mode is empty and cannot be represented")
	}
	out := &DHCPConfiguration{
		Mode:                         in.Mode,
		DNSServerIPAddressesOverride: in.DNSServerIPAddressesOverride,
		LeaseTimeSeconds:             in.LeaseTimeSeconds,
		DomainName:                   in.DomainName,
		PingConflictDetectionEnabled: in.PingConflictDetectionEnabled,
	}
	if in.IPAddressRange != nil {
		out.IPAddressRange = &DHCPIPAddressRange{Start: in.IPAddressRange.Start, Stop: in.IPAddressRange.Stop}
	}
	return out, nil
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
