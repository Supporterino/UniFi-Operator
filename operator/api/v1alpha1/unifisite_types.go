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

// UnifiSiteFinalizer drains owned child resources before a UnifiSite is
// removed, so a child finalizer can still resolve spec.siteRef to reach the
// controller. The site controller adds and removes it symmetrically.
const UnifiSiteFinalizer = "unifi.supporterino.de/finalizer"

// UnifiSiteSpec defines the desired state of UnifiSite. A site binds an
// existing upstream UniFi site to a UnifiController; it never carries
// connection details of its own.
type UnifiSiteSpec struct {
	// ControllerRef references the UnifiController that holds the connection
	// details for this site. The reference resolves in the same namespace.
	ControllerRef CoreRef `json:"controllerRef"`

	// InternalReference names an existing upstream site ("internalReference"
	// in GET /v1/sites). The operator adopts the matching site and never
	// creates or deletes upstream sites. The upstream UUID is recorded in
	// status only.
	// +kubebuilder:validation:MinLength=1
	InternalReference string `json:"internalReference"`
}

// UnifiSiteStatus defines the observed state of UnifiSite.
type UnifiSiteStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiSite resource.
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

	// siteID is the UUID of the adopted upstream UniFi site. It is recorded in
	// status only, never in spec.
	// +optional
	SiteID string `json:"siteID,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="InternalReference",type=string,JSONPath=`.spec.internalReference`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiSite is the Schema for the unifisites API
type UnifiSite struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiSite
	// +required
	Spec UnifiSiteSpec `json:"spec"`

	// status defines the observed state of UnifiSite
	// +optional
	Status UnifiSiteStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiSiteList contains a list of UnifiSite
type UnifiSiteList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiSite `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiSite{}, &UnifiSiteList{})
}
