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
	"sort"

	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
)

// This file holds the shared ordering primitive reused by every ordering kind
// (firewall policy ordering, per source/destination zone pair; ACL rule
// ordering, site-global). The kind-specific reconciler supplies only a scope
// callback; the conflict detection, drift reporting, and system anchoring are
// shared (design D2).

// OrderingScopeKey maps an ordering object to a stable identity for the scope it
// is authoritative for. Two ordering objects with the same scope key are
// duplicates and both fail closed, because CRD markers cannot express
// cross-object uniqueness (design D2). The kind-specific reconciler supplies the
// callback: a source/destination zone pair for firewall policies, the site for
// ACL rules.
type OrderingScopeKey func(client.Object) string

// OrderingClaimants returns the Kubernetes names of every object in all whose
// scope key equals current's, sorted. More than one claimant is a duplicate
// conflict. current is included when it is present in all.
func OrderingClaimants(all []client.Object, current client.Object, scopeKey OrderingScopeKey) []string {
	key := scopeKey(current)
	claimants := make([]string, 0, len(all))
	for _, other := range all {
		if scopeKey(other) == key {
			claimants = append(claimants, other.GetName())
		}
	}
	sort.Strings(claimants)
	return claimants
}

// OrderingEntry is an upstream object that participates in ordering (a firewall
// policy or an ACL rule). System entries are anchored and are never reordered.
type OrderingEntry struct {
	// ID is the upstream UUID.
	ID string
	// Name is the human-readable name, used in drift reporting.
	Name string
	// System reports whether the upstream object is system-defined. A system
	// entry is anchored: it is never reordered and never reported as drift.
	System bool
}

// OrderingDrift returns the names of the user-defined upstream entries that are
// not present in ordered (the set of upstream IDs referenced by the ordering
// object), sorted. System entries are anchored and excluded, so a system object
// absent from the ordering object is not drift (design D2).
func OrderingDrift(ordered map[string]struct{}, upstream []OrderingEntry) []string {
	var drift []string
	for _, entry := range upstream {
		if entry.System {
			continue
		}
		if _, ok := ordered[entry.ID]; !ok {
			drift = append(drift, entry.Name)
		}
	}
	sort.Strings(drift)
	return drift
}

// OrderingIDs resolves the ordering object's ordered references to upstream
// UUIDs, preserving order. It returns the IDs and, sorted, the names of any
// references that did not resolve, so the caller can fail closed rather than
// apply a partial order.
func OrderingIDs(refs []unifiv1alpha1.CoreRef, resolve func(string) (string, bool)) (ids []string, missing []string) {
	ids = make([]string, 0, len(refs))
	for _, ref := range refs {
		id, ok := resolve(ref.Name)
		if !ok {
			missing = append(missing, ref.Name)
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(missing)
	return ids, missing
}

// OrderingOrderedSet returns the set of upstream IDs referenced by an ordering
// object's before/after lists.
func OrderingOrderedSet(before, after []string) map[string]struct{} {
	ordered := make(map[string]struct{}, len(before)+len(after))
	for _, id := range before {
		ordered[id] = struct{}{}
	}
	for _, id := range after {
		ordered[id] = struct{}{}
	}
	return ordered
}
