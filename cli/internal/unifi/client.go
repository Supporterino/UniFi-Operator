// Package unifi is a small HTTP client for the UniFi Network Integration v1 API.
//
// It mirrors the operator's client surface for the endpoints the CLI consumes, as
// documented in docs/unifi-api.md (the workspace source of truth). It returns plain Go
// structs and never imports Kubernetes types.
//
// Upstream correlation identifiers (UUIDs) are returned to callers, which keep them out
// of an emitted Custom Resource's spec (see docs/crd-conventions.md). The API key is
// carried in the X-API-Key header and is never logged, embedded in an error, or written
// to output.
package unifi

import (
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

	// maxResponseBytes caps how much of a controller response is read so a misbehaving
	// endpoint cannot exhaust memory.
	maxResponseBytes = 4 << 20

	// listPageLimit is the page size requested for list pagination. It is the upstream
	// maximum (200), which minimizes the number of round trips.
	listPageLimit = 200

	// maxListPages bounds list pagination so a controller that never advances the page
	// offset cannot loop forever.
	maxListPages = 1000
)

// APIKeyHeader is the request header that carries a UniFi API key. See docs/unifi-api.md.
// The value is never logged.
const APIKeyHeader = "X-API-Key"

// basePathSegments is the fixed Integration v1 prefix every request is built under.
// Untrusted path segments are always appended after it, never before.
var basePathSegments = []string{"proxy", "network", "integration", "v1"}

// InvalidSpecError reports a caller-supplied value that cannot be used to build a safe
// controller request, such as an empty, traversing, or separator-bearing path segment.
type InvalidSpecError struct {
	// Field names the offending input, for example "siteID".
	Field string
	// Reason describes why the value is unusable.
	Reason string
}

// Error implements the error interface.
func (e *InvalidSpecError) Error() string {
	return fmt.Sprintf("unifi: invalid %s: %s", e.Field, e.Reason)
}

// Info is the response of GET /v1/info: the reachability probe and the source of the
// detected application version that gates capability availability.
type Info struct {
	// ApplicationVersion is the detected UniFi Network application version.
	ApplicationVersion string `json:"applicationVersion"`
}

// Site is a site as returned by GET /v1/sites.
type Site struct {
	// ID is the site UUID used in every site-scoped path.
	ID string `json:"id"`
	// InternalReference is the site's internal unique name used by older APIs.
	InternalReference string `json:"internalReference"`
	// Name is the human-readable site name.
	Name string `json:"name"`
}

// NetworkMetadata is the observed metadata of a network.
type NetworkMetadata struct {
	// Origin is the entity origin (USER_DEFINED, SYSTEM_DEFINED, DERIVED, or
	// ORCHESTRATED). It is observed, never authored, so it is emitted in status only.
	Origin string `json:"origin"`
}

// DHCPGuarding holds the trusted DHCP server addresses for a network.
type DHCPGuarding struct {
	// TrustedDHCPServerIPAddresses is the list of trusted DHCP server IP addresses.
	TrustedDHCPServerIPAddresses []string `json:"trustedDhcpServerIpAddresses"`
}

// IPv4Configuration is the IPv4 configuration of a network, shared by the gateway- and
// switch-managed variants. The upstream optional `dhcpConfiguration` union and
// WAN-specific `natOutboundIpAddressConfiguration` are deliberately unmodeled.
type IPv4Configuration struct {
	AutoScaleEnabled        bool     `json:"autoScaleEnabled"`
	HostIPAddress           string   `json:"hostIpAddress"`
	PrefixLength            int32    `json:"prefixLength"`
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`
}

// IPv6ClientAddressAssignment configures IPv6 client addressing. Only SLAAC is modeled.
type IPv6ClientAddressAssignment struct {
	SLAACEnabled bool `json:"slaacEnabled"`
}

// RouterAdvertisement configures IPv6 router advertisement.
type RouterAdvertisement struct {
	Priority string `json:"priority"`
}

// IPv6Configuration is the interface-type-discriminated IPv6 configuration.
type IPv6Configuration struct {
	InterfaceType                string                      `json:"interfaceType"`
	ClientAddressAssignment      IPv6ClientAddressAssignment `json:"clientAddressAssignment"`
	AdditionalHostIPSubnets      []string                    `json:"additionalHostIpSubnets,omitempty"`
	DNSServerIPAddressesOverride []string                    `json:"dnsServerIpAddressesOverride,omitempty"`
	RouterAdvertisement          *RouterAdvertisement        `json:"routerAdvertisement,omitempty"`

	// PrefixDelegationWANInterfaceID is set for interfaceType PREFIX_DELEGATION. It is an
	// opaque WAN UUID; the emitted CR models the WAN by name, so it cannot be emitted
	// directly.
	PrefixDelegationWANInterfaceID string `json:"prefixDelegationWanInterfaceId,omitempty"`
	// HostIPAddress and PrefixLength are set for interfaceType STATIC.
	HostIPAddress string `json:"hostIpAddress,omitempty"`
	PrefixLength  int32  `json:"prefixLength,omitempty"`
}

// Network is a UniFi network as returned by the controller. It is a flat plain struct:
// the union-specific fields are populated only for the matching management variant.
//
// ID, ZoneID, and DeviceID are upstream UUIDs; callers must never copy them into a
// Custom Resource spec (see docs/crd-conventions.md). Enabled is a pointer so an omitted
// upstream field can be distinguished from an explicit false.
type Network struct {
	ID           string          `json:"id"`
	Management   string          `json:"management"`
	Name         string          `json:"name"`
	Enabled      *bool           `json:"enabled,omitempty"`
	VLANID       int32           `json:"vlanId"`
	Default      bool            `json:"default"`
	Metadata     NetworkMetadata `json:"metadata"`
	DHCPGuarding *DHCPGuarding   `json:"dhcpGuarding,omitempty"`

	// ZoneID is populated for management GATEWAY; DeviceID for management SWITCH. Both
	// are observed upstream UUIDs.
	ZoneID   string `json:"zoneId,omitempty"`
	DeviceID string `json:"deviceId,omitempty"`

	// Union-observed fields. Pointer types distinguish an absent field from an explicit
	// false.
	CellularBackupEnabled *bool              `json:"cellularBackupEnabled,omitempty"`
	InternetAccessEnabled *bool              `json:"internetAccessEnabled,omitempty"`
	IsolationEnabled      *bool              `json:"isolationEnabled,omitempty"`
	MDNSForwardingEnabled *bool              `json:"mdnsForwardingEnabled,omitempty"`
	IPv4Configuration     *IPv4Configuration `json:"ipv4Configuration,omitempty"`
	IPv6Configuration     *IPv6Configuration `json:"ipv6Configuration,omitempty"`
}

// APIError describes a non-successful response from the UniFi controller. The
// statusName, code, message, timestamp, requestPath, and requestID fields are decoded
// from the controller's flat Error Message body; they never contain credentials.
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// StatusName is the controller-provided status name (for example "UNAUTHORIZED").
	StatusName string
	// Code is the controller-provided machine-readable error code.
	Code string
	// Message is the controller-provided error text.
	Message string
	// Timestamp is the controller-provided error timestamp.
	Timestamp string
	// RequestPath is the request path the controller reported.
	RequestPath string
	// RequestID is the controller-provided request ID, for server-side correlation.
	RequestID string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Message != "" {
		return http.StatusText(e.StatusCode) + ": " + e.Message
	}
	return http.StatusText(e.StatusCode)
}

// Client is a UniFi controller client that speaks the Integration v1 API described in
// docs/unifi-api.md. The base URL and API key are supplied by the caller; the key is
// carried in the X-API-Key header and is never logged or recorded in an error. TLS
// verification is on unless the caller explicitly opts out.
type Client struct {
	baseURL *url.URL
	apiKey  string
	client  *http.Client
}

// NewClient constructs a Client for the given console base URL and API key. The base
// URL must be absolute and hierarchical with an http or https scheme; userinfo, opaque
// URLs, and credentials embedded in the URL are rejected so no secret can reach request
// or error text. When insecureSkipVerify is true, TLS certificate verification is
// disabled; this is insecure and must only be used for a self-signed console in a
// trusted environment.
func NewClient(baseURL, apiKey string, insecureSkipVerify bool) (*Client, error) {
	if baseURL == "" {
		return nil, errors.New("unifi: controller URL is required")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		// url.Error embeds the raw URL, which may contain userinfo credentials; wrap
		// only the cause so no secret reaches error text or logs.
		return nil, fmt.Errorf("unifi: parse controller URL: %w", urlErrorCause(err))
	}
	if u.User != nil {
		// Credentials come from an API key, never from userinfo in the URL. Rejecting it
		// also keeps userinfo out of request/error text. The error does not echo the URL.
		return nil, errors.New("unifi: controller URL must not contain credentials")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("unifi: controller URL must be http or https, got %q", u.Scheme)
	}
	if u.Opaque != "" {
		// An opaque URL (for example "https:user:s3cret@example.com") can carry userinfo
		// while leaving User nil; reject it without echoing the URL.
		return nil, errors.New("unifi: controller URL must be hierarchical, not opaque")
	}
	if u.Host == "" {
		// Static message: the raw base URL may carry credentials, so it must not be
		// interpolated here.
		return nil, errors.New("unifi: controller URL must include a host")
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if insecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig

	return &Client{
		baseURL: u,
		apiKey:  apiKey,
		client:  &http.Client{Timeout: defaultTimeout, Transport: transport},
	}, nil
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

// GetInfo returns the application info used as the reachability probe and version-gating
// input.
func (c *Client) GetInfo(ctx context.Context) (Info, error) {
	endpoint, err := c.endpoint("info")
	if err != nil {
		return Info{}, err
	}
	var info Info
	if err := c.do(ctx, http.MethodGet, endpoint, &info); err != nil {
		return Info{}, fmt.Errorf("get info: %w", err)
	}
	return info, nil
}

// ListSites returns every site managed by the console.
func (c *Client) ListSites(ctx context.Context) ([]Site, error) {
	endpoint, err := c.endpoint("sites")
	if err != nil {
		return nil, err
	}
	sites, err := listAll[Site](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	return sites, nil
}

// ListNetworks returns every network on the given site.
func (c *Client) ListNetworks(ctx context.Context, siteID string) ([]Network, error) {
	endpoint, err := c.endpoint("sites", siteID, "networks")
	if err != nil {
		return nil, err
	}
	networks, err := listAll[Network](ctx, c, endpoint)
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	return networks, nil
}

// GetNetwork returns the details of one network on the given site. The list endpoint
// (ListNetworks) returns only overview fields, so the management-variant configuration is
// only available through this detail endpoint. The returned Network is never modified by
// the caller before projection; the upstream id, zoneId, and deviceId it carries must not
// reach an emitted spec.
func (c *Client) GetNetwork(ctx context.Context, siteID, networkID string) (Network, error) {
	endpoint, err := c.endpoint("sites", siteID, "networks", networkID)
	if err != nil {
		return Network{}, err
	}
	var network Network
	if err := c.do(ctx, http.MethodGet, endpoint, &network); err != nil {
		return Network{}, fmt.Errorf("get network: %w", err)
	}
	return network, nil
}

// endpoint builds an absolute URL under the fixed Integration v1 base path. Every
// caller-supplied segment is validated and escaped so none of them can traverse out of
// the base path or inject a separator.
func (c *Client) endpoint(segments ...string) (string, error) {
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

// listPage is the Integration v1 list envelope documented in docs/unifi-api.md.
type listPage[T any] struct {
	Count      int32 `json:"count"`
	Data       []T   `json:"data"`
	Limit      int32 `json:"limit"`
	Offset     int64 `json:"offset"`
	TotalCount int64 `json:"totalCount"`
}

// listAll retrieves every element of a paginated list endpoint by following the page
// offset, bounded by maxListPages.
func listAll[T any](ctx context.Context, c *Client, endpoint string) ([]T, error) {
	var all []T
	var offset int64
	for pages := 0; pages < maxListPages; pages++ {
		pageURL, err := withPageQuery(endpoint, offset)
		if err != nil {
			return nil, err
		}
		var page listPage[T]
		if err := c.do(ctx, http.MethodGet, pageURL, &page); err != nil {
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

// do performs an HTTP request and decodes a JSON response into out. A non-2xx response
// is mapped to a typed *APIError built from the controller's flat error body. The API
// key is set as a header and never logged.
func (c *Client) do(ctx context.Context, method, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set(APIKeyHeader, c.apiKey)
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
	if out == nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// parseAPIError maps a non-2xx response to an *APIError. The body is the flat Error
// Message schema; a body that is not the expected JSON falls back to the HTTP status and
// the trimmed body text as the message.
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

// escapeSegment validates and escapes a single URL path segment. url.PathEscape alone is
// insufficient: it leaves "." and ".." intact, and url.JoinPath's path cleaning would
// then drop the segment and traverse the path. Rejecting those values (and path
// separators) keeps the request under the intended prefix.
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
