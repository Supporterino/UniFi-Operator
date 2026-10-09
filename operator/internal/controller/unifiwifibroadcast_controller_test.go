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
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

const (
	testBroadcastResourceName = "wifi-broadcast"
	testBroadcastUpstreamName = "corp"
	testUpstreamBroadcastID   = "broadcast-uuid-1"
	testBroadcastDeviceTag    = "ap-tag"
	testBroadcastDeviceTagID  = "aa11bb22-cc33-4d44-8e55-ff66aa77bb88"
	testBroadcastDeviceID     = "dd11bb22-cc33-4d44-8e55-ff66aa77bb88"
	testOtherNetworkName      = "other-net"
	testPassphraseSecretName  = "wifi-passphrase"
	testPassphraseKey         = "passphrase"
	testBroadcastPassphrase   = "sup3r-s3cret-passphrase"
	testWifiSecurityOpen      = "OPEN"
	testNasIDUserDefined      = "USER_DEFINED"
	testNasIDValue            = "nas"
)

// ownedNetwork returns the parent UnifiNetwork fixture with an upstream network
// UUID and the manager UID needed for the ownerReference.
func ownedNetwork() *unifiv1alpha1.UnifiNetwork {
	network := newGatewayNetwork()
	network.Status.NetworkID = testUpstreamNetworkID
	return network
}

// newWifiBroadcast returns a minimal STANDARD UnifiWifiBroadcast fixture.
func newWifiBroadcast() *unifiv1alpha1.UnifiWifiBroadcast {
	return &unifiv1alpha1.UnifiWifiBroadcast{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testBroadcastResourceName,
			Namespace: testNamespace,
			UID:       types.UID("broadcast-uid"),
		},
		Spec: unifiv1alpha1.UnifiWifiBroadcastSpec{
			NetworkRef:                          unifiv1alpha1.CoreRef{Name: testNetworkResourceName},
			Type:                                "STANDARD",
			Name:                                testBroadcastUpstreamName,
			MulticastToUnicastConversionEnabled: true,
			SecurityConfiguration: unifiv1alpha1.WifiSecurityConfiguration{
				Type: "OPEN",
				Open: &unifiv1alpha1.WifiOpenSecurityConfiguration{},
			},
			Standard: &unifiv1alpha1.StandardWifiOptions{
				BroadcastingFrequenciesGHz: []unifiv1alpha1.WifiBroadcastingFrequencyGHz{"2.4", "5"},
			},
		},
	}
}

// matchingBroadcast returns the upstream detail that equals what
// newWifiBroadcast desires, as the console would report it.
func matchingBroadcast(t *testing.T) unifi.WifiBroadcast {
	t.Helper()
	broadcast := newWifiBroadcast()
	request, err := buildWifiBroadcastRequest(
		broadcast,
		testUpstreamNetworkID,
		&unifi.WifiSecurityConfiguration{Type: "OPEN"},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("buildWifiBroadcastRequest: %v", err)
	}
	return unifi.WifiBroadcast{
		ID:                   testUpstreamBroadcastID,
		Metadata:             unifi.NetworkMetadata{Origin: wifiBroadcastOriginUserDefined},
		WifiBroadcastRequest: request,
	}
}

// passphraseSecret returns a Secret holding the test broadcast passphrase.
func passphraseSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testPassphraseSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testPassphraseKey: []byte(testBroadcastPassphrase)},
	}
}

// controlledBy builds a controller ownerReference for a fixture whose Kind is
// named explicitly (typed fixtures carry no TypeMeta).
func controlledBy(kind string, owner client.Object) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         unifiv1alpha1.GroupVersion.String(),
		Kind:               kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         boolPtr(true),
		BlockOwnerDeletion: boolPtr(true),
	}
}

// newBroadcastFakeClient returns a fake client that counts writes to the status
// subresource and to object metadata, so idempotency can be asserted.
func newBroadcastFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiNetwork{},
			&unifiv1alpha1.UnifiWifiBroadcast{},
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

// getWifiBroadcast loads the reconciled UnifiWifiBroadcast fixture.
func getWifiBroadcast(t *testing.T, c client.Client) *unifiv1alpha1.UnifiWifiBroadcast {
	t.Helper()
	got := &unifiv1alpha1.UnifiWifiBroadcast{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testBroadcastResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get broadcast after reconcile: %v", err)
	}
	return got
}

// broadcastMutations filters recorded calls down to the ones that change
// upstream broadcast state.
func broadcastMutations(calls []string) []string {
	mutations := []string{"CreateWifiBroadcast", "UpdateWifiBroadcast", "DeleteWifiBroadcast"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}

func TestUnifiWifiBroadcastReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		createdBroadcast: unifi.WifiBroadcast{ID: testUpstreamBroadcastID, WifiBroadcastRequest: unifi.WifiBroadcastRequest{Name: testBroadcastUpstreamName}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newBroadcastFakeClient(t,
		newWifiBroadcast(), ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getWifiBroadcast(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.WifiBroadcastID != testUpstreamBroadcastID {
		t.Errorf("wifiBroadcastID = %q, want %q", got.Status.WifiBroadcastID, testUpstreamBroadcastID)
	}
	if got.Status.NetworkID != testUpstreamNetworkID {
		t.Errorf("networkID = %q, want %q", got.Status.NetworkID, testUpstreamNetworkID)
	}
	if got.Status.SiteID != testUpstreamSiteID {
		t.Errorf("siteID = %q, want %q", got.Status.SiteID, testUpstreamSiteID)
	}
	if !metav1.IsControlledBy(got, ownedNetwork()) {
		t.Errorf("broadcast is not owned by its UnifiNetwork")
	}
	if metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("broadcast must not be owned by the UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiWifiBroadcastFinalizer) {
		t.Errorf("broadcast finalizer was not added")
	}

	calls := stub.called()
	if !slices.Contains(calls, "CreateWifiBroadcast") {
		t.Errorf("upstream calls = %v, want CreateWifiBroadcast", calls)
	}
	if slices.Contains(calls, "UpdateWifiBroadcast") {
		t.Errorf("upstream calls = %v, want no UpdateWifiBroadcast on a create", calls)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiWifiBroadcastReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	enterprise := newWifiBroadcast()
	enterprise.Spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
		Type: "WPA2_ENTERPRISE",
		WPA2Enterprise: &unifiv1alpha1.WifiWPA2EnterpriseSecurityConfiguration{
			CoaEnabled: true,
			RadiusConfiguration: unifiv1alpha1.WifiEnterpriseRadiusConfiguration{
				NasID:            unifiv1alpha1.WifiRadiusNasID{Type: testNasIDUserDefined, Value: testNasIDValue},
				RadiusProfileRef: unifiv1alpha1.CoreRef{Name: "radius"},
			},
		},
	}

	notProvisioned := newGatewayNetwork()

	tests := []struct {
		name        string
		objects     func() []client.Object
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        "network missing",
			objects:     func() []client.Object { return []client.Object{newWifiBroadcast()} },
			wantReason:  reasonNetworkRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "network not provisioned",
			objects:     func() []client.Object { return []client.Object{newWifiBroadcast(), notProvisioned} },
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:        "broadcast: site missing",
			objects:     func() []client.Object { return []client.Object{newWifiBroadcast(), ownedNetwork()} },
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name: "broadcast: site not adopted",
			objects: func() []client.Object {
				site := newUnifiSite()
				site.UID = testSiteUID
				return []client.Object{newWifiBroadcast(), ownedNetwork(), site}
			},
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        "broadcast: controller missing",
			objects:     func() []client.Object { return []client.Object{newWifiBroadcast(), ownedNetwork(), ownedSite()} },
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name: "broadcast: application version below minimum",
			objects: func() []client.Object {
				return []client.Object{newWifiBroadcast(), ownedNetwork(), ownedSite(), readyController("10.1.0")}
			},
			wantReason: reasonVersionUnsupported,
		},
		{
			name: "broadcast: version unparseable",
			objects: func() []client.Object {
				return []client.Object{newWifiBroadcast(), ownedNetwork(), ownedSite(), readyController(testUnparseableVersion)}
			},
			wantReason: reasonVersionUnsupported,
		},
		{
			name: "broadcast: application version not reported",
			objects: func() []client.Object {
				return []client.Object{newWifiBroadcast(), ownedNetwork(), ownedSite(), newUnifiController(nil)}
			},
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name: "enterprise security fails closed",
			objects: func() []client.Object {
				return []client.Object{enterprise, ownedNetwork(), ownedSite(), readyController(testAppVersion)}
			},
			wantReason: reasonRadiusProfileUnsupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newBroadcastFakeClient(t, tt.objects()...)
			reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != wifiBroadcastDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, wifiBroadcastDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getWifiBroadcast(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := broadcastMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiWifiBroadcastReconcileUpdatesUpstream(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID

	existing := matchingBroadcast(t)
	existing.Name = "renamed-upstream"

	stub := &stubUnifiClient{
		broadcasts:       []unifi.WifiBroadcast{existing},
		updatedBroadcast: unifi.WifiBroadcast{ID: testUpstreamBroadcastID, WifiBroadcastRequest: unifi.WifiBroadcastRequest{Name: testBroadcastUpstreamName}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getWifiBroadcast(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.WifiBroadcastID != testUpstreamBroadcastID {
		t.Errorf("wifiBroadcastID = %q, want %q", got.Status.WifiBroadcastID, testUpstreamBroadcastID)
	}
	if len(stub.broadcastUpdateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1", len(stub.broadcastUpdateRequests))
	}
	if stub.updatedBroadcastID != testUpstreamBroadcastID {
		t.Errorf("updated broadcast = %q, want the status.wifiBroadcastID %q", stub.updatedBroadcastID, testUpstreamBroadcastID)
	}
	if slices.Contains(stub.called(), "CreateWifiBroadcast") {
		t.Errorf("upstream calls = %v, want no CreateWifiBroadcast when the broadcast exists", stub.called())
	}
}

func TestUnifiWifiBroadcastReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID

	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{matchingBroadcast(t)}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if len(stub.broadcastCreateRequests) != 0 || len(stub.broadcastUpdateRequests) != 0 {
		t.Errorf("upstream mutations = create:%d update:%d, want none on an unchanged object",
			len(stub.broadcastCreateRequests), len(stub.broadcastUpdateRequests))
	}
}

func TestUnifiWifiBroadcastReconcileIsIdempotentWithOmittedPassphrase(t *testing.T) {
	t.Parallel()

	passphraseSelector := corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: testPassphraseSecretName},
		Key:                  testPassphraseKey,
	}

	tests := []struct {
		name     string
		security func() unifiv1alpha1.WifiSecurityConfiguration
		observed func() *unifi.WifiSecurityConfiguration
	}{
		{
			name: "personal passphrase",
			security: func() unifiv1alpha1.WifiSecurityConfiguration {
				passphrase := passphraseSelector
				return unifiv1alpha1.WifiSecurityConfiguration{
					Type: testWifiWPA2Personal,
					WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{
						Passphrase: &passphrase,
					},
				}
			},
			observed: func() *unifi.WifiSecurityConfiguration {
				return &unifi.WifiSecurityConfiguration{Type: testWifiWPA2Personal}
			},
		},
		{
			name: "preshared key",
			security: func() unifiv1alpha1.WifiSecurityConfiguration {
				return unifiv1alpha1.WifiSecurityConfiguration{
					Type: testWifiWPA2Personal,
					WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{
						PresharedKeys: []unifiv1alpha1.WifiPresharedKey{{
							Network:    unifiv1alpha1.CoreRef{Name: testNetworkResourceName},
							Passphrase: passphraseSelector,
						}},
					},
				}
			},
			observed: func() *unifi.WifiSecurityConfiguration {
				return &unifi.WifiSecurityConfiguration{
					Type: testWifiWPA2Personal,
					PresharedKeys: []unifi.WifiPresharedKey{{
						Network: unifi.WifiNetworkReference{Type: wifiBroadcastNetworkReferenceSpecific, NetworkID: testUpstreamNetworkID},
					}},
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broadcast := newWifiBroadcast()
			broadcast.Spec.SecurityConfiguration = tt.security()

			stub := &stubUnifiClient{
				createdBroadcast: unifi.WifiBroadcast{ID: testUpstreamBroadcastID, WifiBroadcastRequest: unifi.WifiBroadcastRequest{Name: testBroadcastUpstreamName}},
			}
			factory := &recordingFactory{client: stub}
			fakeClient, _, _ := newBroadcastFakeClient(t,
				broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret(), passphraseSecret())
			reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
				t.Fatalf("first reconcile: %v", err)
			}
			if len(stub.broadcastCreateRequests) != 1 {
				t.Fatalf("create requests = %d, want 1", len(stub.broadcastCreateRequests))
			}

			// The console detail omits the passphrase; the second reconcile must not
			// issue an update.
			existing := matchingBroadcast(t)
			existing.SecurityConfiguration = tt.observed()
			stub.setWifiBroadcasts([]unifi.WifiBroadcast{existing})
			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
				t.Fatalf("second reconcile: %v", err)
			}
			if len(stub.broadcastUpdateRequests) != 0 {
				t.Errorf("update requests = %d, want 0 when the console omits the passphrase", len(stub.broadcastUpdateRequests))
			}
		})
	}
}

func TestUnifiWifiBroadcastReconcileIgnoresOmittedDesiredPassphrase(t *testing.T) {
	t.Parallel()

	// The CR omits the personal passphrase (it does not manage it), but the
	// adopted upstream broadcast reports one. An unset desired optional field is
	// ignored, so this must not be drift and must not PUT on every reconcile.
	broadcast := newWifiBroadcast()
	broadcast.Spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
		Type:         testWifiWPA2Personal,
		WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{},
	}

	existing := matchingBroadcast(t)
	existing.SecurityConfiguration = &unifi.WifiSecurityConfiguration{
		Type:       testWifiWPA2Personal,
		Passphrase: "console-owned-passphrase",
	}

	stub := &stubUnifiClient{
		broadcastOverviews: []unifi.WifiBroadcastOverview{{ID: testUpstreamBroadcastID, Name: testBroadcastUpstreamName}},
		broadcasts:         []unifi.WifiBroadcast{existing},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret(), passphraseSecret())
	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if len(stub.broadcastCreateRequests) != 0 || len(stub.broadcastUpdateRequests) != 0 {
		t.Errorf("upstream mutations = create:%d update:%d, want none when the CR omits the passphrase",
			len(stub.broadcastCreateRequests), len(stub.broadcastUpdateRequests))
	}
	got := getWifiBroadcast(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
}

func TestUnifiWifiBroadcastReconcileClearsRemovedDeviceScope(t *testing.T) {
	t.Parallel()

	// spec.deviceTags is absent ("broadcast on all AP-capable devices"), but the
	// console still reports a DEVICE_TAGS filter. Removing the scope is drift, so
	// the reconciler must issue a clearing PUT; once the console converges on the
	// no-filter state, a second reconcile must be a no-op.
	broadcast := newWifiBroadcast()
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID

	withFilter := matchingBroadcast(t)
	withFilter.BroadcastingDeviceFilter = &unifi.WifiBroadcastingDeviceFilter{
		Type:         wifiBroadcastDeviceFilterDeviceTags,
		DeviceTagIDs: []string{testBroadcastDeviceTagID},
	}

	stub := &stubUnifiClient{
		broadcasts:       []unifi.WifiBroadcast{withFilter},
		updatedBroadcast: unifi.WifiBroadcast{ID: testUpstreamBroadcastID, WifiBroadcastRequest: unifi.WifiBroadcastRequest{Name: testBroadcastUpstreamName}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	if len(stub.broadcastUpdateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1 (removing the device scope is drift)", len(stub.broadcastUpdateRequests))
	}
	if req := stub.broadcastUpdateRequests[0].BroadcastingDeviceFilter; req != nil {
		t.Errorf("update request filter = %+v, want nil (no filter clears the scope)", req)
	}

	// The console now reports no filter; re-reconciling must not write.
	stub.setWifiBroadcasts([]unifi.WifiBroadcast{matchingBroadcast(t)})
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(stub.broadcastUpdateRequests) != 1 {
		t.Errorf("update requests = %d, want 1 after the console clears the filter", len(stub.broadcastUpdateRequests))
	}
}

func TestUnifiWifiBroadcastCleanupPrefersStatusSite(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
	broadcast.Status.SiteID = testUpstreamSiteID // recorded from site A

	const (
		siteBName       = "other-site"
		controllerBName = "other-controller"
		siteBID         = "site-uuid-2"
		controllerBURL  = "https://other.example.com"
	)

	// The referenced network's spec.siteRef is repointed at site B, which uses a
	// different controller. Cleanup must still use site A's controller because the
	// status-recorded siteID takes precedence (design D2).
	siteB := &unifiv1alpha1.UnifiSite{
		ObjectMeta: metav1.ObjectMeta{Name: siteBName, Namespace: testNamespace},
		Spec: unifiv1alpha1.UnifiSiteSpec{
			ControllerRef:     unifiv1alpha1.CoreRef{Name: controllerBName},
			InternalReference: "branch",
		},
	}
	siteB.Status.SiteID = siteBID
	controllerB := &unifiv1alpha1.UnifiController{
		ObjectMeta: metav1.ObjectMeta{Name: controllerBName, Namespace: testNamespace},
		Spec: unifiv1alpha1.UnifiControllerSpec{
			URL: controllerBURL,
			SecretRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: testSecretName},
				Key:                  testSecretKey,
			},
		},
	}
	network := ownedNetwork()
	network.Spec.SiteRef = unifiv1alpha1.CoreRef{Name: siteBName}

	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{{
		ID:       testUpstreamBroadcastID,
		Metadata: unifi.NetworkMetadata{Origin: wifiBroadcastOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, network, ownedSite(), siteB, readyController(testAppVersion), controllerB, apiKeySecret())
	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if err := fakeClient.Delete(context.Background(), broadcast); err != nil {
		t.Fatalf("delete broadcast: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if factory.baseURL != testControllerURL {
		t.Errorf("cleanup controller baseURL = %q, want the status-recorded site's controller %q",
			factory.baseURL, testControllerURL)
	}
	if !slices.Contains(stub.called(), "DeleteWifiBroadcast") {
		t.Errorf("upstream calls = %v, want DeleteWifiBroadcast", stub.called())
	}
}

func TestUnifiWifiBroadcastReconcileTreatsEmptyOriginAsOwn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		statusOrigin string
	}{
		{name: "recorded user-defined", statusOrigin: wifiBroadcastOriginUserDefined},
		{name: "no recorded origin", statusOrigin: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broadcast := newWifiBroadcast()
			broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
			broadcast.Status.Origin = tt.statusOrigin

			// The console omits metadata.origin on the (just-created) broadcast.
			existing := matchingBroadcast(t)
			existing.Metadata.Origin = ""

			stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{existing}}
			factory := &recordingFactory{client: stub}
			fakeClient, _, _ := newBroadcastFakeClient(t,
				broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())
			reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			got := getWifiBroadcast(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
				t.Errorf("Ready = %s/%s, want True/%s (empty origin is not read-only)", cond.Status, cond.Reason, reasonReconciled)
			}
			if mutations := broadcastMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiWifiBroadcastReconcileRejectsCrossSiteReference(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
		Type: testWifiWPA2Personal,
		WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{
			PresharedKeys: []unifiv1alpha1.WifiPresharedKey{{
				Network: unifiv1alpha1.CoreRef{Name: testOtherNetworkName},
				Passphrase: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testPassphraseSecretName},
					Key:                  testPassphraseKey,
				},
			}},
		},
	}
	// other-net is provisioned but belongs to a different site than the
	// broadcast's own network, so its upstream UUID must not be written.
	otherNetwork := &unifiv1alpha1.UnifiNetwork{
		ObjectMeta: metav1.ObjectMeta{Name: testOtherNetworkName, Namespace: testNamespace},
		Spec: unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: "other-site"},
			Management: testManagementUnmanaged,
			Name:       "other-network",
			VLANID:     10,
		},
	}
	otherNetwork.Status.NetworkID = "other-network-id"

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), otherNetwork, readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	got := getWifiBroadcast(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonCrossSiteReference {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonCrossSiteReference)
	}
	if mutations := broadcastMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none for a cross-site reference", mutations)
	}
}

func TestUnifiWifiBroadcastCleanupFailsClosedOnSiteMismatch(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
	broadcast.Status.SiteID = "stale-site-id" // matches no namespaced UnifiSite

	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{{
		ID:       testUpstreamBroadcastID,
		Metadata: unifi.NetworkMetadata{Origin: wifiBroadcastOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if err := fakeClient.Delete(context.Background(), broadcast); err != nil {
		t.Fatalf("delete broadcast: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err == nil {
		t.Fatal("Reconcile error = nil, want a failure so the finalizer blocks on a site mismatch")
	}
	if slices.Contains(stub.called(), "DeleteWifiBroadcast") {
		t.Errorf("upstream calls = %v, want no DeleteWifiBroadcast on a site mismatch", stub.called())
	}
}

func TestUnifiWifiBroadcastReconcileFailsClosedOnSystemObject(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID

	system := matchingBroadcast(t)
	system.Metadata.Origin = testSystemOrigin

	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{system}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getWifiBroadcast(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonSystemObjectReadOnly {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonSystemObjectReadOnly)
	}
	if mutations := broadcastMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none for a system-managed object", mutations)
	}
}

func TestUnifiWifiBroadcastReconcileDeviceTagScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tags       []unifi.DeviceTag
		wantReason string
		wantTagID  string
	}{
		{
			name:      "single-device tag resolves",
			tags:      []unifi.DeviceTag{{ID: testBroadcastDeviceTagID, Name: testBroadcastDeviceTag, DeviceIDs: []string{testBroadcastDeviceID}}},
			wantTagID: testBroadcastDeviceTagID,
		},
		{
			name:       "missing tag fails closed",
			tags:       []unifi.DeviceTag{{ID: testBroadcastDeviceTagID, Name: "other-tag", DeviceIDs: []string{testBroadcastDeviceID}}},
			wantReason: reasonDeviceTagNotFound,
		},
		{
			name:       "ambiguous tag fails closed",
			tags:       []unifi.DeviceTag{{ID: testBroadcastDeviceTagID, Name: testBroadcastDeviceTag, DeviceIDs: []string{testBroadcastDeviceID, "second-device"}}},
			wantReason: reasonDeviceTagAmbiguous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broadcast := newWifiBroadcast()
			broadcast.Spec.DeviceTags = []unifiv1alpha1.DeviceTagSelector{{Name: testBroadcastDeviceTag}}

			stub := &stubUnifiClient{
				deviceTags:       tt.tags,
				createdBroadcast: unifi.WifiBroadcast{ID: testUpstreamBroadcastID, WifiBroadcastRequest: unifi.WifiBroadcastRequest{Name: testBroadcastUpstreamName}},
			}
			factory := &recordingFactory{client: stub}
			fakeClient, _, _ := newBroadcastFakeClient(t,
				broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())
			reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}

			got := getWifiBroadcast(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if tt.wantReason != "" {
				if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
					t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
				}
				if mutations := broadcastMutations(stub.called()); len(mutations) != 0 {
					t.Errorf("upstream mutations = %v, want none", mutations)
				}
				return
			}
			if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
				t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
			}
			if len(stub.broadcastCreateRequests) != 1 {
				t.Fatalf("create requests = %d, want 1", len(stub.broadcastCreateRequests))
			}
			filter := stub.broadcastCreateRequests[0].BroadcastingDeviceFilter
			if filter == nil || filter.Type != wifiBroadcastDeviceFilterDeviceTags {
				t.Fatalf("broadcasting device filter = %+v, want DEVICE_TAGS", filter)
			}
			if len(filter.DeviceTagIDs) != 1 || filter.DeviceTagIDs[0] != tt.wantTagID {
				t.Errorf("deviceTagIds = %v, want [%s]", filter.DeviceTagIDs, tt.wantTagID)
			}
		})
	}
}

func TestUnifiWifiBroadcastReconcilePassphraseFromSecret(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
		Type: "WPA2_PERSONAL",
		WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{
			Passphrase: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: testPassphraseSecretName},
				Key:                  testPassphraseKey,
			},
			PresharedKeys: []unifiv1alpha1.WifiPresharedKey{{
				Network: unifiv1alpha1.CoreRef{Name: testNetworkResourceName},
				Passphrase: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: testPassphraseSecretName},
					Key:                  testPassphraseKey,
				},
			}},
		},
	}

	stub := &stubUnifiClient{
		createdBroadcast: unifi.WifiBroadcast{ID: testUpstreamBroadcastID, WifiBroadcastRequest: unifi.WifiBroadcastRequest{Name: testBroadcastUpstreamName}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret(), passphraseSecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	var logs bytes.Buffer
	ctx := logf.IntoContext(context.Background(), zap.New(zap.WriteTo(&logs), zap.UseDevMode(true)))
	if _, err := reconciler.Reconcile(ctx, controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if len(stub.broadcastCreateRequests) != 1 {
		t.Fatalf("create requests = %d, want 1", len(stub.broadcastCreateRequests))
	}
	security := stub.broadcastCreateRequests[0].SecurityConfiguration
	if security == nil || security.Passphrase != testBroadcastPassphrase {
		t.Fatalf("create request passphrase = %+v, want the resolved Secret value", security)
	}
	if len(security.PresharedKeys) != 1 || security.PresharedKeys[0].Passphrase != testBroadcastPassphrase {
		t.Fatalf("create request preshared keys = %+v, want the resolved Secret value", security.PresharedKeys)
	}
	if security.PresharedKeys[0].Network.NetworkID != testUpstreamNetworkID {
		t.Errorf("preshared key network = %q, want %q", security.PresharedKeys[0].Network.NetworkID, testUpstreamNetworkID)
	}

	got := getWifiBroadcast(t, fakeClient)
	statusJSON, err := json.Marshal(got.Status)
	if err != nil {
		t.Fatalf("marshal status: %v", err)
	}
	if strings.Contains(string(statusJSON), testBroadcastPassphrase) {
		t.Errorf("status leaked the passphrase: %s", statusJSON)
	}
	if strings.Contains(logs.String(), testBroadcastPassphrase) {
		t.Errorf("logs leaked the passphrase")
	}
}

func TestUnifiWifiBroadcastReconcilePassphraseSecretFailsClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		selector   *corev1.SecretKeySelector
		objects    []client.Object
		wantReason string
	}{
		{
			name: "secret missing",
			selector: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: "missing-secret"},
				Key:                  testPassphraseKey,
			},
			objects:    []client.Object{apiKeySecret()},
			wantReason: reasonSecretNotFound,
		},
		{
			name: "secret key missing",
			selector: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: testPassphraseSecretName},
				Key:                  "missing-key",
			},
			objects:    []client.Object{apiKeySecret(), passphraseSecret()},
			wantReason: reasonSecretKeyMissing,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			broadcast := newWifiBroadcast()
			broadcast.Spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
				Type: "WPA2_PERSONAL",
				WPA2Personal: &unifiv1alpha1.WifiWPA2PersonalSecurityConfiguration{
					Passphrase: tt.selector,
				},
			}

			objects := append([]client.Object{broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion)}, tt.objects...)
			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, _, _ := newBroadcastFakeClient(t, objects...)
			reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}

			got := getWifiBroadcast(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := broadcastMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiWifiBroadcastEnterpriseFailsClosed(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Spec.SecurityConfiguration = unifiv1alpha1.WifiSecurityConfiguration{
		Type: "WPA3_ENTERPRISE",
		WPA3Enterprise: &unifiv1alpha1.WifiWPA3EnterpriseSecurityConfiguration{
			CoaEnabled:   true,
			SecurityMode: "DEFAULT",
			RadiusConfiguration: unifiv1alpha1.WifiEnterpriseRadiusConfiguration{
				NasID:            unifiv1alpha1.WifiRadiusNasID{Type: testNasIDUserDefined, Value: testNasIDValue},
				RadiusProfileRef: unifiv1alpha1.CoreRef{Name: "radius"},
			},
		},
	}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion))

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getWifiBroadcast(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonRadiusProfileUnsupported {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonRadiusProfileUnsupported)
	}
	if factory.calls != 0 {
		t.Errorf("factory calls = %d, want 0 (no console contact before the fail-closed path)", factory.calls)
	}
	if calls := stub.called(); len(calls) != 0 {
		t.Errorf("upstream calls = %v, want none", calls)
	}
}

func TestUnifiWifiBroadcastFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
	broadcast.Status.NetworkID = testUpstreamNetworkID
	broadcast.Status.SiteID = testUpstreamSiteID

	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{{
		ID:       testUpstreamBroadcastID,
		Metadata: unifi.NetworkMetadata{Origin: wifiBroadcastOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), broadcast); err != nil {
		t.Fatalf("delete broadcast: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedBroadcastSiteID != testUpstreamSiteID || stub.deletedBroadcastID != testUpstreamBroadcastID {
		t.Errorf("DeleteWifiBroadcast(%q, %q), want (%q, %q)",
			stub.deletedBroadcastSiteID, stub.deletedBroadcastID, testUpstreamSiteID, testUpstreamBroadcastID)
	}
	if !slices.Contains(stub.called(), "DeleteWifiBroadcast") {
		t.Errorf("upstream calls = %v, want DeleteWifiBroadcast", stub.called())
	}

	got := &unifiv1alpha1.UnifiWifiBroadcast{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testBroadcastResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get broadcast after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiWifiBroadcastFinalizer) {
		t.Errorf("broadcast finalizer was not removed after upstream deletion")
	}
}

func TestUnifiWifiBroadcastFinalizerDeletesUpstreamWithEmptyOrigin(t *testing.T) {
	t.Parallel()

	// An operator-created broadcast whose console omits metadata.origin must still
	// be deleted on finalization, not treated as system-managed and leaked.
	broadcast := newWifiBroadcast()
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
	broadcast.Status.NetworkID = testUpstreamNetworkID
	broadcast.Status.SiteID = testUpstreamSiteID

	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{{
		ID:       testUpstreamBroadcastID,
		Metadata: unifi.NetworkMetadata{},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), broadcast); err != nil {
		t.Fatalf("delete broadcast: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if !slices.Contains(stub.called(), "DeleteWifiBroadcast") {
		t.Errorf("upstream calls = %v, want DeleteWifiBroadcast for an empty-origin object", stub.called())
	}
	got := &unifiv1alpha1.UnifiWifiBroadcast{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testBroadcastResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get broadcast after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiWifiBroadcastFinalizer) {
		t.Errorf("broadcast finalizer was not removed after upstream deletion")
	}
}

func TestUnifiWifiBroadcastFinalizerDeletesAfterBrokenReference(t *testing.T) {
	t.Parallel()

	broadcast := newWifiBroadcast()
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
	broadcast.Status.SiteID = testUpstreamSiteID

	// The referenced UnifiNetwork is gone, but the site still records the
	// status-recorded siteID, so teardown must still resolve the controller.
	stub := &stubUnifiClient{broadcasts: []unifi.WifiBroadcast{{
		ID:       testUpstreamBroadcastID,
		Metadata: unifi.NetworkMetadata{Origin: wifiBroadcastOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		broadcast, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), broadcast); err != nil {
		t.Fatalf("delete broadcast: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if !slices.Contains(stub.called(), "DeleteWifiBroadcast") {
		t.Errorf("upstream calls = %v, want DeleteWifiBroadcast despite the broken networkRef", stub.called())
	}
	if stub.deletedBroadcastSiteID != testUpstreamSiteID {
		t.Errorf("DeleteWifiBroadcast site = %q, want the status-recorded site %q", stub.deletedBroadcastSiteID, testUpstreamSiteID)
	}

	got := &unifiv1alpha1.UnifiWifiBroadcast{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testBroadcastResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get broadcast after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiWifiBroadcastFinalizer) {
		t.Errorf("broadcast finalizer was not removed after upstream deletion")
	}
}

func TestUnifiNetworkFinalizerDrainsBroadcasts(t *testing.T) {
	t.Parallel()

	network := ownedNetwork()
	network.Finalizers = []string{unifiv1alpha1.UnifiNetworkFinalizer}
	broadcast := newWifiBroadcast()
	broadcast.OwnerReferences = []metav1.OwnerReference{controlledBy("UnifiNetwork", network)}
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t,
		network, broadcast, ownedSite(), readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if err := fakeClient.Delete(context.Background(), network); err != nil {
		t.Fatalf("delete network: %v", err)
	}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != childDrainRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, childDrainRequeueAfter)
	}

	child := &unifiv1alpha1.UnifiWifiBroadcast{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testBroadcastResourceName, Namespace: testNamespace}, child); err != nil {
		t.Fatalf("get broadcast: %v", err)
	}
	if child.DeletionTimestamp.IsZero() {
		t.Errorf("broadcast deletion was not initiated")
	}
	if slices.Contains(stub.called(), "DeleteNetwork") {
		t.Errorf("upstream calls = %v, want no DeleteNetwork before the broadcasts have drained", stub.called())
	}
	gotNetwork := &unifiv1alpha1.UnifiNetwork{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testNetworkResourceName).NamespacedName, gotNetwork); err != nil {
		t.Fatalf("get network: %v", err)
	}
	if !controllerutil.ContainsFinalizer(gotNetwork, unifiv1alpha1.UnifiNetworkFinalizer) {
		t.Errorf("network finalizer was removed before the broadcasts finished")
	}
}

func TestRecursiveDrainSiteNetworkBroadcast(t *testing.T) {
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
	site.Status.SiteID = testUpstreamSiteID
	network := ownedNetwork()
	network.Finalizers = []string{unifiv1alpha1.UnifiNetworkFinalizer}
	network.OwnerReferences = []metav1.OwnerReference{controlledBy("UnifiSite", site)}
	broadcast := newWifiBroadcast()
	broadcast.Finalizers = []string{unifiv1alpha1.UnifiWifiBroadcastFinalizer}
	broadcast.OwnerReferences = []metav1.OwnerReference{controlledBy("UnifiNetwork", network)}
	broadcast.Status.WifiBroadcastID = testUpstreamBroadcastID
	broadcast.Status.NetworkID = testUpstreamNetworkID
	broadcast.Status.SiteID = testUpstreamSiteID

	stub := &stubUnifiClient{
		networks:   []unifi.Network{{ID: testUpstreamNetworkID, Metadata: unifi.NetworkMetadata{Origin: networkOriginUserDefined}}},
		broadcasts: []unifi.WifiBroadcast{{ID: testUpstreamBroadcastID, Metadata: unifi.NetworkMetadata{Origin: wifiBroadcastOriginUserDefined}}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newBroadcastFakeClient(t, site, network, broadcast, readyController(testAppVersion), apiKeySecret())

	siteReconciler := &UnifiSiteReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	networkReconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	broadcastReconciler := &UnifiWifiBroadcastReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if err := fakeClient.Delete(context.Background(), site); err != nil {
		t.Fatalf("delete site: %v", err)
	}

	// Site drains the network; the site finalizer stays until children are gone.
	if _, err := siteReconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName)); err != nil {
		t.Fatalf("site reconcile: %v", err)
	}
	gotNetwork := &unifiv1alpha1.UnifiNetwork{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testNetworkResourceName).NamespacedName, gotNetwork); err != nil {
		t.Fatalf("get network: %v", err)
	}
	if gotNetwork.DeletionTimestamp.IsZero() {
		t.Fatalf("network deletion was not initiated by the site drain")
	}

	// Network drains the broadcast; the network finalizer stays until it is gone.
	if _, err := networkReconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("network reconcile: %v", err)
	}
	gotBroadcast := &unifiv1alpha1.UnifiWifiBroadcast{}
	if err := fakeClient.Get(context.Background(), controllerRequest(testBroadcastResourceName).NamespacedName, gotBroadcast); err != nil {
		t.Fatalf("get broadcast: %v", err)
	}
	if gotBroadcast.DeletionTimestamp.IsZero() {
		t.Fatalf("broadcast deletion was not initiated by the network drain")
	}

	// Broadcast cleans up upstream and removes its finalizer.
	if _, err := broadcastReconciler.Reconcile(context.Background(), controllerRequest(testBroadcastResourceName)); err != nil {
		t.Fatalf("broadcast reconcile: %v", err)
	}
	if !slices.Contains(stub.called(), "DeleteWifiBroadcast") {
		t.Fatalf("upstream calls = %v, want DeleteWifiBroadcast", stub.called())
	}
	// The API server garbage-collects the child once its finalizer is gone; the
	// fake client does not, so drive it explicitly.
	if err := fakeClient.Delete(context.Background(), gotBroadcast); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete drained broadcast: %v", err)
	}

	// Network now cleans up upstream and removes its finalizer.
	if _, err := networkReconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("network reconcile: %v", err)
	}
	if !slices.Contains(stub.called(), "DeleteNetwork") {
		t.Fatalf("upstream calls = %v, want DeleteNetwork after the broadcast drained", stub.called())
	}
	if err := fakeClient.Delete(context.Background(), gotNetwork); err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("delete drained network: %v", err)
	}

	// Site now removes its finalizer.
	if _, err := siteReconciler.Reconcile(context.Background(), controllerRequest(testSiteResourceName)); err != nil {
		t.Fatalf("site reconcile: %v", err)
	}
	gotSite := &unifiv1alpha1.UnifiSite{}
	err := fakeClient.Get(context.Background(), controllerRequest(testSiteResourceName).NamespacedName, gotSite)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get site after drain: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(gotSite, unifiv1alpha1.UnifiSiteFinalizer) {
		t.Errorf("site finalizer was not removed after the recursive drain")
	}
	if len(stub.called()) == 0 {
		t.Errorf("recursive drain made no upstream calls")
	}
}
