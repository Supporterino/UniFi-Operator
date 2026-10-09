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
	"sigs.k8s.io/controller-runtime/pkg/event"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	testFirewallZoneResourceName = "zone"
	testZoneUpstreamName         = "IoT"
	testZoneResourceUUID         = "zone-uuid-9"
	testZoneCustomUUID           = "zone-uuid-custom-1"
	testOtherSite                = "other-site"
	testOtherNamespace           = "other"
	testSystemOrigin             = "SYSTEM_DEFINED"
	testZoneResourceAName        = "zone-a"
	testZoneResourceBName        = "zone-b"
	testZoneUpstreamMembersA     = "members-a"
	testZoneUpstreamMembersB     = "members-b"
)

// newFirewallZone returns a minimal UnifiFirewallZone fixture with a UID (needed
// for the ownerReference) and no member networks.
func newFirewallZone() *unifiv1alpha1.UnifiFirewallZone {
	return &unifiv1alpha1.UnifiFirewallZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testFirewallZoneResourceName,
			Namespace: testNamespace,
			UID:       types.UID("zone-resource-uid"),
		},
		Spec: unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Name:    testZoneUpstreamName,
		},
	}
}

// newZoneWithMembers returns a firewall-zone fixture that claims one network.
func newZoneWithMembers() *unifiv1alpha1.UnifiFirewallZone {
	zone := newFirewallZone()
	zone.Spec.NetworkRefs = []unifiv1alpha1.CoreRef{{Name: testNetworkResourceName}}
	return zone
}

// memberNetwork returns a UnifiNetwork fixture that has reported its upstream
// network UUID, so it can be a zone member.
func memberNetwork() *unifiv1alpha1.UnifiNetwork {
	network := newGatewayNetwork()
	network.Status.NetworkID = testUpstreamNetworkID
	return network
}

// matchingUpstreamZone returns the upstream zone that equals what
// newZoneWithMembers desires, as the console would report it.
func matchingUpstreamZone() unifi.FirewallZone {
	return unifi.FirewallZone{
		ID:         testZoneResourceUUID,
		Name:       testZoneUpstreamName,
		NetworkIDs: []string{testUpstreamNetworkID},
		Metadata:   unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined},
	}
}

// newFirewallZoneFakeClient returns a fake client that counts writes to the zone
// status subresource and to object metadata, so idempotency can be asserted.
func newFirewallZoneFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiNetwork{},
			&unifiv1alpha1.UnifiFirewallZone{},
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

// getFirewallZone loads a reconciled UnifiFirewallZone fixture by name.
func getFirewallZone(t *testing.T, c client.Client, name string) *unifiv1alpha1.UnifiFirewallZone {
	t.Helper()
	got := &unifiv1alpha1.UnifiFirewallZone{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get firewall zone after reconcile: %v", err)
	}
	return got
}

// firewallZoneMutations filters recorded calls down to the ones that change
// upstream zone state.
func firewallZoneMutations(calls []string) []string {
	mutations := []string{"CreateZone", "UpdateZone", "DeleteZone"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiFirewallZoneReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		createdZone: unifi.FirewallZone{
			ID:       testZoneResourceUUID,
			Name:     testZoneUpstreamName,
			Metadata: unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined},
		},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newFirewallZoneFakeClient(t,
		newZoneWithMembers(), ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getFirewallZone(t, fakeClient, testFirewallZoneResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.ZoneID != testZoneResourceUUID {
		t.Errorf("zoneID = %q, want %q", got.Status.ZoneID, testZoneResourceUUID)
	}
	if got.Status.Origin != firewallZoneOriginUserDefined {
		t.Errorf("origin = %q, want %q", got.Status.Origin, firewallZoneOriginUserDefined)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("firewall zone is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiFirewallZoneFinalizer) {
		t.Errorf("firewall zone finalizer was not added")
	}

	calls := stub.called()
	if !slices.Contains(calls, "CreateZone") {
		t.Errorf("upstream calls = %v, want CreateZone", calls)
	}
	if slices.Contains(calls, "UpdateZone") {
		t.Errorf("upstream calls = %v, want no UpdateZone on a create", calls)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiFirewallZoneReconcileUpdatesUpstream(t *testing.T) {
	t.Parallel()

	zone := newZoneWithMembers()
	zone.Status.ZoneID = testZoneResourceUUID

	stale := matchingUpstreamZone()
	stale.NetworkIDs = []string{"stale-network"}
	stub := &stubUnifiClient{
		zones:       []unifi.FirewallZone{stale},
		updatedZone: matchingUpstreamZone(),
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zone, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getFirewallZone(t, fakeClient, testFirewallZoneResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ZoneID != testZoneResourceUUID {
		t.Errorf("zoneID = %q, want %q", got.Status.ZoneID, testZoneResourceUUID)
	}

	calls := stub.called()
	if !slices.Contains(calls, "UpdateZone") {
		t.Errorf("upstream calls = %v, want UpdateZone", calls)
	}
	if slices.Contains(calls, "CreateZone") {
		t.Errorf("upstream calls = %v, want no CreateZone when the zone exists", calls)
	}
	if len(stub.zoneUpdateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1", len(stub.zoneUpdateRequests))
	}
	if stub.updatedZoneID != testZoneResourceUUID {
		t.Errorf("updated zone = %q, want the status.zoneID %q", stub.updatedZoneID, testZoneResourceUUID)
	}
	if !slices.Equal(stub.zoneUpdateRequests[0].NetworkIDs, []string{testUpstreamNetworkID}) {
		t.Errorf("update request membership = %v, want [%s]", stub.zoneUpdateRequests[0].NetworkIDs, testUpstreamNetworkID)
	}
}

func TestUnifiFirewallZoneReconcileResolvesByStatusIDOnRename(t *testing.T) {
	t.Parallel()

	zone := newZoneWithMembers()
	zone.Spec.Name = "renamed-zone"
	zone.Status.ZoneID = testZoneResourceUUID

	// The upstream object was renamed but is still the one recorded in status;
	// the reconciler must update it in place rather than create a second zone.
	renamed := matchingUpstreamZone()
	renamed.Name = "old-zone-name"
	stub := &stubUnifiClient{
		zones:       []unifi.FirewallZone{renamed},
		updatedZone: matchingUpstreamZone(),
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zone, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if len(stub.zoneCreateRequests) != 0 {
		t.Errorf("create requests = %d, want 0 (rename must update in place)", len(stub.zoneCreateRequests))
	}
	if len(stub.zoneUpdateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1", len(stub.zoneUpdateRequests))
	}
	if stub.updatedZoneID != testZoneResourceUUID {
		t.Errorf("updated zone = %q, want the status.zoneID %q", stub.updatedZoneID, testZoneResourceUUID)
	}
}

func TestUnifiFirewallZoneReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	zone := newZoneWithMembers()
	zone.Status.ZoneID = testZoneResourceUUID

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{matchingUpstreamZone()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newFirewallZoneFakeClient(t,
		zone, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if len(stub.zoneCreateRequests) != 0 || len(stub.zoneUpdateRequests) != 0 {
		t.Errorf("upstream mutations = create:%d update:%d, want none on an unchanged object",
			len(stub.zoneCreateRequests), len(stub.zoneUpdateRequests))
	}
}

func TestUnifiFirewallZoneReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	notAdoptedNetwork := newGatewayNetwork()

	crossSiteNetwork := newGatewayNetwork()
	crossSiteNetwork.Spec.SiteRef.Name = testOtherSite

	tests := []struct {
		name        string
		zone        *unifiv1alpha1.UnifiFirewallZone
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		network     *unifiv1alpha1.UnifiNetwork
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        "site missing",
			zone:        newFirewallZone(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "site being deleted",
			zone:        newFirewallZone(),
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "site not adopted",
			zone:        newFirewallZone(),
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        "controller missing",
			zone:        newFirewallZone(),
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       "application version below minimum",
			zone:       newFirewallZone(),
			site:       ownedSite(),
			controller: readyController("10.1.83"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       "version unparseable",
			zone:       newFirewallZone(),
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        "application version not reported",
			zone:        newFirewallZone(),
			site:        ownedSite(),
			controller:  newUnifiController(nil),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:        "referenced network missing",
			zone:        newZoneWithMembers(),
			site:        ownedSite(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:        "referenced network not adopted",
			zone:        newZoneWithMembers(),
			site:        ownedSite(),
			controller:  readyController(testAppVersion),
			network:     notAdoptedNetwork,
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:       "cross-site network reference",
			zone:       newZoneWithMembers(),
			site:       ownedSite(),
			controller: readyController(testAppVersion),
			network:    crossSiteNetwork,
			wantReason: reasonCrossSiteReference,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []client.Object{tt.zone}
			if tt.site != nil {
				objects = append(objects, tt.site)
			}
			if tt.controller != nil {
				objects = append(objects, tt.controller, apiKeySecret())
			}
			if tt.network != nil {
				objects = append(objects, tt.network)
			}

			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newFirewallZoneFakeClient(t, objects...)
			reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != firewallZoneDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, firewallZoneDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getFirewallZone(t, fakeClient, testFirewallZoneResourceName)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := firewallZoneMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiFirewallZoneAdoptsSystemObjectReadOnly(t *testing.T) {
	t.Parallel()

	systemZone := matchingUpstreamZone()
	systemZone.Metadata.Origin = testSystemOrigin
	systemZone.NetworkIDs = []string{"system-member"}

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{systemZone}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		newZoneWithMembers(), ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getFirewallZone(t, fakeClient, testFirewallZoneResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonSystemObjectReadOnly {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonSystemObjectReadOnly)
	}
	if got.Status.ZoneID != "" {
		t.Errorf("zoneID = %q, want empty: a system zone's ID must not be persisted", got.Status.ZoneID)
	}
	if got.Status.Origin != testSystemOrigin {
		t.Errorf("origin = %q, want the adopted %q", got.Status.Origin, testSystemOrigin)
	}
	if mutations := firewallZoneMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none for a system-managed object", mutations)
	}
}

// TestUnifiFirewallZoneSystemZoneRenameReResolves reproduces the rename trap: a
// CR that adopted a system zone read-only must not persist that zone's ID, or a
// later spec.name rename keeps resolving the same still-present system zone and
// the CR can never be repurposed for a custom zone.
func TestUnifiFirewallZoneSystemZoneRenameReResolves(t *testing.T) {
	t.Parallel()

	systemZone := matchingUpstreamZone()
	systemZone.Metadata.Origin = testSystemOrigin
	systemZone.NetworkIDs = []string{"system-member"}

	customZone := matchingUpstreamZone()
	customZone.ID = testZoneCustomUUID

	stub := &stubUnifiClient{
		zones:       []unifi.FirewallZone{systemZone},
		createdZone: customZone,
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		newZoneWithMembers(), ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	// First reconcile adopts the system zone read-only by name.
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	adopted := getFirewallZone(t, fakeClient, testFirewallZoneResourceName)
	if cond := requireReady(t, adopted.Status.Conditions); cond.Status != metav1.ConditionFalse || cond.Reason != reasonSystemObjectReadOnly {
		t.Fatalf("adopted Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonSystemObjectReadOnly)
	}

	// Rename to a custom name. The system zone still exists upstream, so a
	// persisted ID would keep resolving it; the reconciler must re-resolve by
	// the new name and create a custom zone instead.
	adopted.Spec.Name = "custom-zone"
	if err := fakeClient.Update(context.Background(), adopted); err != nil {
		t.Fatalf("rename zone: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}

	if len(stub.zoneCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1 (rename must create a custom zone)", len(stub.zoneCreateRequests))
	}
	if len(stub.zoneUpdateRequests) != 0 {
		t.Errorf("update requests = %d, want 0 (must not overwrite the system zone)", len(stub.zoneUpdateRequests))
	}
	got := getFirewallZone(t, fakeClient, testFirewallZoneResourceName)
	if cond := requireReady(t, got.Status.Conditions); cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("renamed Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ZoneID != testZoneCustomUUID {
		t.Errorf("zoneID = %q, want the created custom zone %q", got.Status.ZoneID, testZoneCustomUUID)
	}
}

func TestUnifiFirewallZoneMembershipConflict(t *testing.T) {
	t.Parallel()

	zoneA := newZoneWithMembers()
	zoneA.Name = testZoneResourceAName
	zoneA.Spec.Name = testZoneUpstreamMembersA
	zoneB := newZoneWithMembers()
	zoneB.Name = testZoneResourceBName
	zoneB.Spec.Name = testZoneUpstreamMembersB

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zoneA, zoneB, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	for _, name := range []string{testZoneResourceAName, testZoneResourceBName} {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(name)); err != nil {
			t.Fatalf("reconcile %s: %v", name, err)
		}
		got := getFirewallZone(t, fakeClient, name)
		cond := requireReady(t, got.Status.Conditions)
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonMembershipConflict {
			t.Errorf("zone %s Ready = %s/%s, want False/%s", name, cond.Status, cond.Reason, reasonMembershipConflict)
		}
	}

	if mutations := firewallZoneMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none while membership is contested", mutations)
	}
}

func TestUnifiFirewallZoneNameConflict(t *testing.T) {
	t.Parallel()

	zoneA := newFirewallZone()
	zoneA.Name = testZoneResourceAName
	zoneB := newFirewallZone()
	zoneB.Name = testZoneResourceBName

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zoneA, zoneB, ownedSite(), readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	for _, name := range []string{testZoneResourceAName, testZoneResourceBName} {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(name)); err != nil {
			t.Fatalf("reconcile %s: %v", name, err)
		}
		got := getFirewallZone(t, fakeClient, name)
		cond := requireReady(t, got.Status.Conditions)
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonZoneNameConflict {
			t.Errorf("zone %s Ready = %s/%s, want False/%s", name, cond.Status, cond.Reason, reasonZoneNameConflict)
		}
	}

	if mutations := firewallZoneMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none while the zone name is contested", mutations)
	}
}

// TestUnifiFirewallZoneConflictMarksPreExistingClaimant reproduces the stale
// conflict bug: a zone reconciles cleanly (Ready=True), then a second claimant
// appears. Reconciling the new claimant must fail closed on both, not only on
// the zone being reconciled. It covers both conflict kinds (design D3).
func TestUnifiFirewallZoneConflictMarksPreExistingClaimant(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		configure  func(a, b *unifiv1alpha1.UnifiFirewallZone)
		wantReason string
	}{
		{
			name: "membership conflict",
			configure: func(a, b *unifiv1alpha1.UnifiFirewallZone) {
				a.Spec.Name, b.Spec.Name = testZoneUpstreamMembersA, testZoneUpstreamMembersB
				a.Spec.NetworkRefs = []unifiv1alpha1.CoreRef{{Name: testNetworkResourceName}}
				b.Spec.NetworkRefs = []unifiv1alpha1.CoreRef{{Name: testNetworkResourceName}}
			},
			wantReason: reasonMembershipConflict,
		},
		{
			name: "zone name conflict",
			configure: func(a, b *unifiv1alpha1.UnifiFirewallZone) {
				a.Spec.Name, b.Spec.Name = testZoneUpstreamName, testZoneUpstreamName
			},
			wantReason: reasonZoneNameConflict,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			zoneA := newFirewallZone()
			zoneA.Name = testZoneResourceAName
			zoneB := newFirewallZone()
			zoneB.Name = testZoneResourceBName
			zoneB.UID = types.UID("zone-b-resource-uid")
			tt.configure(zoneA, zoneB)

			stub := &stubUnifiClient{createdZone: matchingUpstreamZone()}
			factory := &recordingFactory{client: stub}
			fakeClient, _, _ := newFirewallZoneFakeClient(t,
				zoneA, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())
			reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			// Zone A reconciles alone first and becomes Ready.
			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testZoneResourceAName)); err != nil {
				t.Fatalf("reconcile zone A: %v", err)
			}
			if cond := requireReady(t, getFirewallZone(t, fakeClient, testZoneResourceAName).Status.Conditions); cond.Status != metav1.ConditionTrue {
				t.Fatalf("zone A Ready = %s, want True before the conflict exists", cond.Status)
			}

			// A second claimant appears. Reconciling it must fail closed on both.
			if err := fakeClient.Create(context.Background(), zoneB); err != nil {
				t.Fatalf("create conflicting zone B: %v", err)
			}
			mutationsBefore := firewallZoneMutations(stub.called())
			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testZoneResourceBName)); err != nil {
				t.Fatalf("reconcile zone B: %v", err)
			}

			for _, name := range []string{testZoneResourceAName, testZoneResourceBName} {
				got := getFirewallZone(t, fakeClient, name)
				cond := requireReady(t, got.Status.Conditions)
				if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
					t.Errorf("zone %s Ready = %s/%s, want False/%s", name, cond.Status, cond.Reason, tt.wantReason)
				}
			}

			if mutationsAfter := firewallZoneMutations(stub.called()); len(mutationsAfter) != len(mutationsBefore) {
				t.Errorf("upstream mutations changed while conflicted: before=%v after=%v", mutationsBefore, mutationsAfter)
			}
		})
	}
}

// TestUnifiFirewallZoneConflictSelfHealsAfterClaimantRemoved ensures the
// survivor recovers once the other claimant is gone, rather than staying stuck
// with a stale Ready=False.
func TestUnifiFirewallZoneConflictSelfHealsAfterClaimantRemoved(t *testing.T) {
	t.Parallel()

	zoneA := newZoneWithMembers()
	zoneA.Name = testZoneResourceAName
	zoneA.Spec.Name = testZoneUpstreamMembersA
	zoneB := newZoneWithMembers()
	zoneB.Name = testZoneResourceBName
	zoneB.UID = types.UID("zone-b-resource-uid")
	zoneB.Spec.Name = testZoneUpstreamMembersB

	stub := &stubUnifiClient{createdZone: matchingUpstreamZone()}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zoneA, zoneB, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	for _, name := range []string{testZoneResourceAName, testZoneResourceBName} {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(name)); err != nil {
			t.Fatalf("reconcile %s while conflicted: %v", name, err)
		}
	}

	if err := fakeClient.Delete(context.Background(), zoneB); err != nil {
		t.Fatalf("delete zone B: %v", err)
	}
	// B carries the zone finalizer added before the conflict was detected, so
	// its deletion reconciles once to drain the finalizer before it is gone.
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testZoneResourceBName)); err != nil {
		t.Fatalf("reconcile deleting zone B: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testZoneResourceAName)); err != nil {
		t.Fatalf("reconcile zone A after the conflict cleared: %v", err)
	}

	got := getFirewallZone(t, fakeClient, testZoneResourceAName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("zone A Ready = %s/%s, want True/%s after the conflict cleared", cond.Status, cond.Reason, reasonReconciled)
	}
}

func TestUnifiFirewallZoneFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	zone := newZoneWithMembers()
	zone.Finalizers = []string{unifiv1alpha1.UnifiFirewallZoneFinalizer}
	zone.Status.ZoneID = testZoneResourceUUID

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{matchingUpstreamZone()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zone, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), zone); err != nil {
		t.Fatalf("delete zone: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedZoneSiteID != testUpstreamSiteID || stub.deletedZoneID != testZoneResourceUUID {
		t.Errorf("DeleteZone(%q, %q), want (%q, %q)",
			stub.deletedZoneSiteID, stub.deletedZoneID, testUpstreamSiteID, testZoneResourceUUID)
	}
	if !slices.Contains(stub.called(), "DeleteZone") {
		t.Errorf("upstream calls = %v, want DeleteZone", stub.called())
	}

	got := &unifiv1alpha1.UnifiFirewallZone{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testFirewallZoneResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get zone after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiFirewallZoneFinalizer) {
		t.Errorf("firewall zone finalizer was not removed after upstream deletion")
	}
}

func TestUnifiFirewallZoneFinalizerSkipsSystemObject(t *testing.T) {
	t.Parallel()

	zone := newZoneWithMembers()
	zone.Finalizers = []string{unifiv1alpha1.UnifiFirewallZoneFinalizer}
	zone.Status.ZoneID = testZoneResourceUUID

	systemZone := matchingUpstreamZone()
	systemZone.Metadata.Origin = testSystemOrigin
	stub := &stubUnifiClient{zones: []unifi.FirewallZone{systemZone}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zone, ownedSite(), readyController(testAppVersion), memberNetwork(), apiKeySecret())

	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), zone); err != nil {
		t.Fatalf("delete zone: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if slices.Contains(stub.called(), "DeleteZone") {
		t.Errorf("upstream calls = %v, want no DeleteZone for a system-managed object", stub.called())
	}

	got := &unifiv1alpha1.UnifiFirewallZone{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testFirewallZoneResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get zone after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiFirewallZoneFinalizer) {
		t.Errorf("firewall zone finalizer was not removed when the upstream object is system-managed")
	}
}

// TestUnifiNetworkWatchFirewallZoneEnqueuesReferencedNetworks verifies the
// network controller's UnifiFirewallZone watch map function: only networks in
// the changed zone's namespace that the zone lists in spec.networkRefs are
// enqueued, so a membership write refreshes exactly the affected networks and
// the zone's status churn does not bounce the rest of the namespace.
func TestUnifiNetworkWatchFirewallZoneEnqueuesReferencedNetworks(t *testing.T) {
	t.Parallel()

	unreferenced := newGatewayNetwork()
	unreferenced.Name = "unreferenced"

	otherNamespaceNetwork := newGatewayNetwork()
	otherNamespaceNetwork.Namespace = testOtherNamespace

	fakeClient, _, _ := newNetworkFakeClient(t, memberNetwork(), unreferenced, otherNamespaceNetwork)
	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme()}

	changed := &unifiv1alpha1.UnifiFirewallZone{
		ObjectMeta: metav1.ObjectMeta{Name: "zone", Namespace: testNamespace},
		Spec: unifiv1alpha1.UnifiFirewallZoneSpec{
			NetworkRefs: []unifiv1alpha1.CoreRef{{Name: testNetworkResourceName}, {Name: "missing"}},
		},
	}
	requests := reconciler.networksForZone(context.Background(), changed)
	if len(requests) != 1 {
		t.Fatalf("requests = %d (%v), want 1 (only the referenced network in the namespace)", len(requests), requests)
	}
	if requests[0].Name != testNetworkResourceName || requests[0].Namespace != testNamespace {
		t.Errorf("request = %v, want %s/%s", requests[0].NamespacedName, testNamespace, testNetworkResourceName)
	}

	// A zone that claims no networks (for example a system zone adopted
	// read-only) enqueues nothing.
	if got := reconciler.networksForZone(context.Background(), &unifiv1alpha1.UnifiFirewallZone{
		ObjectMeta: metav1.ObjectMeta{Name: "system-zone", Namespace: testNamespace},
	}); len(got) != 0 {
		t.Errorf("requests = %v, want none for a zone with no networkRefs", got)
	}
}

// TestUnifiFirewallZoneChangeEnqueuesSitePeers verifies the self-watch map
// function: a zone change enqueues every zone sharing its namespace and site,
// so a conflict flips on all claimants and a resolved conflict clears the
// survivor. Zones on another site or in another namespace are not enqueued.
func TestUnifiFirewallZoneChangeEnqueuesSitePeers(t *testing.T) {
	t.Parallel()

	peer := newFirewallZone()
	peer.Name = "peer"
	otherSite := newFirewallZone()
	otherSite.Name = "other-site-zone"
	otherSite.Spec.SiteRef.Name = testOtherSite
	otherNamespace := newFirewallZone()
	otherNamespace.Name = "other-namespace-zone"
	otherNamespace.Namespace = testOtherNamespace

	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		newFirewallZone(), peer, otherSite, otherNamespace)
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme()}

	changed := newFirewallZone()
	requests := reconciler.zonesForZoneChange(context.Background(), changed)
	if len(requests) != 2 {
		t.Fatalf("requests = %d (%v), want 2 (the changed zone and its same-site peer)", len(requests), requests)
	}
	names := []string{requests[0].Name, requests[1].Name}
	slices.Sort(names)
	if !slices.Equal(names, []string{"peer", testFirewallZoneResourceName}) {
		t.Errorf("requests = %v, want the changed zone and its same-site peer", requests)
	}
}

// TestFirewallZonePredicateIgnoresStatusOnlyUpdates guards the self-watch
// against an infinite loop: the reconciler writes peer statuses, and only
// generation changes (or a first deletion) may re-enqueue.
func TestFirewallZonePredicateIgnoresStatusOnlyUpdates(t *testing.T) {
	t.Parallel()

	predicate := firewallZonePredicate()

	before := newFirewallZone()
	before.Generation = 3
	after := before.DeepCopy()
	after.Generation = 3
	after.Status.Summary = "peer status refreshed"
	if predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: after}) {
		t.Errorf("status-only update enqueued; want it dropped to avoid a reconcile loop")
	}

	bumped := before.DeepCopy()
	bumped.Generation = 4
	if !predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: bumped}) {
		t.Errorf("generation change was not enqueued")
	}
}

// TestUnifiFirewallZoneWatchNetworkEnqueuesReferencingZones verifies the
// UnifiNetwork dependency watch map function: only zones in the changed
// network's namespace whose spec.networkRefs name that network are enqueued.
// Zones that do not claim the network, or live in another namespace, are not.
func TestUnifiFirewallZoneWatchNetworkEnqueuesReferencingZones(t *testing.T) {
	t.Parallel()

	referencing := newZoneWithMembers()
	referencing.Name = "referencing"
	nonMember := newZoneWithMembers()
	nonMember.Name = "non-member"
	nonMember.Spec.NetworkRefs = []unifiv1alpha1.CoreRef{{Name: "other-network"}}
	otherNamespace := newZoneWithMembers()
	otherNamespace.Name = "other-namespace"
	otherNamespace.Namespace = testOtherNamespace

	fakeClient, _, _ := newFirewallZoneFakeClient(t, referencing, nonMember, otherNamespace)
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme()}

	requests := reconciler.zonesForNetwork(context.Background(), memberNetwork())
	if len(requests) != 1 {
		t.Fatalf("requests = %d (%v), want 1 (only the zone referencing the network)", len(requests), requests)
	}
	if requests[0].Name != "referencing" || requests[0].Namespace != testNamespace {
		t.Errorf("request = %v, want %s/referencing", requests[0].NamespacedName, testNamespace)
	}
}

// TestNetworkDependencyPredicateTriggersOnRelevantChanges guards the zone
// controller's UnifiNetwork watch: a first deletion or a changed upstream
// networkID must re-enqueue the zones referencing the network, while
// status-only churn (conditions, summary) is dropped so the network
// controller's status writes cannot bounce reconciles between controllers.
func TestNetworkDependencyPredicateTriggersOnRelevantChanges(t *testing.T) {
	t.Parallel()

	predicate := networkDependencyPredicate()

	before := memberNetwork()

	deleted := before.DeepCopy()
	deleted.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}
	if !predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: deleted}) {
		t.Errorf("first deletion was not enqueued")
	}

	reidentified := before.DeepCopy()
	reidentified.Status.NetworkID = "net-uuid-2"
	if !predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: reidentified}) {
		t.Errorf("changed status.networkID was not enqueued")
	}

	bumped := before.DeepCopy()
	bumped.Generation = before.Generation + 1
	if !predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: bumped}) {
		t.Errorf("generation change was not enqueued")
	}

	statusChurn := before.DeepCopy()
	statusChurn.Status.Summary = "network status refreshed"
	if predicate.Update(event.UpdateEvent{ObjectOld: before, ObjectNew: statusChurn}) {
		t.Errorf("status-only update enqueued; want it dropped to avoid a reconcile loop")
	}
}

// TestUnifiFirewallZoneReconcileReportsMissingMemberAfterDelete verifies the
// dependency watch contract end to end: once a member UnifiNetwork is gone, a
// zone that was Ready=True must report DependencyNotReady instead of keeping
// stale upstream membership.
func TestUnifiFirewallZoneReconcileReportsMissingMemberAfterDelete(t *testing.T) {
	t.Parallel()

	zone := newZoneWithMembers()
	stub := &stubUnifiClient{
		zones:       []unifi.FirewallZone{matchingUpstreamZone()},
		createdZone: matchingUpstreamZone(),
	}
	factory := &recordingFactory{client: stub}
	member := memberNetwork()
	fakeClient, _, _ := newFirewallZoneFakeClient(t,
		zone, ownedSite(), readyController(testAppVersion), member, apiKeySecret())
	reconciler := &UnifiFirewallZoneReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if cond := requireReady(t, getFirewallZone(t, fakeClient, testFirewallZoneResourceName).Status.Conditions); cond.Status != metav1.ConditionTrue {
		t.Fatalf("zone Ready = %s, want True while the member network exists", cond.Status)
	}

	if err := fakeClient.Delete(context.Background(), member); err != nil {
		t.Fatalf("delete member network: %v", err)
	}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallZoneResourceName))
	if err != nil {
		t.Fatalf("reconcile after member deletion: %v", err)
	}
	if result.RequeueAfter != firewallZoneDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, firewallZoneDependencyRequeueAfter)
	}

	cond := requireReady(t, getFirewallZone(t, fakeClient, testFirewallZoneResourceName).Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonDependencyNotReady {
		t.Errorf("zone Ready = %s/%s, want False/%s after the member was deleted",
			cond.Status, cond.Reason, reasonDependencyNotReady)
	}
}
