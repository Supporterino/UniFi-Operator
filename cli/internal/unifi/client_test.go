package unifi

import (
	"context"
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
)

func newTestClient(t *testing.T, handler http.Handler) *Client {
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
		gotAPIKey = r.Header.Get(APIKeyHeader)
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
		if want := "/proxy/network/integration/v1/sites"; r.URL.Path != want {
			t.Errorf("path = %q, want %q", r.URL.Path, want)
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
	if networks[0].Enabled == nil || !*networks[0].Enabled {
		t.Errorf("Enabled = %v, want true", networks[0].Enabled)
	}
}

func TestListNetworksDecodesUnionFields(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"count":1,"data":[
			{"id":"`+testNetworkID+`","management":"GATEWAY","name":"lan","enabled":true,
			 "vlanId":10,"default":false,"metadata":{"origin":"USER_DEFINED"},
			 "zoneId":"zone-1","cellularBackupEnabled":true,"internetAccessEnabled":true,
			 "isolationEnabled":false,"mdnsForwardingEnabled":true,
			 "dhcpGuarding":{"trustedDhcpServerIpAddresses":["10.0.0.2"]},
			 "ipv4Configuration":{"autoScaleEnabled":true,"hostIpAddress":"10.0.0.1","prefixLength":24},
			 "ipv6Configuration":{"interfaceType":"STATIC","clientAddressAssignment":{"slaacEnabled":true},
			   "hostIpAddress":"fd00::1","prefixLength":64}}],
			"limit":200,"offset":0,"totalCount":1}`)
	}))

	networks, err := client.ListNetworks(context.Background(), testSiteID)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("got %d networks, want 1", len(networks))
	}
	n := networks[0]
	if n.Management != "GATEWAY" || n.ZoneID != "zone-1" {
		t.Errorf("unexpected network: %+v", n)
	}
	if n.CellularBackupEnabled == nil || !*n.CellularBackupEnabled {
		t.Errorf("CellularBackupEnabled = %v, want true", n.CellularBackupEnabled)
	}
	if n.IPv4Configuration == nil || n.IPv4Configuration.HostIPAddress != "10.0.0.1" {
		t.Errorf("IPv4Configuration = %+v", n.IPv4Configuration)
	}
	if n.DHCPGuarding == nil || len(n.DHCPGuarding.TrustedDHCPServerIPAddresses) != 1 {
		t.Errorf("DHCPGuarding = %+v", n.DHCPGuarding)
	}
	if n.IPv6Configuration == nil || n.IPv6Configuration.InterfaceType != "STATIC" {
		t.Errorf("IPv6Configuration = %+v", n.IPv6Configuration)
	}
}

func TestGetNetwork(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(APIKeyHeader)
		_, _ = io.WriteString(w, `{
			"id":"`+testNetworkID+`","management":"GATEWAY","name":"Guest WiFi",
			"enabled":false,"vlanId":20,"default":false,"metadata":{"origin":"USER_DEFINED"},
			"zoneId":"zone-1","cellularBackupEnabled":false,"internetAccessEnabled":true,
			"isolationEnabled":true,"mdnsForwardingEnabled":true,
			"dhcpGuarding":{"trustedDhcpServerIpAddresses":["10.0.0.2"]},
			"ipv4Configuration":{"autoScaleEnabled":true,"hostIpAddress":"192.168.20.1","prefixLength":24},
			"ipv6Configuration":{"interfaceType":"STATIC","clientAddressAssignment":{"slaacEnabled":true},
			  "hostIpAddress":"fd00:20::1","prefixLength":64}}`)
	}))

	network, err := client.GetNetwork(context.Background(), testSiteID, testNetworkID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	want := fmt.Sprintf("/proxy/network/integration/v1/sites/%s/networks/%s", testSiteID, testNetworkID)
	if gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != testAPIKey {
		t.Errorf("api key header = %q, want %q", gotAPIKey, testAPIKey)
	}
	if network.ID != testNetworkID || network.Management != "GATEWAY" || network.Name != "Guest WiFi" {
		t.Errorf("unexpected network: %+v", network)
	}
	if network.Enabled == nil || *network.Enabled {
		t.Errorf("Enabled = %v, want false", network.Enabled)
	}
	if network.ZoneID != "zone-1" {
		t.Errorf("ZoneID = %q, want zone-1", network.ZoneID)
	}
	if network.CellularBackupEnabled == nil || *network.CellularBackupEnabled {
		t.Errorf("CellularBackupEnabled = %v, want false", network.CellularBackupEnabled)
	}
	if network.InternetAccessEnabled == nil || !*network.InternetAccessEnabled {
		t.Errorf("InternetAccessEnabled = %v, want true", network.InternetAccessEnabled)
	}
	if network.MDNSForwardingEnabled == nil || !*network.MDNSForwardingEnabled {
		t.Errorf("MDNSForwardingEnabled = %v, want true", network.MDNSForwardingEnabled)
	}
	if network.DHCPGuarding == nil || len(network.DHCPGuarding.TrustedDHCPServerIPAddresses) != 1 {
		t.Errorf("DHCPGuarding = %+v", network.DHCPGuarding)
	}
	if network.IPv4Configuration == nil || network.IPv4Configuration.HostIPAddress != "192.168.20.1" {
		t.Errorf("IPv4Configuration = %+v", network.IPv4Configuration)
	}
	if network.IPv6Configuration == nil || network.IPv6Configuration.InterfaceType != "STATIC" {
		t.Errorf("IPv6Configuration = %+v", network.IPv6Configuration)
	}
}

func TestGetNetworkNotFound(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"statusCode":404,"statusName":"NOT_FOUND",
			"code":"api.network.not-found","message":"Network not found"}`)
	}))

	_, err := client.GetNetwork(context.Background(), testSiteID, testNetworkID)
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Errorf("StatusCode = %d, want 404", apiErr.StatusCode)
	}
}

func TestGetNetworkRejectsUnsafeNetworkID(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"slash traversal":      "../../evil",
		"parent path element":  "..",
		"current path element": ".",
		"forward slash":        "a/b",
		"backslash":            `a\b`,
		"empty":                "",
	}
	for name, networkID := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var called bool
			client := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))

			_, err := client.GetNetwork(context.Background(), testSiteID, networkID)
			if err == nil {
				t.Fatalf("GetNetwork with network %q = nil error, want rejection", networkID)
			}
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("error = %v, want *InvalidSpecError", err)
			}
			if called {
				t.Errorf("unsafe network %q reached the controller", networkID)
			}
		})
	}
}

func TestListSitesAPIError(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{
			"statusCode":401,"statusName":"UNAUTHORIZED",
			"code":"api.authentication.missing-credentials","message":"Missing credentials",
			"timestamp":"2024-11-27T08:13:46.966Z","requestPath":"/integration/v1/sites/123",
			"requestId":"3fa85f64-5717-4562-b3fc-2c963f66afa6"}`)
	}))

	_, err := client.ListSites(context.Background())
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
}

func TestNewClientRejectsCredentialsInURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https userinfo":            "https://admin:secret@controller.local",
		"http userinfo":             "http://admin:secret@controller.local",
		"user only":                 "https://admin@controller.local",
		"opaque credential URL":     "https:admin:secret@controller.local",
		"malformed credential URL":  "https://admin:secret@contr oller.local",
		"opaque empty-host":         "https:admin:secret@controller",
		"credential after encoding": "https://admin:%73ecret@controller.local",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClient(raw, "", false)
			if err == nil {
				t.Fatalf("NewClient(%q) = nil error, want rejection", raw)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("error %q leaks the password", err.Error())
			}
		})
	}
}

func TestNewClientRejectsInvalidURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"empty":      "",
		"bad scheme": "ftp://example.com",
		"no host":    "https://",
		"relative":   "controller.local",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(raw, "", false); err == nil {
				t.Errorf("NewClient(%q) = nil error, want error", raw)
			}
		})
	}
}

func TestClientRejectsUnsafeSite(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"slash traversal":      "../../evil",
		"parent path element":  "..",
		"current path element": ".",
		"forward slash":        "a/b",
		"backslash":            `a\b`,
		"empty":                "",
	}
	for name, siteID := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var called bool
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, testAPIKey, false)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.ListNetworks(context.Background(), siteID)
			if err == nil {
				t.Fatalf("ListNetworks with site %q = nil error, want rejection", siteID)
			}
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("error = %v, want *InvalidSpecError", err)
			}
			if called {
				t.Errorf("unsafe site %q reached the controller", siteID)
			}
		})
	}
}

func TestClientEscapesSiteSegment(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"count":0,"data":[],"limit":200,"offset":0,"totalCount":0}`)
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, testAPIKey, false)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ListNetworks(context.Background(), "site 1"); err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if want := "/proxy/network/integration/v1/sites/site%201/networks"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
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
