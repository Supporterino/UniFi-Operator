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

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	testRadiusProfileResourceName = "radiusprofile"
	testRadiusProfileUpstreamName = "Corp RADIUS"
	testRadiusProfileUUID         = "profile-uuid-7"
)

// newRadiusProfile returns a minimal UnifiRadiusProfile fixture with a UID
// (needed for the ownerReference).
func newRadiusProfile() *unifiv1alpha1.UnifiRadiusProfile {
	return &unifiv1alpha1.UnifiRadiusProfile{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testRadiusProfileResourceName,
			Namespace: testNamespace,
			UID:       types.UID("radius-profile-uid"),
		},
		Spec: unifiv1alpha1.UnifiRadiusProfileSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Name:    testRadiusProfileUpstreamName,
		},
	}
}

// matchingRadiusProfile returns the upstream profile that equals what
// newRadiusProfile adopts, as the console would report it.
func matchingRadiusProfile() unifi.RadiusProfile {
	return unifi.RadiusProfile{
		ID:       testRadiusProfileUUID,
		Name:     testRadiusProfileUpstreamName,
		Metadata: unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined},
	}
}

// newRadiusProfileFakeClient returns a fake client that counts writes to the
// profile status subresource and to object metadata, so idempotency can be
// asserted.
func newRadiusProfileFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiRadiusProfile{},
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

// getRadiusProfile loads a reconciled UnifiRadiusProfile fixture by name.
func getRadiusProfile(t *testing.T, c client.Client, name string) *unifiv1alpha1.UnifiRadiusProfile {
	t.Helper()
	got := &unifiv1alpha1.UnifiRadiusProfile{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get radius profile after reconcile: %v", err)
	}
	return got
}

// radiusProfileMutations filters recorded calls down to the ones that would
// change upstream profile state. The adoption kind must perform none of them.
func radiusProfileMutations(calls []string) []string {
	mutations := []string{"CreateRadiusProfile", "UpdateRadiusProfile", "DeleteRadiusProfile"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiRadiusProfileReconcileAdopts(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{radiusProfiles: []unifi.RadiusProfile{matchingRadiusProfile()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newRadiusProfileFakeClient(t,
		newRadiusProfile(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getRadiusProfile(t, fakeClient, testRadiusProfileResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.RadiusProfileID != testRadiusProfileUUID {
		t.Errorf("radiusProfileID = %q, want %q", got.Status.RadiusProfileID, testRadiusProfileUUID)
	}
	if got.Status.Origin != firewallZoneOriginUserDefined {
		t.Errorf("origin = %q, want %q", got.Status.Origin, firewallZoneOriginUserDefined)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("radius profile is not owned by its UnifiSite")
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("finalizers = %v, want none: adoption has no upstream effect", got.Finalizers)
	}

	calls := stub.called()
	if !slices.Contains(calls, "ListRadiusProfiles") {
		t.Errorf("upstream calls = %v, want ListRadiusProfiles", calls)
	}
	if mutations := radiusProfileMutations(calls); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none for an adoption kind", mutations)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference)", *metadataWrites)
	}
}

func TestUnifiRadiusProfileReconcileUnknownProfileFailsClosed(t *testing.T) {
	t.Parallel()

	profile := newRadiusProfile()
	profile.Status.RadiusProfileID = "stale-uuid"
	profile.Status.Origin = firewallZoneOriginUserDefined

	stub := &stubUnifiClient{radiusProfiles: []unifi.RadiusProfile{{ID: testOtherNamespace, Name: testOtherObjectName}}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t,
		profile, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != radiusProfileDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, radiusProfileDependencyRequeueAfter)
	}
	if *statusWrites != 1 {
		t.Fatalf("status writes = %d, want 1", *statusWrites)
	}

	got := getRadiusProfile(t, fakeClient, testRadiusProfileResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonProfileNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonProfileNotFound)
	}
	if got.Status.RadiusProfileID != "" {
		t.Errorf("radiusProfileID = %q, want it cleared so no stale UUID is consumed", got.Status.RadiusProfileID)
	}
	if mutations := radiusProfileMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none", mutations)
	}
}

func TestUnifiRadiusProfileReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	profile := newRadiusProfile()
	profile.Status.RadiusProfileID = testRadiusProfileUUID

	stub := &stubUnifiClient{radiusProfiles: []unifi.RadiusProfile{matchingRadiusProfile()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newRadiusProfileFakeClient(t,
		profile, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
}

func TestUnifiRadiusProfileReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	tests := []struct {
		name        string
		profile     *unifiv1alpha1.UnifiRadiusProfile
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        testNameSiteMissing,
			profile:     newRadiusProfile(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteBeingDeleted,
			profile:     newRadiusProfile(),
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteNotAdopted,
			profile:     newRadiusProfile(),
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        testNameControllerMissing,
			profile:     newRadiusProfile(),
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       testNameVersionBelowMinimum,
			profile:    newRadiusProfile(),
			site:       ownedSite(),
			controller: readyController("10.1.77"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       testNameVersionUnparseable,
			profile:    newRadiusProfile(),
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        testNameVersionNotReported,
			profile:     newRadiusProfile(),
			site:        ownedSite(),
			controller:  newUnifiController(nil),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []client.Object{tt.profile}
			if tt.site != nil {
				objects = append(objects, tt.site)
			}
			if tt.controller != nil {
				objects = append(objects, tt.controller, apiKeySecret())
			}

			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t, objects...)
			reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != radiusProfileDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, radiusProfileDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getRadiusProfile(t, fakeClient, testRadiusProfileResourceName)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := radiusProfileMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

// TestUnifiRadiusProfileForSiteEnqueuesReferencingProfiles verifies the
// dependency watch map function: only profiles in the changed site's namespace
// whose spec.siteRef names that site are enqueued.
func TestUnifiRadiusProfileForSiteEnqueuesReferencingProfiles(t *testing.T) {
	t.Parallel()

	referencing := newRadiusProfile()
	referencing.Name = testReferencingName
	otherSite := newRadiusProfile()
	otherSite.Name = "other-site-profile"
	otherSite.Spec.SiteRef.Name = testOtherSite
	otherNamespace := newRadiusProfile()
	otherNamespace.Name = "other-namespace"
	otherNamespace.Namespace = testOtherNamespace

	fakeClient, _, _ := newRadiusProfileFakeClient(t, referencing, otherSite, otherNamespace)
	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme()}

	requests := reconciler.profilesForSite(context.Background(), ownedSite())
	if len(requests) != 1 {
		t.Fatalf("requests = %d (%v), want 1", len(requests), requests)
	}
	if requests[0].Name != testReferencingName || requests[0].Namespace != testNamespace {
		t.Errorf("request = %v, want %s/referencing", requests[0].NamespacedName, testNamespace)
	}
}
