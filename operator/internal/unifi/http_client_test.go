package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testSiteID    = "9f7f6a2e-1c1a-4a6d-9d3e-2f0f7a2b1c3d"
	testNetworkID = "1b2c3d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"
	testAPIKey    = "test-api-key"

	testNetworkName   = "lan"
	testHostIPAddress = "10.0.0.1"
)

func newTestClient(t *testing.T, handler http.Handler) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL, testAPIKey, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func TestGetInfo(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
	}))

	info, err := client.GetInfo(context.Background())
	if err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if info.ApplicationVersion != "10.4.57" {
		t.Errorf("ApplicationVersion = %q, want %q", info.ApplicationVersion, "10.4.57")
	}
	if want := "/proxy/network/integration/v1/info"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
}

func TestListSitesPaginates(t *testing.T) {
	t.Parallel()

	var offsets []string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/proxy/network/integration/v1/sites" {
			t.Errorf("path = %q", r.URL.Path)
		}
		offset := r.URL.Query().Get("offset")
		offsets = append(offsets, offset)
		if limit := r.URL.Query().Get("limit"); limit != "200" {
			t.Errorf("limit = %q, want 200", limit)
		}
		switch offset {
		case "0":
			_, _ = io.WriteString(w, `{"count":2,"data":[
				{"id":"site-1","internalReference":"default","name":"Default"},
				{"id":"site-2","internalReference":"branch","name":"Branch"}],
				"limit":2,"offset":0,"totalCount":3}`)
		case "2":
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"site-3","internalReference":"lab","name":"Lab"}],
				"limit":2,"offset":2,"totalCount":3}`)
		default:
			t.Errorf("unexpected offset %q", offset)
		}
	}))

	sites, err := client.ListSites(context.Background())
	if err != nil {
		t.Fatalf("ListSites: %v", err)
	}
	if len(sites) != 3 {
		t.Fatalf("got %d sites, want 3: %+v", len(sites), sites)
	}
	if sites[0].InternalReference != "default" || sites[2].Name != "Lab" {
		t.Errorf("unexpected sites: %+v", sites)
	}
	if len(offsets) != 2 {
		t.Errorf("offsets = %v, want two pages", offsets)
	}
}

func TestListNetworksPaginates(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if r.URL.Query().Get("offset") == "0" {
			_, _ = io.WriteString(w, `{"count":1,"data":[
				{"id":"net-1","management":"UNMANAGED","name":"lan","enabled":true,"vlanId":1,"default":true,"metadata":{"origin":"USER_DEFINED"}}],
				"limit":1,"offset":0,"totalCount":2}`)
			return
		}
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"net-2","management":"GATEWAY","name":"iot","enabled":true,"vlanId":20,"default":false,"metadata":{"origin":"USER_DEFINED"},"zoneId":"zone-1"}],
			"limit":1,"offset":1,"totalCount":2}`)
	}))

	networks, err := client.ListNetworks(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/networks", testSiteID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if len(networks) != 2 {
		t.Fatalf("got %d networks, want 2", len(networks))
	}
	if networks[1].ZoneID != "zone-1" || networks[0].Default != true {
		t.Errorf("unexpected networks: %+v", networks)
	}
}

func TestGetNetwork(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{
			"id":"`+testNetworkID+`","management":"GATEWAY","name":"lan","enabled":true,
			"vlanId":10,"default":false,"metadata":{"origin":"USER_DEFINED"},
			"zoneId":"zone-1","cellularBackupEnabled":true,"internetAccessEnabled":true,
			"isolationEnabled":false,"mdnsForwardingEnabled":true,
			"ipv4Configuration":{"autoScaleEnabled":true,"hostIpAddress":"10.0.0.1","prefixLength":24},
			"ipv6Configuration":{"interfaceType":"STATIC","clientAddressAssignment":{"slaacEnabled":true},"hostIpAddress":"fd00::1","prefixLength":64}}`)
	}))

	network, err := client.GetNetwork(context.Background(), testSiteID, testNetworkID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/networks/%s", testSiteID, testNetworkID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if network.ID != testNetworkID || network.Management != managementGateway || network.ZoneID != "zone-1" {
		t.Errorf("unexpected network: %+v", network)
	}
	if network.CellularBackupEnabled == nil || !*network.CellularBackupEnabled {
		t.Errorf("CellularBackupEnabled = %v, want true", network.CellularBackupEnabled)
	}
	if network.IPv4Configuration == nil || network.IPv4Configuration.HostIPAddress != testHostIPAddress {
		t.Errorf("IPv4Configuration = %+v", network.IPv4Configuration)
	}
	if network.IPv6Configuration == nil || network.IPv6Configuration.InterfaceType != "STATIC" {
		t.Errorf("IPv6Configuration = %+v", network.IPv6Configuration)
	}
}

// decodeBody decodes a request body into a generic map for shape assertions.
func decodeBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("decode body %q: %v", raw, err)
	}
	return body
}

func TestCreateNetworkGateway(t *testing.T) {
	t.Parallel()

	var gotMethod string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		body := decodeBody(t, r)
		if body["management"] != managementGateway {
			t.Errorf("management = %v, want GATEWAY", body["management"])
		}
		if _, ok := body["gateway"]; ok {
			t.Errorf("body must be flat, got nested gateway: %v", body["gateway"])
		}
		if body["cellularBackupEnabled"] != true || body["internetAccessEnabled"] != true {
			t.Errorf("gateway booleans = %v", body)
		}
		ipv4, ok := body["ipv4Configuration"].(map[string]any)
		if !ok || ipv4["hostIpAddress"] != testHostIPAddress {
			t.Errorf("ipv4Configuration = %v", body["ipv4Configuration"])
		}
		if _, ok := body["zoneId"]; ok {
			t.Errorf("request must not set zoneId: %v", body["zoneId"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testNetworkID+`","management":"GATEWAY","name":"lan","enabled":true,"vlanId":10,"default":false,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	req := NetworkRequest{
		Management: managementGateway,
		Name:       testNetworkName,
		Enabled:    true,
		VLANID:     10,
		Gateway: &GatewayNetworkRequest{
			CellularBackupEnabled: true,
			InternetAccessEnabled: true,
			IsolationEnabled:      false,
			IPv4Configuration: IPv4Configuration{
				AutoScaleEnabled: true,
				HostIPAddress:    testHostIPAddress,
				PrefixLength:     24,
			},
		},
	}
	network, err := client.CreateNetwork(context.Background(), testSiteID, req)
	if err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if network.ID != testNetworkID {
		t.Errorf("network ID = %q, want %q", network.ID, testNetworkID)
	}
}

func TestCreateNetworkSwitch(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		if body["management"] != managementSwitch {
			t.Errorf("management = %v, want SWITCH", body["management"])
		}
		if body["deviceId"] != "device-uuid" {
			t.Errorf("deviceId = %v, want device-uuid", body["deviceId"])
		}
		if _, ok := body["internetAccessEnabled"]; ok {
			t.Errorf("switch body must not carry gateway fields: %v", body)
		}
		if _, ok := body["ipv6Configuration"]; ok {
			t.Errorf("switch body must not carry ipv6Configuration: %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testNetworkID+`","management":"SWITCH","name":"lan","enabled":true,"vlanId":10,"default":false,"metadata":{"origin":"USER_DEFINED"},"deviceId":"device-uuid"}`)
	}))

	req := NetworkRequest{
		Management: managementSwitch,
		Name:       testNetworkName,
		Enabled:    true,
		VLANID:     10,
		Switch: &SwitchNetworkRequest{
			CellularBackupEnabled: false,
			IsolationEnabled:      true,
			IPv4Configuration: IPv4Configuration{
				AutoScaleEnabled: false,
				HostIPAddress:    "10.0.1.1",
				PrefixLength:     24,
			},
			DeviceID: "device-uuid",
		},
	}
	if _, err := client.CreateNetwork(context.Background(), testSiteID, req); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
}

func TestCreateNetworkUnmanaged(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := decodeBody(t, r)
		if body["management"] != managementUnmanaged {
			t.Errorf("management = %v, want UNMANAGED", body["management"])
		}
		for _, key := range []string{"gateway", "switch", "deviceId", "ipv4Configuration"} {
			if _, ok := body[key]; ok {
				t.Errorf("unmanaged body must not carry %q: %v", key, body)
			}
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":"`+testNetworkID+`","management":"UNMANAGED","name":"lan","enabled":true,"vlanId":10,"default":false,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	req := NetworkRequest{Management: managementUnmanaged, Name: testNetworkName, Enabled: true, VLANID: 10}
	if _, err := client.CreateNetwork(context.Background(), testSiteID, req); err != nil {
		t.Fatalf("CreateNetwork: %v", err)
	}
}

func TestUpdateNetwork(t *testing.T) {
	t.Parallel()

	var gotMethod, gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		_, _ = io.WriteString(w, `{"id":"`+testNetworkID+`","management":"UNMANAGED","name":"lan","enabled":true,"vlanId":10,"default":false,"metadata":{"origin":"USER_DEFINED"}}`)
	}))

	req := NetworkRequest{Management: managementUnmanaged, Name: testNetworkName, Enabled: true, VLANID: 10}
	if _, err := client.UpdateNetwork(context.Background(), testSiteID, testNetworkID, req); err != nil {
		t.Fatalf("UpdateNetwork: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/networks/%s", testSiteID, testNetworkID); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestDeleteNetwork(t *testing.T) {
	t.Parallel()

	var gotMethod string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.DeleteNetwork(context.Background(), testSiteID, testNetworkID); err != nil {
		t.Fatalf("DeleteNetwork: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", gotMethod)
	}
}

func TestCreateNetworkRejectsVariantMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  NetworkRequest
	}{
		{name: "gateway management without gateway fields", req: NetworkRequest{Management: managementGateway, Name: testNetworkName, VLANID: 1}},
		{name: "switch management without switch fields", req: NetworkRequest{Management: managementSwitch, Name: testNetworkName, VLANID: 1}},
		{name: "unmanaged with gateway fields", req: NetworkRequest{Management: managementUnmanaged, Name: testNetworkName, VLANID: 1, Gateway: &GatewayNetworkRequest{}}},
		{name: "unknown management", req: NetworkRequest{Management: "OTHER", Name: testNetworkName, VLANID: 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			client := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))

			_, err := client.CreateNetwork(context.Background(), testSiteID, tt.req)
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("error = %v, want *InvalidSpecError", err)
			}
			if called {
				t.Errorf("invalid request reached the controller")
			}
		})
	}
}

func TestPathTraversalRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		siteID    string
		networkID string
	}{
		{name: "site slash traversal", siteID: "../../evil"},
		{name: "site parent element", siteID: ".."},
		{name: "site current element", siteID: "."},
		{name: "site forward slash", siteID: "a/b"},
		{name: "site backslash", siteID: `a\b`},
		{name: "site empty", siteID: ""},
		{name: "network parent element", siteID: testSiteID, networkID: ".."},
		{name: "network separator", siteID: testSiteID, networkID: "a/b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			client := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))

			_, err := client.GetNetwork(context.Background(), tt.siteID, tt.networkID)
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("error = %v, want *InvalidSpecError", err)
			}
			if called {
				t.Errorf("unsafe path reached the controller")
			}
		})
	}
}

func TestAPIErrorParsesFlatBody(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{
			"statusCode":401,"statusName":"UNAUTHORIZED",
			"code":"api.authentication.missing-credentials","message":"Missing credentials",
			"timestamp":"2024-11-27T08:13:46.966Z","requestPath":"/integration/v1/sites/123",
			"requestId":"3fa85f64-5717-4562-b3fc-2c963f66afa6"}`)
	}))

	_, err := client.GetInfo(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if apiErr.StatusName != "UNAUTHORIZED" || apiErr.Code != "api.authentication.missing-credentials" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if apiErr.Message != "Missing credentials" || apiErr.RequestID != "3fa85f64-5717-4562-b3fc-2c963f66afa6" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if apiErr.RequestPath != "/integration/v1/sites/123" || apiErr.Timestamp == "" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if apiErr.Retryable() {
		t.Errorf("401 must not be retryable")
	}
}

func TestAPIErrorRetryable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		wantRetryable bool
	}{
		{name: "server error is transient", status: http.StatusInternalServerError, wantRetryable: true},
		{name: "bad gateway is transient", status: http.StatusBadGateway, wantRetryable: true},
		{name: "too many requests is transient", status: http.StatusTooManyRequests, wantRetryable: true},
		{name: "bad request is terminal", status: http.StatusBadRequest, wantRetryable: false},
		{name: "unauthorized is terminal", status: http.StatusUnauthorized, wantRetryable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprintf(w, `{"statusCode":%d,"message":"boom"}`, tt.status)
			}))

			_, err := client.GetInfo(context.Background())
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error = %v, want *APIError", err)
			}
			if apiErr.Retryable() != tt.wantRetryable {
				t.Errorf("Retryable() = %v, want %v", apiErr.Retryable(), tt.wantRetryable)
			}
		})
	}
}

func TestAPIErrorNotFoundMatchesSentinel(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND","message":"not found"}`)
	}))

	_, err := client.GetNetwork(context.Background(), testSiteID, testNetworkID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("errors.Is(err, ErrNotFound) = false, err = %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
}

func TestNewClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "bad scheme", baseURL: "ftp://controller.local"},
		{name: "no host", baseURL: "https://"},
		{name: "relative", baseURL: "controller.local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(tt.baseURL, "", false); err == nil {
				t.Fatalf("NewClient(%q) = nil error, want error", tt.baseURL)
			}
		})
	}
}

func TestNewClientRejectsURLCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "parseable userinfo", baseURL: "https://user:s3cret@controller.local"},
		{name: "malformed credential URL", baseURL: "https://user:s3cret@contr oller.local"},
		{name: "opaque empty-host credential URL", baseURL: "https:user:s3cret@controller.local"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClient(tt.baseURL, "", false)
			if err == nil {
				t.Fatalf("NewClient(%q) = nil error, want rejection", tt.baseURL)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error leaked credentials: %q", err.Error())
			}
		})
	}
}

func TestClientTLSVerification(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
	}))
	t.Cleanup(srv.Close)

	verified, err := NewClient(srv.URL, testAPIKey, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := verified.GetInfo(context.Background()); err == nil {
		t.Errorf("GetInfo with verification against a self-signed server = nil error, want TLS error")
	}

	insecure, err := NewClient(srv.URL, testAPIKey, true)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := insecure.GetInfo(context.Background()); err != nil {
		t.Errorf("GetInfo with insecureSkipVerify = %v, want success", err)
	}
}
