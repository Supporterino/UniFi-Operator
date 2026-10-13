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

// UnifiAclRuleOrderingFinalizer blocks removal of a UnifiAclRuleOrdering until
// it is the last writer of the site's ACL-rule order. Clearing ordering has no
// upstream-delete semantics, so the finalizer is removed directly.
const UnifiAclRuleOrderingFinalizer = "unifi.supporterino.de/acl-rule-ordering-finalizer"

// UnifiAclRuleOrderingSpec defines the desired state of UnifiAclRuleOrdering. It
// is authoritative for the flat, site-global ACL-rule order (mirroring the
// upstream orderedAclRuleIds list). Rules are referenced by Kubernetes identity;
// system/derived rules are anchored and are never reordered.
type UnifiAclRuleOrderingSpec struct {
	// SiteRef references the UnifiSite that owns this ordering. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// OrderedAclRules lists the UnifiAclRule references in the desired order.
	// The list may be empty.
	// +optional
	OrderedAclRules []CoreRef `json:"orderedAclRules"`
}

// UnifiAclRuleOrderingStatus defines the observed state of
// UnifiAclRuleOrdering.
type UnifiAclRuleOrderingStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiAclRuleOrdering
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

	// drift lists the names of user-defined UnifiAclRule objects present
	// upstream for the site but absent from this ordering object. They are
	// reported here rather than silently reordered.
	// +optional
	Drift []string `json:"drift,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiAclRuleOrdering is the Schema for the unifiaclruleorderings API
type UnifiAclRuleOrdering struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiAclRuleOrdering
	// +required
	Spec UnifiAclRuleOrderingSpec `json:"spec"`

	// status defines the observed state of UnifiAclRuleOrdering
	// +optional
	Status UnifiAclRuleOrderingStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiAclRuleOrderingList contains a list of UnifiAclRuleOrdering
type UnifiAclRuleOrderingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiAclRuleOrdering `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiAclRuleOrdering{}, &UnifiAclRuleOrderingList{})
}
