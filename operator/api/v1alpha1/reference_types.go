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

package v1alpha1

// CoreRef is a reference to another Custom Resource by Kubernetes name.
//
// References are deliberately name-only and same-namespace: a reference never
// exposes a namespace field because a cross-namespace object cannot be an
// ownerReference, and same-namespace resolution keeps RBAC to namespaced Roles.
// See docs/crd-conventions.md for the reference and ownership model.
type CoreRef struct {
	// Name is the name of the referenced object in the referencing object's
	// namespace.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// DeviceTagSelector selects an existing upstream device tag by its
// human-readable name (GET /v1/sites/{siteId}/device-tags). Device tags are
// read-only upstream, so there is no device-tag Custom Resource and no opaque
// tag or device identifier in spec. The controller resolves the name through
// the read-only tag list at reconcile time; a name that resolves to zero or
// more than one device fails closed.
type DeviceTagSelector struct {
	// Name is the human-readable device-tag name.
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}
