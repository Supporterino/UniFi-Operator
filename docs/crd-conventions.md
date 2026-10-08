# CRD Conventions

This is the canonical design bar for every Custom Resource in UniFi-Operator. It exists to
keep the `spec` a stable, human-authored contract and the `status` a faithful report of
reality. Every API type in `operator/api/v1alpha1/` follows these rules, and the root
`AGENTS.md` CRD golden rule links here.

## The core distinction: `spec` vs `status`

- **`spec` is desired state, authored by humans.** It is the input. A field in `spec` must
  never be a mirror of something the controller observed; if a human cannot meaningfully set
  it, it does not belong in `spec`.
- **`status` is observed state, written only by the controller.** It is the output. Conditions,
  upstream identifiers, and summaries all live here.

## Rule 1 — No ambiguous IDs in `spec`

Never expose a UniFi internal identifier (`_id`) or any opaque numeric ID in `spec`. Users
should not have to know or copy controller-internal IDs.

Instead:

- **Reference another CR by Kubernetes identity.** Use a `name` (+ optional `namespace`) field
  or a typed selector (`metav1.LabelSelector` or a purpose-built selector struct). The
  controller resolves the reference to an upstream ID at reconcile time.
- **Put correlation IDs in `status`.** If you need to record the upstream `_id` a CR maps to,
  expose it in `status` (for example `status.controllerID`) so tooling can correlate, without
  making it part of the contract users author.

```go
// Good: spec references by name.
type VLANSpec struct {
    NetworkRef NetworkReference `json:"networkRef"`
}

type NetworkReference struct {
    Name      string `json:"name"`
    Namespace string `json:"namespace,omitempty"`
}

// Good: upstream id in status only.
type VLANStatus struct {
    ControllerID string `json:"controllerID,omitempty"`
}
```

```go
// Bad: an internal id authored in spec.
type VLANSpec struct {
    NetworkID string `json:"networkId"` // opaque UniFi _id — forbidden
}
```

## Rule 2 — Every CRD has a rich `status`

Every kind defines a `Status` with:

- `conditions []metav1.Condition` — standard Kubernetes conditions. A `Ready` condition is the
  primary readiness signal; use `Synced`/`Reconciling`/`Error` as needed. Always set
  `observedGeneration`, `reason`, and `message`.
- `observedGeneration int64` — the `metadata.generation` the status reflects. Set it to
  `obj.Generation` only once the reconcile for that generation has completed.
- A human-readable summary field (for example `status.summary` or a small struct) so
  `kubectl get -o wide` is useful without reading conditions.

Add these markers:

```go
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Ready",type=string,JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type VLAN struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`
    Spec   VLANSpec   `json:"spec,omitempty"`
    Status VLANStatus `json:"status,omitempty"`
}
```

## Rule 3 — Finalizers are symmetric

Any CRD whose deletion must clean up UniFi-side state uses a finalizer:

- Add the finalizer before creating the upstream object.
- On `deletionTimestamp` being set with the finalizer present, delete the upstream object,
  then remove the finalizer.
- Add and remove in the same reconcile path so a retry cannot leave a half-cleaned object.

The controller owns child Kubernetes objects via `ownerReferences` so garbage collection works.

## Rule 4 — Validation lives in markers

Express validation declaratively with `+kubebuilder:validation:*` markers so it is enforced by
the API server and the CLI can rely on it:

```go
// +kubebuilder:validation:Pattern=`^([0-9]{1,3}\.){3}[0-9]{1,3}(/[0-9]{1,2})?$`
CIDR string `json:"cidr"`

// +kubebuilder:validation:Enum=Enabled;Disabled
State string `json:"state"`
```

Use required/optional intent explicitly. Only add a validating webhook when the rule is
genuinely cross-field and cannot be expressed in markers.

## Rule 5 — Versioning and compatibility

- The initial served version is `v1alpha1`.
- Add fields; do not remove them. Deprecate in a field comment before removing.
- A breaking schema change introduces a new version (`v1beta1`, `v1`) and uses the
  hub/spoke conversion pattern, never a silent in-place change.

## Rule 6 — CLI-emitted CRs obey the same rules

The CLI snapshot command emits CRs that a user applies. Those CRs must be valid against the
CRDs and must not carry upstream `_id`s in `spec`. If a controller object cannot be represented
losslessly, the CLI records the gap in an annotation and warns rather than inventing a
spec field.

## Checklist for a new kind

- [ ] API type in `operator/api/v1alpha1/` with `Spec`, `Status`, and type/object root markers.
- [ ] Status has `conditions`, `observedGeneration`, and a summary; `+kubebuilder:subresource:status`.
- [ ] Printer columns for readiness and age.
- [ ] No opaque IDs in `spec`; references by name/namespace/selector.
- [ ] Finalizer added/removed symmetrically if deletion has upstream effects.
- [ ] `make manifests generate` run; `config/crd/` and deepcopy updated with no diff.
- [ ] Reconciler updates status on every terminal path.

## Related

- [Architecture](architecture.md)
- [UniFi API contract](unifi-api.md)
- [Testing](testing.md)
