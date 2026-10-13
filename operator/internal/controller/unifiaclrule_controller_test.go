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
	testAclRuleResourceName  = "acl-rule"
	testAclRuleUpstreamName  = "Block guest"
	testAclRuleUUID          = "acl-rule-uuid-1"
	testEnforcingDeviceTag   = "core-switch"
	testEnforcingDeviceTagID = "tag-uuid-1"
	testEnforcingDeviceID    = "device-uuid-1"
	testDerivedOrigin        = "DERIVED"
)

// newAclRule returns a minimal IPV4 UnifiAclRule fixture with a UID.
func newAclRule() *unifiv1alpha1.UnifiAclRule {
	return &unifiv1alpha1.UnifiAclRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testAclRuleResourceName,
			Namespace: testNamespace,
			UID:       types.UID("acl-rule-uid"),
		},
		Spec: unifiv1alpha1.UnifiAclRuleSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Name:    testAclRuleUpstreamName,
			Action:  testActionBlock,
			Type:    testTypeIPv4,
			Source: unifiv1alpha1.AclRuleEndpoint{
				Type:     testEndpointNetworks,
				Networks: &unifiv1alpha1.AclNetworksEndpoint{NetworkRefs: []unifiv1alpha1.CoreRef{{Name: testNetworkResourceName}}},
			},
			Destination: unifiv1alpha1.AclRuleEndpoint{
				Type:  testEndpointPorts,
				Ports: &unifiv1alpha1.AclPortsEndpoint{Ports: []unifiv1alpha1.AclPort{80, 443}},
			},
		},
	}
}

// newMacAclRule returns an IP-address-free MAC rule with a networkRef.
func newMacAclRule() *unifiv1alpha1.UnifiAclRule {
	rule := newAclRule()
	rule.Spec.Type = testTypeMac
	rule.Spec.Action = testActionAllow
	mac := func() unifiv1alpha1.AclRuleEndpoint {
		return unifiv1alpha1.AclRuleEndpoint{
			Type:         "MAC_ADDRESSES",
			MacAddresses: &unifiv1alpha1.AclMacAddressesEndpoint{MacAddresses: []string{"aa:bb:cc:dd:ee:ff"}},
		}
	}
	rule.Spec.Source = mac()
	rule.Spec.Destination = mac()
	rule.Spec.NetworkRef = &unifiv1alpha1.CoreRef{Name: testNetworkResourceName}
	return rule
}

// enforcingDeviceTag returns the read-only device tag the enforcing selector
// resolves against.
func enforcingDeviceTag() unifi.DeviceTag {
	return unifi.DeviceTag{ID: testEnforcingDeviceTagID, Name: testEnforcingDeviceTag, DeviceIDs: []string{testEnforcingDeviceID}}
}

// matchingAclRule returns the upstream rule that equals what newAclRule desires.
func matchingAclRule() unifi.AclRule {
	return unifi.AclRule{
		ID:       testAclRuleUUID,
		Metadata: unifi.NetworkMetadata{Origin: aclRuleOriginUserDefined},
		AclRuleRequest: unifi.AclRuleRequest{
			Type:        testTypeIPv4,
			Name:        testAclRuleUpstreamName,
			Enabled:     true,
			Action:      testActionBlock,
			Source:      unifi.AclRuleEndpoint{Type: testEndpointNetworks, NetworkIDs: []string{testUpstreamNetworkID}},
			Destination: unifi.AclRuleEndpoint{Type: testEndpointPorts, PortFilter: []int32{80, 443}},
		},
	}
}

// newAclRuleFakeClient returns a fake client that counts writes to the rule
// status subresource and to object metadata.
func newAclRuleFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiAclRule{},
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

// getAclRule loads a reconciled fixture by name.
func getAclRule(t *testing.T, c client.Client, name string) *unifiv1alpha1.UnifiAclRule {
	t.Helper()
	got := &unifiv1alpha1.UnifiAclRule{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: name, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get ACL rule after reconcile: %v", err)
	}
	return got
}

// aclRuleMutations filters recorded calls down to the ones that change upstream
// rule state.
func aclRuleMutations(calls []string) []string {
	mutations := []string{"CreateAclRule", "UpdateAclRule", "DeleteAclRule"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiAclRuleReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{createdAclRule: unifi.AclRule{ID: testAclRuleUUID}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newAclRuleFakeClient(t,
		newAclRule(), memberNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclRuleResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getAclRule(t, fakeClient, testAclRuleResourceName)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.RuleID != testAclRuleUUID {
		t.Errorf("ruleID = %q, want %q", got.Status.RuleID, testAclRuleUUID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("ACL rule is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiAclRuleFinalizer) {
		t.Errorf("ACL rule finalizer was not added")
	}

	if len(stub.aclRuleCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.aclRuleCreateRequests))
	}
	req := stub.aclRuleCreateRequests[0]
	if !slices.Equal(req.Source.NetworkIDs, []string{testUpstreamNetworkID}) {
		t.Errorf("source networkIds = %v, want [%s]", req.Source.NetworkIDs, testUpstreamNetworkID)
	}
	if !slices.Equal(req.Destination.PortFilter, []int32{80, 443}) {
		t.Errorf("destination ports = %v, want [80 443]", req.Destination.PortFilter)
	}
	if req.EnforcingDeviceFilter != nil {
		t.Errorf("enforcingDeviceFilter = %+v, want nil when no tag selector is set", req.EnforcingDeviceFilter)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiAclRuleReconcileResolvesEnforcingDeviceTags(t *testing.T) {
	t.Parallel()

	rule := newAclRule()
	rule.Spec.EnforcingDeviceTags = []unifiv1alpha1.DeviceTagSelector{{Name: testEnforcingDeviceTag}}

	stub := &stubUnifiClient{
		createdAclRule: unifi.AclRule{ID: testAclRuleUUID},
		deviceTags:     []unifi.DeviceTag{enforcingDeviceTag()},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newAclRuleFakeClient(t,
		rule, memberNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclRuleResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if len(stub.aclRuleCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.aclRuleCreateRequests))
	}
	filter := stub.aclRuleCreateRequests[0].EnforcingDeviceFilter
	if filter == nil || filter.Type != aclDeviceFilterType || !slices.Equal(filter.DeviceIDs, []string{testEnforcingDeviceID}) {
		t.Errorf("enforcingDeviceFilter = %+v, want DEVICES [%s]", filter, testEnforcingDeviceID)
	}
}

func TestUnifiAclRuleReconcileUnknownDeviceTagFailsClosed(t *testing.T) {
	t.Parallel()

	rule := newAclRule()
	rule.Spec.EnforcingDeviceTags = []unifiv1alpha1.DeviceTagSelector{{Name: "missing-tag"}}

	stub := &stubUnifiClient{deviceTags: []unifi.DeviceTag{enforcingDeviceTag()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newAclRuleFakeClient(t,
		rule, memberNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclRuleResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	cond := requireReady(t, getAclRule(t, fakeClient, testAclRuleResourceName).Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonDeviceTagNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonDeviceTagNotFound)
	}
	if mutations := aclRuleMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none when the enforcing tag is unknown", mutations)
	}
}

func TestUnifiAclRuleReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	rule := newAclRule()
	rule.Status.RuleID = testAclRuleUUID

	stub := &stubUnifiClient{aclRules: []unifi.AclRule{matchingAclRule()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newAclRuleFakeClient(t,
		rule, memberNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclRuleResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if mutations := aclRuleMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none on an unchanged object", mutations)
	}
}

func TestUnifiAclRuleReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	tests := []struct {
		name        string
		rule        *unifiv1alpha1.UnifiAclRule
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		network     *unifiv1alpha1.UnifiNetwork
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        testNameSiteMissing,
			rule:        newAclRule(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameSiteNotAdopted,
			rule:        newAclRule(),
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        testNameSiteBeingDeleted,
			rule:        newAclRule(),
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        testNameControllerMissing,
			rule:        newAclRule(),
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       testNameVersionBelowMinimum,
			rule:       newAclRule(),
			site:       ownedSite(),
			controller: readyController("10.1.83"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       testNameVersionUnparseable,
			rule:       newAclRule(),
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        "network reference missing",
			rule:        newAclRule(),
			site:        ownedSite(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonNetworkRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "MAC networkRef missing",
			rule:        newMacAclRule(),
			site:        ownedSite(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonNetworkRefNotFound,
			wantRequeue: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []client.Object{tt.rule}
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
			fakeClient, statusWrites, _ := newAclRuleFakeClient(t, objects...)
			reconciler := &UnifiAclRuleReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclRuleResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != aclRuleDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, aclRuleDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getAclRule(t, fakeClient, testAclRuleResourceName)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := aclRuleMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiAclRuleFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	rule := newAclRule()
	rule.Finalizers = []string{unifiv1alpha1.UnifiAclRuleFinalizer}
	rule.Status.RuleID = testAclRuleUUID

	stub := &stubUnifiClient{aclRules: []unifi.AclRule{matchingAclRule()}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newAclRuleFakeClient(t,
		rule, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiAclRuleReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), rule); err != nil {
		t.Fatalf("delete rule: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testAclRuleResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedAclRuleSiteID != testUpstreamSiteID || stub.deletedAclRuleID != testAclRuleUUID {
		t.Errorf("DeleteAclRule(%q, %q), want (%q, %q)",
			stub.deletedAclRuleSiteID, stub.deletedAclRuleID, testUpstreamSiteID, testAclRuleUUID)
	}
	if !slices.Contains(stub.called(), "DeleteAclRule") {
		t.Errorf("upstream calls = %v, want DeleteAclRule", stub.called())
	}

	got := &unifiv1alpha1.UnifiAclRule{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testAclRuleResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get rule after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiAclRuleFinalizer) {
		t.Errorf("ACL rule finalizer was not removed after upstream deletion")
	}
}
