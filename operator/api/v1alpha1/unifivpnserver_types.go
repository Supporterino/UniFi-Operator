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

// UnifiVpnServerSpec defines the desired state of UnifiVpnServer. The frozen
// Integration v1 v10.4.57 surface exposes VPN servers read-only, so the CR
// adopts an existing upstream server by name; it authors no variant
// configuration and no pre-shared key. The spec never contains a UniFi internal
// identifier; the resolved upstream UUID and observed type/enabled belong in
// status only.
type UnifiVpnServerSpec struct {
	// SiteRef references the UnifiSite that owns this server. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable name of the existing upstream VPN server to
	// adopt.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// UnifiVpnServerStatus defines the observed state of UnifiVpnServer.
type UnifiVpnServerStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiVpnServer resource.
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

	// serverID is the UUID of the adopted upstream VPN server. It is recorded in
	// status only, never in spec.
	// +optional
	ServerID string `json:"serverID,omitempty"`

	// type is the observed upstream server type (WIREGUARD, OPENVPN, L2TP,
	// PPTP, or UID).
	// +optional
	Type string `json:"type,omitempty"`

	// enabled is the observed upstream enabled state.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.status.type`
// +kubebuilder:printcolumn:name="Enabled",type=boolean,JSONPath=`.status.enabled`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiVpnServer is the Schema for the unifivpnservers API
type UnifiVpnServer struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiVpnServer
	// +required
	Spec UnifiVpnServerSpec `json:"spec"`

	// status defines the observed state of UnifiVpnServer
	// +optional
	Status UnifiVpnServerStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiVpnServerList contains a list of UnifiVpnServer
type UnifiVpnServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiVpnServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiVpnServer{}, &UnifiVpnServerList{})
}
