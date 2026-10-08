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
	"reflect"
	"strings"
	"testing"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

func TestReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := unifiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	resource := &unifiv1alpha1.UnifiNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: testResourceName, Namespace: testNamespace},
		Spec:       unifiv1alpha1.UnifiNetworkSpec{Site: testSite, Name: testNetwork},
	}

	var statusWrites int
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&unifiv1alpha1.UnifiNetwork{}).
		WithObjects(resource).
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
		}).
		Build()

	reconciler := &UnifiNetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		UniFi:  unifi.NewFixtureClient(unifi.Network{ID: testUpstreamID, Name: testNetwork, Enabled: true}),
	}
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: testResourceName, Namespace: testNamespace}}

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if statusWrites != 1 {
		t.Fatalf("after first reconcile statusWrites = %d, want 1", statusWrites)
	}

	got := &unifiv1alpha1.UnifiNetwork{}
	if err := fakeClient.Get(context.Background(), req.NamespacedName, got); err != nil {
		t.Fatalf("get after first reconcile: %v", err)
	}
	cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		t.Fatalf("Ready condition = %+v, want True", cond)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Fatalf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.ControllerID != testUpstreamID {
		t.Fatalf("controllerID = %q, want %q", got.Status.ControllerID, testUpstreamID)
	}
	if !strings.Contains(got.Status.Summary, "not yet reconciled") {
		t.Errorf("summary = %q, want it to state fields are not yet reconciled", got.Status.Summary)
	}
	if cond.Message != got.Status.Summary {
		t.Errorf("condition message = %q, want it to match summary %q", cond.Message, got.Status.Summary)
	}
	firstResourceVersion := got.ResourceVersion
	firstStatus := got.Status.DeepCopy()

	if _, err := reconciler.Reconcile(context.Background(), req); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if statusWrites != 1 {
		t.Fatalf("second reconcile wrote status; statusWrites = %d, want 1", statusWrites)
	}

	after := &unifiv1alpha1.UnifiNetwork{}
	if err := fakeClient.Get(context.Background(), req.NamespacedName, after); err != nil {
		t.Fatalf("get after second reconcile: %v", err)
	}
	if after.ResourceVersion != firstResourceVersion {
		t.Errorf("resourceVersion changed on second reconcile: %q -> %q", firstResourceVersion, after.ResourceVersion)
	}
	if !reflect.DeepEqual(after.Status, *firstStatus) {
		t.Errorf("status changed on second reconcile:\n first=%+v\n after=%+v", *firstStatus, after.Status)
	}
}

func TestReconcileUpstreamNotFoundRequeues(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := unifiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add to scheme: %v", err)
	}

	resource := &unifiv1alpha1.UnifiNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: testResourceName, Namespace: testNamespace},
		Spec:       unifiv1alpha1.UnifiNetworkSpec{Site: testSite, Name: testNetwork},
	}

	var statusWrites int
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithStatusSubresource(&unifiv1alpha1.UnifiNetwork{}).
		WithObjects(resource).
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
		}).
		Build()

	reconciler := &UnifiNetworkReconciler{
		Client: fakeClient,
		Scheme: scheme,
		UniFi:  unifi.NewFixtureClient(),
	}
	req := reconcile.Request{NamespacedName: types.NamespacedName{Name: testResourceName, Namespace: testNamespace}}

	result, err := reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("first reconcile RequeueAfter = %v, want > 0", result.RequeueAfter)
	}
	if statusWrites != 1 {
		t.Fatalf("after first reconcile statusWrites = %d, want 1", statusWrites)
	}

	result, err = reconciler.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if result.RequeueAfter <= 0 {
		t.Errorf("second reconcile RequeueAfter = %v, want > 0", result.RequeueAfter)
	}
	if statusWrites != 1 {
		t.Errorf("second reconcile wrote status; statusWrites = %d, want 1", statusWrites)
	}

	got := &unifiv1alpha1.UnifiNetwork{}
	if err := fakeClient.Get(context.Background(), req.NamespacedName, got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionFalse || cond.Reason != reasonUpstreamNotFound {
		t.Errorf("Ready condition = %+v, want False/%s", cond, reasonUpstreamNotFound)
	}
}
