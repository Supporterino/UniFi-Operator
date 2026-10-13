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
	"slices"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

// TestOrderingClaimants verifies the shared primitive's duplicate-scope
// detection: only objects sharing the scope key are claimants.
func TestOrderingClaimants(t *testing.T) {
	t.Parallel()

	scope := func(obj client.Object) string {
		ordering, ok := obj.(*unifiv1alpha1.UnifiFirewallPolicyOrdering)
		if !ok {
			return ""
		}
		return ordering.Spec.SiteRef.Name + "|" + firewallZoneSelectorLabel(ordering.Spec.Source) + "|" +
			firewallZoneSelectorLabel(ordering.Spec.Destination)
	}

	first := newFirewallPolicyOrdering()
	peer := newFirewallPolicyOrdering()
	peer.Name = testPeerName
	other := newFirewallPolicyOrdering()
	other.Name = testOtherNamespace
	other.Spec.Source = unifiv1alpha1.FirewallZoneSelector{ZoneRef: &unifiv1alpha1.CoreRef{Name: "other-zone"}}

	objs := []client.Object{first, peer, other}

	claimants := OrderingClaimants(objs, first, scope)
	if want := []string{testOrderingResourceName, testPeerName}; !slices.Equal(claimants, want) {
		t.Errorf("claimants = %v, want %v", claimants, want)
	}

	sole := OrderingClaimants(objs, other, scope)
	if want := []string{testOtherNamespace}; !slices.Equal(sole, want) {
		t.Errorf("claimants = %v, want %v", sole, want)
	}
}

// TestOrderingDrift verifies drift is the set of user-defined upstream entries
// absent from the ordering object, with system entries anchored (excluded).
func TestOrderingDrift(t *testing.T) {
	t.Parallel()

	ordered := OrderingOrderedSet([]string{"a"}, []string{"b"})
	upstream := []OrderingEntry{
		{ID: "a", Name: "A"},
		{ID: "b", Name: "B"},
		{ID: "c", Name: "C"},
		{ID: "s", Name: "SYS", System: true},
	}

	drift := OrderingDrift(ordered, upstream)
	if want := []string{"C"}; !slices.Equal(drift, want) {
		t.Errorf("drift = %v, want %v (system entries are anchored)", drift, want)
	}

	if len(OrderingDrift(OrderingOrderedSet([]string{"a", "b", "c"}, nil), upstream)) != 0 {
		t.Errorf("drift = non-empty, want none when every user entry is ordered")
	}
}

// TestOrderingIDsResolvesAndReportsMissing verifies references resolve to
// upstream IDs in order, with unresolved names reported (not silently dropped).
func TestOrderingIDsResolvesAndReportsMissing(t *testing.T) {
	t.Parallel()

	resolveTable := map[string]string{"a": "A", "b": "B"}
	resolve := func(name string) (string, bool) {
		id, ok := resolveTable[name]
		return id, ok
	}

	ids, missing := OrderingIDs([]unifiv1alpha1.CoreRef{{Name: "a"}, {Name: testMissingName}, {Name: "b"}}, resolve)
	if want := []string{"A", "B"}; !slices.Equal(ids, want) {
		t.Errorf("ids = %v, want %v", ids, want)
	}
	if want := []string{testMissingName}; !slices.Equal(missing, want) {
		t.Errorf("missing = %v, want %v", missing, want)
	}
}
