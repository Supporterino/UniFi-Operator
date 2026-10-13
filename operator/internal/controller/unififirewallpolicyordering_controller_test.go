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
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	testOrderingResourceName = "firewall-ordering"
	testBeforePolicyName     = "before-policy"
	testAfterPolicyName      = "after-policy"
	testBeforePolicyUUID     = "policy-uuid-before"
	testAfterPolicyUUID      = "policy-uuid-after"
	testDriftPolicyUUID      = "policy-uuid-drift"
	testDriftPolicyName      = "drifter"
	testAnchoredPolicyUUID   = "policy-uuid-anchored"
)

// newFirewallPolicyOrdering returns a minimal UnifiFirewallPolicyOrdering
// fixture with a UID (needed for the ownerReference).
func newFirewallPolicyOrdering() *unifiv1alpha1.UnifiFirewallPolicyOrdering {
	return &unifiv1alpha1.UnifiFirewallPolicyOrdering{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testOrderingResourceName,
			Namespace: testNamespace,
			UID:       types.UID("ordering-uid"),
		},
		Spec: unifiv1alpha1.UnifiFirewallPolicyOrderingSpec{
			SiteRef:             unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Source:              unifiv1alpha1.FirewallZoneSelector{ZoneRef: &unifiv1alpha1.CoreRef{Name: testFirewallZoneResourceName}},
			Destination:         unifiv1alpha1.FirewallZoneSelector{BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}},
			BeforeSystemDefined: []unifiv1alpha1.CoreRef{{Name: testBeforePolicyName}},
			AfterSystemDefined:  []unifiv1alpha1.CoreRef{{Name: testAfterPolicyName}},
		},
	}
}

// orderedPolicy returns a UnifiFirewallPolicy fixture that has reported its
// upstream policy UUID and belongs to the test site.
func orderedPolicy(name, upstreamID string) *unifiv1alpha1.UnifiFirewallPolicy {
	policy := newFirewallPolicy()
	policy.Name = name
	policy.UID = types.UID(name + "-uid")
	policy.Status.PolicyID = upstreamID
	return policy
}

// upstreamOrderingPolicies returns the upstream policies for the ordering's zone
// pair: the two referenced user policies, an extra user policy (drift), and a
// system-defined policy (anchored, not drift).
func upstreamOrderingPolicies() []unifi.FirewallPolicy {
	entry := func(id, name, origin string) unifi.FirewallPolicy {
		return unifi.FirewallPolicy{
			ID:       id,
			Metadata: unifi.NetworkMetadata{Origin: origin},
			FirewallPolicyRequest: unifi.FirewallPolicyRequest{
				Name:        name,
				Source:      unifi.FirewallPolicyZone{ZoneID: testZoneResourceUUID},
				Destination: unifi.FirewallPolicyZone{ZoneID: testSystemZoneUUID},
			},
		}
	}
	return []unifi.FirewallPolicy{
		entry(testBeforePolicyUUID, testBeforePolicyName, firewallPolicyOriginUserDefined),
		entry(testAfterPolicyUUID, testAfterPolicyName, firewallPolicyOriginUserDefined),
		entry(testDriftPolicyUUID, testDriftPolicyName, firewallPolicyOriginUserDefined),
		entry(testAnchoredPolicyUUID, "anchored", testSystemOrigin),
	}
}

// newOrderingFakeClient returns a fake client that counts writes to the ordering
// status subresource and to object metadata, so idempotency can be asserted.
func newOrderingFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiFirewallPolicyOrdering{},
		).
		WithInterceptorFuncs(interceptor.Funcs{
			Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
				metadataWrites++
				return cl.Update(ctx, obj, opts...)
			},
			SubResourceUpdate: func(
				ctx context.Context,
				cl client.Client,
				subResourceName string,
				obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				if subResourceName == statusSubresource {
					statusWrites++
				}
				return cl.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		})
	return builder.Build(), &statusWrites, &metadataWrites
}

// getOrdering loads a reconciled ordering fixture by name.
func getOrdering(t *testing.T, c client.Client, name string) *unifiv1alpha1.UnifiFirewallPolicyOrdering {
	t.Helper()
	got := &unifiv1alpha1.UnifiFirewallPolicyOrdering{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get firewall policy ordering after reconcile: %v", err)
	}
	return got
}

func TestUnifiFirewallPolicyOrderingReconcileApplies(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		zones:            []unifi.FirewallZone{systemUpstreamZone()},
		firewallPolicies: upstreamOrderingPolicies(),
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newOrderingFakeClient(t,
		newFirewallPolicyOrdering(),
		orderedPolicy(testBeforePolicyName, testBeforePolicyUUID),
		orderedPolicy(testAfterPolicyName, testAfterPolicyUUID),
		ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testOrderingResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getOrdering(t, fakeClient, testOrderingResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.SourceZoneID != testZoneResourceUUID || got.Status.DestinationZoneID != testSystemZoneUUID {
		t.Errorf("zone IDs = %q/%q, want %q/%q",
			got.Status.SourceZoneID, got.Status.DestinationZoneID, testZoneResourceUUID, testSystemZoneUUID)
	}
	if !slices.Equal(got.Status.Drift, []string{testDriftPolicyName}) {
		t.Errorf("drift = %v, want [%s] (system policies are anchored)", got.Status.Drift, testDriftPolicyName)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("firewall policy ordering is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiFirewallPolicyOrderingFinalizer) {
		t.Errorf("firewall policy ordering finalizer was not added")
	}

	if len(stub.firewallPolicyOrderingUpdateReqs) != 1 {
		t.Fatalf("ordering update requests = %d, want 1", len(stub.firewallPolicyOrderingUpdateReqs))
	}
	req := stub.firewallPolicyOrderingUpdateReqs[0]
	if !slices.Equal(req.OrderedFirewallPolicyIDs.BeforeSystemDefined, []string{testBeforePolicyUUID}) {
		t.Errorf("before = %v, want [%s]", req.OrderedFirewallPolicyIDs.BeforeSystemDefined, testBeforePolicyUUID)
	}
	if !slices.Equal(req.OrderedFirewallPolicyIDs.AfterSystemDefined, []string{testAfterPolicyUUID}) {
		t.Errorf("after = %v, want [%s]", req.OrderedFirewallPolicyIDs.AfterSystemDefined, testAfterPolicyUUID)
	}
	if stub.updatedFirewallOrderingSourceZoneID != testZoneResourceUUID ||
		stub.updatedFirewallOrderingDestinationZoneID != testSystemZoneUUID {
		t.Errorf("updated zone pair = %q/%q, want the resolved IDs",
			stub.updatedFirewallOrderingSourceZoneID, stub.updatedFirewallOrderingDestinationZoneID)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiFirewallPolicyOrderingReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		zones:            []unifi.FirewallZone{systemUpstreamZone()},
		firewallPolicies: upstreamOrderingPolicies(),
		firewallPolicyOrdering: unifi.FirewallPolicyOrdering{
			OrderedFirewallPolicyIDs: unifi.FirewallPolicyOrderingIDs{
				BeforeSystemDefined: []string{testBeforePolicyUUID},
				AfterSystemDefined:  []string{testAfterPolicyUUID},
			},
		},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newOrderingFakeClient(t,
		newFirewallPolicyOrdering(),
		orderedPolicy(testBeforePolicyName, testBeforePolicyUUID),
		orderedPolicy(testAfterPolicyName, testAfterPolicyUUID),
		ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testOrderingResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if len(stub.firewallPolicyOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none when the upstream order matches",
			len(stub.firewallPolicyOrderingUpdateReqs))
	}
}

func TestUnifiFirewallPolicyOrderingDuplicateConflict(t *testing.T) {
	t.Parallel()

	second := newFirewallPolicyOrdering()
	second.Name = "firewall-ordering-peer"
	second.UID = types.UID("ordering-peer-uid")

	stub := &stubUnifiClient{
		zones:            []unifi.FirewallZone{systemUpstreamZone()},
		firewallPolicies: upstreamOrderingPolicies(),
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newOrderingFakeClient(t,
		newFirewallPolicyOrdering(), second,
		orderedPolicy(testBeforePolicyName, testBeforePolicyUUID),
		orderedPolicy(testAfterPolicyName, testAfterPolicyUUID),
		ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for _, name := range []string{testOrderingResourceName, "firewall-ordering-peer"} {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(name)); err != nil {
			t.Fatalf("reconcile %s: %v", name, err)
		}
		cond := requireReady(t, getOrdering(t, fakeClient, name).Status.Conditions)
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonZonePairConflict {
			t.Errorf("ordering %s Ready = %s/%s, want False/%s", name, cond.Status, cond.Reason, reasonZonePairConflict)
		}
	}
	if len(stub.firewallPolicyOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none while the zone pair is contested",
			len(stub.firewallPolicyOrderingUpdateReqs))
	}
}

func TestUnifiFirewallPolicyOrderingUnresolvedPolicyFailsClosed(t *testing.T) {
	t.Parallel()

	ordering := newFirewallPolicyOrdering()
	ordering.Spec.BeforeSystemDefined = []unifiv1alpha1.CoreRef{{Name: "missing-policy"}}
	ordering.Spec.AfterSystemDefined = nil

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{systemUpstreamZone()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newOrderingFakeClient(t,
		ordering, ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testOrderingResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != firewallOrderingDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, firewallOrderingDependencyRequeueAfter)
	}

	cond := requireReady(t, getOrdering(t, fakeClient, testOrderingResourceName).Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonPolicyRefNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonPolicyRefNotFound)
	}
	if len(stub.firewallPolicyOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none when a referenced policy is unresolved",
			len(stub.firewallPolicyOrderingUpdateReqs))
	}
}

// TestUnifiFirewallPolicyOrderingZonePairMismatchFailsClosed verifies that an
// ordered policy belonging to a different source/destination zone pair fails
// closed rather than having its UUID written into this pair's order.
func TestUnifiFirewallPolicyOrderingZonePairMismatchFailsClosed(t *testing.T) {
	t.Parallel()

	policy := orderedPolicy(testBeforePolicyName, testBeforePolicyUUID)
	policy.Status.SourceZoneID = "other-source-zone"
	policy.Status.DestinationZoneID = "other-destination-zone"

	ordering := newFirewallPolicyOrdering()
	ordering.Spec.AfterSystemDefined = nil

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{systemUpstreamZone()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newOrderingFakeClient(t,
		ordering, policy, ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testOrderingResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != firewallOrderingDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, firewallOrderingDependencyRequeueAfter)
	}

	cond := requireReady(t, getOrdering(t, fakeClient, testOrderingResourceName).Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonFirewallPolicyZonePairMismatch {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonFirewallPolicyZonePairMismatch)
	}
	if len(stub.firewallPolicyOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none when a referenced policy is on another zone pair",
			len(stub.firewallPolicyOrderingUpdateReqs))
	}
}
