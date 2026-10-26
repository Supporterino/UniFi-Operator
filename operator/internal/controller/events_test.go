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
	"slices"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"

	unifiv1alpha1 "github.com/Supporterino/UniFi-Operator/operator/api/v1alpha1"
	"github.com/Supporterino/UniFi-Operator/operator/internal/unifi"
)

// fakeEvent renders the string record.FakeRecorder puts on its channel for one
// Event, so an assertion states the type, reason, and message it expects.
func fakeEvent(eventType, reason, message string) string {
	return eventType + " " + reason + " " + message
}

// drainEvents returns the Events recorded so far without blocking, so a test can
// observe the count between reconciles.
func drainEvents(recorder *record.FakeRecorder) []string {
	var events []string
	for {
		select {
		case event := <-recorder.Events:
			events = append(events, event)
		default:
			return events
		}
	}
}

// TestEventRecordsOnlyConditionTransitions pins design D8 at the helper: a
// transition to False is a Warning whose reason is the condition reason, a
// transition to True is a Normal, and an unchanged condition records nothing
// however often the object is reconciled.
func TestEventRecordsOnlyConditionTransitions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		readyChanged bool
		ready        bool
		reason       string
		message      string
		want         []string
	}{
		{
			name:         "transition to false is a warning",
			readyChanged: true,
			ready:        false,
			reason:       reasonSecretNotFound,
			message:      "Secret not found",
			want:         []string{fakeEvent(corev1.EventTypeWarning, reasonSecretNotFound, "Secret not found")},
		},
		{
			name:         "transition to true is normal",
			readyChanged: true,
			ready:        true,
			reason:       reasonReconciled,
			message:      "adopted upstream RADIUS profile",
			want:         []string{fakeEvent(corev1.EventTypeNormal, reasonReconciled, "adopted upstream RADIUS profile")},
		},
		{
			name:         "unchanged condition records nothing",
			readyChanged: false,
			ready:        false,
			reason:       reasonSecretNotFound,
			message:      "Secret not found",
			want:         nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			recorder := record.NewFakeRecorder(1)
			recordConditionTransition(recorder, newRadiusProfile(), tt.readyChanged, tt.ready, tt.reason, tt.message)

			if got := drainEvents(recorder); !slices.Equal(got, tt.want) {
				t.Errorf("events = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestEventMessageIsRecordedVerbatim proves the helper uses Event rather than
// Eventf: controller- and upstream-derived text must never be interpreted as a
// format string, which would corrupt the recorded message.
func TestEventMessageIsRecordedVerbatim(t *testing.T) {
	t.Parallel()

	const message = "console rejected the request: 100% invalid (want %s here)"

	recorder := record.NewFakeRecorder(1)
	recordConditionTransition(recorder, newRadiusProfile(), true, false, reasonInvalidSpec, message)

	want := []string{fakeEvent(corev1.EventTypeWarning, reasonInvalidSpec, message)}
	if got := drainEvents(recorder); !slices.Equal(got, want) {
		t.Errorf("events = %v, want %v", got, want)
	}
}

// TestEventNilRecorderRecordsNothing covers a reconciler constructed without a
// recorder, which is every unit test that does not assert on Events.
func TestEventNilRecorderRecordsNothing(t *testing.T) {
	t.Parallel()

	recordConditionTransition(nil, newRadiusProfile(), true, false, reasonInvalidSpec, "no recorder")
}

// TestEventReconcileRecordsFailureOnceThenRecovery drives the helper through a
// real reconcile loop: the first failure is recorded, an identical repeated
// failure is not (the condition did not change), and the recovery is recorded as
// a Normal Event.
func TestEventReconcileRecordsFailureOnceThenRecovery(t *testing.T) {
	stub := &stubUnifiClient{}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t,
		newRadiusProfile(), ownedSite(), readyController(testAppVersion), apiKeySecret())
	recorder := record.NewFakeRecorder(4)

	reconciler := &UnifiRadiusProfileReconciler{
		Client:    fakeClient,
		Scheme:    fakeClient.Scheme(),
		NewClient: factory.new,
		Recorder:  recorder,
	}
	reconcile := func(label string) {
		t.Helper()
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName)); err != nil {
			t.Fatalf("%s: Reconcile error = %v", label, err)
		}
	}

	notFound := "no upstream RADIUS profile named \"" + testRadiusProfileUpstreamName + "\"; nothing adopted"

	reconcile("first failure")
	if got, want := drainEvents(recorder), []string{fakeEvent(corev1.EventTypeWarning, reasonProfileNotFound, notFound)}; !slices.Equal(got, want) {
		t.Fatalf("first failure events = %v, want %v", got, want)
	}
	if *statusWrites != 1 {
		t.Fatalf("status writes after the first failure = %d, want 1", *statusWrites)
	}

	reconcile("repeated failure")
	if got := drainEvents(recorder); len(got) != 0 {
		t.Errorf("repeated failure events = %v, want none: the condition did not change", got)
	}
	if *statusWrites != 1 {
		t.Errorf("status writes after the repeated failure = %d, want 1 (unchanged status is not rewritten)", *statusWrites)
	}

	stub.setRadiusProfiles([]unifi.RadiusProfile{matchingRadiusProfile()})
	reconcile("recovery")
	adopted := "adopted upstream RADIUS profile \"" + testRadiusProfileUpstreamName + "\""
	if got, want := drainEvents(recorder), []string{fakeEvent(corev1.EventTypeNormal, reasonReconciled, adopted)}; !slices.Equal(got, want) {
		t.Errorf("recovery events = %v, want %v", got, want)
	}
}

// TestEventUpstreamIDUpdateDoesNotEmitEvent pins the gate on the Ready
// condition's own change flag rather than the reconciler's merged "something in
// status changed" flag: refreshing an upstream identifier leaves the condition
// untouched and must record no Event.
func TestEventUpstreamIDUpdateDoesNotEmitEvent(t *testing.T) {
	t.Parallel()

	stub := &stubUnifiClient{radiusProfiles: []unifi.RadiusProfile{matchingRadiusProfile()}}
	factory := &recordingFactory{client: stub}
	fakeClient, statusWrites, _ := newRadiusProfileFakeClient(t,
		newRadiusProfile(), ownedSite(), readyController(testAppVersion), apiKeySecret())
	recorder := record.NewFakeRecorder(4)

	reconciler := &UnifiRadiusProfileReconciler{
		Client:    fakeClient,
		Scheme:    fakeClient.Scheme(),
		NewClient: factory.new,
		Recorder:  recorder,
	}
	reconcile := func(label string) {
		t.Helper()
		if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testRadiusProfileResourceName)); err != nil {
			t.Fatalf("%s: Reconcile error = %v", label, err)
		}
	}

	reconcile("adoption")
	if got := drainEvents(recorder); len(got) != 1 {
		t.Fatalf("adoption events = %v, want exactly one Normal Event", got)
	}
	writesAfterAdoption := *statusWrites

	rotated := matchingRadiusProfile()
	rotated.ID = testRadiusProfileUUID + "-rotated"
	stub.setRadiusProfiles([]unifi.RadiusProfile{rotated})

	reconcile("upstream id refresh")
	if got := drainEvents(recorder); len(got) != 0 {
		t.Errorf("upstream-id-refresh events = %v, want none: the Ready condition did not change", got)
	}
	if *statusWrites != writesAfterAdoption+1 {
		t.Errorf("status writes = %d, want %d: the new upstream ID is still recorded",
			*statusWrites, writesAfterAdoption+1)
	}
}

// TestEventSecretKeyMissingNamesSecretAndKeyOnly checks the Event text against
// docs/security.md: a credential-resolution failure may name the Secret and the
// key it looked for, and must not echo a credential value from that Secret.
func TestEventSecretKeyMissingNamesSecretAndKeyOnly(t *testing.T) {
	t.Parallel()

	const otherKey = "password"

	fakeClient, _ := newFakeClient(t,
		[]client.Object{&unifiv1alpha1.UnifiController{}},
		newUnifiController(nil),
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: testSecretName, Namespace: testNamespace},
			Data:       map[string][]byte{otherKey: []byte(testSecretValue)},
		})
	recorder := record.NewFakeRecorder(2)

	reconciler := &UnifiControllerReconciler{
		Client:   fakeClient,
		Scheme:   fakeClient.Scheme(),
		Recorder: recorder,
	}
	if _, err := reconciler.Reconcile(context.Background(), controllerRequest(testControllerName)); err != nil {
		t.Fatalf("Reconcile error = %v, want nil", err)
	}

	message := "Secret \"" + testSecretName + "\" has no key \"" + testSecretKey + "\""
	want := []string{fakeEvent(corev1.EventTypeWarning, reasonSecretKeyMissing, message)}
	got := drainEvents(recorder)
	if !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	if strings.Contains(strings.Join(got, "\n"), testSecretValue) {
		t.Errorf("Event text contains the credential value: %v", got)
	}
}
