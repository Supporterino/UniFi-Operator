package unifi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultTimeout bounds a single controller round-trip.
	defaultTimeout = 30 * time.Second

	// maxResponseBytes caps how much of a controller response is read so a
	// misbehaving endpoint cannot exhaust memory.
	maxResponseBytes = 4 << 20

	// listPageLimit is the page size requested for list pagination. It is the
	// upstream maximum (200), which minimizes the number of round trips.
	listPageLimit = 200

	// maxListPages bounds list pagination so a controller that never advances
	// the page offset cannot loop forever.
	maxListPages = 1000

	// apiKeyHeader is the header carrying the API key, per docs/unifi-api.md.
	// The value is never logged.
	apiKeyHeader = "X-API-Key"
)

// basePathSegments is the fixed Integration v1 prefix every request is built
// under. Untrusted path segments are always appended after it, never before.
var basePathSegments = []string{"proxy", "network", "integration", "v1"}

// HTTPClient is a UniFi controller client that speaks the Integration v1 API
// described in docs/unifi-api.md. The base URL and API key are supplied by the
// caller; the key is carried in the X-API-Key header and is never logged or
// recorded in an error. TLS verification is on unless the caller explicitly
// opts out.
type HTTPClient struct {
	baseURL *url.URL
	apiKey  string
	client  *http.Client
}

// NewClient constructs an HTTPClient for the given console base URL and API
// key. The base URL must be absolute and hierarchical with an http or https
// scheme; userinfo, opaque URLs, and credentials embedded in the URL are
// rejected so no secret can reach request or error text. When
// insecureSkipVerify is true, TLS certificate verification is disabled; this is
// insecure and must only be used for a self-signed console in a trusted
// environment.
func NewClient(baseURL, apiKey string, insecureSkipVerify bool) (*HTTPClient, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		// url.Error embeds the raw URL, which may contain userinfo credentials;
		// wrap only the cause so no secret reaches error text or logs.
		return nil, fmt.Errorf("parse base URL: %w", urlErrorCause(err))
	}
	if u.User != nil {
		// Credentials come from an API key read from a Secret, never from
		// userinfo in the URL. Rejecting it also keeps userinfo out of
		// request/error text. The error deliberately does not echo the URL.
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
		// Static message: the raw baseURL may carry credentials, so it must not
		// be interpolated here.
		return nil, errors.New("base URL has no host")
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	return &HTTPClient{
		baseURL: u,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: defaultTimeout, Transport: transport},
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

// GetInfo implements Client.
func (c *HTTPClient) GetInfo(ctx context.Context) (Info, error) {
	endpoint, err := c.endpoint("info")
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &info); err != nil {
		return Info{}, err
	}
	return info, nil
}

// ListSites implements Client.
func (c *HTTPClient) ListSites(ctx context.Context) ([]Site, error) {
	endpoint, err := c.endpoint("sites")
	if err != nil {
		return nil, err
	}
	return listAll[Site](ctx, c, endpoint)
}

// ListNetworks implements Client.
func (c *HTTPClient) ListNetworks(ctx context.Context, siteID string) ([]Network, error) {
	endpoint, err := c.endpoint("sites", siteID, "networks")
	if err != nil {
		return nil, err
	}
	return listAll[Network](ctx, c, endpoint)
}

// GetNetwork implements Client.
func (c *HTTPClient) GetNetwork(ctx context.Context, siteID, networkID string) (Network, error) {
	endpoint, err := c.endpoint("sites", siteID, "networks", networkID)
	if err != nil {
		return Network{}, err
	}
	var network Network
	if err := c.do(ctx, http.MethodGet, endpoint, nil, &network); err != nil {
		return Network{}, err
	}
	return network, nil
}

// CreateNetwork implements Client.
func (c *HTTPClient) CreateNetwork(ctx context.Context, siteID string, req NetworkRequest) (Network, error) {
	if err := req.validate(); err != nil {
		return Network{}, err
	}
	endpoint, err := c.endpoint("sites", siteID, "networks")
	if err != nil {
		return Network{}, err
	}
	var network Network
	if err := c.do(ctx, http.MethodPost, endpoint, req, &network); err != nil {
		return Network{}, err
	}
	return network, nil
}

// UpdateNetwork implements Client.
func (c *HTTPClient) UpdateNetwork(ctx context.Context, siteID, networkID string, req NetworkRequest) (Network, error) {
	if err := req.validate(); err != nil {
		return Network{}, err
	}
	endpoint, err := c.endpoint("sites", siteID, "networks", networkID)
	if err != nil {
		return Network{}, err
	}
	var network Network
	if err := c.do(ctx, http.MethodPut, endpoint, req, &network); err != nil {
		return Network{}, err
	}
	return network, nil
}

// DeleteNetwork implements Client.
func (c *HTTPClient) DeleteNetwork(ctx context.Context, siteID, networkID string) error {
	endpoint, err := c.endpoint("sites", siteID, "networks", networkID)
	if err != nil {
		return err
	}
	return c.do(ctx, http.MethodDelete, endpoint, nil, nil)
}

// endpoint builds an absolute URL under the fixed Integration v1 base path.
// Every caller-supplied segment is validated and escaped so none of them can
// traverse out of the base path or inject a separator.
func (c *HTTPClient) endpoint(segments ...string) (string, error) {
	parts := make([]string, 0, len(basePathSegments)+len(segments))
	parts = append(parts, basePathSegments...)
	for _, segment := range segments {
		escaped, err := escapeSegment("path segment", segment)
		if err != nil {
			return "", err
		}
		parts = append(parts, escaped)
	}
	endpoint, err := url.JoinPath(c.baseURL.String(), parts...)
	if err != nil {
		return "", fmt.Errorf("build endpoint: %w", err)
	}
	return endpoint, nil
}

// listPage is the Integration v1 list envelope documented in
// docs/unifi-api.md.
type listPage[T any] struct {
	Count      int32 `json:"count"`
	Data       []T   `json:"data"`
	Limit      int32 `json:"limit"`
	Offset     int64 `json:"offset"`
	TotalCount int64 `json:"totalCount"`
}

// listAll retrieves every element of a paginated list endpoint by following the
// page offset, bounded by maxListPages.
func listAll[T any](ctx context.Context, c *HTTPClient, endpoint string) ([]T, error) {
	var all []T
	var offset int64
	for pages := 0; pages < maxListPages; pages++ {
		pageURL, err := withPageQuery(endpoint, offset)
		if err != nil {
			return nil, err
		}
		var page listPage[T]
		if err := c.do(ctx, http.MethodGet, pageURL, nil, &page); err != nil {
			return nil, err
		}
		all = append(all, page.Data...)
		offset += int64(len(page.Data))
		if len(page.Data) == 0 || int32(len(page.Data)) < page.Limit || (page.TotalCount > 0 && offset >= page.TotalCount) {
			return all, nil
		}
	}
	return nil, fmt.Errorf("list %s: exceeded %d pages", endpoint, maxListPages)
}

// withPageQuery sets the offset and limit query parameters on an endpoint URL.
func withPageQuery(endpoint string, offset int64) (string, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", fmt.Errorf("parse endpoint: %w", err)
	}
	query := u.Query()
	query.Set("offset", strconv.FormatInt(offset, 10))
	query.Set("limit", strconv.Itoa(listPageLimit))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

// do performs an HTTP request and decodes a JSON response into out. A non-2xx
// response is mapped to a typed *APIError built from the controller's flat
// error body. The API key is set as a header and never logged.
func (c *HTTPClient) do(ctx context.Context, method, endpoint string, body, out any) error {
	var bodyReader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set(apiKeyHeader, c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return parseAPIError(resp.StatusCode, data)
	}
	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// parseAPIError maps a non-2xx response to an *APIError. The body is the flat
// Error Message schema; a body that is not the expected JSON falls back to the
// HTTP status and the trimmed body text as the message.
func parseAPIError(statusCode int, body []byte) error {
	apiErr := &APIError{StatusCode: statusCode, Message: strings.TrimSpace(string(body))}

	var parsed struct {
		StatusName  string `json:"statusName"`
		Code        string `json:"code"`
		Message     string `json:"message"`
		Timestamp   string `json:"timestamp"`
		RequestPath string `json:"requestPath"`
		RequestID   string `json:"requestId"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return apiErr
	}
	if parsed.Message != "" {
		apiErr.Message = parsed.Message
	}
	apiErr.StatusName = parsed.StatusName
	apiErr.Code = parsed.Code
	apiErr.Timestamp = parsed.Timestamp
	apiErr.RequestPath = parsed.RequestPath
	apiErr.RequestID = parsed.RequestID
	return apiErr
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
