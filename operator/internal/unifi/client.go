// Package unifi provides the typed UniFi controller client consumed by the
// operator's reconcilers.
//
// The package is deliberately Kubernetes-agnostic: it returns plain Go structs,
// takes a context.Context on every call, and never imports Kubernetes types.
// Upstream correlation identifiers (the UniFi "_id") are returned to callers,
// which keep them out of a CR's spec and record them in status only (see
// docs/crd-conventions.md).
//
// The current implementation is a scaffold. HTTPClient speaks the legacy REST
// contract documented in docs/unifi-api.md, while FixtureClient returns canned
// data for tests and local runs. docs/unifi-api.md remains the source of truth
// for the upstream contract; changing that contract updates this package and
// that document together. No method here contacts a live controller during a
// test.
package unifi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrNotFound is returned when the controller has no object for the requested
// identifier.
var ErrNotFound = errors.New("unifi: object not found")

// InvalidSpecError reports a caller-supplied value that cannot be used to build
// a safe controller request, such as a site or identifier containing a path
// separator or a "."/".." path element. It is terminal: callers should surface a
// failing condition (reason InvalidSpec) rather than retry.
type InvalidSpecError struct {
	// Field names the offending input, for example "site".
	Field string
	// Reason describes why the value is unusable.
	Reason string
}

// Error implements the error interface.
func (e *InvalidSpecError) Error() string {
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Reason)
}

// Network is a UniFi network (a networkconf object) as returned by the
// controller. It is a plain Go struct with no Kubernetes types.
type Network struct {
	// ID is the upstream UniFi identifier ("_id"). Callers must keep it out of
	// a CR's spec and record it in status only.
	ID string `json:"_id"`
	// Name is the human-readable network name.
	Name string `json:"name"`
	// SiteID is the upstream identifier of the site the network belongs to.
	SiteID string `json:"site_id,omitempty"`
	// VLAN is the 802.1Q VLAN tag, or 0 when untagged.
	VLAN int `json:"vlan,omitempty"`
	// Subnet is the network's IPv4 subnet in CIDR notation, if configured.
	Subnet string `json:"ip_subnet,omitempty"`
	// Enabled reports whether the network is enabled.
	Enabled bool `json:"enabled"`
}

// Client is the subset of the UniFi controller API the operator consumes. It
// is defined here so reconcilers can depend on the behavior rather than a
// concrete implementation; HTTPClient talks to a controller and FixtureClient
// serves canned data.
//
// Every method is site-scoped: the site is a spec/config input, never a
// hard-coded constant, so the same client can serve multiple sites without
// name collisions.
type Client interface {
	// ListNetworks returns every network configured on the given site.
	ListNetworks(ctx context.Context, site string) ([]Network, error)
	// GetNetwork returns the network with the given upstream identifier on the
	// given site.
	GetNetwork(ctx context.Context, site, id string) (Network, error)
}

// APIError describes a non-successful response from the UniFi controller.
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Message is the controller-provided error text. It never contains
	// credentials.
	Message string
}

// Error implements the error interface.
func (e *APIError) Error() string {
	return http.StatusText(e.StatusCode) + ": " + e.Message
}

// Retryable reports whether the failure is transient and the caller should
// back off and retry rather than surface a terminal condition. Client errors
// (4xx, except 429) are terminal.
func (e *APIError) Retryable() bool {
	return e.StatusCode >= http.StatusInternalServerError || e.StatusCode == http.StatusTooManyRequests
}
