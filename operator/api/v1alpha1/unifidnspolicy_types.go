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

// UnifiDnsPolicyFinalizer blocks removal of a UnifiDnsPolicy until its ownable
// upstream record has been deleted. The reconciler adds it before creating the
// upstream record and removes it after the upstream record is gone.
const UnifiDnsPolicyFinalizer = "unifi.supporterino.de/dns-policy-finalizer"

// DnsARecord is the A_RECORD variant: an IPv4 address record.
type DnsARecord struct {
	// IPv4Address is the IPv4 address the domain resolves to.
	// +kubebuilder:validation:MinLength=1
	IPv4Address string `json:"ipv4Address"`

	// TTLSeconds is the record time to live in seconds (0..86400).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=86400
	TTLSeconds int32 `json:"ttlSeconds"`
}

// DnsAaaaRecord is the AAAA_RECORD variant: an IPv6 address record.
type DnsAaaaRecord struct {
	// IPv6Address is the IPv6 address the domain resolves to.
	// +kubebuilder:validation:MinLength=1
	IPv6Address string `json:"ipv6Address"`

	// TTLSeconds is the record time to live in seconds (0..86400).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=86400
	TTLSeconds int32 `json:"ttlSeconds"`
}

// DnsCnameRecord is the CNAME_RECORD variant: an alias record.
type DnsCnameRecord struct {
	// TargetDomain is the canonical name the domain aliases.
	// +kubebuilder:validation:MinLength=1
	TargetDomain string `json:"targetDomain"`

	// TTLSeconds is the record time to live in seconds (0..604800).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=604800
	TTLSeconds int32 `json:"ttlSeconds"`
}

// DnsMxRecord is the MX_RECORD variant: a mail-exchange record.
type DnsMxRecord struct {
	// MailServerDomain is the mail server the domain routes mail to.
	// +kubebuilder:validation:MinLength=1
	MailServerDomain string `json:"mailServerDomain"`

	// Priority ranks this mail server; a lower number is preferred
	// (0..65535).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Priority int32 `json:"priority"`
}

// DnsSrvRecord is the SRV_RECORD variant: a service-location record.
type DnsSrvRecord struct {
	// Port is the service port (0..65535).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port"`

	// Priority ranks this record; a lower number is preferred (0..65535).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Priority int32 `json:"priority"`

	// Protocol is the service protocol (for example _tcp).
	// +kubebuilder:validation:MinLength=1
	Protocol string `json:"protocol"`

	// ServerDomain is the domain of the server running the service.
	// +kubebuilder:validation:MinLength=1
	ServerDomain string `json:"serverDomain"`

	// Service is the service name (for example _ldap).
	// +kubebuilder:validation:MinLength=1
	Service string `json:"service"`

	// Weight is the relative weight for records with the same priority
	// (0..65535).
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=65535
	Weight int32 `json:"weight"`
}

// DnsTxtRecord is the TXT_RECORD variant: a free-text record.
type DnsTxtRecord struct {
	// Text is the text value.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=1024
	Text string `json:"text"`
}

// DnsForwardDomain is the FORWARD_DOMAIN variant: a forwarding policy.
type DnsForwardDomain struct {
	// IPAddress is the DNS server the query is forwarded to.
	// +kubebuilder:validation:MinLength=1
	IPAddress string `json:"ipAddress"`
}

// +kubebuilder:validation:XValidation:rule="self.type == 'A_RECORD' ? has(self.aRecord) : !has(self.aRecord)",message="aRecord fields require type A_RECORD"
// +kubebuilder:validation:XValidation:rule="self.type == 'AAAA_RECORD' ? has(self.aaaaRecord) : !has(self.aaaaRecord)",message="aaaaRecord fields require type AAAA_RECORD"
// +kubebuilder:validation:XValidation:rule="self.type == 'CNAME_RECORD' ? has(self.cnameRecord) : !has(self.cnameRecord)",message="cnameRecord fields require type CNAME_RECORD"
// +kubebuilder:validation:XValidation:rule="self.type == 'MX_RECORD' ? has(self.mxRecord) : !has(self.mxRecord)",message="mxRecord fields require type MX_RECORD"
// +kubebuilder:validation:XValidation:rule="self.type == 'SRV_RECORD' ? has(self.srvRecord) : !has(self.srvRecord)",message="srvRecord fields require type SRV_RECORD"
// +kubebuilder:validation:XValidation:rule="self.type == 'TXT_RECORD' ? has(self.txtRecord) : !has(self.txtRecord)",message="txtRecord fields require type TXT_RECORD"
// +kubebuilder:validation:XValidation:rule="self.type == 'FORWARD_DOMAIN' ? has(self.forwardDomain) : !has(self.forwardDomain)",message="forwardDomain fields require type FORWARD_DOMAIN"

// UnifiDnsPolicySpec defines the desired state of UnifiDnsPolicy. It mirrors the
// Integration v1 DNS-policy union (spec v10.4.57): Type selects exactly one
// variant sub-object, and variant-specific fields are only accepted for the
// matching type (CEL). The spec never contains a UniFi internal identifier; the
// resolved upstream record UUID belongs in status only.
type UnifiDnsPolicySpec struct {
	// SiteRef references the UnifiSite that owns this policy. The reference
	// resolves in the same namespace.
	SiteRef CoreRef `json:"siteRef"`

	// Type is the DNS-policy union discriminator.
	// +kubebuilder:validation:Enum=A_RECORD;AAAA_RECORD;CNAME_RECORD;MX_RECORD;SRV_RECORD;TXT_RECORD;FORWARD_DOMAIN
	Type string `json:"type"`

	// Domain is the domain the record applies to.
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=127
	Domain string `json:"domain"`

	// Enabled controls whether the record is active.
	// +kubebuilder:default=true
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// ARecord holds the A_RECORD variant fields. Required for type A_RECORD and
	// forbidden otherwise.
	// +optional
	ARecord *DnsARecord `json:"aRecord,omitempty"`

	// AaaaRecord holds the AAAA_RECORD variant fields. Required for type
	// AAAA_RECORD and forbidden otherwise.
	// +optional
	AaaaRecord *DnsAaaaRecord `json:"aaaaRecord,omitempty"`

	// CnameRecord holds the CNAME_RECORD variant fields. Required for type
	// CNAME_RECORD and forbidden otherwise.
	// +optional
	CnameRecord *DnsCnameRecord `json:"cnameRecord,omitempty"`

	// MxRecord holds the MX_RECORD variant fields. Required for type MX_RECORD
	// and forbidden otherwise.
	// +optional
	MxRecord *DnsMxRecord `json:"mxRecord,omitempty"`

	// SrvRecord holds the SRV_RECORD variant fields. Required for type
	// SRV_RECORD and forbidden otherwise.
	// +optional
	SrvRecord *DnsSrvRecord `json:"srvRecord,omitempty"`

	// TxtRecord holds the TXT_RECORD variant fields. Required for type
	// TXT_RECORD and forbidden otherwise.
	// +optional
	TxtRecord *DnsTxtRecord `json:"txtRecord,omitempty"`

	// ForwardDomain holds the FORWARD_DOMAIN variant fields. Required for type
	// FORWARD_DOMAIN and forbidden otherwise.
	// +optional
	ForwardDomain *DnsForwardDomain `json:"forwardDomain,omitempty"`
}

// UnifiDnsPolicyStatus defines the observed state of UnifiDnsPolicy.
type UnifiDnsPolicyStatus struct {
	// observedGeneration is the metadata.generation the controller last
	// reconciled.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions represent the current state of the UnifiDnsPolicy resource.
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

	// policyID is the UUID of the upstream DNS policy record. It is recorded in
	// status only, never in spec.
	// +optional
	PolicyID string `json:"policyID,omitempty"`

	// origin is the observed upstream record origin. Only user-defined records
	// are written; any other origin is adopted read-only and reported here.
	// +optional
	Origin string `json:"origin,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Domain",type=string,JSONPath=`.spec.domain`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// UnifiDnsPolicy is the Schema for the unifidnspolicies API
type UnifiDnsPolicy struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of UnifiDnsPolicy
	// +required
	Spec UnifiDnsPolicySpec `json:"spec"`

	// status defines the observed state of UnifiDnsPolicy
	// +optional
	Status UnifiDnsPolicyStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// UnifiDnsPolicyList contains a list of UnifiDnsPolicy
type UnifiDnsPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UnifiDnsPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UnifiDnsPolicy{}, &UnifiDnsPolicyList{})
}
