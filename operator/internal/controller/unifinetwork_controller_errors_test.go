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
	"net/http"
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

// stubClient is a unifi.Client that always fails with a fixed error, used to
// exercise the reconciler's error classification without any network. It records
// the site it is called with so the spec.site pass-through can be asserted.
type stubClient struct {
	err  error
	site *string
}

func (s stubClient) ListNetworks(_ context.Context, site string) ([]unifi.Network, error) {
	if s.site != nil {
		*s.site = site
	}
	return nil, s.err
}

func (s stubClient) GetNetwork(context.Context, string, string) (unifi.Network, error) {
	return unifi.Network{}, s.err
}

func TestReconcileErrorClassification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		clientErr        error
		wantErr          bool
		wantStatusWrites int
		wantReady        metav1.ConditionStatus
		wantReason       string
	}{
		{
			name:             "transport error is transient",
			clientErr:        errors.New("dial tcp 10.0.0.1:443: connect: connection refused"),
			wantErr:          true,
			wantStatusWrites: 0,
		},
		{
			name:             "retryable api error is transient",
			clientErr:        &unifi.APIError{StatusCode: http.StatusInternalServerError, Message: "boom"},
			wantErr:          true,
			wantStatusWrites: 0,
		},
		{
			name:             "terminal api error writes a not-ready condition",
			clientErr:        &unifi.APIError{StatusCode: http.StatusBadRequest, Message: "bad"},
			wantErr:          false,
			wantStatusWrites: 1,
			wantReady:        metav1.ConditionFalse,
			wantReason:       reasonInvalidSpec,
		},
		{
			name:             "unauthorized is an auth failure",
			clientErr:        &unifi.APIError{StatusCode: http.StatusUnauthorized, Message: "unauthorized"},
			wantErr:          false,
			wantStatusWrites: 1,
			wantReady:        metav1.ConditionFalse,
			wantReason:       reasonAuthFailed,
		},
		{
			name:             "forbidden is an auth failure",
			clientErr:        &unifi.APIError{StatusCode: http.StatusForbidden, Message: "forbidden"},
			wantErr:          false,
			wantStatusWrites: 1,
			wantReady:        metav1.ConditionFalse,
			wantReason:       reasonAuthFailed,
		},
		{
			name:             "invalid spec error writes a not-ready condition",
			clientErr:        &unifi.InvalidSpecError{Field: "site", Reason: `".." is not a valid path segment`},
			wantErr:          false,
			wantStatusWrites: 1,
			wantReady:        metav1.ConditionFalse,
			wantReason:       reasonInvalidSpec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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

			var gotSite string
			reconciler := &UnifiNetworkReconciler{
				Client: fakeClient,
				Scheme: scheme,
				UniFi:  stubClient{err: tt.clientErr, site: &gotSite},
			}
			req := reconcile.Request{NamespacedName: types.NamespacedName{Name: testResourceName, Namespace: testNamespace}}

			_, err := reconciler.Reconcile(context.Background(), req)
			if gotSite != testSite {
				t.Errorf("ListNetworks site = %q, want spec.site %q", gotSite, testSite)
			}
			if tt.wantErr && err == nil {
				t.Fatalf("Reconcile error = nil, want error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if statusWrites != tt.wantStatusWrites {
				t.Fatalf("statusWrites = %d, want %d", statusWrites, tt.wantStatusWrites)
			}

			if tt.wantStatusWrites == 0 {
				return
			}

			got := &unifiv1alpha1.UnifiNetwork{}
			if err := fakeClient.Get(context.Background(), req.NamespacedName, got); err != nil {
				t.Fatalf("get after reconcile: %v", err)
			}
			cond := apimeta.FindStatusCondition(got.Status.Conditions, conditionReady)
			if cond == nil || cond.Status != tt.wantReady {
				t.Fatalf("Ready condition = %+v, want %v", cond, tt.wantReady)
			}
			if cond.Reason != tt.wantReason {
				t.Errorf("Ready reason = %q, want %q", cond.Reason, tt.wantReason)
			}
		})
	}
}
