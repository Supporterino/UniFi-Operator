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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
	testFirewallPolicyResourceName = "firewall-policy"
	testFirewallPolicyUpstreamName = "Allow LAN to WAN"
	testFirewallPolicyUUID         = "policy-uuid-9"
	testBuiltInZoneName            = "External"
	testSystemZoneUUID             = "zone-uuid-system"
	testTrafficListUpstreamID      = "list-uuid-filter-1"
	testFilterListResourceName     = "filter-list"
)

// newFirewallPolicy returns a minimal UnifiFirewallPolicy fixture with a UID
// (needed for the ownerReference). Its source is a configurable zone reference
// and its destination is a built-in system zone.
func newFirewallPolicy() *unifiv1alpha1.UnifiFirewallPolicy {
	return &unifiv1alpha1.UnifiFirewallPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFirewallPolicyResourceName,
			Namespace: testNamespace,
			UID:       types.UID("firewall-policy-uid"),
		},
		Spec: unifiv1alpha1.UnifiFirewallPolicySpec{
			SiteRef:         unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Name:            testFirewallPolicyUpstreamName,
			Action:          unifiv1alpha1.FirewallPolicyAction{Type: testActionAllow, AllowReturnTraffic: boolPtr(true)},
			IPProtocolScope: unifiv1alpha1.FirewallIPProtocolScope{IPVersion: testIPVersionDual},
			Source:          unifiv1alpha1.FirewallPolicyEndpoint{ZoneRef: &unifiv1alpha1.CoreRef{Name: testFirewallZoneResourceName}},
			Destination:     unifiv1alpha1.FirewallPolicyEndpoint{BuiltInZone: &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: testBuiltInZoneName}},
		},
	}
}

// ownedFirewallZone returns a UnifiFirewallZone fixture that has reported its
// upstream zone UUID, so it can be a policy zone reference.
func ownedFirewallZone() *unifiv1alpha1.UnifiFirewallZone {
	zone := newFirewallZone()
	zone.Status.ZoneID = testZoneResourceUUID
	return zone
}

// systemUpstreamZone returns the system-defined upstream zone the built-in
// selector testBuiltInZoneName resolves to.
func systemUpstreamZone() unifi.FirewallZone {
	return unifi.FirewallZone{
		ID:       testSystemZoneUUID,
		Name:     testBuiltInZoneName,
		Metadata: unifi.NetworkMetadata{Origin: testSystemOrigin},
	}
}

// matchingFirewallPolicy returns the upstream policy that equals what
// newFirewallPolicy desires, as the console would report it.
func matchingFirewallPolicy() unifi.FirewallPolicy {
	return unifi.FirewallPolicy{
		ID:       testFirewallPolicyUUID,
		Metadata: unifi.NetworkMetadata{Origin: firewallPolicyOriginUserDefined},
		FirewallPolicyRequest: unifi.FirewallPolicyRequest{
			Name:            testFirewallPolicyUpstreamName,
			Enabled:         true,
			LoggingEnabled:  false,
			Action:          unifi.FirewallPolicyAction{Type: testActionAllow, AllowReturnTraffic: boolPtr(true)},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: testIPVersionDual},
			Source:          unifi.FirewallPolicyZone{ZoneID: testZoneResourceUUID},
			Destination:     unifi.FirewallPolicyZone{ZoneID: testSystemZoneUUID},
		},
	}
}

// ipFilterList returns a provisioned IPV4 UnifiTrafficMatchingList fixture with
// a resolved upstream UUID.
func ipFilterList() *unifiv1alpha1.UnifiTrafficMatchingList {
	return &unifiv1alpha1.UnifiTrafficMatchingList{
		ObjectMeta: metav1.ObjectMeta{Name: testFilterListResourceName, Namespace: testNamespace, UID: types.UID("filter-list-uid")},
		Spec: unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Name:    "Protected IPv4",
			Type:    unifiv1alpha1.TrafficMatchingListTypeIPv4,
			Items:   []unifiv1alpha1.TrafficMatchingEntry{{Type: testEndpointIPAddress, Address: &unifiv1alpha1.TrafficMatchingAddressEntry{Value: "192.168.1.5"}}},
		},
		Status: unifiv1alpha1.UnifiTrafficMatchingListStatus{ListID: testTrafficListUpstreamID},
	}
}

// newFirewallPolicyFakeClient returns a fake client that counts writes to the
// policy status subresource and to object metadata, so idempotency can be
// asserted.
func newFirewallPolicyFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiFirewallPolicy{},
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

// getFirewallPolicy loads a reconciled fixture by name.
func getFirewallPolicy(t *testing.T, c client.Client) *unifiv1alpha1.UnifiFirewallPolicy {
	t.Helper()
	got := &unifiv1alpha1.UnifiFirewallPolicy{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testFirewallPolicyResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get firewall policy after reconcile: %v", err)
	}
	return got
}

// firewallPolicyMutations filters recorded calls down to the ones that change
// upstream policy state.
func firewallPolicyMutations(calls []string) []string {
	mutations := []string{"CreateFirewallPolicy", "UpdateFirewallPolicy", "DeleteFirewallPolicy"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiFirewallPolicyReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		zones:                 []unifi.FirewallZone{systemUpstreamZone()},
		createdFirewallPolicy: unifi.FirewallPolicy{ID: testFirewallPolicyUUID},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newFirewallPolicyFakeClient(t,
		newFirewallPolicy(), ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getFirewallPolicy(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.PolicyID != testFirewallPolicyUUID {
		t.Errorf("policyID = %q, want %q", got.Status.PolicyID, testFirewallPolicyUUID)
	}
	if got.Status.SourceZoneID != testZoneResourceUUID || got.Status.DestinationZoneID != testSystemZoneUUID {
		t.Errorf("zone IDs = %q/%q, want %q/%q",
			got.Status.SourceZoneID, got.Status.DestinationZoneID, testZoneResourceUUID, testSystemZoneUUID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("firewall policy is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiFirewallPolicyFinalizer) {
		t.Errorf("firewall policy finalizer was not added")
	}

	calls := stub.called()
	if !slices.Contains(calls, "CreateFirewallPolicy") {
		t.Errorf("upstream calls = %v, want CreateFirewallPolicy", calls)
	}
	if len(stub.firewallPolicyCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.firewallPolicyCreateRequests))
	}
	req := stub.firewallPolicyCreateRequests[0]
	if req.Source.ZoneID != testZoneResourceUUID || req.Destination.ZoneID != testSystemZoneUUID {
		t.Errorf("request zones = %q/%q, want the resolved IDs", req.Source.ZoneID, req.Destination.ZoneID)
	}
	if req.Source.TrafficFilter != nil || req.Destination.TrafficFilter != nil {
		t.Errorf("request filters = %+v/%+v, want none", req.Source.TrafficFilter, req.Destination.TrafficFilter)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiFirewallPolicyReconcileResolvesTrafficFilter(t *testing.T) {
	t.Parallel()

	policy := newFirewallPolicy()
	policy.Spec.Source.Filter = &unifiv1alpha1.FirewallTrafficFilter{
		Type:      testEndpointIPAddress,
		IPAddress: &unifiv1alpha1.FirewallTrafficMatchingListFilter{ListRef: unifiv1alpha1.CoreRef{Name: testFilterListResourceName}},
	}

	stub := &stubUnifiClient{
		zones:                 []unifi.FirewallZone{systemUpstreamZone()},
		createdFirewallPolicy: unifi.FirewallPolicy{ID: testFirewallPolicyUUID},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t,
		policy, ownedFirewallZone(), ipFilterList(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if len(stub.firewallPolicyCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.firewallPolicyCreateRequests))
	}
	filter := stub.firewallPolicyCreateRequests[0].Source.TrafficFilter
	if filter == nil || filter.IPAddressFilter == nil {
		t.Fatalf("source filter = %+v, want an ipAddress filter", filter)
	}
	if filter.IPAddressFilter.Type != firewallFilterTrafficMatchingList ||
		filter.IPAddressFilter.TrafficMatchingListID != testTrafficListUpstreamID {
		t.Errorf("ipAddressFilter = %+v, want the resolved traffic matching list UUID", filter.IPAddressFilter)
	}
}

func TestUnifiFirewallPolicyReconcileUnresolvableFilterFailsClosed(t *testing.T) {
	t.Parallel()

	policy := newFirewallPolicy()
	policy.Spec.Source.Filter = &unifiv1alpha1.FirewallTrafficFilter{
		Type:      testEndpointIPAddress,
		IPAddress: &unifiv1alpha1.FirewallTrafficMatchingListFilter{ListRef: unifiv1alpha1.CoreRef{Name: "missing-list"}},
	}

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{systemUpstreamZone()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t,
		policy, ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != firewallPolicyDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, firewallPolicyDependencyRequeueAfter)
	}

	got := getFirewallPolicy(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonFilterRefNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonFilterRefNotFound)
	}
	if mutations := firewallPolicyMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none", mutations)
	}
}

func TestUnifiFirewallPolicyReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	policy := newFirewallPolicy()
	policy.Status.PolicyID = testFirewallPolicyUUID

	stub := &stubUnifiClient{
		zones:            []unifi.FirewallZone{systemUpstreamZone()},
		firewallPolicies: []unifi.FirewallPolicy{matchingFirewallPolicy()},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newFirewallPolicyFakeClient(t,
		policy, ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if mutations := firewallPolicyMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none on an unchanged object", mutations)
	}
}

func TestUnifiFirewallPolicyReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	missingZonePolicy := newFirewallPolicy()
	missingZonePolicy.Spec.Source.ZoneRef = &unifiv1alpha1.CoreRef{Name: "missing-zone"}

	missingBuiltInPolicy := newFirewallPolicy()
	missingBuiltInPolicy.Spec.Destination.BuiltInZone = &unifiv1alpha1.BuiltInFirewallZoneSelector{Name: "Nope"}

	tests := []struct {
		name        string
		policy      *unifiv1alpha1.UnifiFirewallPolicy
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		zone        *unifiv1alpha1.UnifiFirewallZone
		zones       []unifi.FirewallZone
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        testNameSiteMissing,
			policy:      newFirewallPolicy(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteNotAdopted,
			policy:      newFirewallPolicy(),
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        testNameSiteBeingDeleted,
			policy:      newFirewallPolicy(),
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameControllerMissing,
			policy:      newFirewallPolicy(),
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       testNameVersionBelowMinimum,
			policy:     newFirewallPolicy(),
			site:       ownedSite(),
			controller: readyController("10.1.83"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       testNameVersionUnparseable,
			policy:     newFirewallPolicy(),
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        testNameVersionNotReported,
			policy:      newFirewallPolicy(),
			site:        ownedSite(),
			controller:  newUnifiController(nil),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:        "configurable zone missing",
			policy:      missingZonePolicy,
			site:        ownedSite(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonZoneRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "built-in zone missing",
			policy:      missingBuiltInPolicy,
			site:        ownedSite(),
			controller:  readyController(testAppVersion),
			zone:        ownedFirewallZone(),
			zones:       []unifi.FirewallZone{systemUpstreamZone()},
			wantReason:  reasonBuiltInZoneNotFound,
			wantRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []client.Object{tt.policy}
			if tt.site != nil {
				objects = append(objects, tt.site)
			}
			if tt.controller != nil {
				objects = append(objects, tt.controller, apiKeySecret())
			}
			if tt.zone != nil {
				objects = append(objects, tt.zone)
			}

			stub := &stubUnifiClient{zones: tt.zones}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newFirewallPolicyFakeClient(t, objects...)
			reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != firewallPolicyDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, firewallPolicyDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getFirewallPolicy(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := firewallPolicyMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiFirewallPolicyAdoptsSystemObjectReadOnly(t *testing.T) {
	t.Parallel()

	existing := matchingFirewallPolicy()
	existing.Metadata.Origin = testSystemOrigin

	stub := &stubUnifiClient{
		zones:            []unifi.FirewallZone{systemUpstreamZone()},
		firewallPolicies: []unifi.FirewallPolicy{existing},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t,
		newFirewallPolicy(), ownedFirewallZone(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getFirewallPolicy(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonSystemObjectReadOnly {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonSystemObjectReadOnly)
	}
	if mutations := firewallPolicyMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none for a system-managed object", mutations)
	}
}

func TestUnifiFirewallPolicyFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	policy := newFirewallPolicy()
	policy.Finalizers = []string{unifiv1alpha1.UnifiFirewallPolicyFinalizer}
	policy.Status.PolicyID = testFirewallPolicyUUID

	stub := &stubUnifiClient{firewallPolicies: []unifi.FirewallPolicy{matchingFirewallPolicy()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallPolicyFakeClient(t,
		policy, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), policy); err != nil {
		t.Fatalf("delete policy: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedFirewallPolicySiteID != testUpstreamSiteID || stub.deletedFirewallPolicyID != testFirewallPolicyUUID {
		t.Errorf("DeleteFirewallPolicy(%q, %q), want (%q, %q)",
			stub.deletedFirewallPolicySiteID, stub.deletedFirewallPolicyID, testUpstreamSiteID, testFirewallPolicyUUID)
	}
	if !slices.Contains(stub.called(), "DeleteFirewallPolicy") {
		t.Errorf("upstream calls = %v, want DeleteFirewallPolicy", stub.called())
	}

	got := &unifiv1alpha1.UnifiFirewallPolicy{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testFirewallPolicyResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get policy after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiFirewallPolicyFinalizer) {
		t.Errorf("firewall policy finalizer was not removed after upstream deletion")
	}
}
