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

// UnifiRadiusProfileSpec defines the desired state of UnifiRadiusProfile. The
// frozen Integration v1 v10.4.57 surface exposes RADIUS profiles read-only
// (GET /v1/sites/{siteId}/radius/profiles; schema {id, metadata, name}), so the
// CR adopts an existing upstream profile by name: it authors no authentication
// or accounting servers, no server ports, and no shared secret, and the
// operator never creates, updates, or deletes the profile upstream. The spec
// never contains a UniFi internal identifier; the resolved upstream UUID belongs
// in status only.
type UnifiRadiusProfileSpec struct {
	// SiteRef references the UnifiSite that owns this profile. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Name is the human-readable name of the existing upstream RADIUS profile
	// to adopt. The profile must already exist on the referenced site; the
	// operator never creates, updates, or deletes it. The resolved upstream
	// UUID is recorded in status only.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// UnifiRadiusProfileStatus defines the observed state of UnifiRadiusProfile.
type UnifiRadiusProfileStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiRadiusProfile resource.
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

	// radiusProfileID is the UUID of the adopted upstream RADIUS profile. It is
	// recorded in status only, never in spec.
	// +optional
	RadiusProfileID string `json:"radiusProfileID,omitempty"`

	// origin is the observed upstream profile origin (USER_DEFINED,
	// SYSTEM_DEFINED, or DERIVED). It is observed, never authored.
	// +optional
	Origin string `json:"origin,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Name",type=string,JSONPath=`.spec.name`
// +kubebuilder:printcolumn:name="Origin",type=string,JSONPath=`.status.origin`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiRadiusProfile is the Schema for the unifiradiusprofiles API
type UnifiRadiusProfile struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiRadiusProfile
	// +required
	Spec UnifiRadiusProfileSpec `json:"spec"`

	// status defines the observed state of UnifiRadiusProfile
	// +optional
	Status UnifiRadiusProfileStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiRadiusProfileList contains a list of UnifiRadiusProfile
type UnifiRadiusProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiRadiusProfile `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiRadiusProfile{}, &UnifiRadiusProfileList{})
}
