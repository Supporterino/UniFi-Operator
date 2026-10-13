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
	testTrafficListResourceName = "traffic-list"
	testTrafficListUpstreamName = "Allowed ports"
	testTrafficListUUID         = "list-uuid-3"
)

// newTrafficMatchingList returns a minimal PORTS UnifiTrafficMatchingList
// fixture with a UID (needed for the ownerReference).
func newTrafficMatchingList() *unifiv1alpha1.UnifiTrafficMatchingList {
	return &unifiv1alpha1.UnifiTrafficMatchingList{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testTrafficListResourceName,
			Namespace: testNamespace,
			UID:       types.UID("traffic-list-uid"),
		},
		Spec: unifiv1alpha1.UnifiTrafficMatchingListSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Name:    testTrafficListUpstreamName,
			Type:    unifiv1alpha1.TrafficMatchingListTypePorts,
			Items: []unifiv1alpha1.TrafficMatchingEntry{
				{Type: testPortEntryNumber, Port: &unifiv1alpha1.TrafficMatchingPortEntry{Value: 80}},
				{Type: testPortEntryRange, PortRange: &unifiv1alpha1.TrafficMatchingPortRangeEntry{Start: 8000, Stop: 8080}},
			},
		},
	}
}

// matchingTrafficMatchingList returns the upstream list that equals what
// newTrafficMatchingList desires, as the console would report it.
func matchingTrafficMatchingList() unifi.TrafficMatchingList {
	return unifi.TrafficMatchingList{
		ID:   testTrafficListUUID,
		Name: testTrafficListUpstreamName,
		Type: unifiv1alpha1.TrafficMatchingListTypePorts,
		Items: []unifi.TrafficMatchingListItem{
			{Type: testPortEntryNumber, Value: "80"},
			{Type: testPortEntryRange, Start: "8000", Stop: "8080"},
		},
	}
}

// newTrafficListFakeClient returns a fake client that counts writes to the list
// status subresource and to object metadata, so idempotency can be asserted.
func newTrafficListFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiTrafficMatchingList{},
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

// getTrafficMatchingList loads a reconciled fixture by name.
func getTrafficMatchingList(t *testing.T, c client.Client) *unifiv1alpha1.UnifiTrafficMatchingList {
	t.Helper()
	got := &unifiv1alpha1.UnifiTrafficMatchingList{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testTrafficListResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get traffic matching list after reconcile: %v", err)
	}
	return got
}

// trafficListMutations filters recorded calls down to the ones that change
// upstream list state.
func trafficListMutations(calls []string) []string {
	mutations := []string{"CreateTrafficMatchingList", "UpdateTrafficMatchingList", "DeleteTrafficMatchingList"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiTrafficMatchingListReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		createdTrafficMatchingList: unifi.TrafficMatchingList{
			ID: testTrafficListUUID, Name: testTrafficListUpstreamName, Type: unifiv1alpha1.TrafficMatchingListTypePorts,
		},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newTrafficListFakeClient(t,
		newTrafficMatchingList(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiTrafficMatchingListReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testTrafficListResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getTrafficMatchingList(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.ListID != testTrafficListUUID {
		t.Errorf("listID = %q, want %q", got.Status.ListID, testTrafficListUUID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("traffic matching list is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiTrafficMatchingListFinalizer) {
		t.Errorf("traffic matching list finalizer was not added")
	}

	calls := stub.called()
	if !slices.Contains(calls, "CreateTrafficMatchingList") {
		t.Errorf("upstream calls = %v, want CreateTrafficMatchingList", calls)
	}
	if len(stub.trafficMatchingListCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.trafficMatchingListCreateRequests))
	}
	if items := stub.trafficMatchingListCreateRequests[0].Items; len(items) != 2 || items[0].Value != "80" {
		t.Errorf("create items = %+v, want the two port entries", items)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiTrafficMatchingListReconcileUpdatesUpstream(t *testing.T) {
	t.Parallel()

	list := newTrafficMatchingList()
	list.Status.ListID = testTrafficListUUID

	stale := matchingTrafficMatchingList()
	stale.Items = []unifi.TrafficMatchingListItem{{Type: testPortEntryNumber, Value: "22"}}
	stub := &stubUnifiClient{
		trafficMatchingLists:       []unifi.TrafficMatchingList{stale},
		updatedTrafficMatchingList: matchingTrafficMatchingList(),
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newTrafficListFakeClient(t,
		list, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiTrafficMatchingListReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testTrafficListResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getTrafficMatchingList(t, fakeClient)
	if cond := requireReady(t, got.Status.Conditions); cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if !slices.Contains(stub.called(), "UpdateTrafficMatchingList") {
		t.Errorf("upstream calls = %v, want UpdateTrafficMatchingList", stub.called())
	}
	if stub.updatedTrafficMatchingListID != testTrafficListUUID {
		t.Errorf("updated list = %q, want the status.listID %q", stub.updatedTrafficMatchingListID, testTrafficListUUID)
	}
}

func TestUnifiTrafficMatchingListReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	list := newTrafficMatchingList()
	list.Status.ListID = testTrafficListUUID

	stub := &stubUnifiClient{trafficMatchingLists: []unifi.TrafficMatchingList{matchingTrafficMatchingList()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newTrafficListFakeClient(t,
		list, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiTrafficMatchingListReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testTrafficListResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if mutations := trafficListMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none on an unchanged object", mutations)
	}
}

func TestUnifiTrafficMatchingListReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	tests := []struct {
		name        string
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        testNameSiteMissing,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteBeingDeleted,
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteNotAdopted,
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        testNameControllerMissing,
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       testNameVersionBelowMinimum,
			site:       ownedSite(),
			controller: readyController("10.1.83"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       testNameVersionUnparseable,
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        testNameVersionNotReported,
			site:        ownedSite(),
			controller:  newUnifiController(nil),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []client.Object{newTrafficMatchingList()}
			if tt.site != nil {
				objects = append(objects, tt.site)
			}
			if tt.controller != nil {
				objects = append(objects, tt.controller, apiKeySecret())
			}

			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newTrafficListFakeClient(t, objects...)
			reconciler := &UnifiTrafficMatchingListReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testTrafficListResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != trafficMatchingListDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, trafficMatchingListDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getTrafficMatchingList(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := trafficListMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

// TestUnifiTrafficMatchingListReconcileRejectsMalformedEntry verifies the
// reconciler's defensive handling of an entry whose type requires a variant
// sub-object that is not set. CRD/CEL rejects it at the API server; a fake
// client does not enforce CEL, so the reconciler must fail closed (InvalidSpec)
// without mutating upstream.
func TestUnifiTrafficMatchingListReconcileRejectsMalformedEntry(t *testing.T) {
	t.Parallel()

	list := newTrafficMatchingList()
	list.Spec.Items = []unifiv1alpha1.TrafficMatchingEntry{{Type: testPortEntryNumber}}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newTrafficListFakeClient(t,
		list, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiTrafficMatchingListReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testTrafficListResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0 for a terminal spec error", result.RequeueAfter)
	}

	got := getTrafficMatchingList(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonInvalidSpec {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonInvalidSpec)
	}
	if mutations := trafficListMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none", mutations)
	}
}

func TestUnifiTrafficMatchingListFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	list := newTrafficMatchingList()
	list.Finalizers = []string{unifiv1alpha1.UnifiTrafficMatchingListFinalizer}
	list.Status.ListID = testTrafficListUUID

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newTrafficListFakeClient(t,
		list, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiTrafficMatchingListReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), list); err != nil {
		t.Fatalf("delete list: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testTrafficListResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedTrafficMatchingListSiteID != testUpstreamSiteID || stub.deletedTrafficMatchingListID != testTrafficListUUID {
		t.Errorf("DeleteTrafficMatchingList(%q, %q), want (%q, %q)",
			stub.deletedTrafficMatchingListSiteID, stub.deletedTrafficMatchingListID, testUpstreamSiteID, testTrafficListUUID)
	}
	if !slices.Contains(stub.called(), "DeleteTrafficMatchingList") {
		t.Errorf("upstream calls = %v, want DeleteTrafficMatchingList", stub.called())
	}

	got := &unifiv1alpha1.UnifiTrafficMatchingList{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testTrafficListResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get list after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiTrafficMatchingListFinalizer) {
		t.Errorf("traffic matching list finalizer was not removed after upstream deletion")
	}
}
