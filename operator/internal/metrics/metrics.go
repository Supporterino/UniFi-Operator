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

// Package metrics exports what the operator reports about its own health: the
// outcome of each terminal reconcile together with the reason written to the
// object's Ready condition, the traffic it sends to the UniFi controller, and
// the build it was compiled from.
//
// Every label value is drawn from a closed, enumerable set — a Custom Resource
// kind, a condition reason, a fixed result or outcome, a bounded operation
// name, or the build version. No object name, namespace, upstream UUID, URL,
// condition message, or credential is ever exported (docs/security.md).
//
// Collectors are registered by an explicit Register call at startup, not by an
// import side effect, so the manager controls when the served registry is
// populated and tests can register into a registry of their own.
package metrics

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/Supporterino/UniFi-Operator/operator/internal/version"
)

// Fixed label values. The result and outcome sets are closed: no other value
// is passed to those labels (design D2).
const (
	// ResultReady labels a reconcile that wrote Ready=True.
	ResultReady = "ready"
	// ResultNotReady labels a reconcile that wrote Ready=False.
	ResultNotReady = "not_ready"
	// OutcomeSuccess labels a UniFi controller request that returned no error.
	OutcomeSuccess = "success"
	// OutcomeError labels a UniFi controller request that failed.
	OutcomeError = "error"
)

var (
	reconcileTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unifi_operator_reconcile_total",
		Help: "Terminal reconcile outcomes by Custom Resource kind, result, and the reason written to the Ready condition.",
	}, []string{"kind", "result", "reason"})

	upstreamRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "unifi_operator_upstream_requests_total",
		Help: "Requests sent to the UniFi controller by logical operation and outcome.",
	}, []string{"operation", "outcome"})

	upstreamRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "unifi_operator_upstream_request_duration_seconds",
		Help: "Latency of requests sent to the UniFi controller by logical operation.",
	}, []string{"operation"})

	buildInfo = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "unifi_operator_build_info",
		Help: "Build identity of the running operator; the value is always 1.",
	}, []string{"version"})

	// collectors is the set RegisterTo adds to a registry.
	collectors = []prometheus.Collector{reconcileTotal, upstreamRequestsTotal, upstreamRequestDuration, buildInfo}

	// registerOnce makes Register a one-time operation for the process, so a
	// second manager construction cannot re-register behind the first.
	registerOnce sync.Once
)

// Register adds the operator's collectors to the controller-runtime registry
// that the metrics server serves, and records the build version. The manager
// calls it once at startup; tests that gather from the served registry call it
// explicitly, because registration is deliberately not an import side effect
// (docs/go.md, "Packages and layering").
//
// It is idempotent: repeated calls do nothing, and a collector that is already
// registered is skipped rather than treated as fatal. Any other registration
// error is a programming mistake and panics at startup.
func Register() {
	registerOnce.Do(func() {
		if err := RegisterTo(ctrlmetrics.Registry); err != nil {
			panic(fmt.Errorf("register operator metrics: %w", err))
		}
	})
}

// RegisterTo adds the operator's collectors to reg and records the build
// version gauge. Callers that serve the controller-runtime registry use
// Register; tests pass a fresh prometheus.Registry so their assertions do not
// depend on process-wide registration state.
//
// A collector that reg already holds is skipped rather than treated as fatal,
// so registering twice into one registry is safe. Any other registration error
// is returned, because it is a programming mistake.
func RegisterTo(reg prometheus.Registerer) error {
	for _, collector := range collectors {
		err := reg.Register(collector)
		if err == nil {
			continue
		}
		var registered prometheus.AlreadyRegisteredError
		if errors.As(err, &registered) {
			continue
		}
		return fmt.Errorf("register metrics collector %T: %w", collector, err)
	}
	buildInfo.WithLabelValues(version.Version).Set(1)
	return nil
}

// RecordReconcile counts one terminal reconcile outcome. Callers pass the ready
// state and the reason they have just written to the object's Ready condition,
// so the exported reason always matches status instead of being re-derived.
//
// It is called once at the end of each reconciler's updateStatus, including
// reconciles that changed nothing: a self-retrying failure (for example a
// dependency or credential-resolution requeue) keeps incrementing so a rate()
// query over it stays non-zero, while a terminal failure re-counts only when
// the object is next reconciled. Transient errors that return without a status
// write never reach it and are therefore not reported as terminal.
//
// A whole-site conflict sweep (marking peer firewall zones, or peer ordering
// objects, that collide on a runtime uniqueness rule) re-runs updateStatus for
// every object it refreshes. One reconcile of one claimant therefore records
// one outcome per refreshed object, not one outcome overall. That is
// deliberate: each refreshed object's failure is genuinely queryable, and
// summing the series would over-count the claimants.
func RecordReconcile(kind string, ready bool, reason string) {
	result := ResultNotReady
	if ready {
		result = ResultReady
	}
	reconcileTotal.WithLabelValues(kind, result, reason).Inc()
}

// RecordUpstreamRequest counts one request to the UniFi controller and observes
// its latency under the same operation label. Callers pass the request's error
// rather than an outcome string: the closed success|error set is then derived
// in one place and cannot be violated, and the counter and the histogram are
// always recorded together for that request. A caller that runs it from a
// deferred call therefore records exactly once per request, whichever way the
// request ended — including a failed status, a transport error, and a decode
// error.
func RecordUpstreamRequest(operation string, err error, elapsed time.Duration) {
	outcome := OutcomeSuccess
	if err != nil {
		outcome = OutcomeError
	}
	upstreamRequestsTotal.WithLabelValues(operation, outcome).Inc()
	upstreamRequestDuration.WithLabelValues(operation).Observe(elapsed.Seconds())
}

// UpstreamRecorder records the UniFi client's requests in the upstream metrics.
// It implements unifi.RequestObserver, so the client depends on its own plain
// interface and never imports this package or anything else in the Kubernetes
// world (docs/go.md, "Packages and layering"). A value of this type carries no
// state and is safe to share between reconcilers.
type UpstreamRecorder struct{}

// ObserveRequest counts one controller request and observes its latency under
// the same operation label.
func (UpstreamRecorder) ObserveRequest(operation string, err error, elapsed time.Duration) {
	RecordUpstreamRequest(operation, err, elapsed)
}
