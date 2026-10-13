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
	"encoding/json"
	"slices"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	testSwitchLagResourceName    = "switching-lag"
	testSwitchLagUUID            = "lag-uuid-1"
	testSwitchLagDeviceTag       = "core-switch"
	testSwitchDomainResourceName = "switching-domain"
	testSwitchDomainUUID         = "domain-uuid-1"
	testSwitchDomainName         = "Core MC-LAG"
	testSwitchStackResourceName  = "switching-stack"
	testSwitchStackUUID          = "stack-uuid-1"
	testSwitchStackName          = "Core Stack"
	testSwitchMemberDeviceID     = "device-uuid-1"
	testSwitchMemberSecondDevice = "device-uuid-2"
)

// switchingDeviceTag returns the read-only device tag the LAG member selector
// resolves against.
func switchingDeviceTag() unifi.DeviceTag {
	return unifi.DeviceTag{ID: "tag-uuid-1", Name: testSwitchLagDeviceTag, DeviceIDs: []string{testSwitchMemberDeviceID}}
}

// newSwitchingLag returns a LAG fixture using the typed composite selector.
func newSwitchingLag() *unifiv1alpha1.UnifiSwitchingLag {
	return &unifiv1alpha1.UnifiSwitchingLag{
		ObjectMeta: metav1.ObjectMeta{Name: testSwitchLagResourceName, Namespace: testNamespace, UID: types.UID("lag-uid")},
		Spec: unifiv1alpha1.UnifiSwitchingLagSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Type:    testLagTypeLocal,
			Members: []unifiv1alpha1.SwitchingLagMemberSelector{{
				DeviceTag: unifiv1alpha1.DeviceTagSelector{Name: testSwitchLagDeviceTag},
				PortIdxs:  []int32{1, 2},
			}},
		},
	}
}

// matchingUpstreamLag returns the upstream LAG that matches newSwitchingLag's
// selector.
func matchingUpstreamLag() unifi.Lag {
	return unifi.Lag{
		ID: testSwitchLagUUID, Type: testLagTypeLocal,
		Metadata: unifi.NetworkMetadata{Origin: aclRuleOriginUserDefined},
		Members:  []unifi.LagMember{{DeviceID: testSwitchMemberDeviceID, PortIdxs: []int32{1, 2}}},
	}
}

func newSwitchingMcLagDomain() *unifiv1alpha1.UnifiSwitchingMcLagDomain {
	return &unifiv1alpha1.UnifiSwitchingMcLagDomain{
		ObjectMeta: metav1.ObjectMeta{Name: testSwitchDomainResourceName, Namespace: testNamespace, UID: types.UID("domain-uid")},
		Spec:       unifiv1alpha1.UnifiSwitchingMcLagDomainSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName}, Name: testSwitchDomainName},
	}
}

func newSwitchingSwitchStack() *unifiv1alpha1.UnifiSwitchingSwitchStack {
	return &unifiv1alpha1.UnifiSwitchingSwitchStack{
		ObjectMeta: metav1.ObjectMeta{Name: testSwitchStackResourceName, Namespace: testNamespace, UID: types.UID("stack-uid")},
		Spec:       unifiv1alpha1.UnifiSwitchingSwitchStackSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName}, Name: testSwitchStackName},
	}
}

// newSwitchingFakeClient returns a fake client that counts writes to the
// switching status subresources.
func newSwitchingFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int) {
	t.Helper()
	statusWrites := 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiSwitchingLag{},
			&unifiv1alpha1.UnifiSwitchingMcLagDomain{},
			&unifiv1alpha1.UnifiSwitchingSwitchStack{},
		).
		WithInterceptorFuncs(interceptor.Funcs{
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
	return builder.Build(), &statusWrites
}

func TestUnifiSwitchingLagAdopts(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		lags:       []unifi.Lag{matchingUpstreamLag()},
		deviceTags: []unifi.DeviceTag{switchingDeviceTag()},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites := newSwitchingFakeClient(t,
		newSwitchingLag(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSwitchingLagReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSwitchLagResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := &unifiv1alpha1.UnifiSwitchingLag{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testSwitchLagResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get lag: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.LagID != testSwitchLagUUID || got.Status.Type != testLagTypeLocal {
		t.Errorf("status = %+v, want lagID %q type LOCAL", got.Status, testSwitchLagUUID)
	}
	if len(got.Status.Members) != 1 || got.Status.Members[0].DeviceID != testSwitchMemberDeviceID {
		t.Errorf("members = %+v, want the observed member", got.Status.Members)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("lag is not owned by its UnifiSite")
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("finalizers = %v, want none: adoption has no upstream effect", got.Finalizers)
	}
	if slices.Contains(stub.called(), "CreateLag") || slices.Contains(stub.called(), "UpdateLag") || slices.Contains(stub.called(), "DeleteLag") {
		t.Errorf("upstream calls = %v, want no mutation", stub.called())
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
}

func TestUnifiSwitchingLagSelectorNoMatchFailsClosed(t *testing.T) {
	t.Parallel()

	// The upstream LAG exists but its member ports do not match the selector.
	nonMatching := matchingUpstreamLag()
	nonMatching.Members[0].PortIdxs = []int32{3, 4}
	stub := &stubUnifiClient{
		lags:       []unifi.Lag{nonMatching},
		deviceTags: []unifi.DeviceTag{switchingDeviceTag()},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newSwitchingFakeClient(t,
		newSwitchingLag(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSwitchingLagReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSwitchLagResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != switchingLagDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, switchingLagDependencyRequeueAfter)
	}

	got := &unifiv1alpha1.UnifiSwitchingLag{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testSwitchLagResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get lag: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonLagSelectorNoMatch {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonLagSelectorNoMatch)
	}
	if got.Status.LagID != "" {
		t.Errorf("lagID = %q, want it cleared so no stale UUID is recorded", got.Status.LagID)
	}
}

func TestUnifiSwitchingLagSelectorAmbiguousFailsClosed(t *testing.T) {
	t.Parallel()

	duplicate := matchingUpstreamLag()
	duplicate.ID = "lag-uuid-2"
	stub := &stubUnifiClient{
		lags:       []unifi.Lag{matchingUpstreamLag(), duplicate},
		deviceTags: []unifi.DeviceTag{switchingDeviceTag()},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newSwitchingFakeClient(t,
		newSwitchingLag(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSwitchingLagReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSwitchLagResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0: an ambiguous selector needs a user change to converge", result.RequeueAfter)
	}

	got := &unifiv1alpha1.UnifiSwitchingLag{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testSwitchLagResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get lag: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonLagSelectorAmbiguous {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonLagSelectorAmbiguous)
	}
	if got.Status.LagID != "" {
		t.Errorf("lagID = %q, want it cleared", got.Status.LagID)
	}
}

func TestUnifiSwitchingLagUnknownDeviceTagFailsClosed(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		lags:       []unifi.Lag{matchingUpstreamLag()},
		deviceTags: []unifi.DeviceTag{switchingDeviceTag()},
	}
	lag := newSwitchingLag()
	lag.Spec.Members[0].DeviceTag.Name = "missing-tag"
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newSwitchingFakeClient(t,
		lag, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSwitchingLagReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSwitchLagResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := &unifiv1alpha1.UnifiSwitchingLag{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testSwitchLagResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get lag: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonDeviceTagNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonDeviceTagNotFound)
	}
}

func TestUnifiSwitchingMcLagDomainAdopts(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{mcLagDomains: []unifi.McLagDomain{{
		ID: testSwitchDomainUUID, Name: testSwitchDomainName,
		Metadata: unifi.NetworkMetadata{Origin: aclRuleOriginUserDefined},
		Peers:    []unifi.McLagPeer{{DeviceID: testSwitchMemberDeviceID, LinkPortIdxs: []int32{1}, Role: "TOP"}},
		Lags:     []unifi.McLagLocal{{ID: "lag-1", Members: []unifi.LagMember{{DeviceID: testSwitchMemberDeviceID, PortIdxs: []int32{1, 2}}}}},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newSwitchingFakeClient(t,
		newSwitchingMcLagDomain(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSwitchingMcLagDomainReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSwitchDomainResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := &unifiv1alpha1.UnifiSwitchingMcLagDomain{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testSwitchDomainResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get domain: %v", err)
	}
	if cond := requireReady(t, got.Status.Conditions); cond.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %s/%s, want True", cond.Status, cond.Reason)
	}
	if got.Status.DomainID != testSwitchDomainUUID {
		t.Errorf("domainID = %q, want %q", got.Status.DomainID, testSwitchDomainUUID)
	}
	if len(got.Status.Peers) != 1 || got.Status.Peers[0].Role != "TOP" {
		t.Errorf("peers = %+v, want the observed peer", got.Status.Peers)
	}
	if len(got.Status.Members) != 1 || got.Status.Members[0].DeviceID != testSwitchMemberDeviceID {
		t.Errorf("members = %+v, want the aggregated LAG members", got.Status.Members)
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("finalizers = %v, want none", got.Finalizers)
	}
}

func TestUnifiSwitchingSwitchStackAdopts(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{switchStacks: []unifi.SwitchStack{{
		ID: testSwitchStackUUID, Name: testSwitchStackName,
		Metadata: unifi.NetworkMetadata{Origin: aclRuleOriginUserDefined},
		Members:  []unifi.SwitchStackMember{{DeviceID: testSwitchMemberDeviceID}, {DeviceID: testSwitchMemberSecondDevice}},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newSwitchingFakeClient(t,
		newSwitchingSwitchStack(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSwitchingSwitchStackReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSwitchStackResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := &unifiv1alpha1.UnifiSwitchingSwitchStack{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testSwitchStackResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get stack: %v", err)
	}
	if cond := requireReady(t, got.Status.Conditions); cond.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %s/%s, want True", cond.Status, cond.Reason)
	}
	if got.Status.StackID != testSwitchStackUUID || len(got.Status.Members) != 2 {
		t.Errorf("status = %+v, want stackID %q with two members", got.Status, testSwitchStackUUID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("switch stack is not owned by its UnifiSite")
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("finalizers = %v, want none", got.Finalizers)
	}
}

func TestUnifiSwitchingUnknownObjectFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		objects []client.Object
		stub    *stubUnifiClient
	}{
		{
			name:    "lag selector matches nothing",
			objects: []client.Object{newSwitchingLag(), ownedSite(), readyController(testAppVersion), apiKeySecret()},
			stub: &stubUnifiClient{
				deviceTags: []unifi.DeviceTag{switchingDeviceTag()},
				lags:       []unifi.Lag{{ID: testOtherNamespace, Type: testLagTypeLocal, Members: []unifi.LagMember{{DeviceID: "other-device", PortIdxs: []int32{9}}}}},
			},
		},
		{
			name:    "mc-lag domain missing upstream",
			objects: []client.Object{newSwitchingMcLagDomain(), ownedSite(), readyController(testAppVersion), apiKeySecret()},
			stub:    &stubUnifiClient{mcLagDomains: []unifi.McLagDomain{{ID: testOtherNamespace, Name: testOtherObjectName}}},
		},
		{
			name:    "switch stack missing upstream",
			objects: []client.Object{newSwitchingSwitchStack(), ownedSite(), readyController(testAppVersion), apiKeySecret()},
			stub:    &stubUnifiClient{switchStacks: []unifi.SwitchStack{{ID: testOtherNamespace, Name: testOtherObjectName}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			factory := &recordingFactory{client: tt.stub}
			fakeClient, _ := newSwitchingFakeClient(t, tt.objects...)
			req := controllerRequest(tt.objects[0].GetName())

			var (
				requeueAfter ctrl.Result
				reason       string
			)
			switch tt.objects[0].(type) {
			case *unifiv1alpha1.UnifiSwitchingLag:
				reconciler := &UnifiSwitchingLagReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
				res, err := reconciler.Reconcile(context.Background(), req)
				if err != nil {
					t.Fatalf("Reconcile error = %v, want nil", err)
				}
				requeueAfter = res
				got := &unifiv1alpha1.UnifiSwitchingLag{}
				_ = fakeClient.Get(context.Background(), req.NamespacedName, got)
				reason = requireReady(t, got.Status.Conditions).Reason
			case *unifiv1alpha1.UnifiSwitchingMcLagDomain:
				reconciler := &UnifiSwitchingMcLagDomainReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
				res, err := reconciler.Reconcile(context.Background(), req)
				if err != nil {
					t.Fatalf("Reconcile error = %v, want nil", err)
				}
				requeueAfter = res
				got := &unifiv1alpha1.UnifiSwitchingMcLagDomain{}
				_ = fakeClient.Get(context.Background(), req.NamespacedName, got)
				reason = requireReady(t, got.Status.Conditions).Reason
			case *unifiv1alpha1.UnifiSwitchingSwitchStack:
				reconciler := &UnifiSwitchingSwitchStackReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
				res, err := reconciler.Reconcile(context.Background(), req)
				if err != nil {
					t.Fatalf("Reconcile error = %v, want nil", err)
				}
				requeueAfter = res
				got := &unifiv1alpha1.UnifiSwitchingSwitchStack{}
				_ = fakeClient.Get(context.Background(), req.NamespacedName, got)
				reason = requireReady(t, got.Status.Conditions).Reason
			}

			if requeueAfter.RequeueAfter == 0 {
				t.Errorf("RequeueAfter = 0, want a bounded requeue")
			}
			if reason == "" || reason == reasonReconciled {
				t.Errorf("Ready reason = %q, want a not-found reason", reason)
			}
		})
	}
}

// TestSwitchingSpecsHaveNoOpaqueIdentifier asserts each switching spec marshals
// to only typed, non-opaque fields: the LAG spec is {members, siteRef, type}, and
// the MC-LAG/stack specs are {name, siteRef}, so no opaque upstream identifier
// can be authored.
func TestSwitchingSpecsHaveNoOpaqueIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		spec   any
		fields []string
	}{
		{name: "lag", spec: newSwitchingLag().Spec, fields: []string{"members", testSpecFieldSiteRef, "type"}},
		{name: "mc-lag domain", spec: newSwitchingMcLagDomain().Spec, fields: []string{testSpecFieldName, testSpecFieldSiteRef}},
		{name: "switch stack", spec: newSwitchingSwitchStack().Spec, fields: []string{testSpecFieldName, testSpecFieldSiteRef}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data, err := json.Marshal(tt.spec)
			if err != nil {
				t.Fatalf("marshal spec: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatalf("unmarshal spec: %v", err)
			}
			gotKeys := make([]string, 0, len(fields))
			for key := range fields {
				gotKeys = append(gotKeys, key)
			}
			slices.Sort(gotKeys)
			if !slices.Equal(gotKeys, tt.fields) {
				t.Errorf("spec fields = %v, want exactly %v", gotKeys, tt.fields)
			}
			for key := range fields {
				if key == "id" || key == "uuid" || key == "deviceIDs" || key == "deviceIds" {
					t.Errorf("unexpected opaque spec field %q", key)
				}
			}
		})
	}
}
