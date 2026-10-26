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
	"maps"
	"net/http"
	"reflect"
	"slices"
	"sort"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/metrics"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// reconcileKey identifies one unifi_operator_reconcile_total series.
type reconcileKey struct {
	kind   string
	result string
	reason string
}

func (k reconcileKey) String() string {
	return k.kind + "/" + k.result + "/" + k.reason
}

// reconcileSample is one series of the reconcile counter together with the
// label set it was exposed with.
type reconcileSample struct {
	value  float64
	labels map[string]string
}

// The reconcile counter is process-wide, so every test in this file that reads
// it runs sequentially and asserts deltas around the reconcile under test.

// TestMetricReconcileCounterTracksFirewallPolicyStatus drives a terminal
// Ready=False reconcile and asserts the counter moved with the reason status
// recorded, and that the series carries no object identity.
func TestMetricReconcileCounterTracksFirewallPolicyStatus(t *testing.T) {
	policy := newFirewallPolicy()
	policy.Spec.Source.ZoneRef = &unifiv1alpha1.CoreRef{Name: "missing-zone"}

	stub := &stubUnifiClient{zones: []unifi.FirewallZone{systemUpstreamZone()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newFirewallPolicyFakeClient(t,
		policy, ownedSite(), readyController(testAppVersion), apiKeySecret())

	notReady := reconcileKey{kind: reconcileKindFirewallPolicy, result: metrics.ResultNotReady, reason: reasonZoneRefNotFound}
	ready := reconcileKey{kind: reconcileKindFirewallPolicy, result: metrics.ResultReady, reason: reasonReconciled}
	before := reconcileSamples(t)

	reconciler := &UnifiFirewallPolicyReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testFirewallPolicyResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if *statusWrites != 1 {
		t.Fatalf("status writes = %d, want 1", *statusWrites)
	}

	after := reconcileSamples(t)
	assertReconcileDelta(t, before, after, notReady, 1)
	assertReconcileDelta(t, before, after, ready, 0)

	// The series must carry the closed label set and nothing that identifies the
	// object or its namespace (design D2, docs/security.md).
	want := map[string]string{
		"kind":   reconcileKindFirewallPolicy,
		"result": metrics.ResultNotReady,
		"reason": reasonZoneRefNotFound,
	}
	if got := after[notReady].labels; !maps.Equal(got, want) {
		t.Errorf("label set = %v, want %v", got, want)
	}
}

func TestMetricReconcileCounterTracksRadiusProfileStatus(t *testing.T) {
	stub := &stubUnifiClient{radiusProfiles: []unifi.RadiusProfile{{ID: testOtherNamespace, Name: testOtherObjectName}}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t,
		newRadiusProfile(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	notReady := reconcileKey{kind: reconcileKindRadiusProfile, result: metrics.ResultNotReady, reason: reasonProfileNotFound}
	before := reconcileSamples(t)

	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}
	if *statusWrites != 1 {
		t.Fatalf("status writes = %d, want 1", *statusWrites)
	}

	assertReconcileDelta(t, before, reconcileSamples(t), notReady, 1)
}

// TestMetricReconcileCounterCountsUnchangedOutcome pins the decision that every
// terminal reconcile is counted, not only the ones that changed status: a
// steady failure has to keep incrementing for a rate() alert over it to fire.
func TestMetricReconcileCounterCountsUnchangedOutcome(t *testing.T) {
	stub := &stubUnifiClient{radiusProfiles: []unifi.RadiusProfile{matchingRadiusProfile()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t,
		newRadiusProfile(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	ready := reconcileKey{kind: reconcileKindRadiusProfile, result: metrics.ResultReady, reason: reasonReconciled}
	before := reconcileSamples(t)

	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	for i := 0; i < 2; i++ {
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName)); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	if *statusWrites != 1 {
		t.Fatalf("status writes = %d, want 1 (the second reconcile must be a no-op)", *statusWrites)
	}

	assertReconcileDelta(t, before, reconcileSamples(t), ready, 2)
}

func TestMetricReconcileCounterSkipsTransientErrors(t *testing.T) {
	stub := &stubUnifiClient{radiusProfilesErr: &unifi.APIError{StatusCode: http.StatusInternalServerError, Message: "controller unavailable"}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t,
		newRadiusProfile(), ownedSite(), readyController(testAppVersion), apiKeySecret())

	before := reconcileSamples(t)

	reconciler := &UnifiRadiusProfileReconciler{Client: fakeClient, Scheme: fakeClient.Scheme(), NewClient: factory.new}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName)); err == nil {
		t.Fatal("Reconcile error = nil, want the transient failure to be returned for a retry")
	}
	if *statusWrites != 0 {
		t.Fatalf("status writes = %d, want 0 (a transient error writes no terminal status)", *statusWrites)
	}

	after := reconcileSamples(t)
	for key, sample := range after {
		if key.kind != reconcileKindRadiusProfile {
			continue
		}
		if want := before[key].value; sample.value != want {
			t.Errorf("series %s = %v, want %v: a retried transient error is not a terminal outcome", key, sample.value, want)
		}
	}
}

// TestMetricReconcileKindsMatchAPITypes guards the closed kind set: every label
// value passed to RecordReconcile is a Custom Resource kind the API server
// knows, and no kind is missing from the instrumentation.
func TestMetricReconcileKindsMatchAPITypes(t *testing.T) {
	instrumented := []string{
		reconcileKindAclRule,
		reconcileKindAclRuleOrdering,
		reconcileKindController,
		reconcileKindDnsPolicy,
		reconcileKindFirewallPolicy,
		reconcileKindFirewallPolicyOrdering,
		reconcileKindFirewallZone,
		reconcileKindNetwork,
		reconcileKindRadiusProfile,
		reconcileKindSite,
		reconcileKindSiteToSiteTunnel,
		reconcileKindSwitchingLag,
		reconcileKindSwitchingMcLagDomain,
		reconcileKindSwitchingSwitchStack,
		reconcileKindTrafficMatchingList,
		reconcileKindVpnServer,
		reconcileKindWifiBroadcast,
	}
	slices.Sort(instrumented)

	if got := apiKinds(t); !slices.Equal(got, instrumented) {
		t.Errorf("instrumented kinds = %v, want the API kinds %v", instrumented, got)
	}
}

// apiKinds returns the sorted Custom Resource kinds registered on the UniFi
// scheme, excluding the list wrappers and the helper types the scheme builder
// adds for the same group version.
func apiKinds(t *testing.T) []string {
	t.Helper()

	scheme := runtime.NewScheme()
	if err := unifiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("build the UniFi scheme: %v", err)
	}
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
		if apimeta.IsListType(object) {
			continue
		}
		if reflect.Indirect(reflect.ValueOf(object)).Type().PkgPath() != apiTypes {
			continue
		}
		kinds = append(kinds, gvk.Kind)
	}
	sort.Strings(kinds)
	return kinds
}

// reconcileSamples gathers the reconcile counter keyed by its label set. It
// gathers a registry built for the call rather than the process-wide one, so the
// metric assertions do not depend on registration having happened elsewhere;
// the reconcilers increment the same collectors either way.
func reconcileSamples(t *testing.T) map[reconcileKey]reconcileSample {
	t.Helper()

	registry := prometheus.NewRegistry()
	if err := metrics.RegisterTo(registry); err != nil {
		t.Fatalf("register metrics: %v", err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	samples := map[reconcileKey]reconcileSample{}
	for _, family := range families {
		if family.GetName() != "unifi_operator_reconcile_total" {
			continue
		}
		for _, metric := range family.GetMetric() {
			labels := make(map[string]string, len(metric.GetLabel()))
			for _, pair := range metric.GetLabel() {
				labels[pair.GetName()] = pair.GetValue()
			}
			key := reconcileKey{kind: labels["kind"], result: labels["result"], reason: labels["reason"]}
			samples[key] = reconcileSample{value: metric.GetCounter().GetValue(), labels: labels}
		}
	}
	return samples
}

// assertReconcileDelta asserts how far one series moved between two gathers.
func assertReconcileDelta(t *testing.T, before, after map[reconcileKey]reconcileSample, key reconcileKey, want float64) {
	t.Helper()

	if got := after[key].value - before[key].value; got != want {
		t.Errorf("series %s moved by %v, want %v", key, got, want)
	}
}
