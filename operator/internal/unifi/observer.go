package unifi

import "time"

// RequestObserver receives one observation per outbound request to the UniFi
// controller. The client calls it exactly once for every request, whatever the
// outcome: operation is the bounded label derived by operationFromEndpoint
// (never a URL or an upstream identifier), err is nil only for a request that
// succeeded, and elapsed is the wall time the request took.
//
// The interface is declared here so the client can report its traffic without
// importing a metrics package — or anything else in the Kubernetes world
// (docs/go.md, "Packages and layering"; docs/unifi-api.md). A caller that wants
// no telemetry passes no option and gets the no-op observer.
type RequestObserver interface {
	// ObserveRequest records one request and how long it took.
	ObserveRequest(operation string, err error, elapsed time.Duration)
}

// noopRequestObserver discards every observation. It is the default so a client
// built without an observer needs no nil check on the request path.
type noopRequestObserver struct{}

// ObserveRequest implements RequestObserver.
func (noopRequestObserver) ObserveRequest(string, error, time.Duration) {}

// Option configures an HTTPClient at construction time.
type Option func(*HTTPClient)

// WithRequestObserver makes the client report every request to obs. A nil obs
// leaves the no-op observer in place, so an unset dependency degrades to
// missing telemetry rather than a panic on the request path.
func WithRequestObserver(obs RequestObserver) Option {
	return func(c *HTTPClient) {
		if obs != nil {
			c.observer = obs
		}
	}
}
