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
	"context"
	"testing"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// TestResolveZoneSelectorConfigurable verifies a zoneRef resolves through the
// referenced UnifiFirewallZone's status.zoneID.
func TestResolveZoneSelectorConfigurable(t *testing.T) {
	t.Parallel()

	policy := newFirewallPolicy()
	fakeClient, _, _ := newFirewallPolicyFakeClient(t, ownedFirewallZone())

	res := resolveZoneSelector(context.Background(), fakeClient, policy.Namespace, policy.Spec.SiteRef.Name,
		policy.Spec.Source.ZoneRef, nil, nil)
	if res.stopped() {
		t.Fatalf("resolveZoneSelector = %+v, want a resolved value", res)
	}
	if res.value != testZoneResourceUUID {
		t.Errorf("zone ID = %q, want %q", res.value, testZoneResourceUUID)
	}
}

// TestResolveZoneSelectorBuiltIn verifies a built-in selector resolves to the
// system-defined zone of that name in the read-only zone list.
func TestResolveZoneSelectorBuiltIn(t *testing.T) {
	t.Parallel()

	res := resolveZoneSelector(context.Background(), nil, testNamespace, testSiteResourceName,
		nil, &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}, []unifi.FirewallZone{systemUpstreamZone()})
	if res.stopped() {
		t.Fatalf("resolveZoneSelector = %+v, want a resolved value", res)
	}
	if res.value != testSystemZoneUUID {
		t.Errorf("zone ID = %q, want the system zone %q", res.value, testSystemZoneUUID)
	}
}

// TestResolveZoneSelectorBuiltInNotFoundFailsClosed verifies an unknown built-in
// zone fails closed with a requeue.
func TestResolveZoneSelectorBuiltInNotFoundFailsClosed(t *testing.T) {
	t.Parallel()

	res := resolveZoneSelector(context.Background(), nil, testNamespace, testSiteResourceName,
		nil, &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: "Nope"}, []unifi.FirewallZone{systemUpstreamZone()})
	if !res.stopped() || res.reason != reasonBuiltInZoneNotFound || !res.retry {
		t.Fatalf("resolveZoneSelector = %+v, want %s with retry", res, reasonBuiltInZoneNotFound)
	}
}

// TestResolveZoneSelectorUserDefinedNameFailsClosed verifies a built-in selector
// that names a user-defined zone fails closed terminally rather than silently
// targeting a configurable zone.
func TestResolveZoneSelectorUserDefinedNameFailsClosed(t *testing.T) {
	t.Parallel()

	userDefined := unifi.FirewallZone{
		ID: "zone-uuid-user", Name: testBuiltInZoneName,
		Metadata: unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined},
	}
	res := resolveZoneSelector(context.Background(), nil, testNamespace, testSiteResourceName,
		nil, &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}, []unifi.FirewallZone{userDefined})
	if !res.stopped() || res.reason != reasonInvalidSpec || res.retry {
		t.Fatalf("resolveZoneSelector = %+v, want a terminal %s", res, reasonInvalidSpec)
	}
}

// TestResolveTrafficFilterResolvesList verifies a filter resolves to the
// referenced UnifiTrafficMatchingList's upstream UUID.
func TestResolveTrafficFilterResolvesList(t *testing.T) {
	t.Parallel()

	filter := &unifiv1alpha1.FirewallTrafficFilter{
		Type:      testEndpointIPAddress,
		IPAddress: &unifiv1alpha1.FirewallTrafficMatchingListFilter{ListRef: unifiv1alpha1.CoreRef{Name: testFilterListResourceName}},
	}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t, ipFilterList())

	res := resolveTrafficFilter(context.Background(), fakeClient, testNamespace, testSiteResourceName, filter)
	if res.stopped() {
		t.Fatalf("resolveTrafficFilter = %+v, want a resolved value", res)
	}
	if res.value != testTrafficListUpstreamID {
		t.Errorf("filter list ID = %q, want %q", res.value, testTrafficListUpstreamID)
	}
}

// TestResolveTrafficFilterUnresolvableFailsClosed verifies an unresolvable
// filter reference fails closed with a requeue.
func TestResolveTrafficFilterUnresolvableFailsClosed(t *testing.T) {
	t.Parallel()

	filter := &unifiv1alpha1.FirewallTrafficFilter{
		Type:      testEndpointIPAddress,
		IPAddress: &unifiv1alpha1.FirewallTrafficMatchingListFilter{ListRef: unifiv1alpha1.CoreRef{Name: "missing-list"}},
	}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t)

	res := resolveTrafficFilter(context.Background(), fakeClient, testNamespace, testSiteResourceName, filter)
	if !res.stopped() || res.reason != reasonFilterRefNotFound || !res.retry {
		t.Fatalf("resolveTrafficFilter = %+v, want %s with retry", res, reasonFilterRefNotFound)
	}
}

// TestResolveTrafficFilterTypeMismatchFailsClosed verifies a filter whose list
// type does not match the filter dimension fails closed terminally.
func TestResolveTrafficFilterTypeMismatchFailsClosed(t *testing.T) {
	t.Parallel()

	filter := &unifiv1alpha1.FirewallTrafficFilter{
		Type: "PORT",
		Port: &unifiv1alpha1.FirewallTrafficMatchingListFilter{ListRef: unifiv1alpha1.CoreRef{Name: testFilterListResourceName}},
	}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t, ipFilterList())

	res := resolveTrafficFilter(context.Background(), fakeClient, testNamespace, testSiteResourceName, filter)
	if !res.stopped() || res.reason != reasonInvalidSpec || res.retry {
		t.Fatalf("resolveTrafficFilter = %+v, want a terminal %s", res, reasonInvalidSpec)
	}
}
