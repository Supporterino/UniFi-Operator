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
	"strings"
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
	testVpnServerResourceName = "vpn-server"
	testVpnServerUUID         = "server-uuid-1"
	testVpnServerUpstream     = "Remote Access"
	testTunnelResourceName    = "vpn-tunnel"
	testTunnelUUID            = "tunnel-uuid-1"
	testTunnelUpstream        = "Branch Tunnel"
)

func newVpnServer() *unifiv1alpha1.UnifiVpnServer {
	return &unifiv1alpha1.UnifiVpnServer{
		ObjectMeta: metav1.ObjectMeta{Name: testVpnServerResourceName, Namespace: testNamespace, UID: types.UID("vpn-server-uid")},
		Spec:       unifiv1alpha1.UnifiVpnServerSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName}, Name: testVpnServerUpstream},
	}
}

func newSiteToSiteTunnel() *unifiv1alpha1.UnifiSiteToSiteTunnel {
	return &unifiv1alpha1.UnifiSiteToSiteTunnel{
		ObjectMeta: metav1.ObjectMeta{Name: testTunnelResourceName, Namespace: testNamespace, UID: types.UID("tunnel-uid")},
		Spec:       unifiv1alpha1.UnifiSiteToSiteTunnelSpec{SiteRef: unifiv1alpha1.CoreRef{Name: testSiteResourceName}, Name: testTunnelUpstream},
	}
}

// newVpnFakeClient returns a fake client that counts writes to the VPN status
// subresources.
func newVpnFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int) {
	t.Helper()
	statusWrites := 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(
			&unifiv1alpha1.UnifiSite{},
			&unifiv1alpha1.UnifiVpnServer{},
			&unifiv1alpha1.UnifiSiteToSiteTunnel{},
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

func TestUnifiVpnServerAdopts(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{vpnServers: []unifi.VpnServer{{
		ID: testVpnServerUUID, Name: testVpnServerUpstream, Type: testVpnTypeWireguard, Enabled: true,
		Metadata: unifi.NetworkMetadata{Origin: aclRuleOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites := newVpnFakeClient(t,
		newVpnServer(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiVpnServerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testVpnServerResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := &unifiv1alpha1.UnifiVpnServer{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testVpnServerResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get server: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ServerID != testVpnServerUUID || got.Status.Type != testVpnTypeWireguard {
		t.Errorf("status = %+v, want serverID %q type WIREGUARD", got.Status, testVpnServerUUID)
	}
	if got.Status.Enabled == nil || !*got.Status.Enabled {
		t.Errorf("enabled = %v, want true", got.Status.Enabled)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("server is not owned by its UnifiSite")
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("finalizers = %v, want none: adoption has no upstream effect", got.Finalizers)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
}

func TestUnifiSiteToSiteTunnelAdopts(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{siteToSiteTunnels: []unifi.SiteToSiteTunnel{{
		ID: testTunnelUUID, Name: testTunnelUpstream, Type: testVpnTypeIpsec,
		Metadata: unifi.NetworkMetadata{Origin: aclRuleOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newVpnFakeClient(t,
		newSiteToSiteTunnel(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSiteToSiteTunnelReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testTunnelResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := &unifiv1alpha1.UnifiSiteToSiteTunnel{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testTunnelResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get tunnel: %v", err)
	}
	if cond := requireReady(t, got.Status.Conditions); cond.Status != metav1.ConditionTrue {
		t.Errorf("Ready = %s/%s, want True", cond.Status, cond.Reason)
	}
	if got.Status.TunnelID != testTunnelUUID || got.Status.Type != testVpnTypeIpsec {
		t.Errorf("status = %+v, want tunnelID %q type IPSEC", got.Status, testTunnelUUID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("tunnel is not owned by its UnifiSite")
	}
	if len(got.Finalizers) != 0 {
		t.Errorf("finalizers = %v, want none", got.Finalizers)
	}
}

func TestUnifiVpnServerUnknownFailsClosed(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{vpnServers: []unifi.VpnServer{{ID: testOtherNamespace, Name: testOtherObjectName, Type: testVpnTypeWireguard}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newVpnFakeClient(t,
		newVpnServer(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiVpnServerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testVpnServerResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != vpnServerDependencyRequeueAfter {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, vpnServerDependencyRequeueAfter)
	}

	got := &unifiv1alpha1.UnifiVpnServer{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testVpnServerResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get server: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonVpnServerNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonVpnServerNotFound)
	}
	if got.Status.ServerID != "" {
		t.Errorf("serverID = %q, want it cleared", got.Status.ServerID)
	}
}

func TestUnifiSiteToSiteTunnelUnknownFailsClosed(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{siteToSiteTunnels: []unifi.SiteToSiteTunnel{{ID: testOtherNamespace, Name: testOtherObjectName, Type: testVpnTypeIpsec}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newVpnFakeClient(t,
		newSiteToSiteTunnel(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiSiteToSiteTunnelReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testTunnelResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := &unifiv1alpha1.UnifiSiteToSiteTunnel{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testTunnelResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get tunnel: %v", err)
	}
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonTunnelNotFound {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonTunnelNotFound)
	}
}

// TestUnifiVpnStatusCarriesNoSecret asserts the VPN status never carries
// credential-like material: the read-only surface exposes no PSK and the
// reconciler authors none.
func TestUnifiVpnStatusCarriesNoSecret(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		vpnServers:        []unifi.VpnServer{{ID: testVpnServerUUID, Name: testVpnServerUpstream, Type: "OPENVPN", Enabled: true}},
		siteToSiteTunnels: []unifi.SiteToSiteTunnel{{ID: testTunnelUUID, Name: testTunnelUpstream, Type: "OPENVPN"}},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _ := newVpnFakeClient(t,
		newVpnServer(), newSiteToSiteTunnel(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	serverReconciler := &UnifiVpnServerReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := serverReconciler.Reconcile(context.Background(), controllerRequest(testVpnServerResourceName)); err != nil {
		t.Fatalf("server reconcile: %v", err)
	}
	tunnelReconciler := &UnifiSiteToSiteTunnelReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := tunnelReconciler.Reconcile(context.Background(), controllerRequest(testTunnelResourceName)); err != nil {
		t.Fatalf("tunnel reconcile: %v", err)
	}

	server := &unifiv1alpha1.UnifiVpnServer{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testVpnServerResourceName, Namespace: testNamespace}, server); err != nil {
		t.Fatalf("get server: %v", err)
	}
	tunnel := &unifiv1alpha1.UnifiSiteToSiteTunnel{}
	if err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testTunnelResourceName, Namespace: testNamespace}, tunnel); err != nil {
		t.Fatalf("get tunnel: %v", err)
	}

	for name, value := range map[string]any{"server status": server.Status, "tunnel status": tunnel.Status, "server spec": server.Spec, "tunnel spec": tunnel.Spec} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s: %v", name, err)
		}
		lower := strings.ToLower(string(data))
		for _, needle := range []string{"secret", "passphrase", "password", "psk", "preshared", "credential"} {
			if strings.Contains(lower, needle) {
				t.Errorf("%s contains %q: %s", name, needle, data)
			}
		}
	}
}

// TestUnifiVpnSpecsHaveNoOpaqueIdentifier asserts both VPN specs marshal to only
// {name, siteRef}, so no opaque upstream identifier can be authored.
func TestUnifiVpnSpecsHaveNoOpaqueIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		spec any
	}{
		{name: "vpn server", spec: newVpnServer().Spec},
		{name: "site-to-site tunnel", spec: newSiteToSiteTunnel().Spec},
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
			if len(fields) != 2 {
				t.Errorf("spec fields = %v, want exactly name and siteRef", fields)
			}
			for key := range fields {
				if key != testSpecFieldName && key != testSpecFieldSiteRef {
					t.Errorf("unexpected spec field %q; an opaque identifier is forbidden", key)
				}
			}
		})
	}
}
