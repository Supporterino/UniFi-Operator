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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnifiNetworkFinalizer blocks removal of a UnifiNetwork until its upstream
// network has been deleted. The network reconciler adds it before creating the
// upstream network and removes it after the upstream network is gone.
const UnifiNetworkFinalizer = "unifi.supporterino.de/network-finalizer"

// +kubebuilder:validation:XValidation:rule="self.management == 'GATEWAY' ? has(self.gateway) : !has(self.gateway)",message="gateway fields require management GATEWAY"
// +kubebuilder:validation:XValidation:rule="self.management == 'SWITCH' ? has(self.switch) : !has(self.switch)",message="switch fields require management SWITCH"

// UnifiNetworkSpec defines the desired state of UnifiNetwork. It mirrors the
// discriminated `Create or update Network` union of the UniFi Integration v1
// API (spec v10.4.57) and never contains a UniFi internal identifier: the
// controller resolves the upstream network through status only.
type UnifiNetworkSpec struct {
	// SiteRef references the UnifiSite that owns this network. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Management is the union discriminator (GATEWAY, SWITCH, or UNMANAGED).
	// +kubebuilder:validation:Enum=GATEWAY;SWITCH;UNMANAGED
	Management string `json:"management"`

	// Name is the human-readable network name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Enabled controls whether the network is enabled.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// VLANID is the 802.1Q VLAN tag (1..4009; 1 is the default network).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4009
	VLANID int32 `json:"vlanId"`

	// DHCPGuarding configures trusted DHCP server addresses for this network.
	// Omit to disable the feature.
	// +optional
	DHCPGuarding *DHCPGuarding `json:"dhcpGuarding,omitempty"`

	// Gateway configures a network with management GATEWAY. It is required for
	// management GATEWAY and forbidden otherwise (CEL).
	// +optional
	Gateway *GatewayNetworkOptions `json:"gateway,omitempty"`

	// Switch configures a network with management SWITCH. It is required for
	// management SWITCH and forbidden otherwise (CEL).
	// +optional
	Switch *SwitchNetworkOptions `json:"switch,omitempty"`
}

// DHCPGuarding holds the trusted DHCP server addresses for a network.
type DHCPGuarding struct {
	// TrustedDHCPServerIPAddresses is the list of trusted DHCP server IP
	// addresses. At least one entry is required when DHCP guarding is enabled.
	// Each entry must be a non-empty, well-formed IPv4 address: a console can
	// return junk entries such as ["192.168.2.1","",""], which the API server
	// now rejects so they are never round-tripped onto a Custom Resource.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:MinLength=1
	// +kubebuilder:validation:items:MaxLength=15
	// +kubebuilder:validation:items:XValidation:rule="ip(self).family() == 4",message="must be a valid IPv4 address"
	TrustedDHCPServerIPAddresses []string `json:"trustedDhcpServerIpAddresses"`
}

// GatewayNetworkOptions holds the gateway-managed variant fields. All required
// fields are required upstream for management GATEWAY.
type GatewayNetworkOptions struct {
	// CellularBackupEnabled allows this network to use cellular data when WAN
	// connections are down.
	CellularBackupEnabled bool `json:"cellularBackupEnabled"`

	// InternetAccessEnabled allows internet access for devices on this network.
	InternetAccessEnabled bool `json:"internetAccessEnabled"`

	// IsolationEnabled isolates this network from all other networks.
	IsolationEnabled bool `json:"isolationEnabled"`

	// IPv4Configuration is the IPv4 configuration for this network.
	IPv4Configuration GatewayManagedIPv4Configuration `json:"ipv4Configuration"`

	// IPv6Configuration is the IPv6 configuration for this network. Omit to
	// leave IPv6 unconfigured.
	// +optional
	IPv6Configuration *IPv6Configuration `json:"ipv6Configuration,omitempty"`

	// MDNSForwardingEnabled controls whether this network participates in mDNS
	// traffic forwarding. Omit to use the site mDNS default.
	// +optional
	MDNSForwardingEnabled *bool `json:"mdnsForwardingEnabled,omitempty"`
}

// SwitchNetworkOptions holds the switch-managed variant fields. All required
// fields are required upstream for management SWITCH.
type SwitchNetworkOptions struct {
	// CellularBackupEnabled allows this network to use cellular data when WAN
	// connections are down.
	CellularBackupEnabled bool `json:"cellularBackupEnabled"`

	// IsolationEnabled isolates this network from all other networks.
	IsolationEnabled bool `json:"isolationEnabled"`

	// IPv4Configuration is the IPv4 configuration for this network.
	IPv4Configuration SwitchManagedIPv4Configuration `json:"ipv4Configuration"`

	// DeviceTag selects the existing device tag whose member device manages
	// this network. Tags are read-only upstream (there is no device-tag Custom
	// Resource), so the selector names the tag and keeps the opaque upstream
	// device UUID out of spec. The reconciler resolves the tag through the
	// read-only device-tags list and requires exactly one member device.
	DeviceTag DeviceTagSelector `json:"deviceTag"`
}

// GatewayManagedIPv4Configuration is the gateway IPv4 configuration. Only the
// fields the operator reconciles are modeled; the upstream
// `natOutboundIpAddressConfiguration` (WAN specific) is deliberately unmodeled
// and is left to a later change. The upstream `dhcpConfiguration` object is
// modeled as DHCPConfiguration.
type GatewayManagedIPv4Configuration struct {
	// AutoScaleEnabled allows the subnet to scale based on active DHCP leases.
	AutoScaleEnabled bool `json:"autoScaleEnabled"`

	// HostIPAddress is the IPv4 address assigned to the network.
	HostIPAddress string `json:"hostIpAddress"`

	// PrefixLength is the IPv4 subnet prefix length (8..30).
	// +kubebuilder:validation:Minimum=8
	// +kubebuilder:validation:Maximum=30
	PrefixLength int32 `json:"prefixLength"`

	// AdditionalHostIPSubnets are additional host IP subnets for this VLAN.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`

	// DHCPConfiguration is the DHCP configuration for this network. Omit to
	// leave DHCP unconfigured; the controller then changes no DHCP settings
	// upstream.
	// +optional
	DHCPConfiguration *DHCPConfiguration `json:"dhcpConfiguration,omitempty"`
}

// SwitchManagedIPv4Configuration is the switch IPv4 configuration. As with the
// gateway variant, only the fields the operator reconciles are modeled; the
// upstream `dhcpConfiguration` object is modeled as DHCPConfiguration.
type SwitchManagedIPv4Configuration struct {
	// AutoScaleEnabled allows the subnet to scale based on active DHCP leases.
	AutoScaleEnabled bool `json:"autoScaleEnabled"`

	// HostIPAddress is the IPv4 address assigned to the network.
	HostIPAddress string `json:"hostIpAddress"`

	// PrefixLength is the IPv4 subnet prefix length (8..30).
	// +kubebuilder:validation:Minimum=8
	// +kubebuilder:validation:Maximum=30
	PrefixLength int32 `json:"prefixLength"`

	// AdditionalHostIPSubnets are additional host IP subnets for this VLAN.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=64
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`

	// DHCPConfiguration is the DHCP configuration for this network. Omit to
	// leave DHCP unconfigured; the controller then changes no DHCP settings
	// upstream.
	// +optional
	DHCPConfiguration *DHCPConfiguration `json:"dhcpConfiguration,omitempty"`
}

// DHCPConfiguration is the DHCP configuration of a network's IPv4
// configuration. It mirrors the upstream `dhcpConfiguration` object, modeled
// flat so a snapshot can round-trip the observed DHCP settings; a
// mode-discriminated union can replace it when a non-`SERVER` mode appears. It
// is pure desired state and never carries an upstream identifier.
//
// Mode is required. Every other field follows "omit to leave unchanged": an
// unset optional field is not treated as desired state, so a CR that authors
// only part of the DHCP block leaves the console's other DHCP settings
// untouched.
type DHCPConfiguration struct {
	// Mode is the DHCP addressing mode (for example SERVER).
	// +kubebuilder:validation:MinLength=1
	Mode string `json:"mode"`

	// IPAddressRange is the address range handed to DHCP clients. Omit to
	// leave the console's range unchanged.
	// +optional
	IPAddressRange *DHCPIPAddressRange `json:"ipAddressRange,omitempty"`

	// DNSServerIPAddressesOverride are the DNS servers handed to DHCP clients.
	// Omit to leave the console's override list unchanged.
	// +optional
	DNSServerIPAddressesOverride []string `json:"dnsServerIpAddressesOverride,omitempty"`

	// LeaseTimeSeconds is the DHCP lease time in seconds. Omit (or 0) to leave
	// the console's lease time unchanged.
	// +optional
	LeaseTimeSeconds int32 `json:"leaseTimeSeconds,omitempty"`

	// DomainName is the DNS domain handed to DHCP clients. Omit to leave the
	// console's domain unchanged.
	// +optional
	DomainName string `json:"domainName,omitempty"`

	// PingConflictDetectionEnabled enables ICMP conflict detection before a
	// lease is granted. Omit (or false) to leave the console's setting
	// unchanged.
	// +optional
	PingConflictDetectionEnabled bool `json:"pingConflictDetectionEnabled,omitempty"`
}

// DHCPIPAddressRange is the lease address range of a DHCP configuration.
type DHCPIPAddressRange struct {
	// Start is the first address of the lease range.
	Start string `json:"start"`

	// Stop is the last address of the lease range.
	Stop string `json:"stop"`
}

// +kubebuilder:validation:XValidation:rule="self.interfaceType == 'STATIC' ? (has(self.hostIpAddress) && has(self.prefixLength)) : (!has(self.hostIpAddress) && !has(self.prefixLength))",message="static fields require interfaceType STATIC"
// +kubebuilder:validation:XValidation:rule="self.interfaceType == 'PREFIX_DELEGATION' ? has(self.prefixDelegationWan) : !has(self.prefixDelegationWan)",message="prefixDelegationWan requires interfaceType PREFIX_DELEGATION"

// IPv6Configuration is the interface-type-discriminated IPv6 configuration.
type IPv6Configuration struct {
	// InterfaceType selects the IPv6 addressing variant.
	// +kubebuilder:validation:Enum=PREFIX_DELEGATION;STATIC
	InterfaceType string `json:"interfaceType"`

	// ClientAddressAssignment configures how client addresses are assigned.
	ClientAddressAssignment IPv6ClientAddressAssignment `json:"clientAddressAssignment"`

	// AdditionalHostIPSubnets are additional host IP subnets for this VLAN.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`

	// DNSServerIPAddressesOverride are the IPv6 DNS servers assigned to this
	// network. Omit to select them automatically.
	// +optional
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=4
	DNSServerIPAddressesOverride []string `json:"dnsServerIpAddressesOverride,omitempty"`

	// RouterAdvertisement configures IPv6 router advertisement.
	// +optional
	RouterAdvertisement *RouterAdvertisement `json:"routerAdvertisement,omitempty"`

	// PrefixDelegationWAN selects the upstream WAN interface the prefix is
	// delegated from by name. Required for interfaceType PREFIX_DELEGATION.
	// +optional
	PrefixDelegationWAN *WANSelector `json:"prefixDelegationWan,omitempty"`

	// HostIPAddress is the static IPv6 address assigned to this network.
	// Required for interfaceType STATIC.
	// +optional
	HostIPAddress string `json:"hostIpAddress,omitempty"`

	// PrefixLength is the static IPv6 subnet prefix length (64..127). Required
	// for interfaceType STATIC.
	// +optional
	// +kubebuilder:validation:Minimum=64
	// +kubebuilder:validation:Maximum=127
	PrefixLength int32 `json:"prefixLength,omitempty"`
}

// IPv6ClientAddressAssignment configures IPv6 client addressing. Only SLAAC is
// modeled; the upstream optional DHCPv6 configuration is deliberately
// unmodeled and is left to a later change.
type IPv6ClientAddressAssignment struct {
	// SLAACEnabled allows devices to obtain addresses via stateless address
	// autoconfiguration.
	SLAACEnabled bool `json:"slaacEnabled"`
}

// RouterAdvertisement configures IPv6 router advertisement.
type RouterAdvertisement struct {
	// Priority is the router advertisement priority.
	// +kubebuilder:validation:Enum=LOW;MEDIUM;HIGH
	Priority string `json:"priority"`
}

// WANSelector selects an upstream WAN interface by its human-readable name
// (GET /v1/sites/{siteId}/wans). Using the name keeps the opaque WAN UUID out
// of spec (design D8). The controller resolves it at reconcile time; until a
// WAN lookup lands in the client, prefix-delegation IPv6 fails closed.
type WANSelector struct {
	// Name is the human-readable WAN interface name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// UnifiNetworkStatus defines the observed state of UnifiNetwork.
type UnifiNetworkStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiNetwork resource.
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

	// networkID is the UUID of the upstream UniFi network. It is recorded in
	// status only, never in spec.
	// +optional
	NetworkID string `json:"networkID,omitempty"`

	// zoneID is the UUID of the firewall zone the network belongs to. Zone
	// membership is owned by UnifiFirewallZone; this resource only reports the
	// resolved zone in status.
	// +optional
	ZoneID string `json:"zoneID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Management",type=string,JSONPath=`.spec.management`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiNetwork is the Schema for the unifinetworks API
type UnifiNetwork struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiNetwork
	// +required
	Spec UnifiNetworkSpec `json:"spec"`

	// status defines the observed state of UnifiNetwork
	// +optional
	Status UnifiNetworkStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiNetworkList contains a list of UnifiNetwork
type UnifiNetworkList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiNetwork `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiNetwork{}, &UnifiNetworkList{})
}
