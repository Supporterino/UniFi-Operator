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

// UnifiFirewallPolicyOrderingFinalizer blocks removal of a
// UnifiFirewallPolicyOrdering until its upstream ordering has been cleared. The
// reconciler adds it before writing order and removes it after the object is
// gone. Clearing ordering has no upstream-delete semantics, so the finalizer is
// removed directly.
const UnifiFirewallPolicyOrderingFinalizer = "unifi.supporterino.de/firewall-policy-ordering-finalizer"

// UnifiFirewallPolicyOrderingSpec defines the desired state of
// UnifiFirewallPolicyOrdering. It identifies a source/destination zone pair and
// exposes two ordered lists of UnifiFirewallPolicy references mirroring the
// upstream before/after-system-defined partition. System-defined policies are
// anchored and are never reordered; the ordering object is authoritative for
// its user-defined scope.
type UnifiFirewallPolicyOrderingSpec struct {
	// SiteRef references the UnifiSite that owns this ordering. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Source identifies the originating zone of the pair.
	Source FirewallZoneSelector `json:"source"`

	// Destination identifies the target zone of the pair.
	Destination FirewallZoneSelector `json:"destination"`

	// BeforeSystemDefined lists the UnifiFirewallPolicy references ordered
	// before the system-defined policies for the zone pair.
	// +optional
	BeforeSystemDefined []CoreRef `json:"beforeSystemDefined"`

	// AfterSystemDefined lists the UnifiFirewallPolicy references ordered after
	// the system-defined policies for the zone pair.
	// +optional
	AfterSystemDefined []CoreRef `json:"afterSystemDefined"`
}

// UnifiFirewallPolicyOrderingStatus defines the observed state of
// UnifiFirewallPolicyOrdering.
type UnifiFirewallPolicyOrderingStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiFirewallPolicyOrdering
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

	// drift lists the names of user-defined UnifiFirewallPolicy objects present
	// upstream for the zone pair but absent from this ordering object. They are
	// reported here rather than silently reordered.
	// +optional
	Drift []string `json:"drift,omitempty"`

	// sourceZoneID is the resolved UUID of the upstream source zone. It is
	// recorded in status only, never in spec.
	// +optional
	SourceZoneID string `json:"sourceZoneID,omitempty"`

	// destinationZoneID is the resolved UUID of the upstream destination zone.
	// It is recorded in status only, never in spec.
	// +optional
	DestinationZoneID string `json:"destinationZoneID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiFirewallPolicyOrdering is the Schema for the unififirewallpolicyorderings
// API
type UnifiFirewallPolicyOrdering struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiFirewallPolicyOrdering
	// +required
	Spec UnifiFirewallPolicyOrderingSpec `json:"spec"`

	// status defines the observed state of UnifiFirewallPolicyOrdering
	// +optional
	Status UnifiFirewallPolicyOrderingStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiFirewallPolicyOrderingList contains a list of UnifiFirewallPolicyOrdering
type UnifiFirewallPolicyOrderingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiFirewallPolicyOrdering `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiFirewallPolicyOrdering{}, &UnifiFirewallPolicyOrderingList{})
}
