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

package metrics

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/version"
)

// Representative object identity no label value may ever carry: the values
// these tests produce are bare kinds, reasons, outcomes, and operations.
const (
	labelHygieneObjectName = "firewall-policy"
	labelHygieneNamespace  = "default"

	// Label names asserted here and in the series lookups below.
	labelOperation = "operation"
	labelOutcome   = "outcome"
)

var (
	// uuidPattern matches the 8-4-4-4-12 form UniFi uses for object IDs.
	uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// compactIDPattern matches a UUID with its hyphens stripped.
	compactIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{32}$`)
)

// TestRecordReconcileCountsNotReadyOutcome asserts a failing reconcile is
// attributed to the kind, the not_ready result, and the reason it recorded.
//
// These tests read and write the process-wide controller-runtime registry, so
// they run sequentially: a parallel test would make their counter deltas
// ambiguous.
func TestRecordReconcileCountsNotReadyOutcome(t *testing.T) {
	before := reconcileCount("UnifiController", false, "AuthenticationFailed")

	RecordReconcile("UnifiController", false, "AuthenticationFailed")

	if got := reconcileCount("UnifiController", false, "AuthenticationFailed") - before; got != 1 {
		t.Errorf("not_ready increments = %v, want 1", got)
	}
	if got := reconcileCount("UnifiController", true, "AuthenticationFailed"); got != 0 {
		t.Errorf("ready series = %v, want 0: a failing reconcile must not be counted as ready", got)
	}
}

func TestRecordReconcileCountsReadyOutcome(t *testing.T) {
	before := reconcileCount("UnifiSite", true, "SiteAdopted")

	RecordReconcile("UnifiSite", true, "SiteAdopted")

	if got := reconcileCount("UnifiSite", true, "SiteAdopted") - before; got != 1 {
		t.Errorf("ready increments = %v, want 1", got)
	}
	if got := reconcileCount("UnifiSite", false, "SiteAdopted"); got != 0 {
		t.Errorf("not_ready series = %v, want 0: a successful reconcile must not be counted as failing", got)
	}
}

func TestBuildInfoReportsVersion(t *testing.T) {
	// Registration records the build version; there is no import-time side
	// effect any more, so a caller that wants the gauge set asks for it.
	if err := RegisterTo(prometheus.NewRegistry()); err != nil {
		t.Fatalf("register metrics: %v", err)
	}

	if got := testutil.ToFloat64(buildInfo.WithLabelValues(version.Version)); got != 1 {
		t.Errorf("build_info{version=%q} = %v, want 1", version.Version, got)
	}

	// Comparing the whole collected family pins the label set to exactly
	// `version`, so the build identity cannot spread to the other metrics
	// (design D7).
	want := fmt.Sprintf(`# HELP unifi_operator_build_info Build identity of the running operator; the value is always 1.
# TYPE unifi_operator_build_info gauge
unifi_operator_build_info{version=%q} 1
`, version.Version)
	if err := testutil.CollectAndCompare(buildInfo, strings.NewReader(want)); err != nil {
		t.Errorf("build_info exposition mismatch: %v", err)
	}
}

// TestRecordUpstreamRequestDerivesOutcome pins the helper to the closed
// outcome set: the error argument, not the caller, decides success or error.
// One call per request observes latency too, which the client-level test in
// internal/unifi asserts together with the counter.
func TestRecordUpstreamRequestDerivesOutcome(t *testing.T) {
	successBefore := testutil.ToFloat64(upstreamRequestsTotal.WithLabelValues("info", OutcomeSuccess))
	errorBefore := testutil.ToFloat64(upstreamRequestsTotal.WithLabelValues("info", OutcomeError))

	RecordUpstreamRequest("info", nil, 5*time.Millisecond)
	RecordUpstreamRequest("info", errors.New("controller unreachable"), 7*time.Millisecond)

	if got := testutil.ToFloat64(upstreamRequestsTotal.WithLabelValues("info", OutcomeSuccess)) - successBefore; got != 1 {
		t.Errorf("success increments = %v, want 1", got)
	}
	if got := testutil.ToFloat64(upstreamRequestsTotal.WithLabelValues("info", OutcomeError)) - errorBefore; got != 1 {
		t.Errorf("error increments = %v, want 1", got)
	}
}

// TestRegisterTwiceDoesNotPanic covers the explicit-registration model: the
// manager calls Register once at startup, and a second call — a test run, a
// second manager construction, or RegisterTo on a registry that already holds
// the collectors — must be a no-op rather than a panic.
func TestRegisterTwiceDoesNotPanic(t *testing.T) {
	Register()
	Register()

	registry := newTestRegistry(t)
	if err := RegisterTo(registry); err != nil {
		t.Fatalf("re-registering an existing registry: %v", err)
	}

	// Give every collector a child so all four families are exposed.
	RecordReconcile("UnifiSite", true, "Reconciled")
	upstreamRequestsTotal.WithLabelValues("networks", OutcomeSuccess).Inc()
	upstreamRequestDuration.WithLabelValues("networks").Observe(0.01)

	// The served registry is populated by the explicit Register call, and the
	// collectors are exposed exactly once in it.
	assertFamiliesOnce(t, "served", ctrlmetrics.Registry)

	// The same holds for a registry registered twice.
	assertFamiliesOnce(t, "test", registry)
}

// TestUpstreamRecorderRecordsRequest proves the adapter production injects into
// the UniFi client records the counter and the latency histogram together, and
// derives the outcome from the request's error rather than from the caller.
func TestUpstreamRecorderRecordsRequest(t *testing.T) {
	registry := newTestRegistry(t)
	const operation = "firewall.zones"

	UpstreamRecorder{}.ObserveRequest(operation, nil, 5*time.Millisecond)
	UpstreamRecorder{}.ObserveRequest(operation, errors.New("controller unreachable"), 7*time.Millisecond)

	success := map[string]string{labelOperation: operation, labelOutcome: OutcomeSuccess}
	if got, _ := sampleValue(t, registry, "unifi_operator_upstream_requests_total", success); got != 1 {
		t.Errorf("success increments = %v, want 1", got)
	}
	failure := map[string]string{labelOperation: operation, labelOutcome: OutcomeError}
	if got, _ := sampleValue(t, registry, "unifi_operator_upstream_requests_total", failure); got != 1 {
		t.Errorf("error increments = %v, want 1", got)
	}
	if _, samples := sampleValue(t, registry, "unifi_operator_upstream_request_duration_seconds", map[string]string{labelOperation: operation}); samples != 2 {
		t.Errorf("latency observations = %d, want one per request", samples)
	}
}

// assertFamiliesOnce asserts each exported family is present exactly once, so a
// duplicate registration cannot hide behind a successful gather.
func assertFamiliesOnce(t *testing.T, registryName string, gatherer prometheus.Gatherer) {
	t.Helper()

	families, err := gatherer.Gather()
	if err != nil {
		t.Fatalf("gather the %s registry: %v", registryName, err)
	}
	seen := map[string]int{}
	for _, family := range families {
		seen[family.GetName()]++
	}
	for _, name := range []string{
		"unifi_operator_reconcile_total",
		"unifi_operator_upstream_requests_total",
		"unifi_operator_upstream_request_duration_seconds",
		"unifi_operator_build_info",
	} {
		if seen[name] != 1 {
			t.Errorf("%s registry: families named %q = %d, want 1", registryName, name, seen[name])
		}
	}
}

// TestLabelValuesAreBoundedAndCredentialFree is the label-hygiene guard for the
// whole package: every value the four metrics expose must come from a closed
// set — a Custom Resource kind, a condition reason, a fixed result or outcome,
// a logical operation, or the build version — and must never carry an object
// name, a namespace, or an upstream identifier.
func TestLabelValuesAreBoundedAndCredentialFree(t *testing.T) {
	registry := newTestRegistry(t)
	kinds := knownKinds(t)
	reasons := knownReasons(t)
	operations := []string{"firewall.policies", "firewall.zones", "info", "networks"}

	for _, kind := range kinds {
		for _, reason := range reasons {
			RecordReconcile(kind, false, reason)
			RecordReconcile(kind, true, reason)
		}
	}
	for _, operation := range operations {
		upstreamRequestsTotal.WithLabelValues(operation, OutcomeSuccess).Inc()
		upstreamRequestsTotal.WithLabelValues(operation, OutcomeError).Inc()
		upstreamRequestDuration.WithLabelValues(operation).Observe(0.01)
	}

	allowed := map[string]map[string]struct{}{
		"kind":         valueSet(kinds...),
		"result":       valueSet(ResultReady, ResultNotReady),
		"reason":       valueSet(reasons...),
		labelOperation: valueSet(operations...),
		labelOutcome:   valueSet(OutcomeSuccess, OutcomeError),
		"version":      valueSet(version.Version),
	}

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	checked := 0
	for _, family := range families {
		if !strings.HasPrefix(family.GetName(), "unifi_operator_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, pair := range metric.GetLabel() {
				checked++
				name, value := pair.GetName(), pair.GetValue()
				values, known := allowed[name]
				if !known {
					t.Errorf("%s: unexpected label %q", family.GetName(), name)
					continue
				}
				if _, ok := values[value]; !ok {
					t.Errorf("%s: label %q value %q is not in its closed set", family.GetName(), name, value)
				}
				assertHarmlessLabelValue(t, family.GetName(), name, value)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no unifi_operator_* label values were gathered; the hygiene check did not run")
	}
}

// assertHarmlessLabelValue rejects the shapes a leaked identifier would take.
func assertHarmlessLabelValue(t *testing.T, family, name, value string) {
	t.Helper()

	switch {
	case uuidPattern.MatchString(value), compactIDPattern.MatchString(value):
		t.Errorf("%s: label %q value %q looks like an upstream UUID", family, name, value)
	case strings.Contains(value, "/"):
		t.Errorf("%s: label %q value %q contains a path separator", family, name, value)
	case strings.ContainsAny(value, " \t\n"):
		t.Errorf("%s: label %q value %q contains whitespace", family, name, value)
	case strings.Contains(value, labelHygieneObjectName):
		t.Errorf("%s: label %q value %q contains an object name", family, name, value)
	case strings.Contains(value, labelHygieneNamespace):
		t.Errorf("%s: label %q value %q contains a namespace", family, name, value)
	}
}

// reconcileCount reads one reconcile counter series. Reading a series that does
// not exist yet returns 0 and creates it at 0, so a test asserting "no
// increment" cannot mistake absence for a healthy path.
func reconcileCount(kind string, ready bool, reason string) float64 {
	result := ResultNotReady
	if ready {
		result = ResultReady
	}
	return testutil.ToFloat64(reconcileTotal.WithLabelValues(kind, result, reason))
}

// newTestRegistry returns a fresh registry holding this package's collectors, so
// a test asserts on the exported values without depending on whether Register
// has populated the process-wide registry. Registration is explicit, not an
// import side effect.
func newTestRegistry(t *testing.T) *prometheus.Registry {
	t.Helper()

	registry := prometheus.NewRegistry()
	if err := RegisterTo(registry); err != nil {
		t.Fatalf("register metrics: %v", err)
	}
	return registry
}

// sampleValue reads one series out of reg, matching on its full label set. It
// returns the counter value and the histogram sample count for the same series,
// so one lookup answers either assertion; a series that does not exist yet reads
// as zero.
func sampleValue(t *testing.T, reg *prometheus.Registry, familyName string, want map[string]string) (float64, uint64) {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != familyName {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}
			if maps.Equal(labels, want) {
				return metric.GetCounter().GetValue(), metric.GetHistogram().GetSampleCount()
			}
		}
	}
	return 0, 0
}

// knownKinds returns the Custom Resource kinds the operator reconciles. The API
// scheme is the source of truth, so a new kind joins the checked set without
// editing this test.
func knownKinds(t *testing.T) []string {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := unifiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("build the UniFi scheme: %v", err)
	}
	// The builder registers helper types (list/get options, watch events) on the
	// same group version, so keep only objects whose Go type is an API type, and
	// drop the list wrapper of each kind.
	apiTypes := reflect.TypeOf(unifiv1alpha1.UnifiSite{}).PkgPath()
	kinds := make([]string, 0, len(scheme.AllKnownTypes()))
	for gvk := range scheme.AllKnownTypes() {
		if gvk.GroupVersion() != unifiv1alpha1.GroupVersion {
			continue
		}
		object, err := scheme.New(gvk)
		if err != nil {
			t.Fatalf("instantiate %s: %v", gvk, err)
		}
		if reflect.Indirect(reflect.ValueOf(object)).Type().PkgPath() != apiTypes || apimeta.IsListType(object) {
			continue
		}
		kinds = append(kinds, gvk.Kind)
	}
	sort.Strings(kinds)
	if len(kinds) == 0 {
		t.Fatal("no kinds found in the UniFi scheme")
	}
	return kinds
}

// knownReasons returns the condition reasons the reconcilers write. They are
// unexported in the controller package and this package must not import it —
// that would invert the dependency and let the reason taxonomy drift — so the
// constants are read from the file that declares them rather than duplicated
// here. Only package-level constant declarations count: a function-local
// literal is not part of the taxonomy.
func knownReasons(t *testing.T) []string {
	t.Helper()

	const conditionsFile = "../controller/conditions.go"
	file, err := parser.ParseFile(token.NewFileSet(), conditionsFile, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", conditionsFile, err)
	}
	var reasons []string
	for _, node := range file.Decls {
		decl, ok := node.(*ast.GenDecl)
		if !ok || decl.Tok != token.CONST {
			continue
		}
		for _, spec := range decl.Specs {
			values, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, expr := range values.Values {
				literal, ok := expr.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil {
					t.Fatalf("unquote %s: %v", literal.Value, err)
				}
				reasons = append(reasons, value)
			}
		}
	}
	if len(reasons) < 20 {
		t.Fatalf("found %d string constants in %s, want the full reason taxonomy", len(reasons), conditionsFile)
	}
	sort.Strings(reasons)
	return reasons
}

// valueSet builds the membership set a label's values must belong to.
func valueSet(values ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}
