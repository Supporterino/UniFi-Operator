// Package unifi is a small HTTP client for the UniFi Network controller API.
//
// It mirrors the operator's client surface for the endpoints the CLI consumes, as
// documented in docs/unifi-api.md (the workspace source of truth). It returns plain Go
// structs and never imports Kubernetes types.
package unifi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// maxResponseBytes caps how much of a controller response is read, guarding against a
	// misbehaving endpoint exhausting memory.
	maxResponseBytes = 10 << 20

	// defaultTimeout bounds a single controller request.
	defaultTimeout = 30 * time.Second
)

// APIKeyHeader is the request header that carries a UniFi API key. See docs/unifi-api.md.
const APIKeyHeader = "X-API-Key"

// APIError is a typed error for a non-OK UniFi controller response.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("unifi: controller returned status %d", e.StatusCode)
	}
	return fmt.Sprintf("unifi: controller returned status %d: %s", e.StatusCode, e.Message)
}

// InvalidSegmentError reports a URL path segment that cannot be safely placed in a
// controller request path: empty, "." or "..", or containing a path separator.
type InvalidSegmentError struct {
	Field  string
	Reason string
}

func (e *InvalidSegmentError) Error() string {
	return fmt.Sprintf("unifi: invalid %s: %s", e.Field, e.Reason)
}

// Network is a UniFi network configuration object returned by
// GET /proxy/network/api/s/{site}/rest/networkconf. It mirrors the operator's
// unifi.Network struct except that Enabled is a pointer so an omitted upstream field can
// be distinguished from an explicit false.
//
// ID is the upstream _id and is retained for correlation only. Callers must not copy it
// into a Custom Resource spec; per docs/crd-conventions.md it belongs in status only.
type Network struct {
	ID      string `json:"_id"`
	Name    string `json:"name"`
	SiteID  string `json:"site_id,omitempty"`
	VLAN    int    `json:"vlan,omitempty"`
	Subnet  string `json:"ip_subnet,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// Client talks to a UniFi Network controller.
type Client struct {
	baseURL    *url.URL
	site       string
	httpClient *http.Client
	apiKey     string
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey sets the API key sent on every request. The key is never logged.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithHTTPClient overrides the underlying HTTP client. It is used by tests to point the
// client at an httptest.Server; a nil client leaves the default in place.
func WithHTTPClient(hc *http.Client) Option {
	return func(c *Client) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// NewClient constructs a Client for the given controller base URL and site. The base URL
// must be an http(s) URL with a host and must not carry userinfo credentials; an empty
// site defaults to "default".
func NewClient(baseURL, site string, opts ...Option) (*Client, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("unifi: controller URL is required")
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		// url.Error embeds the raw URL, which may contain credentials; wrap only the cause.
		return nil, fmt.Errorf("unifi: parse controller URL: %w", urlErrorCause(err))
	}
	// Credentials belong in env/a Secret, never in the URL. Reject userinfo without
	// echoing the URL so a password cannot leak into error text or logs.
	if u.User != nil {
		return nil, fmt.Errorf("unifi: controller URL must not contain credentials")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unifi: controller URL must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("unifi: controller URL must include a host")
	}
	if site == "" {
		site = "default"
	}
	c := &Client{
		baseURL:    u,
		site:       site,
		httpClient: &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// urlErrorCause unwraps a *url.Error to its underlying cause. The wrapper's Error text
// includes the original URL, which may carry credentials, so it must not be embedded in
// surfaced errors.
func urlErrorCause(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// ListNetworks lists the networks configured for the client's site.
func (c *Client) ListNetworks(ctx context.Context) ([]Network, error) {
	var networks []Network
	if err := c.get(ctx, []string{"rest", "networkconf"}, &networks); err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	return networks, nil
}

// envelope is the legacy REST response wrapper described in docs/unifi-api.md.
type envelope struct {
	Meta struct {
		RC  string `json:"rc"`
		Msg string `json:"msg,omitempty"`
	} `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// get performs a GET against a site-scoped REST path and returns the decoded data. The
// site and every caller-supplied segment are validated and escaped before the path is
// assembled, so none of them can traverse out of the intended base path.
func (c *Client) get(ctx context.Context, segments []string, out any) error {
	parts := make([]string, 0, 5+len(segments))
	parts = append(parts, "proxy", "network", "api", "s")

	escapedSite, err := escapeSegment("site", c.site)
	if err != nil {
		return err
	}
	parts = append(parts, escapedSite)

	for _, segment := range segments {
		escaped, err := escapeSegment("path segment", segment)
		if err != nil {
			return err
		}
		parts = append(parts, escaped)
	}

	endpoint, err := url.JoinPath(c.baseURL.String(), parts...)
	if err != nil {
		return fmt.Errorf("build endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set(APIKeyHeader, c.apiKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("decode response envelope: %w", err)
	}
	if env.Meta.RC != "" && env.Meta.RC != "ok" {
		return &APIError{StatusCode: resp.StatusCode, Message: env.Meta.Msg}
	}
	if len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

// escapeSegment validates and escapes a single URL path segment. url.PathEscape alone is
// insufficient: it leaves "." and ".." intact, and url.JoinPath's path cleaning would then
// drop the segment and traverse the path. Rejecting those values (and path separators)
// keeps the request under the intended prefix.
func escapeSegment(field, segment string) (string, error) {
	switch segment {
	case "", ".", "..":
		return "", &InvalidSegmentError{Field: field, Reason: fmt.Sprintf("%q is not a valid path segment", segment)}
	}
	if strings.ContainsAny(segment, `/\`) {
		return "", &InvalidSegmentError{Field: field, Reason: fmt.Sprintf("%q must not contain path separators", segment)}
	}
	return url.PathEscape(segment), nil
}
