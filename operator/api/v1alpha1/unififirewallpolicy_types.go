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

// UnifiFirewallPolicyFinalizer blocks removal of a UnifiFirewallPolicy until its
// ownable upstream policy has been deleted. The reconciler adds it before
// creating the upstream policy and removes it after the upstream policy is
// gone. A system-defined upstream policy is adopted read-only and is never
// deleted.
const UnifiFirewallPolicyFinalizer = "unifi.supporterino.de/firewall-policy-finalizer"

// BuiltInFirewallZoneSelector selects a system-defined upstream firewall zone
// (for example Internal or External) by its name. System zones are read-only
// and are resolved against the read-only zone list at reconcile time; no opaque
// zone UUID appears in spec.
type BuiltInFirewallZoneSelector struct {
	// Name is the human-readable name of the system-defined firewall zone.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// FirewallZoneSelector identifies an upstream firewall zone: either a
// configurable UnifiFirewallZone by Kubernetes identity or a system-defined
// zone by name. Exactly one of zoneRef or builtInZone is set.
// +kubebuilder:validation:XValidation:rule="has(self.zoneRef) != has(self.builtInZone)",message="exactly one of zoneRef or builtInZone must be set"
type FirewallZoneSelector struct {
	// ZoneRef references a configurable UnifiFirewallZone by name. The reference
	// resolves in the same namespace and the zone's upstream UUID is read from
	// its status.
	// +optional
	ZoneRef *CoreRef `json:"zoneRef,omitempty"`

	// BuiltInZone selects a system-defined zone by name.
	// +optional
	BuiltInZone *BuiltInFirewallZoneSelector `json:"builtInZone,omitempty"`
}

// FirewallPolicyEndpoint is a source or destination of a firewall policy: a
// zone (configurable or system-defined) plus an optional typed traffic filter.
// Exactly one of zoneRef or builtInZone is set.
// +kubebuilder:validation:XValidation:rule="has(self.zoneRef) != has(self.builtInZone)",message="exactly one of zoneRef or builtInZone must be set"
type FirewallPolicyEndpoint struct {
	// ZoneRef references a configurable UnifiFirewallZone by name. The reference
	// resolves in the same namespace and the zone's upstream UUID is read from
	// its status.
	// +optional
	ZoneRef *CoreRef `json:"zoneRef,omitempty"`

	// BuiltInZone selects a system-defined zone by name.
	// +optional
	BuiltInZone *BuiltInFirewallZoneSelector `json:"builtInZone,omitempty"`

	// Filter optionally narrows the endpoint by a typed traffic filter.
	// +optional
	Filter *FirewallTrafficFilter `json:"filter,omitempty"`
}

// FirewallTrafficFilter is a typed traffic filter referencing a
// UnifiTrafficMatchingList by Kubernetes identity; no opaque list UUID appears
// in spec. It is discriminated on type: IP_ADDRESS carries an ipAddress filter,
// PORT carries a port filter.
// +kubebuilder:validation:XValidation:rule="self.type == 'IP_ADDRESS' ? has(self.ipAddress) : !has(self.ipAddress)",message="ipAddress requires type IP_ADDRESS"
// +kubebuilder:validation:XValidation:rule="self.type == 'PORT' ? has(self.port) : !has(self.port)",message="port requires type PORT"
type FirewallTrafficFilter struct {
	// Type is the traffic-filter discriminator (IP_ADDRESS or PORT).
	// +kubebuilder:validation:Enum=IP_ADDRESS;PORT
	Type string `json:"type"`

	// IPAddress matches the endpoint's IP addresses using a matching list.
	// +optional
	IPAddress *FirewallTrafficMatchingListFilter `json:"ipAddress,omitempty"`

	// Port matches the endpoint's ports using a matching list.
	// +optional
	Port *FirewallTrafficMatchingListFilter `json:"port,omitempty"`
}

// FirewallTrafficMatchingListFilter references a UnifiTrafficMatchingList by
// Kubernetes identity. The controller resolves the reference to the list's
// upstream UUID at reconcile time; an unresolvable reference fails closed.
type FirewallTrafficMatchingListFilter struct {
	// ListRef references the UnifiTrafficMatchingList by name. The reference
	// resolves in the same namespace.
	ListRef CoreRef `json:"listRef"`

	// MatchOpposite matches everything except the referenced list's entries.
	// +kubebuilder:default=false
	// +optional
	MatchOpposite *bool `json:"matchOpposite,omitempty"`
}

// FirewallPolicyAction is the action applied to matched traffic. It is
// discriminated on type: ALLOW requires allowReturnTraffic, BLOCK and REJECT
// carry no extra field.
// +kubebuilder:validation:XValidation:rule="self.type == 'ALLOW' ? has(self.allowReturnTraffic) : !has(self.allowReturnTraffic)",message="allowReturnTraffic is required for ALLOW and forbidden otherwise"
type FirewallPolicyAction struct {
	// Type is the action discriminator (ALLOW, BLOCK, or REJECT).
	// +kubebuilder:validation:Enum=ALLOW;BLOCK;REJECT
	Type string `json:"type"`

	// AllowReturnTraffic creates a derived policy for the mirrored zone pair to
	// allow the return traffic. Required for type ALLOW and forbidden otherwise.
	// +optional
	AllowReturnTraffic *bool `json:"allowReturnTraffic,omitempty"`
}

// FirewallIPProtocolScope defines the IP version the policy matches. An unset
// protocol filter matches all protocols.
type FirewallIPProtocolScope struct {
	// IPVersion is the IP-version discriminator (IPV4, IPV4_AND_IPV6, or IPV6).
	// +kubebuilder:validation:Enum=IPV4;IPV4_AND_IPV6;IPV6
	IPVersion string `json:"ipVersion"`
}

// UnifiFirewallPolicySpec defines the desired state of UnifiFirewallPolicy. It
// mirrors the Integration v1 firewall-policy union (spec v10.4.57): configurable
// source/destination zones are referenced by Kubernetes identity, system zones
// through a typed built-in-zone selector, and traffic filters through typed
// references to a UnifiTrafficMatchingList. The spec never contains a UniFi
// internal identifier; upstream policy and resolved zone UUIDs belong in status.
type UnifiFirewallPolicySpec struct {
	// SiteRef references the UnifiSite that owns this policy. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable policy name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// Enabled controls whether the policy is active.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// Description is an optional human-readable description.
	// +optional
	Description *string `json:"description,omitempty"`

	// LoggingEnabled generates syslog entries when traffic is matched.
	// +kubebuilder:default=false
	// +optional
	LoggingEnabled *bool `json:"loggingEnabled,omitempty"`

	// Action is the action applied to matched traffic.
	Action FirewallPolicyAction `json:"action"`

	// IPProtocolScope defines the IP version (and, when set, the protocol) the
	// policy matches.
	IPProtocolScope FirewallIPProtocolScope `json:"ipProtocolScope"`

	// Source is the originating zone plus an optional typed traffic filter.
	Source FirewallPolicyEndpoint `json:"source"`

	// Destination is the target zone plus an optional typed traffic filter.
	Destination FirewallPolicyEndpoint `json:"destination"`
}

// UnifiFirewallPolicyStatus defines the observed state of UnifiFirewallPolicy.
type UnifiFirewallPolicyStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiFirewallPolicy
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

	// policyID is the UUID of the upstream firewall policy. It is recorded in
	// status only, never in spec.
	// +optional
	PolicyID string `json:"policyID,omitempty"`

	// sourceZoneID is the resolved UUID of the upstream source zone. It is
	// recorded in status only, never in spec.
	// +optional
	SourceZoneID string `json:"sourceZoneID,omitempty"`

	// destinationZoneID is the resolved UUID of the upstream destination zone.
	// It is recorded in status only, never in spec.
	// +optional
	DestinationZoneID string `json:"destinationZoneID,omitempty"`

	// origin is the observed upstream policy origin (USER_DEFINED,
	// SYSTEM_DEFINED, DERIVED, or ORCHESTRATED). Only USER_DEFINED policies are
	// written; any other origin is adopted read-only and reported here.
	// +optional
	Origin string `json:"origin,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Action",type=string,JSONPath=`.spec.action.type`
// +kubebuilder:printcolumn:name="Origin",type=string,JSONPath=`.status.origin`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiFirewallPolicy is the Schema for the unififirewallpolicies API
type UnifiFirewallPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiFirewallPolicy
	// +required
	Spec UnifiFirewallPolicySpec `json:"spec"`

	// status defines the observed state of UnifiFirewallPolicy
	// +optional
	Status UnifiFirewallPolicyStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiFirewallPolicyList contains a list of UnifiFirewallPolicy
type UnifiFirewallPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiFirewallPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiFirewallPolicy{}, &UnifiFirewallPolicyList{})
}
