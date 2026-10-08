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
	testNetworkID   = "net-1"
	testNetworkName = "lan"
	testSite        = "default"
	otherSite       = "other-site"
)

func newTestClient(t *testing.T, handler http.Handler) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewHTTPClient(srv.URL, testSite, "test-key")
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	return client
}

func TestHTTPClientListNetworks(t *testing.T) {
	t.Parallel()

	var gotPath, gotAPIKey string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAPIKey = r.Header.Get(apiKeyHeader)
		_, _ = io.WriteString(w, `{"meta":{"rc":"ok"},"data":[{"_id":"net-1","name":"lan","vlan":10,"ip_subnet":"10.0.0.0/24","enabled":true}]}`)
	}))

	networks, err := client.ListNetworks(context.Background(), testSite)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 {
		t.Fatalf("got %d networks, want 1", len(networks))
	}
	if networks[0].ID != testNetworkID || networks[0].Name != testNetworkName || networks[0].VLAN != 10 {
		t.Errorf("unexpected network: %+v", networks[0])
	}
	if want := fmt.Sprintf("/proxy/network/api/s/%s/rest/networkconf", testSite); gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
	if gotAPIKey != "test-key" {
		t.Errorf("api key header = %q, want %q", gotAPIKey, "test-key")
	}
}

func TestHTTPClientUsesSiteArgument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site string
		want string
	}{
		{
			name: "explicit site",
			site: otherSite,
			want: fmt.Sprintf("/proxy/network/api/s/%s/rest/networkconf", otherSite),
		},
		{
			name: "empty site falls back to configured default",
			site: "",
			want: fmt.Sprintf("/proxy/network/api/s/%s/rest/networkconf", testSite),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var gotPath string
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath = r.URL.Path
				_, _ = io.WriteString(w, `{"meta":{"rc":"ok"},"data":[]}`)
			}))

			if _, err := client.ListNetworks(context.Background(), tt.site); err != nil {
				t.Fatalf("ListNetworks: %v", err)
			}
			if gotPath != tt.want {
				t.Errorf("path = %q, want %q", gotPath, tt.want)
			}
		})
	}
}

func TestHTTPClientRejectsUnsafeSite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		site string
	}{
		{name: "slash traversal", site: "../../evil"},
		{name: "parent path element", site: ".."},
		{name: "current path element", site: "."},
		{name: "forward slash", site: "a/b"},
		{name: "backslash", site: `a\b`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var called bool
			client := newTestClient(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))

			_, err := client.ListNetworks(context.Background(), tt.site)
			if err == nil {
				t.Fatalf("ListNetworks(%q) = nil error, want rejection", tt.site)
			}
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("error = %v, want *InvalidSpecError", err)
			}
			if called {
				t.Errorf("unsafe site %q reached the controller", tt.site)
			}
		})
	}
}

func TestHTTPClientEscapesSiteSegment(t *testing.T) {
	t.Parallel()

	var gotPath string
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = io.WriteString(w, `{"meta":{"rc":"ok"},"data":[]}`)
	}))

	if _, err := client.ListNetworks(context.Background(), "site 1"); err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if want := "/proxy/network/api/s/site%201/rest/networkconf"; gotPath != want {
		t.Errorf("path = %q, want %q", gotPath, want)
	}
}

func TestHTTPClientGetNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr error
	}{
		{
			name: "found",
			body: `{"meta":{"rc":"ok"},"data":[{"_id":"net-1","name":"lan","enabled":true}]}`,
		},
		{
			name:    "not found",
			body:    `{"meta":{"rc":"ok"},"data":[]}`,
			wantErr: ErrNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = io.WriteString(w, tt.body)
			}))

			network, err := client.GetNetwork(context.Background(), testSite, testNetworkID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("GetNetwork error = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil && network.ID != testNetworkID {
				t.Errorf("network ID = %q, want %q", network.ID, testNetworkID)
			}
		})
	}
}

func TestHTTPClientAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		status        int
		body          string
		wantRetryable bool
	}{
		{name: "server error is transient", status: http.StatusInternalServerError, body: "boom", wantRetryable: true},
		{name: "bad request is terminal", status: http.StatusBadRequest, body: "bad", wantRetryable: false},
		{name: "not ok rc is terminal", status: http.StatusOK, body: `{"meta":{"rc":"error","msg":"nope"}}`, wantRetryable: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))

			_, err := client.ListNetworks(context.Background(), testSite)
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

func TestNewHTTPClientValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		site    string
	}{
		{name: "bad scheme", baseURL: "ftp://controller.local", site: testSite},
		{name: "no host", baseURL: "https://", site: testSite},
		{name: "empty site", baseURL: "https://controller.local", site: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewHTTPClient(tt.baseURL, tt.site, ""); err == nil {
				t.Fatalf("NewHTTPClient(%q, %q) = nil error, want error", tt.baseURL, tt.site)
			}
		})
	}
}

func TestNewHTTPClientRejectsURLCredentials(t *testing.T) {
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

			_, err := NewHTTPClient(tt.baseURL, testSite, "")
			if err == nil {
				t.Fatalf("NewHTTPClient(%q) = nil error, want rejection", tt.baseURL)
			}
			if strings.Contains(err.Error(), "s3cret") {
				t.Errorf("error leaked credentials: %q", err.Error())
			}
		})
	}
}
