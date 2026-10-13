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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// selectorResolution is the outcome of resolving a typed selector against the
// Kubernetes API and the read-only upstream lists. It is shared by the firewall
// policy and ordering reconcilers, which map a resolved reason onto their own
// status. A non-empty reason means the reconcile must fail closed: retry
// controls whether it is retried after a bounded delay. A non-nil err is a
// transient API-server failure.
type selectorResolution struct {
	value   string
	reason  string
	message string
	retry   bool
	err     error
}

// stopped reports whether the resolution failed and the caller must stop.
func (s selectorResolution) stopped() bool { return s.reason != "" || s.err != nil }

// resolveZoneSelector resolves a zone selector to an upstream zone UUID. A
// configurable zone reference resolves through the UnifiFirewallZone status; a
// built-in selector resolves by name against the read-only zone list and must
// name a system-defined zone.
func resolveZoneSelector(
	ctx context.Context,
	reader client.Reader,
	namespace, siteName string,
	zoneRef *unifiv1alpha1.CoreRef,
	builtIn *unifiv1alpha1.BuiltInFirewallZoneSelector,
	zones []unifi.FirewallZone,
) selectorResolution {
	switch {
	case zoneRef != nil:
		zone := &unifiv1alpha1.UnifiFirewallZone{}
		key := types.NamespacedName{Namespace: namespace, Name: zoneRef.Name}
		if err := reader.Get(ctx, key, zone); err != nil {
			if apierrors.IsNotFound(err) {
				return selectorResolution{reason: reasonZoneRefNotFound, message: fmt.Sprintf("UnifiFirewallZone %q not found", zoneRef.Name), retry: true}
			}
			return selectorResolution{err: fmt.Errorf("get UnifiFirewallZone %q: %w", zoneRef.Name, err)}
		}
		if !zone.DeletionTimestamp.IsZero() {
			return selectorResolution{reason: reasonZoneRefNotFound, message: fmt.Sprintf("UnifiFirewallZone %q is being deleted", zone.Name), retry: true}
		}
		if zone.Spec.SiteRef.Name != siteName {
			return selectorResolution{reason: reasonCrossSiteReference, message: fmt.Sprintf("UnifiFirewallZone %q belongs to UnifiSite %q, not %q; no upstream object was modified",
				zoneRef.Name, zone.Spec.SiteRef.Name, siteName)}
		}
		if zone.Status.ZoneID == "" {
			return selectorResolution{reason: reasonDependencyNotReady, message: fmt.Sprintf("UnifiFirewallZone %q has not reported its upstream zone ID yet", zoneRef.Name), retry: true}
		}
		return selectorResolution{value: zone.Status.ZoneID}
	case builtIn != nil:
		if zone := findZoneByName(zones, builtIn.Name); zone != nil && zone.Metadata.Origin == firewallZoneOriginUserDefined {
			return selectorResolution{reason: reasonInvalidSpec, message: fmt.Sprintf("firewall zone %q is user-defined; reference it with zoneRef, not builtInZone", builtIn.Name)}
		}
		zone := findSystemZoneByName(zones, builtIn.Name)
		if zone == nil {
			return selectorResolution{reason: reasonBuiltInZoneNotFound, message: fmt.Sprintf("no system-defined firewall zone named %q; no upstream object was modified", builtIn.Name), retry: true}
		}
		return selectorResolution{value: zone.ID}
	default:
		return selectorResolution{reason: reasonInvalidSpec, message: "a zone selector is required"}
	}
}

// resolveTrafficFilter resolves a filter's UnifiTrafficMatchingList reference to
// the list's upstream UUID. A nil filter yields no value and no failure. A
// reference that does not resolve, is not yet provisioned, or whose list type
// does not match the filter fails closed.
func resolveTrafficFilter(
	ctx context.Context,
	reader client.Reader,
	namespace, siteName string,
	filter *unifiv1alpha1.FirewallTrafficFilter,
) selectorResolution {
	if filter == nil {
		return selectorResolution{}
	}
	ref, wantPorts := filterListRef(filter)
	if ref == nil {
		return selectorResolution{reason: reasonInvalidSpec, message: "traffic filter is missing its variant value"}
	}

	list := &unifiv1alpha1.UnifiTrafficMatchingList{}
	key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
	if err := reader.Get(ctx, key, list); err != nil {
		if apierrors.IsNotFound(err) {
			return selectorResolution{reason: reasonFilterRefNotFound, message: fmt.Sprintf("UnifiTrafficMatchingList %q not found", ref.Name), retry: true}
		}
		return selectorResolution{err: fmt.Errorf("get UnifiTrafficMatchingList %q: %w", ref.Name, err)}
	}
	if !list.DeletionTimestamp.IsZero() {
		return selectorResolution{reason: reasonFilterRefNotFound, message: fmt.Sprintf("UnifiTrafficMatchingList %q is being deleted", list.Name), retry: true}
	}
	if list.Spec.SiteRef.Name != siteName {
		return selectorResolution{reason: reasonCrossSiteReference, message: fmt.Sprintf("UnifiTrafficMatchingList %q belongs to UnifiSite %q, not %q; no upstream object was modified",
			ref.Name, list.Spec.SiteRef.Name, siteName)}
	}
	if wantPorts != (list.Spec.Type == unifiv1alpha1.TrafficMatchingListTypePorts) {
		want := "an IP-address list"
		if wantPorts {
			want = "a PORTS list"
		}
		return selectorResolution{reason: reasonInvalidSpec, message: fmt.Sprintf("traffic filter references UnifiTrafficMatchingList %q of type %s; %s is required",
			ref.Name, list.Spec.Type, want)}
	}
	if list.Status.ListID == "" {
		return selectorResolution{reason: reasonDependencyNotReady, message: fmt.Sprintf("UnifiTrafficMatchingList %q has not reported its upstream list ID yet", ref.Name), retry: true}
	}
	return selectorResolution{value: list.Status.ListID}
}

// findSystemZoneByName returns the system-defined (non USER_DEFINED) upstream
// zone matching name, or nil.
func findSystemZoneByName(zones []unifi.FirewallZone, name string) *unifi.FirewallZone {
	for i := range zones {
		if zones[i].Name == name && zones[i].Metadata.Origin != firewallZoneOriginUserDefined {
			return &zones[i]
		}
	}
	return nil
}

// filterListRef returns the filter's referenced list and whether the filter is a
// port filter (true) or an IP-address filter (false).
func filterListRef(filter *unifiv1alpha1.FirewallTrafficFilter) (*unifiv1alpha1.CoreRef, bool) {
	switch {
	case filter.Port != nil:
		return &filter.Port.ListRef, true
	case filter.IPAddress != nil:
		return &filter.IPAddress.ListRef, false
	}
	return nil, false
}
