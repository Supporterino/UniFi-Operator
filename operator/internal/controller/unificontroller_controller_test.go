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
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

func newUnifiController(insecure *bool) *unifiv1alpha1.UnifiController {
	return &unifiv1alpha1.UnifiController{
		ObjectMeta: metav1.ObjectMeta{Name: testControllerName, Namespace: testNamespace},
		Spec: unifiv1alpha1.UnifiControllerSpec{
			URL: testControllerURL,
			SecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: testSecretName},
				Key:                  testSecretKey,
			},
			InsecureSkipVerify: insecure,
		},
	}
}

func controllerRequest(name string) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Name: name, Namespace: testNamespace}}
}

func requireReady(t *testing.T, conditions []metav1.Condition) *metav1.Condition {
	t.Helper()
	cond := apimeta.FindStatusCondition(conditions, conditionReady)
	if cond == nil {
		t.Fatalf("Ready condition not found in %+v", conditions)
	}
	return cond
}

func TestUnifiControllerReconcileSuccess(t *testing.T) {
	t.Parallel()

	controller := newUnifiController(nil)
	fakeClient, writes := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiController{}}, controller, apiKeySecret())
	factory := &recordingFactory{client: &stubUnifiClient{info: unifi.Info{ApplicationVersion: testAppVersion}}}

	reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := &unifiv1alpha1.UnifiController{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testControllerName).NamespacedName, got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.ApplicationVersion != testAppVersion {
		t.Errorf("applicationVersion = %q, want %q", got.Status.ApplicationVersion, testAppVersion)
	}
	if !strings.Contains(got.Status.Summary, testAppVersion) {
		t.Errorf("summary = %q, want it to contain the detected version", got.Status.Summary)
	}
	if *writes != 1 {
		t.Errorf("status writes = %d, want 1", *writes)
	}

	if factory.calls != 1 {
		t.Fatalf("factory calls = %d, want 1", factory.calls)
	}
	if factory.baseURL != testControllerURL {
		t.Errorf("factory baseURL = %q, want %q", factory.baseURL, testControllerURL)
	}
	if factory.apiKey != testSecretValue {
		t.Errorf("factory apiKey did not receive the Secret value")
	}
	if factory.insecure {
		t.Errorf("factory insecure = true, want false by default")
	}
}

func TestUnifiControllerReconcileSecretErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		objects    []client.Object
		wantReason string
	}{
		{
			name:       "missing secret",
			objects:    nil,
			wantReason: reasonSecretNotFound,
		},
		{
			name: "missing key",
			objects: []client.Object{&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
				Data:       map[string][]byte{"other-key": []byte("value")},
			}},
			wantReason: reasonSecretKeyMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller := newUnifiController(nil)
			factory := &recordingFactory{client: &stubUnifiClient{}}
			objects := append([]client.Object{controller}, tt.objects...)
			fakeClient, writes := newFakeClient(t, []client.Object{&unifiv1alpha1.UnifiController{}}, objects...)

			reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if result.RequeueAfter != credentialResolutionRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, credentialResolutionRequeueAfter)
			}
			if factory.calls != 0 {
				t.Errorf("factory calls = %d, want 0 (no credential)", factory.calls)
			}

			got := &unifiv1alpha1.UnifiController{}
			if err := fakeClient.Get(context.Background(), controllerRequest(testControllerName).NamespacedName, got); err != nil {
				t.Fatalf("get after reconcile: %v", err)
			}
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if strings.Contains(cond.Message, testSecretValue) || strings.Contains(got.Status.Summary, testSecretValue) {
				t.Errorf("credential leaked into status")
			}
			if *writes != 1 {
				t.Errorf("status writes = %d, want 1", *writes)
			}
		})
	}
}

func TestUnifiControllerReconcileVersionGate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		version     string
		wantReady   bool
		wantReason  string
		wantVersion string
	}{
		{name: "supported", version: testAppVersion, wantReady: true, wantReason: reasonReconciled, wantVersion: testAppVersion},
		{name: "below minimum", version: "9.9.9", wantReady: false, wantReason: reasonVersionUnsupported, wantVersion: "9.9.9"},
		{name: "unparseable", version: testUnparseableVersion, wantReady: false, wantReason: reasonVersionUnsupported, wantVersion: testUnparseableVersion},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller := newUnifiController(nil)
			fakeClient, _ := newFakeClient(t,
				[]client.Object{&unifiv1alpha1.UnifiController{}}, controller, apiKeySecret())
			factory := &recordingFactory{client: &stubUnifiClient{info: unifi.Info{ApplicationVersion: tt.version}}}

			reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName)); err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}

			got := &unifiv1alpha1.UnifiController{}
			if err := fakeClient.Get(context.Background(), controllerRequest(testControllerName).NamespacedName, got); err != nil {
				t.Fatalf("get after reconcile: %v", err)
			}
			cond := requireReady(t, got.Status.Conditions)
			wantStatus := metav1.ConditionFalse
			if tt.wantReady {
				wantStatus = metav1.ConditionTrue
			}
			if cond.Status != wantStatus || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want %s/%s", cond.Status, cond.Reason, wantStatus, tt.wantReason)
			}
			if got.Status.ApplicationVersion != tt.wantVersion {
				t.Errorf("applicationVersion = %q, want %q", got.Status.ApplicationVersion, tt.wantVersion)
			}
		})
	}
}

func TestUnifiControllerReconcileErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		infoErr          error
		wantErr          bool
		wantStatusWrites int
		wantReason       string
	}{
		{
			name:             "transport error is transient",
			infoErr:          errors.New("dial tcp: connection refused"),
			wantErr:          true,
			wantStatusWrites: 0,
		},
		{
			name:             "retryable api error is transient",
			infoErr:          &unifi.APIError{StatusCode: http.StatusInternalServerError, Message: "boom"},
			wantErr:          true,
			wantStatusWrites: 0,
		},
		{
			name:             "unauthorized fails closed",
			infoErr:          &unifi.APIError{StatusCode: http.StatusUnauthorized, Message: "unauthorized"},
			wantStatusWrites: 1,
			wantReason:       reasonAuthenticationFailed,
		},
		{
			name:             "forbidden fails closed",
			infoErr:          &unifi.APIError{StatusCode: http.StatusForbidden, Message: "forbidden"},
			wantStatusWrites: 1,
			wantReason:       reasonAuthenticationFailed,
		},
		{
			name:             "terminal client error is invalid spec",
			infoErr:          &unifi.APIError{StatusCode: http.StatusBadRequest, Message: "bad"},
			wantStatusWrites: 1,
			wantReason:       reasonInvalidSpec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			controller := newUnifiController(nil)
			fakeClient, writes := newFakeClient(t,
				[]client.Object{&unifiv1alpha1.UnifiController{}}, controller, apiKeySecret())
			factory := &recordingFactory{client: &stubUnifiClient{infoErr: tt.infoErr}}

			reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
			_, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName))
			if tt.wantErr && err == nil {
				t.Fatalf("Reconcile error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if *writes != tt.wantStatusWrites {
				t.Fatalf("status writes = %d, want %d", *writes, tt.wantStatusWrites)
			}
			if tt.wantStatusWrites == 0 {
				return
			}

			got := &unifiv1alpha1.UnifiController{}
			if err := fakeClient.Get(context.Background(), controllerRequest(testControllerName).NamespacedName, got); err != nil {
				t.Fatalf("get after reconcile: %v", err)
			}
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
		})
	}
}

func TestUnifiControllerReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	controller := newUnifiController(nil)
	fakeClient, writes := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiController{}}, controller, apiKeySecret())
	factory := &recordingFactory{client: &stubUnifiClient{info: unifi.Info{ApplicationVersion: testAppVersion}}}

	reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *writes != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *writes)
	}
}

// tlsInfoServer starts a TLS server that serves GET /v1/info with the given
// application version and a self-signed certificate.
func tlsInfoServer(t *testing.T, version string) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/network/integration/v1/info" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, fmt.Sprintf(`{"applicationVersion":%q}`, version))
	}))
	t.Cleanup(server.Close)
	return server
}

func TestUnifiControllerReconcileRejectsUntrustedCertificate(t *testing.T) {
	t.Parallel()

	server := tlsInfoServer(t, testAppVersion)
	controller := newUnifiController(nil)
	controller.Spec.URL = server.URL

	fakeClient, _ := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiController{}}, controller, apiKeySecret())
	// NewClient is nil, so the real unifi.NewClient with verification enabled is
	// used and must reject the self-signed certificate.
	reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme()}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil (a certificate failure is terminal)", err)
	}

	got := &unifiv1alpha1.UnifiController{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testControllerName).NamespacedName, got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonCertificateError {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonCertificateError)
	}
	if got.Status.ApplicationVersion != "" {
		t.Errorf("applicationVersion = %q, want empty on a failed probe", got.Status.ApplicationVersion)
	}
}

func TestUnifiControllerReconcileInsecureSkipVerifyOptOut(t *testing.T) {
	t.Parallel()

	server := tlsInfoServer(t, testAppVersion)
	skip := true
	controller := newUnifiController(&skip)
	controller.Spec.URL = server.URL

	fakeClient, _ := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiController{}}, controller, apiKeySecret())
	reconciler := &UnifiControllerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme()}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := &unifiv1alpha1.UnifiController{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testControllerName).NamespacedName, got); err != nil {
		t.Fatalf("get after reconcile: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %s/%s, want True", cond.Status, cond.Reason)
	}
	if got.Status.ApplicationVersion != testAppVersion {
		t.Errorf("applicationVersion = %q, want %q", got.Status.ApplicationVersion, testAppVersion)
	}
}
