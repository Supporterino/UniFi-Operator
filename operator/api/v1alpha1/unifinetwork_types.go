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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// UnifiNetworkSpec defines the desired state of UnifiNetwork. It is authored by
// users and never contains a UniFi internal identifier: the controller matches
// the upstream network by Name.
type UnifiNetworkSpec struct {
	// Site is the UniFi site the network belongs to (for example "default").
	// +kubebuilder:validation:MinLength=1
	Site string `json:"site"`

	// Name is the human-readable network name used to match the upstream
	// networkconf object.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// VLAN is the 802.1Q VLAN tag. Omit for an untagged network.
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=4094
	// +optional
	VLAN *int32 `json:"vlan,omitempty"`

	// Subnet is the network's IPv4 subnet in CIDR notation.
	// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}(/[0-9]{1,2})?$`
	// +optional
	Subnet string `json:"subnet,omitempty"`

	// Enabled controls whether the network is enabled.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// UnifiNetworkStatus defines the observed state of UnifiNetwork.
type UnifiNetworkStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiNetwork resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
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

	// controllerID is the upstream UniFi identifier ("_id") this resource maps
	// to. It is recorded in status only, never in spec.
	// +optional
	ControllerID string `json:"controllerID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiNetwork is the Schema for the unifinetworks API
type UnifiNetwork struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiNetwork.
	//
	// Scaffold limitation: the fixture-backed reconciler matches upstream
	// networks by name only and does not yet reconcile vlan, subnet, or enabled,
	// so a Ready=True condition is not a guarantee that those fields match
	// upstream. Field reconciliation lands with real controller integration.
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
