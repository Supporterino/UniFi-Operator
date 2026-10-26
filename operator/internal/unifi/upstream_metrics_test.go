package unifi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// observation is one request the client reported to its RequestObserver.
type observation struct {
	operation string
	err       error
	elapsed   time.Duration
}

// recordingObserver is the test double for RequestObserver. It keeps every
// observation so a test can assert that the client reports exactly one per
// request, with the bounded operation label and the request's own error.
type recordingObserver struct {
	observations []observation
}

// ObserveRequest implements RequestObserver.
func (o *recordingObserver) ObserveRequest(operation string, err error, elapsed time.Duration) {
	o.observations = append(o.observations, observation{operation: operation, err: err, elapsed: elapsed})
}

// TestClientObservesEveryRequest pins the observation to the request that
// produced it: a successful call reports its operation with a nil error, a
// controller error reports the same shape with the error set, and each request
// is reported exactly once.
func TestClientObservesEveryRequest(t *testing.T) {
	observer := &recordingObserver{}
	client := newClientWithObserver(t, observer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case testIntegrationBasePath + "/v1/info":
			_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
		case testIntegrationBasePath + "/v1/sites/" + testSiteID + "/firewall/policies":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"statusCode":500,"statusName":"INTERNAL_SERVER_ERROR","code":"api.internal","message":"upstream failure"}`)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	if _, err := client.GetInfo(context.Background()); err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
	if _, err := client.ListFirewallPolicies(context.Background(), testSiteID); err == nil {
		t.Fatal("ListFirewallPolicies succeeded against a 500 response, want an error")
	}

	if got := len(observer.observations); got != 2 {
		t.Fatalf("observations = %d, want one per request", got)
	}
	if got := observer.observations[0]; got.operation != testOperationInfo || got.err != nil {
		t.Errorf("observation for GetInfo = %+v, want operation %q with no error", got, testOperationInfo)
	}
	failed := observer.observations[1]
	if failed.operation != testOperationFirewallPolicies {
		t.Errorf("operation for ListFirewallPolicies = %q, want %q", failed.operation, testOperationFirewallPolicies)
	}
	if failed.err == nil {
		t.Error("observation for a failed request has no error, want the request's error")
	}
}

// TestClientObservesTransportFailure asserts the deferred observation still
// fires exactly once when the request never reaches the console, so an
// unreachable controller is visible rather than only slow.
func TestClientObservesTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
	}))
	baseURL := srv.URL
	srv.Close()

	observer := &recordingObserver{}
	client, err := NewClient(baseURL, testAPIKey, false, WithRequestObserver(observer))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if _, err := client.GetInfo(context.Background()); err == nil {
		t.Fatal("GetInfo succeeded against a closed server, want a transport error")
	}

	if got := len(observer.observations); got != 1 {
		t.Fatalf("observations = %d, want exactly one per failed request", got)
	}
	if got := observer.observations[0]; got.operation != testOperationInfo || got.err == nil {
		t.Errorf("observation = %+v, want operation %q with the transport error", got, testOperationInfo)
	}
}

// TestClientWithoutObserverRecordsNothing guards the default: a client built
// without the option still serves every request, and its no-op observer is
// never nil on the request path.
func TestClientWithoutObserverRecordsNothing(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
	}))

	if client.observer == nil {
		t.Fatal("HTTPClient.observer is nil, want the no-op observer")
	}
	if _, err := client.GetInfo(context.Background()); err != nil {
		t.Fatalf("GetInfo: %v", err)
	}
}

// TestWithRequestObserverIgnoresNil asserts a nil observer leaves the no-op
// default in place instead of installing a nil dependency.
func TestWithRequestObserverIgnoresNil(t *testing.T) {
	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
	}))
	var obs RequestObserver

	WithRequestObserver(obs)(client)

	if client.observer == nil {
		t.Fatal("observer is nil after WithRequestObserver(nil), want the no-op observer")
	}
}

// newClientWithObserver returns a client whose requests are reported to obs.
func newClientWithObserver(t *testing.T, obs RequestObserver, handler http.Handler) *HTTPClient {
	t.Helper()

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client, err := NewClient(srv.URL, testAPIKey, false, WithRequestObserver(obs))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}
