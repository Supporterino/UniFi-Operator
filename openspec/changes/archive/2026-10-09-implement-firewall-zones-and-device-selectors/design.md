## Context

See `proposal.md` for motivation. Current state that shapes the approach:

- The Integration v1 slice (`UnifiController`, `UnifiSite`, `UnifiNetwork`) is implemented. The
  reference and ownership model is fixed in
  `openspec/changes/archive/2026-10-09-design-crds-for-unifi-integration-v1/design.md`
  (D2–D5, D8): `child.spec.siteRef -> UnifiSite.spec.controllerRef -> UnifiController`, all
  same-namespace, ownership via `controllerutil.SetControllerReference`, no opaque IDs in `spec`.
- `UnifiFirewallZone` was designed but not built (D5, D10). `UnifiNetwork.status.zoneID` exists
  (`operator/api/v1alpha1/unifinetwork_types.go:274`) but nothing writes it; the network controller
  already reads the upstream `zoneId` (`operator/internal/controller/unifinetwork_controller.go:261`),
  so implementing the zone activates it. The network must be re-triggered after a membership write
  to refresh that status, so the network controller gains a `UnifiFirewallZone` watch (D2).
- `UnifiNetwork` `management: SWITCH` fails closed at
  `operator/internal/controller/unifinetwork_controller.go:206` with reason
  `SwitchManagedUnsupported` (`conditions.go:63`), because
  `UnifiNetworkSpec.Switch.DeviceTagRef` (`unifinetwork_types.go:126`) points at an unimplemented
  `UnifiDeviceTag`.
- The frozen v10.4.57 OpenAPI exposes firewall zones with full CRUD (create/update/delete limited to
  custom zones) and device tags with **list only** (`GET /v1/sites/{siteId}/device-tags`). The
  `Device tag` shape is `{id, name, metadata, deviceIds[]}`; there is no tag-assignment endpoint.
- Capability gating exists in `operator/internal/controller/versions.go`; `CapabilityFirewallDNS`
  (min 10.1.84) is defined at `versions.go:38` but not yet consumed.

## Goals / Non-Goals

**Goals:**

- Implement `UnifiFirewallZone` as the single writer of upstream zone network membership, and make
  `UnifiNetwork.status.zoneID` populated as a consequence.
- Replace the phantom `UnifiDeviceTag` reference with a read-only, name-based device-tag selector
  that reuses the `WANSelector` idiom, and unblock `management: SWITCH`.
- Keep every operation idempotent, fail-closed, and free of opaque identifiers in `spec`.

**Non-Goals:**

- Firewall policies and ordering, ACL rules/ordering, DNS, traffic matching, switching, VPN, RADIUS
  (contracts only).
- Any device-tag write path (create/update/delete or assignment) and any device-tag Custom
  Resource.
- Verifying the resolved device's hardware type (for example that a `SWITCH` binding resolves to a
  switch); that needs a devices lookup not in the v10.4.57 contract. The binding requires exactly
  one device and leaves type correctness to the console.
- Resolving the upstream `configurable` notion for system zones beyond fail-closed read-only.

## Decisions

### D1 - `UnifiFirewallZone` spec/status shape

```go
// operator/api/v1alpha1/unififirewallzone_types.go
type UnifiFirewallZoneSpec struct {
    SiteRef     CoreRef   `json:"siteRef"`      // same-namespace, D2 of the archived design
    Name        string    `json:"name"`         // +kubebuilder:validation:MinLength=1
    NetworkRefs []CoreRef `json:"networkRefs"`  // names of UnifiNetwork CRs; may be empty
}

type UnifiFirewallZoneStatus struct {
    ObservedGeneration int64               `json:"observedGeneration,omitempty"`
    Conditions         []metav1.Condition  `json:"conditions,omitempty"`
    Summary            string              `json:"summary,omitempty"`
    ZoneID             string              `json:"zoneID,omitempty"`  // upstream UUID, status only
    Origin             string              `json:"origin,omitempty"`  // USER_DEFINED | SYSTEM_DEFINED | ...
}
```

The reconciler resolves the upstream zone it manages by `status.zoneID` first and falls back to a
name match, mirroring `resolveExistingNetwork`
(`operator/internal/controller/unifinetwork_controller.go:536`): a `spec.name` rename updates the
same upstream zone in place rather than orphaning it and creating a new one. A `status.zoneID` that
404s is treated as absent so a stale ID cannot block reconciliation.

`networkRefs` are `CoreRef` (name only), matching `CoreRef` in
`operator/api/v1alpha1/reference_types.go:25`. Names resolve to `UnifiNetwork.status.networkID`,
never to a raw UUID in `spec` (golden rule). A spec-level finalizer
(`UnifiFirewallZoneFinalizer`) is added symmetrically, and the reconciler sets the site as
controller owner via `SetControllerReference`, mirroring
`operator/internal/controller/unifinetwork_controller.go:161`.

*Alternative considered:* a typed label selector over `UnifiNetwork`. Rejected — `CoreRef` keeps the
spec explicit and matches the existing reference idiom; label selectors are still unproven in this
codebase.

### D2 - Custom zones are written; system zones are adopted read-only

Upstream `Firewall zone.metadata.origin` distinguishes `USER_DEFINED` from `SYSTEM_DEFINED` (and
`DERIVED`/`ORCHESTRATED`). The controller writes only when the resolved zone is `USER_DEFINED`
(create when absent, update when present, delete on CR deletion). Any other origin is adopted
read-only: the controller records `status.origin` and sets `Ready=False` with a
`SystemObjectReadOnly`-style reason, and never mutates it. It deliberately does **not** persist
`status.zoneID` for a non-`USER_DEFINED` zone: `resolveExistingZone` resolves `status.zoneID` before
the name match, so recording a still-present system zone's UUID would trap a later `spec.name`
rename on that system zone instead of re-resolving by name (the same reason `UnifiNetworkReconciler`
passes `nil` for upstream IDs on its system-object path). `status.zoneID` is still recorded for
managed `USER_DEFINED` zones.

The OpenAPI metadata description hints that some system zones are "configurable" for attached
networks only, but **no `configurable` flag is modeled** in v10.4.57. We fail closed rather than
guess (see Open Questions).

Because membership is written by the zone and observed by the network, the network controller gains
a `UnifiFirewallZone` watch: a zone create/update/delete in the network's namespace enqueues the
networks in that namespace (reusing `networksInNamespace`), so a membership write re-reconciles the
network and refreshes `UnifiNetwork.status.zoneID`. This is the small network-controller change the
earlier "no network-controller change" framing missed.

*Alternative considered:* allow membership-only PUT on system zones. Rejected for now — it requires
a signal we cannot yet read reliably, and a wrong write is destructive.

### D3 - Membership uniqueness is enforced at runtime

A network must have at most one `UnifiFirewallZone` writer. Each zone reconcile lists the
`UnifiFirewallZone` objects in its namespace, filters to those whose `siteRef` matches its own site,
and builds a `(siteRef, networkRef) -> []zone` map. If any network is claimed by more than one zone
in that site, the controller sets `Ready=False` (reason `MembershipConflict`) on every claimant and
writes no membership. Keying on `(siteRef, networkRef)` — not the namespace alone — is required
because a namespace may hold several sites, and a name-only key would false-positive across them.
This mirrors the ordering-uniqueness approach in archived decision D6 and the marker-first rule in
`operator/AGENTS.md` (CEL cannot express cross-object uniqueness).

A `networkRef` that resolves to a `UnifiNetwork` whose `siteRef` differs from the zone's own site is
a cross-site reference: a network can only be a member of a zone on its own site, so the controller
sets `Ready=False` (reason `CrossSiteReference`) and writes no membership rather than silently
skipping it.

A zone `spec.name` must also be unique within a site: two `UnifiFirewallZone` objects whose
`spec.name` matches the same site both report `Ready=False` (reason `ZoneNameConflict`) and neither
writes, because upstream zone identity is by name and two writers would collide on it.

### D4 - Device-tag selector replaces the `UnifiDeviceTag` CR

Tags are read-only upstream, so a CR adds no authorship. Model a name-based selector next to the
other reference types in `operator/api/v1alpha1/reference_types.go`:

```go
type DeviceTagSelector struct {
    // +kubebuilder:validation:MinLength=1
    Name string `json:"name"`
}
```

`UnifiNetworkSpec.Switch` (`unifinetwork_types.go:111`) changes `DeviceTagRef CoreRef` to
`DeviceTag DeviceTagSelector` (json `deviceTag`). The client gains
`ListDeviceTags(ctx, siteID) ([]DeviceTag, error)` returning plain structs
`{ID, Name string; DeviceIDs []string; Metadata NetworkMetadata}` (no Kubernetes imports, per
`operator/internal/unifi/client.go`). Resolution: find the tag by `Name`; require exactly one
`DeviceIDs` entry; set `SwitchNetworkRequest.DeviceID` (already exists at `client.go:158`).

*Alternative considered:* keep an adopted read-only `UnifiDeviceTag` CR (design option A1).
Rejected — a CR whose only role is a name→deviceIds map adds objects and adoption-drift semantics
for no authorship, and D8's own definition ("no CR and never will") fits the selector better.

### D5 - `management: SWITCH` fail-closed reasons become resolution outcomes

`switchUnsupportedMessage` (`unifinetwork_controller.go:475`) is replaced by concrete outcomes:

- tag not found → `Ready=False`, reason `DeviceTagNotFound`;
- tag resolves to 0 or >1 devices → `Ready=False`, reason `DeviceTagAmbiguous`;
- exactly one device → proceed with the normal create/update path.

The resolved device's hardware type is not verified: requiring a switch-specific device needs a
devices lookup absent from the v10.4.57 contract (Non-Goals), so the binding accepts any
single-device tag and lets the console reject an unusable one.

The check runs after the networks capability gate (`versions.go`) and after an explicit
`CheckCapability(CapabilityOfficialAPI)` gate (min 10.1.78), because the device-tags list is part of
the Official API surface and its minimum sits above the networks minimum (10.0.162). A console
between the two minimums fails with `VersionUnsupported` rather than attempting the tag list.

### D6 - Client surface and endpoint map

Add to `operator/internal/unifi/` (plain structs, page decoding via the existing envelope):

| Endpoint | Operator method | CLI consumer |
|----------|-----------------|--------------|
| `GET /v1/sites/{siteId}/device-tags` | `ListDeviceTags` | `snapshot device-tags` (read-only listing) |
| `GET /v1/sites/{siteId}/firewall/zones` | `ListZones` | `snapshot firewall-zones` |
| `GET /v1/sites/{siteId}/firewall/zones/{id}` | `GetZone` | `snapshot firewall-zones` |
| `POST /v1/sites/{siteId}/firewall/zones` | `CreateZone` | — |
| `PUT /v1/sites/{siteId}/firewall/zones/{id}` | `UpdateZone` | — |
| `DELETE /v1/sites/{siteId}/firewall/zones/{id}` | `DeleteZone` | — |

Rows are added to `docs/unifi-api.md` before either consumer is implemented (contract golden rule).
Firewall zone writes carry `{name, networkIds}` only; `metadata.origin` is response-only and recorded
in `status`.

### D7 - Capability gating

Zone CRUD gates on the existing `CapabilityFirewallDNS` (`versions.go:38`, min 10.1.84); device-tag
listing gates on `CapabilityOfficialAPI` (min 10.1.78). Both already resolve through
`CheckCapability`, so no new capability constant is required.

### D8 - CLI and deployment

The CLI (`cli/internal/snapshot/`, `cli/internal/cmd/`) gains `snapshot firewall-zones`, emitting a
`UnifiFirewallZone` per upstream zone, and a read-only `snapshot device-tags` listing (no CR to
emit; it prints the tag names and device counts to stdout as a discovery aid). `snapshot
firewall-zones` must reference the *emitted* network CR names, so the snapshot command computes the
networks first and passes the upstream-network-id -> emitted-name map (including any collision
suffixes) into the zone emitter; it must not re-derive names from `DNSSafeName`, which can disagree
with the emitted network CRs. Likewise the switch emitter reverse-maps the observed `deviceId` to a
device-tag name through the read-only device-tags list; when the device is in no tag or several
tags it keeps the placeholder-plus-annotation fallback rather than emitting an unresolvable
selector. The zone CRD and its RBAC markers ship through both `operator/config/` (kustomize) and
`charts/`, keeping the two deployment paths consistent (root `AGENTS.md`).

## Risks / Trade-offs

- **Undefined system-zone configurability** → fail closed: system zones are adopted read-only until a
  live console exposes a trustworthy signal.
- **Device-tag rename/deletion drift** → resolve by name each reconcile and fail closed on a miss;
  never cache the UUID in `spec`.
- **Duplicate membership writers** → runtime conflict detection marks all claimants `Ready=False`
  and writes nothing (D3).
- **Membership resolution needs a ready network** → if a referenced `UnifiNetwork` has no
  `status.networkID` yet, the zone reports `Ready=False`/`DependencyNotReady` and retries; it never
  invents an ID.
- **`UnifiNetwork.spec.switch` field rename is BREAKING** → pre-release `v1alpha1`, accepted in
  place; update samples and CLI in the same change so the tree stays green.
- **Cross-namespace leakage** → all references and Secret/API lookups are same-namespace; no
  `namespace` field is exposed (`CoreRef`).

## Migration Plan

No live migration: `v1alpha1` is pre-release with no deployed population. Sequence: firewall-zone
API type + selector type → `make manifests generate` (no diff) → client + `httptest` fixtures →
zone reconciler + network `SWITCH` resolution → `cmd/main.go` wiring → kustomize + Helm CRD/RBAC →
CLI emitters → docs → gates. Rollback reverts the change; no upstream state is created until a CR is
applied, and zone deletion is finalizer-bounded.

The in-place `UnifiNetwork.spec.switch` reshape is a breaking change accepted while `v1alpha1` is
unreleased; `docs/crd-conventions.md` Rule 5 is amended in this change to carve out pre-release
in-place breaking changes (no conversion webhook) rather than contradicting the archived precedent.

## Open Questions

_None._ The system-zone `configurable` signal (not modeled in the v10.4.57 OpenAPI) is settled as a
deferred non-goal: system zones are adopted read-only, which is safe and correct regardless; revisit
if a future spec exposes the flag.
