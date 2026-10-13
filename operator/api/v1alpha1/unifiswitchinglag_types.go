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

// SwitchingLagMember is an observed LAG member: a device and its member port
// indexes. Both are upstream identifiers and are recorded in status only.
type SwitchingLagMember struct {
	// DeviceID is the UUID of the member device.
	// +optional
	DeviceID string `json:"deviceID,omitempty"`

	// PortIdxs are the member port indexes on the device.
	// +optional
	PortIdxs []int32 `json:"portIdxs,omitempty"`
}

// SwitchingLagMemberSelector selects one member of a LAG by a read-only
// device-tag name and its member port indexes. The device tag is resolved
// against the read-only device-tags list; no opaque device UUID is authored.
type SwitchingLagMemberSelector struct {
	// DeviceTag selects the member device by its read-only device-tag name.
	DeviceTag DeviceTagSelector `json:"deviceTag"`

	// PortIdxs are the member port indexes on the selected device.
	// +kubebuilder:validation:MinItems=1
	PortIdxs []int32 `json:"portIdxs"`
}

// UnifiSwitchingLagSpec defines the desired state of UnifiSwitchingLag. The
// frozen Integration v1 v10.4.57 surface exposes LAGs read-only and without a
// name, so the CR adopts a LAG by a typed composite selector: its type plus a
// member selector expressed through device-tag selectors and port indexes. The
// operator selects exactly one matching upstream LAG or fails closed. The spec
// never contains a UniFi internal identifier; the resolved upstream UUID and
// observed configuration belong in status only.
type UnifiSwitchingLagSpec struct {
	// SiteRef references the UnifiSite that owns this LAG. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Type is the LAG type discriminator.
	// +kubebuilder:validation:Enum=LOCAL;MULTI_CHASSIS;SWITCH_STACK
	Type string `json:"type"`

	// Members select the LAG's members by device tag and port indexes. The
	// controller adopts the unique upstream LAG whose type and members match
	// every selector; zero or more than one match fails closed.
	// +kubebuilder:validation:MinItems=1
	Members []SwitchingLagMemberSelector `json:"members"`
}

// UnifiSwitchingLagStatus defines the observed state of UnifiSwitchingLag.
type UnifiSwitchingLagStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiSwitchingLag resource.
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

	// lagID is the UUID of the adopted upstream LAG. It is recorded in status
	// only, never in spec.
	// +optional
	LagID string `json:"lagID,omitempty"`

	// type is the observed upstream LAG type (LOCAL, MULTI_CHASSIS, or
	// SWITCH_STACK).
	// +optional
	Type string `json:"type,omitempty"`

	// members is the observed member configuration of the LAG.
	// +optional
	Members []SwitchingLagMember `json:"members,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="LagID",type=string,JSONPath=`.status.lagID`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiSwitchingLag is the Schema for the unifiswitchinglags API
type UnifiSwitchingLag struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiSwitchingLag
	// +required
	Spec UnifiSwitchingLagSpec `json:"spec"`

	// status defines the observed state of UnifiSwitchingLag
	// +optional
	Status UnifiSwitchingLagStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiSwitchingLagList contains a list of UnifiSwitchingLag
type UnifiSwitchingLagList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiSwitchingLag `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiSwitchingLag{}, &UnifiSwitchingLagList{})
}
