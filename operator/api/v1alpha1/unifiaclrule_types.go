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

// UnifiAclRuleFinalizer blocks removal of a UnifiAclRule until its ownable
// upstream rule has been deleted. The reconciler adds it before creating the
// upstream rule and removes it after the upstream rule is gone. A derived
// (non user-defined) upstream rule is adopted read-only and is never deleted.
const UnifiAclRuleFinalizer = "unifi.supporterino.de/acl-rule-finalizer"

// AclPort is a TCP/UDP port number (1..65535).
// +kubebuilder:validation:Minimum=1
// +kubebuilder:validation:Maximum=65535
type AclPort int32

// AclProtocol is a transport protocol an IPV4 ACL rule applies to.
// +kubebuilder:validation:Enum=TCP;UDP
type AclProtocol string

// AclIPAddressesEndpoint matches by IP addresses or subnets, optionally
// restricted to a port set.
type AclIPAddressesEndpoint struct {
	// Addresses are the IP addresses or subnets (for example 192.168.1.5 or
	// 192.168.1.0/24).
	// +kubebuilder:validation:MinItems=1
	Addresses []string `json:"addresses"`

	// Ports optionally restricts the match to these ports.
	// +optional
	// +kubebuilder:validation:MinItems=1
	Ports []AclPort `json:"ports,omitempty"`
}

// AclNetworksEndpoint matches by network membership, optionally restricted to a
// port set. Networks are referenced by Kubernetes identity; no opaque network
// UUID appears in spec.
type AclNetworksEndpoint struct {
	// NetworkRefs list the UnifiNetwork resources whose traffic is matched. Each
	// reference resolves in the same namespace.
	// +kubebuilder:validation:MinItems=1
	NetworkRefs []CoreRef `json:"networkRefs"`

	// Ports optionally restricts the match to these ports.
	// +optional
	// +kubebuilder:validation:MinItems=1
	Ports []AclPort `json:"ports,omitempty"`
}

// AclPortsEndpoint matches by port set.
type AclPortsEndpoint struct {
	// Ports are the ports to match.
	// +kubebuilder:validation:MinItems=1
	Ports []AclPort `json:"ports"`
}

// AclMacAddressesEndpoint matches by MAC address, optionally with a prefix
// length.
type AclMacAddressesEndpoint struct {
	// MacAddresses are the MAC addresses to match.
	// +kubebuilder:validation:MinItems=1
	MacAddresses []string `json:"macAddresses"`

	// PrefixLength restricts the match to a MAC prefix. Omit for a full match.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=48
	PrefixLength *int32 `json:"prefixLength,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.ipAddressesOrSubnets) == (self.type == 'IP_ADDRESSES_OR_SUBNETS')",message="ipAddressesOrSubnets is required for IP_ADDRESSES_OR_SUBNETS and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.networks) == (self.type == 'NETWORKS')",message="networks is required for NETWORKS and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.ports) == (self.type == 'PORTS')",message="ports is required for PORTS and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.macAddresses) == (self.type == 'MAC_ADDRESSES')",message="macAddresses is required for MAC_ADDRESSES and forbidden otherwise"

// AclRuleEndpoint is a source or destination filter. It is a discriminated union
// on Type with exactly one variant sub-object. The traffic-managed filter
// (which references devices or networks) is expressed through typed structures
// rather than opaque identifiers.
type AclRuleEndpoint struct {
	// Type is the endpoint discriminator.
	// +kubebuilder:validation:Enum=IP_ADDRESSES_OR_SUBNETS;NETWORKS;PORTS;MAC_ADDRESSES
	Type string `json:"type"`

	// IPAddressesOrSubnets is set for type IP_ADDRESSES_OR_SUBNETS.
	// +optional
	IPAddressesOrSubnets *AclIPAddressesEndpoint `json:"ipAddressesOrSubnets,omitempty"`

	// Networks is set for type NETWORKS.
	// +optional
	Networks *AclNetworksEndpoint `json:"networks,omitempty"`

	// Ports is set for type PORTS.
	// +optional
	Ports *AclPortsEndpoint `json:"ports,omitempty"`

	// MacAddresses is set for type MAC_ADDRESSES.
	// +optional
	MacAddresses *AclMacAddressesEndpoint `json:"macAddresses,omitempty"`
}

// UnifiAclRuleSpec defines the desired state of UnifiAclRule. It mirrors the
// Integration v1 ACL-rule union (spec v10.4.57): Type selects IPV4 or MAC and
// the source/destination filters must match, network membership is referenced by
// Kubernetes identity, and enforcing devices are selected by a typed,
// name-based device-tag selector. The spec never contains a UniFi internal
// identifier; the resolved upstream rule UUID belongs in status.
// +kubebuilder:validation:XValidation:rule="self.type == 'MAC' ? (self.source.type == 'MAC_ADDRESSES' && self.destination.type == 'MAC_ADDRESSES' && has(self.networkRef) && !has(self.protocolFilter)) : true",message="MAC rules require MAC_ADDRESSES source/destination filters and a networkRef, and forbid protocolFilter"
// +kubebuilder:validation:XValidation:rule="self.type == 'IPV4' ? (self.source.type != 'MAC_ADDRESSES' && self.destination.type != 'MAC_ADDRESSES' && !has(self.networkRef)) : true",message="IPV4 rules require IP-address-family filters and forbid networkRef"
type UnifiAclRuleSpec struct {
	// SiteRef references the UnifiSite that owns this rule. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable ACL-rule name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Description is an optional human-readable description.
	// +optional
	Description *string `json:"description,omitempty"`

	// Enabled controls whether the rule is active.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Action is the ACL action (ALLOW or BLOCK).
	// +kubebuilder:validation:Enum=ALLOW;BLOCK
	Action string `json:"action"`

	// Type is the rule discriminator (IPV4 or MAC).
	// +kubebuilder:validation:Enum=IPV4;MAC
	Type string `json:"type"`

	// Source is the originating traffic filter.
	Source AclRuleEndpoint `json:"source"`

	// Destination is the target traffic filter.
	Destination AclRuleEndpoint `json:"destination"`

	// ProtocolFilter restricts an IPV4 rule to specific transport protocols.
	// Only valid for type IPV4.
	// +optional
	// +kubebuilder:validation:MinItems=1
	ProtocolFilter []AclProtocol `json:"protocolFilter,omitempty"`

	// NetworkRef references the UnifiNetwork a MAC rule applies to. Required for
	// type MAC and forbidden for type IPV4.
	// +optional
	NetworkRef *CoreRef `json:"networkRef,omitempty"`

	// EnforcingDeviceTags scopes enforcement to devices carrying the named tags.
	// Omitting it provisions the rule to all enforcing-capable devices. The
	// selector is name-based; no opaque device identifier appears in spec.
	// +optional
	// +kubebuilder:validation:MinItems=1
	EnforcingDeviceTags []DeviceTagSelector `json:"enforcingDeviceTags,omitempty"`
}

// UnifiAclRuleStatus defines the observed state of UnifiAclRule.
type UnifiAclRuleStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiAclRule resource.
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

	// ruleID is the UUID of the upstream ACL rule. It is recorded in status
	// only, never in spec.
	// +optional
	RuleID string `json:"ruleID,omitempty"`

	// origin is the observed upstream rule origin (USER_DEFINED or DERIVED).
	// Only USER_DEFINED rules are written; any other origin is adopted read-only
	// and reported here.
	// +optional
	Origin string `json:"origin,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiAclRule is the Schema for the unifiaclrules API
type UnifiAclRule struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiAclRule
	// +required
	Spec UnifiAclRuleSpec `json:"spec"`

	// status defines the observed state of UnifiAclRule
	// +optional
	Status UnifiAclRuleStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiAclRuleList contains a list of UnifiAclRule
type UnifiAclRuleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiAclRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiAclRule{}, &UnifiAclRuleList{})
}
