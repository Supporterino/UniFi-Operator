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
	"strings"
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
	testNetworkUpstreamName = "lan"
	testUpstreamNetworkID   = "net-uuid-1"
	testUpstreamZoneID      = "zone-uuid-1"
	testSwitchDeviceTag     = "core-switch"
	testSwitchDeviceID      = "dd1b2c3d-4e5f-4a6b-7c8d-9e0f1a2b3c4d"
	testMissingDeviceTag    = "missing-tag"
	testIPv6InterfaceStatic = "STATIC"
	testIPv6HostAddress     = "fd00:10::1"
)

// ownedSite returns the parent UnifiSite fixture with a UID (needed for the
// ownerReference) and an adopted upstream site ID.
func ownedSite() *unifiv1alpha1.UnifiSite {
	site := newUnifiSite()
	site.UID = testSiteUID
	site.Status.SiteID = testUpstreamSiteID
	return site
}

// readyController returns a controller fixture that has detected the given
// application version.
func readyController(version string) *unifiv1alpha1.UnifiController {
	controller := newUnifiController(nil)
	controller.Status.ApplicationVersion = version
	return controller
}

// newGatewayNetwork returns a minimal GATEWAY-managed UnifiNetwork fixture.
func newGatewayNetwork() *unifiv1alpha1.UnifiNetwork {
	return &unifiv1alpha1.UnifiNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testNetworkResourceName,
			Namespace: testNamespace,
			UID:       types.UID("network-uid"),
		},
		Spec: unifiv1alpha1.UnifiNetworkSpec{
			SiteRef:    unifiv1alpha1.CoreRef{Name: testSiteResourceName},
			Management: networkManagementGateway,
			Name:       testNetworkUpstreamName,
			VLANID:     10,
			Gateway: &unifiv1alpha1.GatewayNetworkOptions{
				InternetAccessEnabled: true,
				IPv4Configuration: unifiv1alpha1.GatewayManagedIPv4Configuration{
					AutoScaleEnabled: true,
					HostIPAddress:    testHostIPAddress,
					PrefixLength:     24,
				},
			},
		},
	}
}

// matchingGatewayNetwork returns the upstream network that equals what
// newGatewayNetwork desires, as the console would report it (with details).
func matchingGatewayNetwork() unifi.Network {
	disabled, enabled := false, true
	return unifi.Network{
		ID:                    testUpstreamNetworkID,
		Management:            networkManagementGateway,
		Name:                  testNetworkUpstreamName,
		Enabled:               true,
		VLANID:                10,
		Metadata:              unifi.NetworkMetadata{Origin: networkOriginUserDefined},
		ZoneID:                testUpstreamZoneID,
		CellularBackupEnabled: &disabled,
		InternetAccessEnabled: &enabled,
		IsolationEnabled:      &disabled,
		IPv4Configuration: &unifi.IPv4Configuration{
			AutoScaleEnabled: true,
			HostIPAddress:    testHostIPAddress,
			PrefixLength:     24,
		},
	}
}

// newSwitchNetwork returns a SWITCH-managed UnifiNetwork fixture.
func newSwitchNetwork() *unifiv1alpha1.UnifiNetwork {
	network := newGatewayNetwork()
	network.Spec.Management = networkManagementSwitch
	network.Spec.Gateway = nil
	network.Spec.Switch = &unifiv1alpha1.SwitchNetworkOptions{
		IsolationEnabled: true,
		IPv4Configuration: unifiv1alpha1.SwitchManagedIPv4Configuration{
			HostIPAddress: "10.0.4.1",
			PrefixLength:  24,
		},
		DeviceTag: unifiv1alpha1.DeviceTagSelector{Name: testSwitchDeviceTag},
	}
	return network
}

// newNetworkFakeClient returns a fake client that counts writes to the network
// status subresource and to object metadata, so idempotency can be asserted.
func newNetworkFakeClient(t *testing.T, objects ...client.Object) (client.Client, *int, *int) {
	t.Helper()
	statusWrites, metadataWrites := 0, 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithStatusSubresource(&unifiv1alpha1.UnifiSite{}, &unifiv1alpha1.UnifiNetwork{}).
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

// getNetwork loads the reconciled UnifiNetwork fixture.
func getNetwork(t *testing.T, c client.Client) *unifiv1alpha1.UnifiNetwork {
	t.Helper()
	got := &unifiv1alpha1.UnifiNetwork{}
	if err := c.Get(context.Background(), types.NamespacedName{Name: testNetworkResourceName, Namespace: testNamespace}, got); err != nil {
		t.Fatalf("get network after reconcile: %v", err)
	}
	return got
}

func TestUnifiNetworkReconcileCreatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		created: unifi.Network{ID: testUpstreamNetworkID, Name: testNetworkUpstreamName, ZoneID: testUpstreamZoneID},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newNetworkFakeClient(t,
		newGatewayNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	result, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName))
	if err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if result.RequeueAfter != 0 {
		t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
	}

	got := getNetwork(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.ObservedGeneration != got.Generation {
		t.Errorf("observedGeneration = %d, want %d", got.Status.ObservedGeneration, got.Generation)
	}
	if got.Status.NetworkID != testUpstreamNetworkID {
		t.Errorf("networkID = %q, want %q", got.Status.NetworkID, testUpstreamNetworkID)
	}
	if got.Status.ZoneID != testUpstreamZoneID {
		t.Errorf("zoneID = %q, want %q", got.Status.ZoneID, testUpstreamZoneID)
	}
	if !metav1.IsControlledBy(got, ownedSite()) {
		t.Errorf("network is not owned by its UnifiSite")
	}
	if !controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiNetworkFinalizer) {
		t.Errorf("network finalizer was not added")
	}

	calls := stub.called()
	if !slices.Contains(calls, "CreateNetwork") {
		t.Errorf("upstream calls = %v, want CreateNetwork", calls)
	}
	if slices.Contains(calls, "UpdateNetwork") {
		t.Errorf("upstream calls = %v, want no UpdateNetwork on a create", calls)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (ownerReference + finalizer)", *metadataWrites)
	}
}

func TestUnifiNetworkReconcileUpdatesUpstream(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{
		networks: []unifi.Network{{
			ID:       testUpstreamNetworkID,
			Name:     testNetworkUpstreamName,
			ZoneID:   testUpstreamZoneID,
			Metadata: unifi.NetworkMetadata{Origin: networkOriginUserDefined},
		}},
		updated: unifi.Network{ID: testUpstreamNetworkID, Name: testNetworkUpstreamName, ZoneID: testUpstreamZoneID},
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		newGatewayNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getNetwork(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if got.Status.NetworkID != testUpstreamNetworkID {
		t.Errorf("networkID = %q, want %q", got.Status.NetworkID, testUpstreamNetworkID)
	}

	calls := stub.called()
	if !slices.Contains(calls, "UpdateNetwork") {
		t.Errorf("upstream calls = %v, want UpdateNetwork", calls)
	}
	if slices.Contains(calls, "CreateNetwork") {
		t.Errorf("upstream calls = %v, want no CreateNetwork when the network exists", calls)
	}
	if len(stub.createRequests) != 0 {
		t.Errorf("create requests = %d, want 0", len(stub.createRequests))
	}
	if len(stub.updateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1", len(stub.updateRequests))
	}
	// The write body must not carry a zoneId (design D5).
	encoded, err := json.Marshal(stub.updateRequests[0])
	if err != nil {
		t.Fatalf("marshal update request: %v", err)
	}
	if strings.Contains(string(encoded), "zoneId") {
		t.Errorf("update request body %s must not contain zoneId", encoded)
	}
}

func TestUnifiNetworkReconcileFailsClosed(t *testing.T) {
	t.Parallel()

	deletingSite := ownedSite()
	deletingSite.Finalizers = []string{unifiv1alpha1.UnifiSiteFinalizer}
	deletingSite.DeletionTimestamp = &metav1.Time{Time: metav1.Now().Time}

	notAdoptedSite := newUnifiSite()
	notAdoptedSite.UID = testSiteUID

	tests := []struct {
		name        string
		network     *unifiv1alpha1.UnifiNetwork
		site        *unifiv1alpha1.UnifiSite
		controller  *unifiv1alpha1.UnifiController
		wantReason  string
		wantRequeue bool
	}{
		{
			name:        "site missing",
			network:     newGatewayNetwork(),
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "site being deleted",
			network:     newGatewayNetwork(),
			site:        deletingSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteRefNotFound,
			wantRequeue: true,
		},
		{
			name:        "site not adopted",
			network:     newGatewayNetwork(),
			site:        notAdoptedSite,
			controller:  readyController(testAppVersion),
			wantReason:  reasonSiteNotAdopted,
			wantRequeue: true,
		},
		{
			name:        "controller missing",
			network:     newGatewayNetwork(),
			site:        ownedSite(),
			wantReason:  reasonControllerNotFound,
			wantRequeue: true,
		},
		{
			name:       "application version below minimum",
			network:    newGatewayNetwork(),
			site:       ownedSite(),
			controller: readyController("10.0.161"),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:       "version unparseable",
			network:    newGatewayNetwork(),
			site:       ownedSite(),
			controller: readyController(testUnparseableVersion),
			wantReason: reasonVersionUnsupported,
		},
		{
			name:        "application version not reported",
			network:     newGatewayNetwork(),
			site:        ownedSite(),
			controller:  newUnifiController(nil),
			wantReason:  reasonDependencyNotReady,
			wantRequeue: true,
		},
		{
			name:       "switch device tag missing",
			network:    newSwitchNetwork(),
			site:       ownedSite(),
			controller: readyController(testAppVersion),
			wantReason: reasonDeviceTagNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objects := []client.Object{tt.network}
			if tt.site != nil {
				objects = append(objects, tt.site)
			}
			if tt.controller != nil {
				objects = append(objects, tt.controller, apiKeySecret())
			}

			stub := &stubUnifiClient{}
			factory := &recordingFactory{client: stub}
			fakeClient, statusWrites, _ := newNetworkFakeClient(t, objects...)
			reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			result, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName))
			if err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}
			if tt.wantRequeue && result.RequeueAfter != networkDependencyRequeueAfter {
				t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, networkDependencyRequeueAfter)
			}
			if !tt.wantRequeue && result.RequeueAfter != 0 {
				t.Errorf("RequeueAfter = %v, want 0", result.RequeueAfter)
			}
			if *statusWrites != 1 {
				t.Fatalf("status writes = %d, want 1", *statusWrites)
			}

			got := getNetwork(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
				t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
			}
			if mutations := upstreamMutations(stub.called()); len(mutations) != 0 {
				t.Errorf("upstream mutations = %v, want none", mutations)
			}
		})
	}
}

func TestUnifiNetworkReconcileSwitchDeviceTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		tags       []unifi.DeviceTag
		wantReason string
		wantDevice string
		wantCreate bool
	}{
		{
			name:       "single device resolves",
			tags:       []unifi.DeviceTag{{Name: testSwitchDeviceTag, DeviceIDs: []string{testSwitchDeviceID}}},
			wantDevice: testSwitchDeviceID,
			wantCreate: true,
		},
		{
			name:       "tag missing fails closed",
			tags:       []unifi.DeviceTag{{Name: testMissingDeviceTag, DeviceIDs: []string{testSwitchDeviceID}}},
			wantReason: reasonDeviceTagNotFound,
		},
		{
			name:       "zero devices fails closed",
			tags:       []unifi.DeviceTag{{Name: testSwitchDeviceTag}},
			wantReason: reasonDeviceTagAmbiguous,
		},
		{
			name:       "multiple devices fails closed",
			tags:       []unifi.DeviceTag{{Name: testSwitchDeviceTag, DeviceIDs: []string{testSwitchDeviceID, "device-2"}}},
			wantReason: reasonDeviceTagAmbiguous,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stub := &stubUnifiClient{
				deviceTags: tt.tags,
				created:    unifi.Network{ID: testUpstreamNetworkID, Name: testNetworkUpstreamName},
			}
			factory := &recordingFactory{client: stub}
			fakeClient, _, _ := newNetworkFakeClient(t,
				newSwitchNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())
			reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

			if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
				t.Fatalf("Reconcile error = %v, want nil", err)
			}

			got := getNetwork(t, fakeClient)
			cond := requireReady(t, got.Status.Conditions)
			if tt.wantReason != "" {
				if cond.Status != metav1.ConditionFalse || cond.Reason != tt.wantReason {
					t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, tt.wantReason)
				}
				if mutations := upstreamMutations(stub.called()); len(mutations) != 0 {
					t.Errorf("upstream mutations = %v, want none", mutations)
				}
				return
			}
			if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
				t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
			}
			if !slices.Contains(stub.called(), "ListDeviceTags") {
				t.Errorf("upstream calls = %v, want ListDeviceTags", stub.called())
			}
			if tt.wantCreate {
				if len(stub.createRequests) != 1 {
					t.Fatalf("create requests = %d, want 1", len(stub.createRequests))
				}
				req := stub.createRequests[0]
				if req.Switch == nil {
					t.Fatalf("create request switch = nil, want the resolved switch variant")
				}
				if req.Switch.DeviceID != tt.wantDevice {
					t.Errorf("create request deviceID = %q, want %q", req.Switch.DeviceID, tt.wantDevice)
				}
			}
		})
	}
}

func TestUnifiNetworkReconcileSwitchBelowOfficialAPIMinimum(t *testing.T) {
	t.Parallel()

	// 10.1.0 satisfies the networks minimum (10.0.162) but not the Official API
	// minimum (10.1.78) the device-tags list requires, so the switch binding
	// fails closed without attempting the tag list.
	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		newSwitchNetwork(), ownedSite(), readyController("10.1.0"), apiKeySecret())
	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getNetwork(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonVersionUnsupported {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonVersionUnsupported)
	}
	if calls := stub.called(); len(calls) != 0 {
		t.Errorf("upstream calls = %v, want none below the device-tags minimum", calls)
	}
}

func TestUnifiNetworkReconcilePrefixDelegationFailsClosed(t *testing.T) {
	t.Parallel()

	network := newGatewayNetwork()
	network.Spec.Gateway.IPv6Configuration = &unifiv1alpha1.IPv6Configuration{
		InterfaceType: "PREFIX_DELEGATION",
		ClientAddressAssignment: unifiv1alpha1.IPv6ClientAddressAssignment{
			SLAACEnabled: true,
		},
		PrefixDelegationWAN: &unifiv1alpha1.WANSelector{Name: "wan"},
	}

	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		network, ownedSite(), readyController(testAppVersion), apiKeySecret())
	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}

	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getNetwork(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonWANLookupUnsupported {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonWANLookupUnsupported)
	}
	if mutations := upstreamMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none", mutations)
	}
}

func TestUnifiNetworkReconcileIsIdempotent(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{networks: []unifi.Network{matchingGatewayNetwork()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, metadataWrites := newNetworkFakeClient(t,
		newGatewayNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
	if *metadataWrites != 1 {
		t.Errorf("metadata writes = %d, want 1 (second reconcile must be a no-op)", *metadataWrites)
	}
	if len(stub.createRequests) != 0 || len(stub.updateRequests) != 0 {
		t.Errorf("upstream mutations = create:%d update:%d, want none on an unchanged object",
			len(stub.createRequests), len(stub.updateRequests))
	}
}

func TestUnifiNetworkReconcileIgnoresOptionalFieldsTheCROmits(t *testing.T) {
	t.Parallel()

	// The CR authors no mdnsForwardingEnabled, but the console reports it
	// enabled. Its contract is "omit to use the site mDNS default", so an
	// observed concrete value is not desired state: the reconciler must not
	// treat it as drift and must issue no write on either reconcile.
	observed := matchingGatewayNetwork()
	mdnsEnabled := true
	observed.MDNSForwardingEnabled = &mdnsEnabled

	stub := &stubUnifiClient{networks: []unifi.Network{observed}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newNetworkFakeClient(t,
		newGatewayNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}

	if len(stub.createRequests) != 0 || len(stub.updateRequests) != 0 {
		t.Errorf("upstream mutations = create:%d update:%d, want none when the CR omits mdnsForwardingEnabled",
			len(stub.createRequests), len(stub.updateRequests))
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
}

func TestUnifiNetworkReconcileIgnoresUnsetDNSServerOverride(t *testing.T) {
	t.Parallel()

	// The CR configures IPv6 but leaves dnsServerIpAddressesOverride unset, so
	// the override is selected automatically. The console reporting concrete
	// override addresses (and additional subnets and a router advertisement the
	// CR also omits) is not drift: no write on either reconcile.
	network := newGatewayNetwork()
	network.Spec.Gateway.IPv6Configuration = &unifiv1alpha1.IPv6Configuration{
		InterfaceType:           testIPv6InterfaceStatic,
		ClientAddressAssignment: unifiv1alpha1.IPv6ClientAddressAssignment{SLAACEnabled: true},
		HostIPAddress:           testIPv6HostAddress,
		PrefixLength:            64,
	}

	observed := matchingGatewayNetwork()
	observed.IPv6Configuration = &unifi.IPv6Configuration{
		InterfaceType:                testIPv6InterfaceStatic,
		ClientAddressAssignment:      unifi.IPv6ClientAddressAssignment{SLAACEnabled: true},
		AdditionalHostIPSubnets:      []string{"fd00:10::/64"},
		DNSServerIPAddressesOverride: []string{"fd00::1"},
		RouterAdvertisement:          &unifi.RouterAdvertisement{Priority: "HIGH"},
		HostIPAddress:                testIPv6HostAddress,
		PrefixLength:                 64,
	}

	stub := &stubUnifiClient{networks: []unifi.Network{observed}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newNetworkFakeClient(t,
		network, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}

	if len(stub.createRequests) != 0 || len(stub.updateRequests) != 0 {
		t.Errorf("upstream mutations = create:%d update:%d, want none when the CR leaves optional IPv6 fields unset",
			len(stub.createRequests), len(stub.updateRequests))
	}
	if *statusWrites != 1 {
		t.Errorf("status writes = %d, want 1 (second reconcile must be a no-op)", *statusWrites)
	}
}

func TestUnifiNetworkReconcileClearsOmittedOptionalFields(t *testing.T) {
	t.Parallel()

	// The CR omits dhcpGuarding and ipv6Configuration, which the contract reads
	// as "disabled" and "unconfigured". The console still reports a configured
	// value for each, so the first reconcile must issue a PUT whose body omits
	// both fields. Once the console converges on the absent state, the second
	// reconcile must be a no-op.
	configured := matchingGatewayNetwork()
	configured.DHCPGuarding = &unifi.DHCPGuarding{
		TrustedDHCPServerIPAddresses: []string{"192.168.1.254"},
	}
	configured.IPv6Configuration = &unifi.IPv6Configuration{
		InterfaceType:           testIPv6InterfaceStatic,
		ClientAddressAssignment: unifi.IPv6ClientAddressAssignment{SLAACEnabled: true},
		HostIPAddress:           testIPv6HostAddress,
		PrefixLength:            64,
	}

	stub := &stubUnifiClient{networks: []unifi.Network{configured}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		newGatewayNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}

	if len(stub.updateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1 (a configured observed value the CR omits is drift)", len(stub.updateRequests))
	}
	encoded, err := json.Marshal(stub.updateRequests[0])
	if err != nil {
		t.Fatalf("marshal update request: %v", err)
	}
	if strings.Contains(string(encoded), "dhcpGuarding") {
		t.Errorf("update request body %s must omit dhcpGuarding to disable the feature", encoded)
	}
	if strings.Contains(string(encoded), "ipv6Configuration") {
		t.Errorf("update request body %s must omit ipv6Configuration to leave IPv6 unconfigured", encoded)
	}

	// The console now reports both fields absent; re-reconciling must not write.
	stub.setNetworks([]unifi.Network{matchingGatewayNetwork()})
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("second reconcile: %v", err)
	}
	if len(stub.updateRequests) != 1 {
		t.Errorf("update requests = %d, want 1 after the console reports the fields absent", len(stub.updateRequests))
	}
}

func TestUnifiNetworkFinalizerDeletesUpstream(t *testing.T) {
	t.Parallel()

	network := newGatewayNetwork()
	network.Finalizers = []string{unifiv1alpha1.UnifiNetworkFinalizer}
	network.Status.NetworkID = testUpstreamNetworkID

	stub := &stubUnifiClient{networks: []unifi.Network{{
		ID:       testUpstreamNetworkID,
		Metadata: unifi.NetworkMetadata{Origin: networkOriginUserDefined},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		network, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), network); err != nil {
		t.Fatalf("delete network: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if stub.deletedSiteID != testUpstreamSiteID || stub.deletedNetworkID != testUpstreamNetworkID {
		t.Errorf("DeleteNetwork(%q, %q), want (%q, %q)",
			stub.deletedSiteID, stub.deletedNetworkID, testUpstreamSiteID, testUpstreamNetworkID)
	}
	if !slices.Contains(stub.called(), "DeleteNetwork") {
		t.Errorf("upstream calls = %v, want DeleteNetwork", stub.called())
	}

	got := &unifiv1alpha1.UnifiNetwork{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testNetworkResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get network after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiNetworkFinalizer) {
		t.Errorf("network finalizer was not removed after upstream deletion")
	}
}

func TestUnifiNetworkReconcileFailsClosedOnSystemObject(t *testing.T) {
	t.Parallel()

	systemNetwork := matchingGatewayNetwork()
	systemNetwork.Metadata.Origin = "SYSTEM_DEFINED"

	stub := &stubUnifiClient{networks: []unifi.Network{systemNetwork}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		newGatewayNetwork(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getNetwork(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionFalse || cond.Reason != reasonSystemObjectReadOnly {
		t.Errorf("Ready = %s/%s, want False/%s", cond.Status, cond.Reason, reasonSystemObjectReadOnly)
	}
	if mutations := upstreamMutations(stub.called()); len(mutations) != 0 {
		t.Errorf("upstream mutations = %v, want none for a system-managed object", mutations)
	}
}

func TestUnifiNetworkReconcileResolvesByStatusID(t *testing.T) {
	t.Parallel()

	network := newGatewayNetwork()
	network.Status.NetworkID = testUpstreamNetworkID

	// The upstream object has been renamed but is still the one recorded in
	// status; the reconciler must update it in place rather than create a
	// second network.
	renamed := matchingGatewayNetwork()
	renamed.Name = "renamed-upstream"
	stub := &stubUnifiClient{
		networks: []unifi.Network{renamed},
		updated:  renamed,
	}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		network, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	got := getNetwork(t, fakeClient)
	cond := requireReady(t, got.Status.Conditions)
	if cond.Status != metav1.ConditionTrue || cond.Reason != reasonReconciled {
		t.Errorf("Ready = %s/%s, want True/%s", cond.Status, cond.Reason, reasonReconciled)
	}
	if len(stub.createRequests) != 0 {
		t.Errorf("create requests = %d, want 0 (rename must update in place)", len(stub.createRequests))
	}
	if len(stub.updateRequests) != 1 {
		t.Fatalf("update requests = %d, want 1", len(stub.updateRequests))
	}
	if stub.updatedNetworkID != testUpstreamNetworkID {
		t.Errorf("updated network = %q, want the status.networkID %q", stub.updatedNetworkID, testUpstreamNetworkID)
	}
	if got.Status.NetworkID != testUpstreamNetworkID {
		t.Errorf("networkID = %q, want %q", got.Status.NetworkID, testUpstreamNetworkID)
	}
}

func TestUnifiNetworkFinalizerSkipsSystemObject(t *testing.T) {
	t.Parallel()

	network := newGatewayNetwork()
	network.Finalizers = []string{unifiv1alpha1.UnifiNetworkFinalizer}
	network.Status.NetworkID = testUpstreamNetworkID

	stub := &stubUnifiClient{networks: []unifi.Network{{
		ID:       testUpstreamNetworkID,
		Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"},
	}}}
	factory := &recordingFactory{client: stub}
	fakeClient, _, _ := newNetworkFakeClient(t,
		network, ownedSite(), readyController(testAppVersion), apiKeySecret())

	reconciler := &UnifiNetworkReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if err := fakeClient.Delete(context.Background(), network); err != nil {
		t.Fatalf("delete network: %v", err)
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testNetworkResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	if slices.Contains(stub.called(), "DeleteNetwork") {
		t.Errorf("upstream calls = %v, want no DeleteNetwork for a system-managed object", stub.called())
	}

	got := &unifiv1alpha1.UnifiNetwork{}
	err := fakeClient.Get(context.Background(), types.NamespacedName{Name: testNetworkResourceName, Namespace: testNamespace}, got)
	if err != nil && !apierrors.IsNotFound(err) {
		t.Fatalf("get network after finalizer: %v", err)
	}
	if err == nil && controllerutil.ContainsFinalizer(got, unifiv1alpha1.UnifiNetworkFinalizer) {
		t.Errorf("network finalizer was not removed when the upstream object is system-managed")
	}
}

// upstreamMutations filters recorded calls down to the ones that change
// upstream state.
func upstreamMutations(calls []string) []string {
	mutations := []string{"CreateNetwork", "UpdateNetwork", "DeleteNetwork"}
	var found []string
	for _, call := range calls {
		if slices.Contains(mutations, call) {
			found = append(found, call)
		}
	}
	return found
}
