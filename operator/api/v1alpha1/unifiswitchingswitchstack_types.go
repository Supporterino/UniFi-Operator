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

// SwitchingStackMember is an observed switch-stack member device. The device ID
// is an upstream identifier and is recorded in status only.
type SwitchingStackMember struct {
	// DeviceID is the UUID of the member device.
	// +optional
	DeviceID string `json:"deviceID,omitempty"`
}

// UnifiSwitchingSwitchStackSpec defines the desired state of
// UnifiSwitchingSwitchStack. The frozen Integration v1 v10.4.57 surface exposes
// switch stacks read-only, so the CR adopts an existing upstream stack by name;
// it authors no member devices. The spec never contains a UniFi internal
// identifier; the resolved upstream UUID and observed members belong in status
// only.
type UnifiSwitchingSwitchStackSpec struct {
	// SiteRef references the UnifiSite that owns this stack. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable name of the existing upstream switch stack to
	// adopt.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// UnifiSwitchingSwitchStackStatus defines the observed state of
// UnifiSwitchingSwitchStack.
type UnifiSwitchingSwitchStackStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiSwitchingSwitchStack
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

	// stackID is the UUID of the adopted upstream switch stack. It is recorded
	// in status only, never in spec.
	// +optional
	StackID string `json:"stackID,omitempty"`

	// members is the observed set of member devices.
	// +optional
	Members []SwitchingStackMember `json:"members,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiSwitchingSwitchStack is the Schema for the unifiswitchingswitchstacks API
type UnifiSwitchingSwitchStack struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiSwitchingSwitchStack
	// +required
	Spec UnifiSwitchingSwitchStackSpec `json:"spec"`

	// status defines the observed state of UnifiSwitchingSwitchStack
	// +optional
	Status UnifiSwitchingSwitchStackStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiSwitchingSwitchStackList contains a list of UnifiSwitchingSwitchStack
type UnifiSwitchingSwitchStackList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiSwitchingSwitchStack `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiSwitchingSwitchStack{}, &UnifiSwitchingSwitchStackList{})
}
