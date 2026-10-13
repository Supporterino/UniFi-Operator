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

// SwitchingMcLagPeer is an observed MC-LAG domain peer: a device, its link port
// indexes, and its role. All are upstream identifiers and are recorded in status
// only.
type SwitchingMcLagPeer struct {
	// DeviceID is the UUID of the peer device.
	// +optional
	DeviceID string `json:"deviceID,omitempty"`

	// LinkPortIdxs are the peer link port indexes.
	// +optional
	LinkPortIdxs []int32 `json:"linkPortIdxs,omitempty"`

	// Role is the peer role (TOP or BOTTOM).
	// +optional
	Role string `json:"role,omitempty"`
}

// UnifiSwitchingMcLagDomainSpec defines the desired state of
// UnifiSwitchingMcLagDomain. The frozen Integration v1 v10.4.57 surface exposes
// MC-LAG domains read-only, so the CR adopts an existing upstream domain by
// name; it authors no peers, links, or member devices. The spec never contains a
// UniFi internal identifier; the resolved upstream UUID and observed
// configuration belong in status only.
type UnifiSwitchingMcLagDomainSpec struct {
	// SiteRef references the UnifiSite that owns this domain. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable name of the existing upstream MC-LAG domain to
	// adopt.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// UnifiSwitchingMcLagDomainStatus defines the observed state of
// UnifiSwitchingMcLagDomain.
type UnifiSwitchingMcLagDomainStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiSwitchingMcLagDomain
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

	// domainID is the UUID of the adopted upstream MC-LAG domain. It is recorded
	// in status only, never in spec.
	// +optional
	DomainID string `json:"domainID,omitempty"`

	// members is the observed union of the domain's local LAG members.
	// +optional
	Members []SwitchingLagMember `json:"members,omitempty"`

	// peers is the observed set of MC-LAG domain peers.
	// +optional
	Peers []SwitchingMcLagPeer `json:"peers,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiSwitchingMcLagDomain is the Schema for the unifiswitchingmclagdomains API
type UnifiSwitchingMcLagDomain struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiSwitchingMcLagDomain
	// +required
	Spec UnifiSwitchingMcLagDomainSpec `json:"spec"`

	// status defines the observed state of UnifiSwitchingMcLagDomain
	// +optional
	Status UnifiSwitchingMcLagDomainStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiSwitchingMcLagDomainList contains a list of UnifiSwitchingMcLagDomain
type UnifiSwitchingMcLagDomainList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiSwitchingMcLagDomain `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiSwitchingMcLagDomain{}, &UnifiSwitchingMcLagDomainList{})
}
