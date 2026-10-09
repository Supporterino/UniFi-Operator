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
	"sync"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// Shared fixtures for the reconciler tests.
const (
	testSecretName          = "unifi-api-key"
	testSecretKey           = "api-key"
	testSecretValue         = "s3cr3t-api-key"
	testAppVersion          = "10.4.57"
	testControllerName      = "controller"
	testSiteResourceName    = "site-resource"
	testInternalReference   = "default"
	testUpstreamSiteID      = "site-uuid-1"
	testNetworkResourceName = "net"
	statusSubresource       = "status"

	testManagementUnmanaged = "UNMANAGED"
	testUnparseableVersion  = "garbage"
	testHostIPAddress       = "10.0.0.1"
)

// stubUnifiClient is a unifi.Client test double. Every method records its call
// so tests can assert that no upstream mutation happened.
type stubUnifiClient struct {
	mu sync.Mutex

	info    unifi.Info
	infoErr error

	sites    []unifi.Site
	sitesErr error

	networks    []unifi.Network
	networksErr error
	// getNetworkErr, when set, is returned by GetNetwork instead of a lookup.
	getNetworkErr error

	created   unifi.Network
	createErr error
	updated   unifi.Network
	updateErr error

	createRequests []unifi.NetworkRequest
	updateRequests []unifi.NetworkRequest
	// updatedNetworkID records the identifier passed to UpdateNetwork.
	updatedNetworkID string

	deleteErr        error
	deletedSiteID    string
	deletedNetworkID string

	zones    []unifi.FirewallZone
	zonesErr error
	// getZoneErr, when set, is returned by GetZone instead of a lookup.
	getZoneErr error

	createdZone   unifi.FirewallZone
	createZoneErr error
	updatedZone   unifi.FirewallZone
	updateZoneErr error

	zoneCreateRequests []unifi.FirewallZoneRequest
	zoneUpdateRequests []unifi.FirewallZoneRequest
	// updatedZoneID records the identifier passed to UpdateZone.
	updatedZoneID string

	deleteZoneErr     error
	deletedZoneSiteID string
	deletedZoneID     string

	deviceTags    []unifi.DeviceTag
	deviceTagsErr error

	calls []string
}

func (s *stubUnifiClient) record(call string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, call)
}

func (s *stubUnifiClient) called() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func (s *stubUnifiClient) GetInfo(context.Context) (unifi.Info, error) {
	s.record("GetInfo")
	return s.info, s.infoErr
}

func (s *stubUnifiClient) ListSites(context.Context) ([]unifi.Site, error) {
	s.record("ListSites")
	return s.sites, s.sitesErr
}

func (s *stubUnifiClient) ListNetworks(context.Context, string) ([]unifi.Network, error) {
	s.record("ListNetworks")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.networks, s.networksErr
}

// setNetworks replaces the networks the stub reports, so a test can model the
// console converging on a written state between reconciles.
func (s *stubUnifiClient) setNetworks(networks []unifi.Network) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.networks = networks
}

func (s *stubUnifiClient) GetNetwork(_ context.Context, _, networkID string) (unifi.Network, error) {
	s.record("GetNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getNetworkErr != nil {
		return unifi.Network{}, s.getNetworkErr
	}
	for _, network := range s.networks {
		if network.ID == networkID {
			return network, nil
		}
	}
	return unifi.Network{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateNetwork(_ context.Context, _ string, req unifi.NetworkRequest) (unifi.Network, error) {
	s.record("CreateNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.createRequests = append(s.createRequests, req)
	return s.created, s.createErr
}

func (s *stubUnifiClient) UpdateNetwork(_ context.Context, _, networkID string, req unifi.NetworkRequest) (unifi.Network, error) {
	s.record("UpdateNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateRequests = append(s.updateRequests, req)
	s.updatedNetworkID = networkID
	return s.updated, s.updateErr
}

func (s *stubUnifiClient) DeleteNetwork(_ context.Context, siteID, networkID string) error {
	s.record("DeleteNetwork")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedSiteID = siteID
	s.deletedNetworkID = networkID
	return s.deleteErr
}

func (s *stubUnifiClient) ListZones(context.Context, string) ([]unifi.FirewallZone, error) {
	s.record("ListZones")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zones, s.zonesErr
}

func (s *stubUnifiClient) GetZone(_ context.Context, _, zoneID string) (unifi.FirewallZone, error) {
	s.record("GetZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getZoneErr != nil {
		return unifi.FirewallZone{}, s.getZoneErr
	}
	for _, zone := range s.zones {
		if zone.ID == zoneID {
			return zone, nil
		}
	}
	return unifi.FirewallZone{}, &unifi.APIError{StatusCode: http.StatusNotFound}
}

func (s *stubUnifiClient) CreateZone(_ context.Context, _ string, req unifi.FirewallZoneRequest) (unifi.FirewallZone, error) {
	s.record("CreateZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zoneCreateRequests = append(s.zoneCreateRequests, req)
	return s.createdZone, s.createZoneErr
}

func (s *stubUnifiClient) UpdateZone(_ context.Context, _, zoneID string, req unifi.FirewallZoneRequest) (unifi.FirewallZone, error) {
	s.record("UpdateZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.zoneUpdateRequests = append(s.zoneUpdateRequests, req)
	s.updatedZoneID = zoneID
	return s.updatedZone, s.updateZoneErr
}

func (s *stubUnifiClient) DeleteZone(_ context.Context, siteID, zoneID string) error {
	s.record("DeleteZone")
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedZoneSiteID = siteID
	s.deletedZoneID = zoneID
	return s.deleteZoneErr
}

func (s *stubUnifiClient) ListDeviceTags(context.Context, string) ([]unifi.DeviceTag, error) {
	s.record("ListDeviceTags")
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.deviceTags, s.deviceTagsErr
}

// recordingFactory is a ConnectionFactory that captures the arguments a
// reconciler passes to it and returns a fixed client or error.
type recordingFactory struct {
	mu sync.Mutex

	client   unifi.Client
	err      error
	calls    int
	baseURL  string
	apiKey   string
	insecure bool
}

func (f *recordingFactory) new(baseURL, apiKey string, insecureSkipVerify bool) (unifi.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.baseURL = baseURL
	f.apiKey = apiKey
	f.insecure = insecureSkipVerify
	return f.client, f.err
}

// newTestScheme returns a scheme with the core and UniFi types registered.
func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := unifiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add unifi scheme: %v", err)
	}
	return scheme
}

// newFakeClient returns a fake client that counts writes to the status
// subresource so idempotency can be asserted.
func newFakeClient(t *testing.T, statusTypes []client.Object, objects ...client.Object) (client.Client, *int) {
	t.Helper()
	writes := 0
	builder := fake.NewClientBuilder().
		WithScheme(newTestScheme(t)).
		WithObjects(objects...).
		WithInterceptorFuncs(interceptor.Funcs{
			SubResourceUpdate: func(
				ctx context.Context,
				cl client.Client,
				subResourceName string,
				obj client.Object,
				opts ...client.SubResourceUpdateOption,
			) error {
				if subResourceName == statusSubresource {
					writes++
				}
				return cl.SubResource(subResourceName).Update(ctx, obj, opts...)
			},
		})
	if len(statusTypes) > 0 {
		builder = builder.WithStatusSubresource(statusTypes...)
	}
	return builder.Build(), &writes
}

// apiKeySecret returns a Secret holding the test API key.
func apiKeySecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
		Data:       map[string][]byte{testSecretKey: []byte(testSecretValue)},
	}
}
