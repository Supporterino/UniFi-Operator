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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// recordConditionTransition records a Kubernetes Event against obj when its
// Ready condition changed, so kubectl describe shows the failure and the
// recovery without an Event per retry or per resync (design D8).
//
// readyChanged is setReadyCondition's own return value, not the reconciler's
// merged "any status field changed" flag: a summary or upstream-identifier
// update leaves the condition alone and must not fire an Event. reason is the
// condition reason, so an Event and the status condition are filterable with the
// same value. message is the already-clamped condition message, which never
// carries credential material (docs/security.md); it is passed to Event rather
// than Eventf so controller-derived text is never interpreted as a format
// string.
//
// A nil recorder records nothing: production wires one per reconciler in
// cmd/main.go with the controller name, while unit tests that do not assert on
// Events leave the field unset.
//
// Keep the RBAC marker above in its own comment block: controller-gen v0.19.0
// does not harvest it from a merged doc comment, and without the "events" verb
// Event emission fails with a 403.
func recordConditionTransition(
	recorder record.EventRecorder,
	obj client.Object,
	readyChanged, ready bool,
	reason, message string,
) {
	if recorder == nil || !readyChanged {
		return
	}
	eventType := corev1.EventTypeWarning
	if ready {
		eventType = corev1.EventTypeNormal
	}
	recorder.Event(obj, eventType, reason, message)
}
