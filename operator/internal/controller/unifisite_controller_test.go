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
	"net/http"
	"reflect"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const testSiteUID = types.UID("site-uid")

func boolPtr(b bool) *bool { return &b }

func newUnifiSite() *unifiv1alpha1.UnifiSite {
	return &unifiv1alpha1.UnifiSite{
		ObjectMeta: metav1.ObjectMeta{Name: testSiteResourceName, Namespace: testNamespace},
		Spec: unifiv1alpha1.UnifiSiteSpec{
			ControllerRef:     unifiv1alpha1.CoreRef{Name: testControllerName},
			InternalReference: testInternalReference,
		},
	}
}

func TestUnifiSiteReconcileAdoption(t *testing.T) {
	t.Parallel()

	site := newUnifiSite()
	stub := &stubUnifiClient{sites: []unifi.Site{
		{ID: testUpstreamSiteID, InternalReference: testInternalReference, Name: "Default"},
	}}
	factory := &recordingFactory{client: stub}
	fakeClient, writes := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiSite{}}, site, readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := &unifiv1alpha1.UnifiSite{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonSiteAdopted {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonSiteAdopted)
	}
	if got.Status.SiteID != testUpstreamSiteID {
		t.Errorf("siteID = %q, want %q", got.Status.SiteID, testUpstreamSiteID)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiSiteFinalizer) {
		t.Errorf("site finalizer was not added")
	}
	if !reflect.DeepEqual(stub.called(), []string{"ListSites"}) {
		t.Errorf("upstream calls = %v, want only ListSites (no mutation)", stub.called())
	}
	if factory.insecure {
		t.Errorf("factory insecure = true, want false by default")
	}
	if *writes != 1 {
		t.Errorf("status writes = %d, want 1", *writes)
	}
}

func TestUnifiSiteReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		objects      []client.Object
		stub         *stubUnifiClient
		wantReason   string
		wantRequeue  bool
		wantErr      bool
		wantSiteID   string
		wantStatuses int
	}{
		{
			name:         "missing controller",
			objects:      nil,
			stub:         &stubUnifiClient{},
			wantReason:   reasonControllerNotFound,
			wantRequeue:  true,
			wantStatuses: 1,
		},
		{
			name:         "missing secret",
			objects:      []client.Object{readyController(testAppVersion)},
			stub:         &stubUnifiClient{},
			wantReason:   reasonSecretNotFound,
			wantRequeue:  true,
			wantStatuses: 1,
		},
		{
			name:         "controller version not reported",
			objects:      []client.Object{newUnifiController(nil), apiKeySecret()},
			stub:         &stubUnifiClient{},
			wantReason:   reasonDependencyNotReady,
			wantRequeue:  true,
			wantStatuses: 1,
		},
		{
			name:         "no matching upstream site",
			objects:      []client.Object{readyController(testAppVersion), apiKeySecret()},
			stub:         &stubUnifiClient{sites: []unifi.Site{{ID: "other", InternalReference: "other"}}},
			wantReason:   reasonSiteNotFound,
			wantRequeue:  true,
			wantStatuses: 1,
		},
		{
			name:         "authentication failure",
			objects:      []client.Object{readyController(testAppVersion), apiKeySecret()},
			stub:         &stubUnifiClient{sitesErr: &unifi.APIError{StatusCode: http.StatusUnauthorized, Message: "no"}},
			wantReason:   reasonAuthenticationFailed,
			wantStatuses: 1,
		},
		{
			name:         "transient failure is retried",
			objects:      []client.Object{readyController(testAppVersion), apiKeySecret()},
			stub:         &stubUnifiClient{sitesErr: &unifi.APIError{StatusCode: http.StatusInternalServerError, Message: "boom"}},
			wantErr:      true,
			wantStatuses: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			site := newUnifiSite()
			factory := &recordingFactory{client: tt.stub}
			objects := append([]client.Object{site}, tt.objects...)
			fakeClient, writes := newFakeClient(t, []client.Object{&unifiv1alpha1.UnifiSite{}}, objects...)

			reconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName))
			if tt.wantErr && err == nil {
				t.Fatalf("Reconcile error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != siteAdoptionRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, siteAdoptionRequeueAfter)
			}
			if *writes != tt.wantStatuses {
				t.Fatalf("status writes = %d, want %d", *writes, tt.wantStatuses)
			}
			if tt.wantStatuses == 0 {
				return
			}

			got := &unifiv1alpha1.UnifiSite{}
			if err := fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, got); err != nil {
				t.Fatalf("get after reconcile: %v", err)
			}
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if got.Status.SiteID != tt.wantSiteID {
				t.Errorf("siteID = %q, want %q", got.Status.SiteID, tt.wantSiteID)
			}
		})
	}
}

func TestUnifiSiteReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	site := newUnifiSite()
	stub := &stubUnifiClient{sites: []unifi.Site{{ID: testUpstreamSiteID, InternalReference: testInternalReference}}}
	factory := &recordingFactory{client: stub}
	fakeClient, writes := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiSite{}}, site, readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *writes != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *writes)
	}
}

func TestUnifiSiteFinalizerDrainsChildren(t *testing.T) {
	t.Parallel()

	site := &unifiv1alpha1.UnifiSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testSiteResourceName,
			Namespace:  testNamespace,
			UID:        testSiteUID,
			Finalizers: []string{unifiv1alpha1.UnifiSiteFinalizer},
		},
		Spec: unifiv1alpha1.UnifiSiteSpec{
			ControllerRef:     unifiv1alpha1.CoreRef{Name: testControllerName},
			InternalReference: testInternalReference,
		},
	}
	network := &unifiv1alpha1.UnifiNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testNetworkResourceName,
			Namespace:  testNamespace,
			UID:        types.UID("net-uid"),
			Finalizers: []string{"test/finalizer"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         unifiv1alpha1.GroupVersion.String(),
				Kind:               "UnifiSite",
				Name:               site.Name,
				UID:                site.UID,
				Controller:         boolPtr(true),
				BlockOwnerDeletion: boolPtr(true),
			}},
		},
		Spec: unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: site.Name},
			Management: testManagementUnmanaged,
			Name:       testNetworkResourceName,
			VLANID:     10,
		},
	}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiSite{}, &unifiv1alpha1.UnifiNetwork{}}, site, network)
	reconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if err := fakeClient.Delete(context.Background(), site); err != nil {
		t.Fatalf("delete site: %v", err)
	}

	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != childDrainRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, childDrainRequeueAfter)
	}

	child := &unifiv1alpha1.UnifiNetwork{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testNetworkResourceName, Namespace: testNamespace}, child); err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.DeletionTimestamp.IsZero() {
		t.Errorf("child deletion was not initiated")
	}
	gotSite := &unifiv1alpha1.UnifiSite{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, gotSite); err != nil {
		t.Fatalf("get site: %v", err)
	}
	if !controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer) {
		t.Errorf("site finalizer was removed before the child finished")
	}
	if len(stub.called()) != 0 {
		t.Errorf("upstream calls = %v, want none during drain", stub.called())
	}

	// Finish the child: clear its finalizer and delete it.
	child.Finalizers = nil
	if err := fakeClient.Update(context.Background(), child); err != nil {
		t.Fatalf("clear child finalizer: %v", err)
	}
	if err := fakeClient.Delete(context.Background(), child); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete child: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName)); err != nil {
		t.Fatalf("second Reconcile error = %v, want nil", err)
	}
	gotSite = &unifiv1alpha1.UnifiSite{}
	err = fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, gotSite)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get site after drain: %v", err)
	}
	// Once the finalizer is removed the API server (and the fake) deletes the
	// object; if it is still present it must no longer carry the finalizer.
	if err == nil && controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer) {
		t.Errorf("site finalizer was not removed after children drained")
	}
	if len(stub.called()) != 0 {
		t.Errorf("upstream calls = %v, want none (upstream site left intact)", stub.called())
	}
}

func TestUnifiSiteFinalizerDrainsFirewallZones(t *testing.T) {
	t.Parallel()

	site := &unifiv1alpha1.UnifiSite{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testSiteResourceName,
			Namespace:  testNamespace,
			UID:        testSiteUID,
			Finalizers: []string{unifiv1alpha1.UnifiSiteFinalizer},
		},
		Spec: unifiv1alpha1.UnifiSiteSpec{
			ControllerRef:     unifiv1alpha1.CoreRef{Name: testControllerName},
			InternalReference: testInternalReference,
		},
	}
	zone := &unifiv1alpha1.UnifiFirewallZone{
		ObjectMeta: metav1.ObjectMeta{
			Name:       testFirewallZoneResourceName,
			Namespace:  testNamespace,
			UID:        types.UID("zone-uid"),
			Finalizers: []string{unifiv1alpha1.UnifiFirewallZoneFinalizer},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         unifiv1alpha1.GroupVersion.String(),
				Kind:               "UnifiSite",
				Name:               site.Name,
				UID:                site.UID,
				Controller:         boolPtr(true),
				BlockOwnerDeletion: boolPtr(true),
			}},
		},
		Spec: unifiv1alpha1.UnifiFirewallZoneSpec{
			SiteRef: unifiv1alpha1.CoreRef{Name: site.Name},
			Name:    testZoneUpstreamName,
		},
	}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiSite{}, &unifiv1alpha1.UnifiFirewallZone{}}, site, zone)
	reconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if err := fakeClient.Delete(context.Background(), site); err != nil {
		t.Fatalf("delete site: %v", err)
	}

	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != childDrainRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, childDrainRequeueAfter)
	}

	child := &unifiv1alpha1.UnifiFirewallZone{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testFirewallZoneResourceName, Namespace: testNamespace}, child); err != nil {
		t.Fatalf("get child: %v", err)
	}
	if child.DeletionTimestamp.IsZero() {
		t.Errorf("child deletion was not initiated")
	}
	gotSite := &unifiv1alpha1.UnifiSite{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, gotSite); err != nil {
		t.Fatalf("get site: %v", err)
	}
	if !controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer) {
		t.Errorf("site finalizer was removed before the child finished")
	}
	if len(stub.called()) != 0 {
		t.Errorf("upstream calls = %v, want none during drain", stub.called())
	}

	// Finish the child: clear its finalizer and delete it.
	child.Finalizers = nil
	if err := fakeClient.Update(context.Background(), child); err != nil {
		t.Fatalf("clear child finalizer: %v", err)
	}
	if err := fakeClient.Delete(context.Background(), child); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete child: %v", err)
	}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName)); err != nil {
		t.Fatalf("second Reconcile error = %v, want nil", err)
	}
	gotSite = &unifiv1alpha1.UnifiSite{}
	err = fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, gotSite)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get site after drain: %v", err)
	}
	// Once the finalizer is removed the API server (and the fake) deletes the
	// object; if it is still present it must no longer carry the finalizer.
	if err == nil && controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer) {
		t.Errorf("site finalizer was not removed after children drained")
	}
	if len(stub.called()) != 0 {
		t.Errorf("upstream calls = %v, want none (upstream site left intact)", stub.called())
	}
}

// ensure the site status carries a summary so kubectl output stays useful.
func TestUnifiSiteStatusSummary(t *testing.T) {
	t.Parallel()

	site := newUnifiSite()
	factory := &recordingFactory{client: &stubUnifiClient{
		sites: []unifi.Site{{ID: testUpstreamSiteID, InternalReference: testInternalReference}},
	}}
	fakeClient, _ := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiSite{}}, site, readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	got := &unifiv1alpha1.UnifiSite{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	if got.Status.Summary == "" {
		t.Errorf("summary is empty, want a human-readable summary")
	}
	if apimeta.FindStatusCondition(got.Status.Conditions, conditionReady) == nil {
		t.Errorf("Ready condition is missing")
	}
}

var _ reconcile.Reconciler = &UnifiSiteReconciler{}
