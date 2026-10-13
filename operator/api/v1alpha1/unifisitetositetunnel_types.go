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

// UnifiSiteToSiteTunnelSpec defines the desired state of
// UnifiSiteToSiteTunnel. The frozen Integration v1 v10.4.57 surface exposes
// site-to-site tunnels read-only, so the CR adopts an existing upstream tunnel
// by name; it authors no local/remote endpoints and no pre-shared key. The spec
// never contains a UniFi internal identifier; the resolved upstream UUID and
// observed type belong in status only.
type UnifiSiteToSiteTunnelSpec struct {
	// SiteRef references the UnifiSite that owns this tunnel. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable name of the existing upstream site-to-site
	// tunnel to adopt.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// UnifiSiteToSiteTunnelStatus defines the observed state of
// UnifiSiteToSiteTunnel.
type UnifiSiteToSiteTunnelStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiSiteToSiteTunnel
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

	// tunnelID is the UUID of the adopted upstream site-to-site tunnel. It is
	// recorded in status only, never in spec.
	// +optional
	TunnelID string `json:"tunnelID,omitempty"`

	// type is the observed upstream tunnel type (IPSEC, OPENVPN, or WIREGUARD).
	// +optional
	Type string `json:"type,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.status.type`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiSiteToSiteTunnel is the Schema for the unifisitetositetunnels API
type UnifiSiteToSiteTunnel struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiSiteToSiteTunnel
	// +required
	Spec UnifiSiteToSiteTunnelSpec `json:"spec"`

	// status defines the observed state of UnifiSiteToSiteTunnel
	// +optional
	Status UnifiSiteToSiteTunnelStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiSiteToSiteTunnelList contains a list of UnifiSiteToSiteTunnel
type UnifiSiteToSiteTunnelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiSiteToSiteTunnel `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiSiteToSiteTunnel{}, &UnifiSiteToSiteTunnelList{})
}
