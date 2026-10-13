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
	testAclOrderingResourceName = "acl-ordering"
	testAclDriftRuleUUID        = "acl-rule-uuid-drift"
	testAclDriftRuleName        = "drifter"
	testDerivedRuleUUID         = "acl-rule-uuid-derived"
)

// newAclRuleOrdering returns a minimal UnifiAclRuleOrdering fixture.
func newAclRuleOrdering() *unifiv1alpha1.UnifiAclRuleOrdering {
	return &unifiv1alpha1.UnifiAclRuleOrdering{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAclOrderingResourceName,
			Namespace: testNamespace,
			UID:       types.UID("acl-ordering-uid"),
		},
		Spec: unifiv1alpha1.UnifiAclRuleOrderingSpec{
			SiteRef:         unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			OrderedAclRules: []unifiv1alpha1.CoreRef{{Name: testAclRuleResourceName}},
		},
	}
}

// aclRuleWithID returns a UnifiAclRule fixture that has reported its upstream
// rule UUID.
func aclRuleWithID(name, upstreamID string) *unifiv1alpha1.UnifiAclRule {
	rule := newAclRule()
	rule.Name = name
	rule.UID = types.UID(name + "-uid")
	rule.Status.RuleID = upstreamID
	return rule
}

// upstreamAclRules returns the site's upstream rules: the ordered user rule, an
// extra user rule (drift), and a derived rule (anchored, not drift).
func upstreamAclRules() []unifi.AclRule {
	entry := func(id, name, origin string) unifi.AclRule {
		return unifi.AclRule{ID: id, Metadata: unifi.NetworkMetadata{Origin: origin}, AclRuleRequest: unifi.AclRuleRequest{Name: name}}
	}
	return []unifi.AclRule{
		entry(testAclRuleUUID, testAclRuleUpstreamName, aclRuleOriginUserDefined),
		entry(testAclDriftRuleUUID, testAclDriftRuleName, aclRuleOriginUserDefined),
		entry(testDerivedRuleUUID, "derived", testDerivedOrigin),
	}
}

// newAclOrderingFakeClient returns a fake client that counts writes to the
// ordering status subresource and to object metadata.
func newAclOrderingFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiAclRuleOrdering{},
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

// getAclOrdering loads a reconciled ordering fixture by name.
func getAclOrdering(t *testing.T, c client.Client, name string) *unifiv1alpha1.UnifiAclRuleOrdering {
	t.Helper()
	got := &unifiv1alpha1.UnifiAclRuleOrdering{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get ACL rule ordering after reconcile: %v", err)
	}
	return got
}

func TestUnifiAclRuleOrderingReconcileApplies(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{aclRules: upstreamAclRules()}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newAclOrderingFakeClient(t,
		newAclRuleOrdering(),
		aclRuleWithID(testAclRuleResourceName, testAclRuleUUID),
		ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclOrderingResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getAclOrdering(t, fakeClient, testAclOrderingResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if !slices.Equal(got.Status.Drift, []string{testAclDriftRuleName}) {
		t.Errorf("drift = %v, want [%s] (derived rules are anchored)", got.Status.Drift, testAclDriftRuleName)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("ACL rule ordering is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiAclRuleOrderingFinalizer) {
		t.Errorf("ACL rule ordering finalizer was not added")
	}

	if len(stub.aclRuleOrderingUpdateReqs) != 1 {
		t.Fatalf("ordering update requests = %d, want 1", len(stub.aclRuleOrderingUpdateReqs))
	}
	if ids := stub.aclRuleOrderingUpdateReqs[0].OrderedAclRuleIDs; !slices.Equal(ids, []string{testAclRuleUUID}) {
		t.Errorf("orderedAclRuleIds = %v, want [%s]", ids, testAclRuleUUID)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiAclRuleOrderingReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		aclRules:        upstreamAclRules(),
		aclRuleOrdering: unifi.AclRuleOrdering{OrderedAclRuleIDs: []string{testAclRuleUUID}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newAclOrderingFakeClient(t,
		newAclRuleOrdering(),
		aclRuleWithID(testAclRuleResourceName, testAclRuleUUID),
		ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclOrderingResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if len(stub.aclRuleOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none when the upstream order matches",
			len(stub.aclRuleOrderingUpdateReqs))
	}
}

func TestUnifiAclRuleOrderingDuplicateConflict(t *testing.T) {
	t.Parallel()

	second := newAclRuleOrdering()
	second.Name = "acl-ordering-peer"
	second.UID = types.UID("acl-ordering-peer-uid")

	stub := &stubUnifiClient{aclRules: upstreamAclRules()}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newAclOrderingFakeClient(t,
		newAclRuleOrdering(), second,
		aclRuleWithID(testAclRuleResourceName, testAclRuleUUID),
		ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for _, name := range []string{testAclOrderingResourceName, "acl-ordering-peer"} {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(name)); err != nil {
			t.Fatalf("reconcile %s: %v", name, err)
		}
		cond := requireReady(t, getAclOrdering(t, fakeClient, name).Status.Conditions)
		if cond.Status != metav1.ConditionFalse || cond.Reason != reasonSiteOrderingConflict {
			t.Errorf("ordering %s Ready = %s/%s, want False/%s", name, cond.Status, cond.Reason, reasonSiteOrderingConflict)
		}
	}
	if len(stub.aclRuleOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none while the site ordering is contested",
			len(stub.aclRuleOrderingUpdateReqs))
	}
}

func TestUnifiAclRuleOrderingUnresolvedRuleFailsClosed(t *testing.T) {
	t.Parallel()

	ordering := newAclRuleOrdering()
	ordering.Spec.OrderedAclRules = []unifiv1alpha1.CoreRef{{Name: "missing-rule"}}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newAclOrderingFakeClient(t,
		ordering, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleOrderingReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclOrderingResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != aclOrderingDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, aclOrderingDependencyRequeueAfter)
	}

	cond := requireReady(t, getAclOrdering(t, fakeClient, testAclOrderingResourceName).Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonAclRuleRefNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonAclRuleRefNotFound)
	}
	if len(stub.aclRuleOrderingUpdateReqs) != 0 {
		t.Errorf("ordering update requests = %d, want none when a referenced rule is unresolved",
			len(stub.aclRuleOrderingUpdateReqs))
	}
}
