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
	testDnsPolicyResourceName = "dns-policy"
	testDnsPolicyUUID         = "dns-policy-uuid-1"
	testDnsUpstreamDomain     = "example.com"
)

// newDnsPolicy returns a minimal A_RECORD UnifiDnsPolicy fixture.
func newDnsPolicy() *unifiv1alpha1.UnifiDnsPolicy {
	return &unifiv1alpha1.UnifiDnsPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDnsPolicyResourceName,
			Namespace: testNamespace,
			UID:       types.UID("dns-policy-uid"),
		},
		Spec: unifiv1alpha1.UnifiDnsPolicySpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Type:    testDnsTypeARecord,
			Domain:  testDnsUpstreamDomain,
			ARecord: &unifiv1alpha1.DnsARecord{IPv4Address: testIPv4Address, TTLSeconds: 14400},
		},
	}
}

// matchingDnsPolicy returns the upstream record that equals what newDnsPolicy
// desires.
func matchingDnsPolicy() unifi.DnsPolicy {
	ttl := int32(14400)
	return unifi.DnsPolicy{
		ID:       testDnsPolicyUUID,
		Metadata: unifi.NetworkMetadata{Origin: dnsPolicyOriginUserDefined},
		DnsPolicyRequest: unifi.DnsPolicyRequest{
			Type: testDnsTypeARecord, Domain: testDnsUpstreamDomain, Enabled: true,
			IPv4Address: testIPv4Address, TTLSeconds: &ttl,
		},
	}
}

// newDnsPolicyFakeClient returns a fake client that counts writes to the policy
// status subresource and to object metadata.
func newDnsPolicyFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiDnsPolicy{},
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

// getDnsPolicy loads a reconciled fixture by name.
func getDnsPolicy(t *testing.T, c client.Client, name string) *unifiv1alpha1.UnifiDnsPolicy {
	t.Helper()
	got := &unifiv1alpha1.UnifiDnsPolicy{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get DNS policy after reconcile: %v", err)
	}
	return got
}

// dnsPolicyMutations filters recorded calls down to the ones that change
// upstream record state.
func dnsPolicyMutations(calls []string) []string {
	mutations := []string{"CreateDnsPolicy", "UpdateDnsPolicy", "DeleteDnsPolicy"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiDnsPolicyReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{createdDnsPolicy: unifi.DnsPolicy{ID: testDnsPolicyUUID}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newDnsPolicyFakeClient(t,
		newDnsPolicy(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiDnsPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testDnsPolicyResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getDnsPolicy(t, fakeClient, testDnsPolicyResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.PolicyID != testDnsPolicyUUID {
		t.Errorf("policyID = %q, want %q", got.Status.PolicyID, testDnsPolicyUUID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("DNS policy is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiDnsPolicyFinalizer) {
		t.Errorf("DNS policy finalizer was not added")
	}

	if len(stub.dnsPolicyCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.dnsPolicyCreateRequests))
	}
	req := stub.dnsPolicyCreateRequests[0]
	if req.Type != testDnsTypeARecord || req.IPv4Address != testIPv4Address || req.TTLSeconds == nil || *req.TTLSeconds != 14400 {
		t.Errorf("create request = %+v, want the A-record fields", req)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiDnsPolicyReconcileMapsSrvVariant(t *testing.T) {
	t.Parallel()

	policy := newDnsPolicy()
	policy.Spec.Type = testDnsTypeSrvRecord
	policy.Spec.ARecord = nil
	policy.Spec.SrvRecord = &unifiv1alpha1.DnsSrvRecord{
		Port: 389, Priority: 10, Protocol: testSrvProtocol, ServerDomain: testSrvServerDomain, Service: testSrvService, Weight: 5,
	}

	stub := &stubUnifiClient{createdDnsPolicy: unifi.DnsPolicy{ID: testDnsPolicyUUID}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newDnsPolicyFakeClient(t,
		policy, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiDnsPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testDnsPolicyResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if len(stub.dnsPolicyCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.dnsPolicyCreateRequests))
	}
	req := stub.dnsPolicyCreateRequests[0]
	if req.Type != testDnsTypeSrvRecord || req.Port == nil || *req.Port != 389 || req.Priority == nil || *req.Priority != 10 ||
		req.Protocol != testSrvProtocol || req.ServerDomain != testSrvServerDomain || req.Service != testSrvService ||
		req.Weight == nil || *req.Weight != 5 {
		t.Errorf("SRV create request = %+v, want the mapped variant", req)
	}
}

func TestUnifiDnsPolicyReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	policy := newDnsPolicy()
	policy.Status.PolicyID = testDnsPolicyUUID

	stub := &stubUnifiClient{dnsPolicies: []unifi.DnsPolicy{matchingDnsPolicy()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newDnsPolicyFakeClient(t,
		policy, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiDnsPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testDnsPolicyResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if mutations := dnsPolicyMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none on an unchanged object", mutations)
	}
}

func TestUnifiDnsPolicyReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	missingVariant := newDnsPolicy()
	missingVariant.Spec.ARecord = nil

	tests := []struct {
		name        string
		policy      *unifiv1alpha1.UnifiDnsPolicy
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        testNameSiteMissing,
			policy:      newDnsPolicy(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteNotAdopted,
			policy:      newDnsPolicy(),
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        testNameSiteBeingDeleted,
			policy:      newDnsPolicy(),
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameControllerMissing,
			policy:      newDnsPolicy(),
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       testNameVersionBelowMinimum,
			policy:     newDnsPolicy(),
			site:       ownedSite(),
			controller: readyController("10.1.83"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       testNameVersionUnparseable,
			policy:     newDnsPolicy(),
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        testNameVersionNotReported,
			policy:      newDnsPolicy(),
			site:        ownedSite(),
			controller:  newUnifiController(nil),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:       "missing variant value",
			policy:     missingVariant,
			site:       ownedSite(),
			controller: readyController(testAppVersion),
			wantReason: reasonInvalidSpec,
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

			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newDnsPolicyFakeClient(t, objects...)
			reconciler := &UnifiDnsPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testDnsPolicyResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != dnsPolicyDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, dnsPolicyDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getDnsPolicy(t, fakeClient, testDnsPolicyResourceName)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := dnsPolicyMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiDnsPolicyFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	policy := newDnsPolicy()
	policy.Finalizers = []string{unifiv1alpha1.UnifiDnsPolicyFinalizer}
	policy.Status.PolicyID = testDnsPolicyUUID

	stub := &stubUnifiClient{dnsPolicies: []unifi.DnsPolicy{matchingDnsPolicy()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newDnsPolicyFakeClient(t,
		policy, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiDnsPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), policy); err != nil {
		t.Fatalf("delete policy: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testDnsPolicyResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedDnsPolicySiteID != testUpstreamSiteID || stub.deletedDnsPolicyID != testDnsPolicyUUID {
		t.Errorf("DeleteDnsPolicy(%q, %q), want (%q, %q)",
			stub.deletedDnsPolicySiteID, stub.deletedDnsPolicyID, testUpstreamSiteID, testDnsPolicyUUID)
	}
	if !slices.Contains(stub.called(), "DeleteDnsPolicy") {
		t.Errorf("upstream calls = %v, want DeleteDnsPolicy", stub.called())
	}

	got := &unifiv1alpha1.UnifiDnsPolicy{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testDnsPolicyResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get policy after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiDnsPolicyFinalizer) {
		t.Errorf("DNS policy finalizer was not removed after upstream deletion")
	}
}
