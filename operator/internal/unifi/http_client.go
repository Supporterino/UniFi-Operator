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

// defaultTimeout bounds a single controller round-trip.
const defaultTimeout = 30 * time.Second

// maxResponseBytes caps how much of a controller response is read so a
// misbehaving endpoint cannot exhaust memory.
const maxResponseBytes = 4 << 20

// apiKeyHeader is the header carrying an API-key credential, per
// docs/unifi-api.md. The value is never logged.
const apiKeyHeader = "X-API-Key"

// HTTPClient is a UniFi controller client that speaks the legacy REST API
// described in docs/unifi-api.md. The base URL and site are supplied by the
// caller; credentials, when used, are passed in and never written to logs or
// status. TLS verification is always on.
type HTTPClient struct {
	baseURL *url.URL
	site    string
	apiKey  string
	client  *http.Client
}

// NewHTTPClient constructs an HTTPClient for the given controller base URL and
// site. apiKey may be empty when the caller authenticates another way. The base
// URL must be absolute with an http or https scheme.
func NewHTTPClient(baseURL, site, apiKey string) (*HTTPClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		// url.Error embeds the raw URL, which may contain userinfo credentials;
		// wrap only the cause so no secret reaches error text or logs.
		return nil, fmt.Errorf("parse base URL: %w", urlErrorCause(err))
	}
	if u.User != nil {
		// Credentials come from an API key or a Secret, never from userinfo in
		// the URL. Rejecting it also keeps userinfo out of request/error text.
		// The error deliberately does not echo the URL.
		return nil, errors.New("base URL must not contain credentials")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported base URL scheme %q", u.Scheme)
	}
	if u.Opaque != "" {
		// An opaque URL (for example "https:user:s3cret@example.com") can carry
		// userinfo while leaving User nil; reject it without echoing the URL.
		return nil, errors.New("base URL must be hierarchical, not opaque")
	}
	if u.Host == "" {
		// Static message: the raw baseURL may carry credentials (for example in
		// an opaque URL), so it must not be interpolated here.
		return nil, errors.New("base URL has no host")
	}
	if site == "" {
		return nil, errors.New("site must not be empty")
	}
	return &HTTPClient{
		baseURL: u,
		site:    site,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: defaultTimeout},
	}, nil
}

// urlErrorCause unwraps a *url.Error to its underlying cause. The wrapper's
// Error text includes the original URL, which may carry credentials, so it must
// not be embedded in surfaced errors.
func urlErrorCause(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// envelope is the legacy REST response wrapper described in docs/unifi-api.md.
type envelope struct {
	Meta responseMeta    `json:"meta"`
	Data json.RawMessage `json:"data"`
}

type responseMeta struct {
	RC  string `json:"rc"`
	Msg string `json:"msg,omitempty"`
}

// ListNetworks implements Client.
func (c *HTTPClient) ListNetworks(ctx context.Context, site string) ([]Network, error) {
	raw, err := c.get(ctx, c.resolveSite(site), "rest", "networkconf")
	if err != nil {
		return nil, err
	}
	networks, err := decodeNetworks(raw)
	if err != nil {
		return nil, err
	}
	return networks, nil
}

// GetNetwork implements Client.
func (c *HTTPClient) GetNetwork(ctx context.Context, site, id string) (Network, error) {
	if id == "" {
		return Network{}, ErrNotFound
	}
	raw, err := c.get(ctx, c.resolveSite(site), "rest", "networkconf", id)
	if err != nil {
		return Network{}, err
	}
	networks, err := decodeNetworks(raw)
	if err != nil {
		return Network{}, err
	}
	if len(networks) == 0 {
		return Network{}, ErrNotFound
	}
	return networks[0], nil
}

// resolveSite returns the site to query. An empty call-site site falls back to
// the client's configured default site.
func (c *HTTPClient) resolveSite(site string) string {
	if site == "" {
		return c.site
	}
	return site
}

// get performs a GET against a site-scoped REST path and returns the decoded
// data payload. The site and every caller-supplied segment are validated and
// isolated with url.PathEscape before the path is assembled, so none of them can
// traverse out of the intended base path.
func (c *HTTPClient) get(ctx context.Context, site string, segments ...string) (json.RawMessage, error) {
	parts := make([]string, 0, 5+len(segments))
	parts = append(parts, "proxy", "network", "api", "s")

	escapedSite, err := escapeSegment("site", site)
	if err != nil {
		return nil, err
	}
	parts = append(parts, escapedSite)

	for _, segment := range segments {
		escaped, err := escapeSegment("path segment", segment)
		if err != nil {
			return nil, err
		}
		parts = append(parts, escaped)
	}

	endpoint, err := url.JoinPath(c.baseURL.String(), parts...)
	if err != nil {
		return nil, fmt.Errorf("build endpoint: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set(apiKeyHeader, c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(body))}
	}

	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode envelope: %w", err)
	}
	if env.Meta.RC != "ok" {
		return nil, &APIError{StatusCode: resp.StatusCode, Message: env.Meta.Msg}
	}
	return env.Data, nil
}

// escapeSegment validates and escapes a single URL path segment. url.PathEscape
// alone is insufficient: it leaves "." and ".." intact, and url.JoinPath's path
// cleaning would then drop the segment and traverse the path. Rejecting those
// values (and path separators) keeps the request under the intended prefix.
func escapeSegment(field, segment string) (string, error) {
	switch segment {
	case "", ".", "..":
		return "", &InvalidSpecError{Field: field, Reason: fmt.Sprintf("%q is not a valid path segment", segment)}
	}
	if strings.ContainsAny(segment, `/\`) {
		return "", &InvalidSpecError{Field: field, Reason: fmt.Sprintf("%q must not contain path separators", segment)}
	}
	return url.PathEscape(segment), nil
}

// decodeNetworks accepts either a single network object or an array of them; the
// legacy endpoint returns an array, but decoding defensively keeps the client
// tolerant of a controller that returns one object.
func decodeNetworks(raw json.RawMessage) ([]Network, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, "[") {
		var networks []Network
		if err := json.Unmarshal(raw, &networks); err != nil {
			return nil, fmt.Errorf("decode networks: %w", err)
		}
		return networks, nil
	}
	var network Network
	if err := json.Unmarshal(raw, &network); err != nil {
		return nil, fmt.Errorf("decode network: %w", err)
	}
	return []Network{network}, nil
}
