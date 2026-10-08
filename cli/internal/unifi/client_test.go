package unifi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListNetworks(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got, want := r.URL.Path, "/proxy/network/api/s/default/rest/networkconf"; got != want {
			t.Errorf("path = %s, want %s", got, want)
		}
		if got, want := r.Header.Get(APIKeyHeader), "test-key"; got != want {
			t.Errorf("%s = %q, want %q", APIKeyHeader, got, want)
		}
		_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[` +
			`{"_id":"opaque","name":"Default","purpose":"corporate",` +
			`"ip_subnet":"192.168.1.0/24","vlan":1,"vlan_enabled":true,"enabled":true}]}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "default", WithAPIKey("test-key"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	networks, err := c.ListNetworks(context.Background())
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("got %d networks, want 1", len(networks))
	}
	got := networks[0]
	if got.ID != "opaque" || got.Name != "Default" || got.Subnet != "192.168.1.0/24" {
		t.Errorf("unexpected network: %+v", got)
	}
}

func TestListNetworksAPIError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"meta":{"rc":"error"},"data":[]}`))
	}))
	defer srv.Close()

	c, err := NewClient(srv.URL, "default")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = c.ListNetworks(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
}

func TestNewClientRejectsCredentialsInURL(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"https userinfo": "https://admin:secret@controller.local",
		"http userinfo":  "http://admin:secret@controller.local",
		"user only":      "https://admin@controller.local",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := NewClient(raw, "default")
			if err == nil {
				t.Fatalf("NewClient(%q) = nil error, want rejection", raw)
			}
			if !strings.Contains(err.Error(), "credentials") {
				t.Errorf("error = %q, want a credentials rejection", err.Error())
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
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewClient(raw, "default"); err == nil {
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
	}
	for name, site := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var called bool
			srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			t.Cleanup(srv.Close)

			c, err := NewClient(srv.URL, site)
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, err = c.ListNetworks(context.Background())
			if err == nil {
				t.Fatalf("ListNetworks with site %q = nil error, want rejection", site)
			}
			var segErr *InvalidSegmentError
			if !errors.As(err, &segErr) {
				t.Fatalf("error = %v, want *InvalidSegmentError", err)
			}
			if called {
				t.Errorf("unsafe site %q reached the controller", site)
			}
		})
	}
}

func TestClientEscapesSiteSegment(t *testing.T) {
	t.Parallel()

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, "site 1")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.ListNetworks(context.Background()); err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if want := "/proxy/network/api/s/site%201/rest/networkconf"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}
