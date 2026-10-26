/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// testInfoPath is the reachability probe GetInfo issues under the Integration v1
// base path.
const testInfoPath = "/proxy/network/integration/v1/info"

// observation is one request a factory-built client reported to its observer.
type observation struct {
	operation string
	err       error
}

// observationRecorder is the RequestObserver double for the connection factory.
// It keeps what the clients built by NewConnectionFactory report, so a factory
// that fails to install the observer is caught here instead of shipping with
// every unifi_operator_upstream_* series absent.
type observationRecorder struct {
	observations []observation
}

// ObserveRequest implements unifi.RequestObserver.
func (o *observationRecorder) ObserveRequest(operation string, err error, _ time.Duration) {
	o.observations = append(o.observations, observation{operation: operation, err: err})
}

// TestNewConnectionFactoryObservesRequests pins the factory cmd/main.go hands to
// every reconciler: the clients it builds must report their requests, or the
// upstream metrics measure nothing while every reconciler test stays green. The
// wiring test reads cmd/main.go; this test exercises the factory it finds there.
func TestNewConnectionFactoryObservesRequests(t *testing.T) {
	observer := &observationRecorder{}
	apiClient, err := NewConnectionFactory(observer)(newInfoServer(t).URL, "test-api-key", false)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	if _, err := apiClient.GetInfo(context.Background()); err != nil {
		t.Fatalf("GetInfo: %v", err)
	}

	if got := len(observer.observations); got != 1 {
		t.Fatalf("observations = %d, want exactly one per request: the factory's client is not observed", got)
	}
	if got := observer.observations[0]; got.operation != "info" || got.err != nil {
		t.Errorf("observation = %+v, want operation %q with no error", got, "info")
	}
}

// TestNewConnectionFactoryWithoutObserverUsesDefault pins the documented nil
// behaviour: a caller that omits observation still gets the plain factory, which
// builds a working but unobserved client rather than a failing one.
func TestNewConnectionFactoryWithoutObserverUsesDefault(t *testing.T) {
	factory := NewConnectionFactory(nil)
	if factory == nil {
		t.Fatal("NewConnectionFactory(nil) = nil, want the default factory")
	}
	if got, want := reflect.ValueOf(factory).Pointer(), reflect.ValueOf(defaultConnectionFactory).Pointer(); got != want {
		t.Errorf("NewConnectionFactory(nil) does not return defaultConnectionFactory, want the unobserved default")
	}

	apiClient, err := factory(newInfoServer(t).URL, "test-api-key", false)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}
	if _, err := apiClient.GetInfo(context.Background()); err != nil {
		t.Fatalf("GetInfo through the default factory: %v", err)
	}
}

// newInfoServer serves the GetInfo endpoint so a test can drive a real client.
func newInfoServer(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != testInfoPath {
			t.Errorf("request path = %q, want %q", r.URL.Path, testInfoPath)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"applicationVersion":"10.4.57"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}
