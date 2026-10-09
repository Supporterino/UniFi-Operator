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

// UnifiFirewallZoneFinalizer blocks removal of a UnifiFirewallZone until its
// ownable upstream zone has been deleted. The zone reconciler adds it before
// creating a custom upstream zone and removes it after the upstream zone is
// gone. System-defined zones are adopted read-only and are never deleted.
const UnifiFirewallZoneFinalizer = "unifi.supporterino.de/firewall-zone-finalizer"

// UnifiFirewallZoneSpec defines the desired state of UnifiFirewallZone. It
// creates, updates, and deletes user-defined (custom) upstream firewall zones
// and owns their network membership. System-defined upstream zones are adopted
// read-only. The spec never contains a UniFi internal identifier: references
// to member networks are Kubernetes names and the upstream zone UUID lives in
// status only.
type UnifiFirewallZoneSpec struct {
	// SiteRef references the UnifiSite that owns this zone. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable firewall-zone name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// NetworkRefs lists the UnifiNetwork resources that are members of this
	// zone. Each reference resolves in the same namespace and is resolved to
	// the network's upstream UUID at reconcile time. The list may be empty.
	// +optional
	NetworkRefs []CoreRef `json:"networkRefs"`
}

// UnifiFirewallZoneStatus defines the observed state of UnifiFirewallZone.
type UnifiFirewallZoneStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiFirewallZone resource.
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

	// zoneID is the UUID of the upstream firewall zone. It is recorded in
	// status only, never in spec.
	// +optional
	ZoneID string `json:"zoneID,omitempty"`

	// origin is the upstream zone origin (USER_DEFINED, SYSTEM_DEFINED,
	// DERIVED, or ORCHESTRATED). Only USER_DEFINED zones are written; any other
	// origin is adopted read-only and reported here.
	// +optional
	Origin string `json:"origin,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Origin",type=string,JSONPath=`.status.origin`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiFirewallZone is the Schema for the unififirewallzones API
type UnifiFirewallZone struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiFirewallZone
	// +required
	Spec UnifiFirewallZoneSpec `json:"spec"`

	// status defines the observed state of UnifiFirewallZone
	// +optional
	Status UnifiFirewallZoneStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiFirewallZoneList contains a list of UnifiFirewallZone
type UnifiFirewallZoneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiFirewallZone `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiFirewallZone{}, &UnifiFirewallZoneList{})
}
