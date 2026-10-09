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
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// drainOwnedChildren lists every object of the given list type in the parent's
// namespace, initiates deletion of the ones parent controls, and returns how
// many controlled children remain. Garbage collection only starts after the
// owner is actually gone, so a parent whose removal must wait for its children
// (a site waiting for its networks, a network waiting for its broadcasts) has
// to drive the deletion itself and re-check.
//
// It is shared by the site drain (networks, firewall zones) and the network
// drain (WiFi broadcasts) rather than hand-written per parent. The list is
// passed by the caller so each parent names exactly the child kinds it owns.
func drainOwnedChildren(ctx context.Context, c client.Client, parent client.Object, list client.ObjectList) (int, error) {
	if err := c.List(ctx, list, client.InNamespace(parent.GetNamespace())); err != nil {
		return 0, fmt.Errorf("list owned %T for %q: %w", list, parent.GetName(), err)
	}
	items, err := apimeta.ExtractList(list)
	if err != nil {
		return 0, fmt.Errorf("extract owned %T items for %q: %w", list, parent.GetName(), err)
	}
	remaining := 0
	for _, item := range items {
		child, ok := item.(client.Object)
		if !ok {
			continue
		}
		if !metav1.IsControlledBy(child, parent) {
			continue
		}
		remaining++
		if !child.GetDeletionTimestamp().IsZero() {
			// Deletion already initiated; wait for the child's own finalizer.
			continue
		}
		if err := c.Delete(ctx, child); err != nil && !apierrors.IsNotFound(err) {
			return remaining, fmt.Errorf("delete owned %T %q: %w", child, child.GetName(), err)
		}
	}
	return remaining, nil
}
