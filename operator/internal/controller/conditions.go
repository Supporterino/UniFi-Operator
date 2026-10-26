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

package controller

import (
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// conditionReady is the primary readiness signal shared by every kind.
const conditionReady = "Ready"

// Condition reasons. Reasons are stable, machine-readable identifiers; messages
// never contain credential material (docs/security.md).
const (
	// reasonReconciled marks a UnifiController that reached the console and
	// passed version gating.
	reasonReconciled = "Reconciled"
	// reasonSiteAdopted marks a UnifiSite that matched an upstream site.
	reasonSiteAdopted = "SiteAdopted"
	// reasonInvalidSpec marks a spec the controller cannot act on (bad URL or a
	// terminal 4xx from the console).
	reasonInvalidSpec = "InvalidSpec"
	// reasonZoneBasedFirewallNotConfigured marks a firewall reconcile the
	// console rejected because Zone-Based Firewall is not configured. The spec
	// is valid; the console's firewall subsystem is absent, so this is terminal
	// and distinct from InvalidSpec (design D5).
	reasonZoneBasedFirewallNotConfigured = "ZoneBasedFirewallNotConfigured"
	// reasonAuthenticationFailed marks a console that rejected the API key.
	reasonAuthenticationFailed = "AuthenticationFailed"
	// reasonCertificateError marks a TLS certificate verification failure.
	reasonCertificateError = "CertificateError"
	// reasonVersionUnsupported marks a console below a capability's minimum app
	// version.
	reasonVersionUnsupported = "VersionUnsupported"
	// reasonControllerNotFound marks a UnifiSite whose controllerRef does not
	// resolve.
	reasonControllerNotFound = "ControllerNotFound"
	// reasonSecretNotFound marks a missing credential Secret.
	reasonSecretNotFound = "SecretNotFound"
	// reasonSecretKeyMissing marks a Secret without the selected key.
	reasonSecretKeyMissing = "SecretKeyMissing"
	// reasonSiteNotFound marks a UnifiSite whose internalReference matches no
	// upstream site.
	reasonSiteNotFound = "SiteNotFound"
	// reasonSiteRefNotFound marks a UnifiNetwork whose spec.siteRef does not
	// resolve to a UnifiSite (absent, or being deleted).
	reasonSiteRefNotFound = "SiteRefNotFound"
	// reasonNetworkRefNotFound marks a UnifiWifiBroadcast whose spec.networkRef
	// does not resolve to a UnifiNetwork (absent, or being deleted).
	reasonNetworkRefNotFound = "NetworkRefNotFound"
	// reasonSiteNotAdopted marks a UnifiNetwork whose UnifiSite has not yet
	// adopted an upstream site (status.siteID is empty).
	reasonSiteNotAdopted = "SiteNotAdopted"
	// reasonDeviceTagNotFound marks a SWITCH-managed network whose device-tag
	// selector names a tag that does not exist on the site. The controller
	// fails closed and does not modify the upstream network (design D5).
	reasonDeviceTagNotFound = "DeviceTagNotFound"
	// reasonDeviceTagAmbiguous marks a SWITCH-managed network whose device-tag
	// selector resolves to zero or more than one device. The binding requires
	// exactly one device, so the controller fails closed (design D5).
	reasonDeviceTagAmbiguous = "DeviceTagAmbiguous"
	// reasonWANLookupUnsupported marks a network whose IPv6 prefix delegation
	// needs a WAN interface lookup the client does not yet provide.
	reasonWANLookupUnsupported = "WANLookupUnsupported"
	// reasonDependencyNotReady marks a resource whose dependency is present but
	// has not reported the status needed to proceed (for example a
	// UnifiController that has not yet recorded its application version). It is
	// retried after a bounded delay rather than treated as a terminal version
	// failure.
	reasonDependencyNotReady = "DependencyNotReady"
	// reasonSystemObjectReadOnly marks an upstream object whose metadata.origin
	// is not USER_DEFINED. The controller refuses to overwrite or delete a
	// system-managed object (design D11).
	reasonSystemObjectReadOnly = "SystemObjectReadOnly"
	// reasonMembershipConflict marks a UnifiFirewallZone that claims a network
	// another zone in the same site also claims. No membership is written until
	// a single writer remains (design D3).
	reasonMembershipConflict = "MembershipConflict"
	// reasonZoneNameConflict marks a UnifiFirewallZone whose spec.name matches
	// another zone in the same site. Upstream zone identity is by name, so
	// neither object creates or updates an upstream zone (design D3).
	reasonZoneNameConflict = "ZoneNameConflict"
	// reasonCrossSiteReference marks a UnifiFirewallZone that lists a
	// UnifiNetwork belonging to a different site. A network can only be a member
	// of a zone on its own site, so no membership is written (design D3).
	reasonCrossSiteReference = "CrossSiteReference"
	// reasonRadiusProfileRefNotFound marks a UnifiWifiBroadcast whose security
	// configuration references a UnifiRadiusProfile that does not resolve
	// (absent, or being deleted). The controller fails closed and does not
	// mutate the upstream broadcast.
	reasonRadiusProfileRefNotFound = "RadiusProfileRefNotFound"
	// reasonProfileNotFound marks a UnifiRadiusProfile whose spec.name matches
	// no upstream profile on the referenced site. The controller adopts only an
	// existing profile, so nothing is created or mutated (adoption kind).
	reasonProfileNotFound = "ProfileNotFound"
	// reasonZoneRefNotFound marks a UnifiFirewallPolicy whose source or
	// destination zoneRef does not resolve to a UnifiFirewallZone (absent, or
	// being deleted). No upstream policy is written.
	reasonZoneRefNotFound = "ZoneRefNotFound"
	// reasonBuiltInZoneNotFound marks a UnifiFirewallPolicy whose built-in-zone
	// selector names no system-defined upstream zone. No upstream policy is
	// written.
	reasonBuiltInZoneNotFound = "BuiltInZoneNotFound"
	// reasonFilterRefNotFound marks a UnifiFirewallPolicy whose traffic filter
	// references a UnifiTrafficMatchingList that does not resolve (absent, or
	// being deleted). No upstream policy is written.
	reasonFilterRefNotFound = "FilterRefNotFound"
	// reasonPolicyRefNotFound marks a UnifiFirewallPolicyOrdering whose ordered
	// policy reference does not resolve to a provisioned UnifiFirewallPolicy.
	// No order is applied.
	reasonPolicyRefNotFound = "PolicyRefNotFound"
	// reasonZonePairConflict marks a UnifiFirewallPolicyOrdering that shares its
	// source/destination zone pair with another ordering object in the same
	// site. No order is applied until a single unambiguous object remains
	// (design D2).
	reasonZonePairConflict = "ZonePairConflict"
	// reasonFirewallPolicyZonePairMismatch marks a UnifiFirewallPolicyOrdering
	// that lists a UnifiFirewallPolicy whose resolved source/destination zone
	// pair differs from the ordering's own pair. No order is applied.
	reasonFirewallPolicyZonePairMismatch = "FirewallPolicyZonePairMismatch"
	// reasonSiteOrderingConflict marks a UnifiAclRuleOrdering that shares its
	// site with another site-global ordering object. No order is applied until a
	// single unambiguous object remains (design D2).
	reasonSiteOrderingConflict = "SiteOrderingConflict"
	// reasonAclRuleRefNotFound marks a UnifiAclRuleOrdering whose ordered ACL
	// rule reference does not resolve to a provisioned UnifiAclRule. No order is
	// applied.
	reasonAclRuleRefNotFound = "AclRuleRefNotFound"
	// reasonLagSelectorNoMatch marks a UnifiSwitchingLag whose typed composite
	// selector matches no upstream LAG on the referenced site. The controller
	// adopts exactly one matching LAG, so nothing is mutated (adoption kind).
	reasonLagSelectorNoMatch = "LagSelectorNoMatch"
	// reasonLagSelectorAmbiguous marks a UnifiSwitchingLag whose typed composite
	// selector matches more than one upstream LAG. Exactly one is required, so
	// nothing is mutated (adoption kind).
	reasonLagSelectorAmbiguous = "LagSelectorAmbiguous"
	// reasonMcLagDomainNotFound marks a UnifiSwitchingMcLagDomain whose spec.name
	// matches no upstream MC-LAG domain on the referenced site (adoption kind).
	reasonMcLagDomainNotFound = "McLagDomainNotFound"
	// reasonSwitchStackNotFound marks a UnifiSwitchingSwitchStack whose spec.name
	// matches no upstream switch stack on the referenced site (adoption kind).
	reasonSwitchStackNotFound = "SwitchStackNotFound"
	// reasonVpnServerNotFound marks a UnifiVpnServer whose spec.name matches no
	// upstream VPN server on the referenced site (adoption kind).
	reasonVpnServerNotFound = "VpnServerNotFound"
	// reasonTunnelNotFound marks a UnifiSiteToSiteTunnel whose spec.name matches
	// no upstream tunnel on the referenced site (adoption kind).
	reasonTunnelNotFound = "TunnelNotFound"
)

// maxStatusMessageLen caps how much controller-provided text is copied into a
// status condition message and summary. Condition messages have an API-server
// maximum length; clamping also stops an oversized body from re-entering the
// retry loop.
const maxStatusMessageLen = 1024

// setReadyCondition sets the Ready condition on conditions, returning true when
// it changed. It is shared by the reconcilers so readiness is reported
// identically on every terminal path.
func setReadyCondition(conditions *[]metav1.Condition, generation int64, ready bool, reason, message string) bool {
	status := metav1.ConditionFalse
	if ready {
		status = metav1.ConditionTrue
	}
	return apimeta.SetStatusCondition(conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: generation,
	})
}

// clampMessage bounds a status message to maxStatusMessageLen runes so
// controller-provided text cannot exceed the CRD's condition message limit. The
// ellipsis is included in the bound: a truncated result is maxStatusMessageLen
// runes or fewer. The UniFi client never puts credentials in error text.
func clampMessage(message string) string {
	const suffix = "…"
	runes := []rune(message)
	if len(runes) <= maxStatusMessageLen {
		return message
	}
	return string(runes[:maxStatusMessageLen-1]) + suffix
}
