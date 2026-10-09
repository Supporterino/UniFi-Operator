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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// UnifiControllerSpec defines the desired state of UnifiController: the
// connection from the operator to a single UniFi Network console over the
// Integration v1 API. It is the only kind that holds connection details.
type UnifiControllerSpec struct {
	// URL is the base URL of the UniFi Network console. It must use HTTPS: the
	// API key is sent on every request and must not travel in cleartext (for
	// example "https://unifi.example.com").
	// +kubebuilder:validation:Pattern=`^https://[^\s]+$`
	URL string `json:"url"`

	// SecretRef references the same-namespace Secret key holding the UniFi API
	// key. The key is resolved at reconcile time and is never inlined here,
	// logged, or written to status. A SecretKeySelector has no namespace field
	// and therefore resolves in this object's namespace.
	SecretRef *corev1.SecretKeySelector `json:"secretRef"`

	// InsecureSkipVerify disables TLS certificate verification when
	// connecting to the console. This is insecure and only intended for a
	// self-signed console certificate in a trusted environment.
	// +kubebuilder:default=false
	// +optional
	InsecureSkipVerify *bool `json:"insecureSkipVerify,omitempty"`
}

// UnifiControllerStatus defines the observed state of UnifiController.
type UnifiControllerStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiController resource.
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

	// applicationVersion is the UniFi Network application version detected via
	// GET /v1/info. It is recorded in status only and gates which Integration
	// v1 capabilities the controller treats as available.
	// +optional
	ApplicationVersion string `json:"applicationVersion,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="URL",type=string,JSONPath=`.spec.url`
// +kubebuilder:printcolumn:name="AppVersion",type=string,JSONPath=`.status.applicationVersion`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiController is the Schema for the unificontrollers API
type UnifiController struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiController
	// +required
	Spec UnifiControllerSpec `json:"spec"`

	// status defines the observed state of UnifiController
	// +optional
	Status UnifiControllerStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiControllerList contains a list of UnifiController
type UnifiControllerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiController `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiController{}, &UnifiControllerList{})
}
