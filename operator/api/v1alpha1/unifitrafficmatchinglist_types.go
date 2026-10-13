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

// UnifiTrafficMatchingListFinalizer blocks removal of a
// UnifiTrafficMatchingList until its ownable upstream list has been deleted. The
// reconciler adds it before creating the upstream list and removes it after the
// upstream list is gone.
const UnifiTrafficMatchingListFinalizer = "unifi.supporterino.de/traffic-matching-list-finalizer"

// Traffic matching list types accepted by spec.type.
const (
	// TrafficMatchingListTypeIPv4 is a list of IPv4 addresses, ranges, and
	// subnets.
	TrafficMatchingListTypeIPv4 = "IPV4_ADDRESSES"
	// TrafficMatchingListTypeIPv6 is a list of IPv6 addresses and subnets.
	TrafficMatchingListTypeIPv6 = "IPV6_ADDRESSES"
	// TrafficMatchingListTypePorts is a list of TCP/UDP port numbers and ranges.
	TrafficMatchingListTypePorts = "PORTS"
)

// +kubebuilder:validation:XValidation:rule="self.type == 'PORTS' ? self.items.all(i, i.type == 'PORT_NUMBER' || i.type == 'PORT_NUMBER_RANGE') : true",message="a PORTS list may contain only PORT_NUMBER or PORT_NUMBER_RANGE entries"
// +kubebuilder:validation:XValidation:rule="self.type == 'IPV4_ADDRESSES' ? self.items.all(i, i.type in ['IP_ADDRESS','IP_ADDRESS_RANGE','SUBNET']) : true",message="an IPV4_ADDRESSES list may contain only IP_ADDRESS, IP_ADDRESS_RANGE, or SUBNET entries"
// +kubebuilder:validation:XValidation:rule="self.type == 'IPV6_ADDRESSES' ? self.items.all(i, i.type in ['IP_ADDRESS','SUBNET']) : true",message="an IPV6_ADDRESSES list may contain only IP_ADDRESS or SUBNET entries"

// UnifiTrafficMatchingListSpec defines the desired state of
// UnifiTrafficMatchingList. It mirrors the Integration v1 discriminated
// `Create or update traffic matching list` union (spec v10.4.57): Type selects
// IPV4_ADDRESSES, IPV6_ADDRESSES, or PORTS, and each Item is a typed value or
// range conforming to the selected type. The spec never contains a UniFi
// internal identifier; the resolved upstream list UUID belongs in status only.
type UnifiTrafficMatchingListSpec struct {
	// SiteRef references the UnifiSite that owns this list. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable matching-list name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Type is the list union discriminator.
	// +kubebuilder:validation:Enum=IPV4_ADDRESSES;IPV6_ADDRESSES;PORTS
	Type string `json:"type"`

	// Items are the typed entries of the list. Each entry's own type must
	// conform to the selected Type (CEL), and at least one entry is required.
	// +kubebuilder:validation:MinItems=1
	Items []TrafficMatchingEntry `json:"items"`
}

// +kubebuilder:validation:XValidation:rule="has(self.address) == (self.type == 'IP_ADDRESS' || self.type == 'SUBNET')",message="address is required for IP_ADDRESS and SUBNET and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.addressRange) == (self.type == 'IP_ADDRESS_RANGE')",message="addressRange is required for IP_ADDRESS_RANGE and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.port) == (self.type == 'PORT_NUMBER')",message="port is required for PORT_NUMBER and forbidden otherwise"
// +kubebuilder:validation:XValidation:rule="has(self.portRange) == (self.type == 'PORT_NUMBER_RANGE')",message="portRange is required for PORT_NUMBER_RANGE and forbidden otherwise"

// TrafficMatchingEntry is one entry of a matching list. It is a discriminated
// union on Type with exactly one variant sub-object: an Address value (an IP
// address or CIDR subnet), an AddressRange, a Port, or a PortRange.
type TrafficMatchingEntry struct {
	// Type is the entry discriminator.
	// +kubebuilder:validation:Enum=IP_ADDRESS;IP_ADDRESS_RANGE;SUBNET;PORT_NUMBER;PORT_NUMBER_RANGE
	Type string `json:"type"`

	// Address is the single IP address (IP_ADDRESS) or CIDR subnet (SUBNET).
	// +optional
	Address *TrafficMatchingAddressEntry `json:"address,omitempty"`

	// AddressRange is the inclusive address range (IP_ADDRESS_RANGE).
	// +optional
	AddressRange *TrafficMatchingAddressRangeEntry `json:"addressRange,omitempty"`

	// Port is the single port number (PORT_NUMBER).
	// +optional
	Port *TrafficMatchingPortEntry `json:"port,omitempty"`

	// PortRange is the inclusive port range (PORT_NUMBER_RANGE).
	// +optional
	PortRange *TrafficMatchingPortRangeEntry `json:"portRange,omitempty"`
}

// TrafficMatchingAddressEntry is a single IP address or CIDR subnet value.
type TrafficMatchingAddressEntry struct {
	// Value is the IP address or CIDR subnet (for example 192.168.1.5 or
	// 192.168.1.0/24).
	// +kubebuilder:validation:MinLength=1
	Value string `json:"value"`
}

// TrafficMatchingAddressRangeEntry is an inclusive IP address range.
type TrafficMatchingAddressRangeEntry struct {
	// Start is the first address of the range.
	// +kubebuilder:validation:MinLength=1
	Start string `json:"start"`

	// Stop is the last address of the range.
	// +kubebuilder:validation:MinLength=1
	Stop string `json:"stop"`
}

// TrafficMatchingPortEntry is a single port number.
type TrafficMatchingPortEntry struct {
	// Value is the port number (1..65535).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Value int32 `json:"value"`
}

// TrafficMatchingPortRangeEntry is an inclusive port range.
type TrafficMatchingPortRangeEntry struct {
	// Start is the first port of the range (1..65535).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Start int32 `json:"start"`

	// Stop is the last port of the range (1..65535).
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Stop int32 `json:"stop"`
}

// UnifiTrafficMatchingListStatus defines the observed state of
// UnifiTrafficMatchingList.
type UnifiTrafficMatchingListStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiTrafficMatchingList
	// resource.
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

	// listID is the UUID of the upstream traffic matching list. It is recorded
	// in status only, never in spec.
	// +optional
	ListID string `json:"listID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiTrafficMatchingList is the Schema for the unifitrafficmatchinglists API
type UnifiTrafficMatchingList struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiTrafficMatchingList
	// +required
	Spec UnifiTrafficMatchingListSpec `json:"spec"`

	// status defines the observed state of UnifiTrafficMatchingList
	// +optional
	Status UnifiTrafficMatchingListStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiTrafficMatchingListList contains a list of UnifiTrafficMatchingList
type UnifiTrafficMatchingListList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiTrafficMatchingList `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiTrafficMatchingList{}, &UnifiTrafficMatchingListList{})
}
