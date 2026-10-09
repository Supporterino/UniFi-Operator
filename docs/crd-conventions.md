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

- **Reference another CR by Kubernetes identity.** Use a `name` field or a typed selector
  (`metav1.LabelSelector` or a purpose-built selector struct). The controller resolves the
  reference to an upstream ID at reconcile time. Every reference is **same-namespace and carries
  `name` only** — see [the reference and ownership model](#the-reference-and-ownership-model).
- **Put correlation IDs in `status`.** If you need to record the upstream `_id` a CR maps to,
  expose it in `status` (for example `status.controllerID`) so tooling can correlate, without
  making it part of the contract users author.

```go
// Good: spec references by name, in the same namespace.
type VLANSpec struct {
    NetworkRef NetworkReference `json:"networkRef"`
}

type NetworkReference struct {
    Name string `json:"name"`
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

## The reference and ownership model

Every CR in the workspace is **namespaced**, and every reference resolves in the **same
namespace**. References carry a `name` only — there is deliberately no `namespace` field,
because a cross-namespace reference cannot be an `ownerReference`, and same-namespace resolution
keeps RBAC to namespaced `Role`s.

### Reference chain

Children do not repeat connection details. Each child points at its site, and the site points at
the controller:

```text
child.spec.siteRef ──▶ UnifiSite.spec.controllerRef ──▶ UnifiController
     (name)                    (name)                       url, secretRef, TLS
```

`UnifiController` is the only kind that holds connection details (`spec.secretRef` API key,
`spec.insecureSkipVerify`), so secrets never leak into children. A `UnifiSite` adopts an existing
upstream site by name (`spec.internalReference`) and records the upstream UUID in `status` only.

### Ownership follows the strongest reference

Ownership is a tree rooted at the site, and each object is owned by the CR its **strongest**
reference points to:

- A child whose strongest reference is `spec.siteRef` is owned by its `UnifiSite`.
- A child with a stronger parent reference is owned by that parent instead. `UnifiWifiBroadcast`
  references its `UnifiNetwork`, so it is owned by the network, not the site.
- Each reconciler sets its own `ownerReference` to its **immediate parent** with
  `controllerutil.SetControllerReference`; a parent reconciler never reaches into child kinds.

The `UnifiSite` carries a **finalizer that drains owned children before removal**. A child's
finalizer needs `spec.siteRef` to reach its controller and delete upstream state, so the site
must stay resolvable until every owned child has finished. The site reconciler initiates
deletion of its direct `ownerReference` children (garbage collection only starts after an owner
is actually gone), removes the finalizer only once no owned children remain, and never deletes
the upstream site.

### One writer per fact: membership ownership

A fact has exactly one owning CR. Firewall-zone membership is the canonical example:

- `UnifiFirewallZone.spec` owns membership — it lists member `UnifiNetwork` references and its
  reconciler writes the upstream zone's membership.
- `UnifiNetwork.spec` exposes **no** `zoneRef`/`zoneId`; the resolved zone is reported in
  `UnifiNetwork.status` only.

A single writer avoids an unresolvable reference cycle (and racy reconciles) between the two
kinds. The same rule applies to ordered collections: a dedicated ordering CR is the single writer
of order rather than a per-object priority field.

CRD markers cannot express cross-object uniqueness, so the single-writer rule is enforced at
runtime. `UnifiFirewallZone` claims networks through `spec.networkRefs`, and the controller keys
the claim on `(siteRef, networkRef)` — not the namespace alone, because one namespace may hold
several sites and a name-only key would false-positive across them. When more than one zone in
the same site claims the same network, the controller marks every claimant `Ready=False` (reason
`MembershipConflict`) and writes no membership upstream until a single writer remains. Zone-name
uniqueness is checked the same way, keyed on `(siteRef, spec.name)`, because an upstream zone is
identified by name.

### Typed selectors for non-CR references

Some upstream objects have no CR and never will (or must not be overwritten). References to them
use a **typed selector struct** that the controller resolves, never a raw opaque UUID in `spec`:

| Target | Selector shape |
|--------|----------------|
| Device (e.g. a switch-managed network's `deviceId`) | `DeviceTagSelector` (name-based, resolved from the read-only device-tags list) |
| DPI application / category (firewall policy filters) | typed application/category selector |
| Country (firewall policy region filters) | typed country selector |
| Built-in/system firewall zones (not configurable, e.g. `Internal`/`External`) | typed built-in-zone selector (enum/name) |

The concrete selector fields land with each kind as it is implemented; the contract is fixed
here: **typed selector, no opaque ID**. This preserves Rule 1 for references that cannot use
Kubernetes identity.

#### Device-tag selector

Device tags are the reference case for a selector over an object that has no CR. The frozen
v10.4.57 surface exposes only `GET /v1/sites/{siteId}/device-tags` — no create, update, or delete
— so there is no `UnifiDeviceTag` Custom Resource and never will be. A consumer (for example
`UnifiNetwork.spec.switch`) selects devices with a name-based `DeviceTagSelector`:

```go
type DeviceTagSelector struct {
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`
}
```

This mirrors the `WANSelector` idiom: a `name`-only struct the controller resolves at reconcile
time through the read-only list, keeping the opaque upstream UUID out of `spec`. The controller
resolves the tag and reports the outcome in the consumer's `status`; an unknown tag, or a tag that
does not resolve to exactly one device, fails closed with `Ready=False` and no upstream mutation.
Never cache the resolved UUID in `spec` — resolve by name each reconcile so a rename or deletion
upstream is observed rather than trusted.

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
Ownership follows the strongest reference and the site drains its children — see
[the reference and ownership model](#the-reference-and-ownership-model).

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
- **Pre-release carve-out.** While `v1alpha1` is unreleased — no deployed population, no
  compatibility obligation — an in-place breaking change MAY be made without a new version or a
  conversion webhook. `UnifiNetwork.spec.switch` dropping `deviceTagRef` for the name-based
  `deviceTag` selector is such a change; it is accepted in place, and the samples, CLI, and docs
  are updated in the same change so the tree stays green. Once `v1alpha1` is released, the
  no-in-place-change rule applies again without exception.

## Rule 6 — CLI-emitted CRs obey the same rules

The CLI snapshot command emits CRs that a user applies. Those CRs must be valid against the
CRDs and must not carry upstream `_id`s in `spec`. If a controller object cannot be represented
losslessly, the CLI records the gap in an annotation and warns rather than inventing a
spec field.

## Checklist for a new kind

- [ ] API type in `operator/api/v1alpha1/` with `Spec`, `Status`, and type/object root markers.
- [ ] Status has `conditions`, `observedGeneration`, and a summary; `+kubebuilder:subresource:status`.
- [ ] Printer columns for readiness and age.
- [ ] No opaque IDs in `spec`; references by `name` (same namespace, no `namespace` field) or a
      typed selector for non-CR targets.
- [ ] `ownerReference` set to the immediate parent (the strongest reference) via
      `controllerutil.SetControllerReference`.
- [ ] Finalizer added/removed symmetrically if deletion has upstream effects.
- [ ] `make manifests generate` run; `config/crd/` and deepcopy updated with no diff.
- [ ] Reconciler updates status on every terminal path.

## Related

- [Architecture](architecture.md)
- [UniFi API contract](unifi-api.md)
- [Testing](testing.md)
