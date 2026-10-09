## Context

See `proposal.md` for motivation. The state that shapes this design:

- The ownership tree today has one level: `UnifiSite` sets `ownerReference` on every child and
  drains its **direct** children (`operator/internal/controller/unifisite_controller.go:169-197`).
  No implemented kind is owned by another resource, so the design's "child owned by its strongest
  reference" branch (design D4) is unexercised.
- `UnifiNetwork` resolves its site via `spec.siteRef` and its controller via
  `UnifiSite.spec.controllerRef` (`operator/internal/controller/unifinetwork_controller.go:128-235`);
  it does not drain children (`reconcileDelete`, `:312`).
- Only `UnifiController` reads a `Secret`, through an uncached `APIReader`
  (`operator/internal/controller/connection.go:71-95`), so the manager never caches Secrets.
- CRD unions are modeled as an enum discriminator plus variant sub-structs validated with CEL
  (`operator/api/v1alpha1/unifinetwork_types.go:28-29,176-177`).
- The frozen v10.4.57 WiFi broadcast surface is a deep union (broadcast `type`, `securityConfiguration`
  `type`, `network` `type`, `broadcastingDeviceFilter` `type`, plus mDNS/multicast/DNS/hotspot/blackout
  sub-unions). The list endpoint is a thin overview; variant configuration is only on the detail endpoint.
- `UnifiRadiusProfile` is design-only; its spec exists but no Go type does.

## Goals / Non-Goals

**Goals:**

- Implement `UnifiWifiBroadcast` end to end against spec v10.4.57 with a full-fidelity `spec`.
- Generalize the ownership/drain pattern so a non-site parent drains its own children.
- Establish the resource-level `Secret`-read and non-CR reference patterns the remaining kinds reuse.

**Non-Goals:**

- Implementing `UnifiRadiusProfile`; enterprise security only references it and fails closed.
- Exposing the raw `DEVICES` filter or the `NATIVE` network variant (opaque IDs).
- A conversion webhook or new API version; `v1alpha1` is pre-release (Rule 5 carve-out applies).

## Decisions

### D1 - Full-fidelity union mapped 1:1, with golden-rule substitutions

Model every authored field of the v10.4.57 create/update union. Raw identifiers are replaced by
references; nothing else is dropped.

| Upstream | CR field | Rationale |
|----------|----------|-----------|
| `type` = `STANDARD`/`IOT_OPTIMIZED` | `spec.type` + `StandardWifiOptions`/`IotOptimizedWifiOptions` | D7 pattern from `unifinetwork_types.go` |
| `network` `SPECIFIC.networkId` | `spec.networkRef` (`CoreRef`) | Rule 1; resolved via `UnifiNetwork.status.networkID` |
| `network` `NATIVE` | — not exposed | no authoring need; native is a normal network |
| `broadcastingDeviceFilter` `DEVICE_TAGS.deviceTagIds` | `spec.deviceTags[]` (`DeviceTagSelector`) | Rule 1/D8; resolved via read-only device-tags list |
| `broadcastingDeviceFilter` `DEVICES.deviceIds` | — not exposed | opaque device UUIDs |
| personal `passphrase`, `presharedKeys[].passphrase` | `SecretKeySelector` | `docs/security.md` |
| `presharedKeys[].network`, mDNS `bridgingNetworkIds` | `CoreRef` to `UnifiNetwork` | Rule 1 |
| enterprise `radiusConfiguration.profileId` | `CoreRef` to `UnifiRadiusProfile` | Rule 1 |
| `id`, `metadata.origin` | `status` only | Rule 1/Rule 2 |

- **Why:** the user chose full fidelity; the golden rule is non-negotiable, so only identifier
  fields are substituted, never capability.
- **Alternatives:** reduced slice (rejected by scope decision); raw-UUID escape hatch (rejected —
  violates Rule 1).
- **`networkRef` is immutable.** Because it determines both the `ownerReference` and the upstream
  `SPECIFIC.networkId`, `spec.networkRef` carries a CEL `self == oldSelf` immutability rule. Moving
  a broadcast to another network is delete-and-recreate; re-pointing in place would strand the old
  network's upstream SSID and churn ownership.
- **Note:** `basicDataRateKbpsByFrequencyGHz` and `dtimPeriodByFrequencyGHzOverride` use JSON keys
  `2.4`/`5`/`6`; Go field names (`GHz2_4`, `GHz5`, `GHz6`) carry the exact `json` tags.

### D2 - Transitive site resolution and recursive drain

The broadcast carries no `siteRef`. The reconciler resolves, in order:
`spec.networkRef` → `UnifiNetwork` → `UnifiNetwork.spec.siteRef` → `UnifiSite` →
`UnifiSite.status.siteID`, then the controller through `UnifiSite.spec.controllerRef`. This mirrors
`unifinetwork_controller.go:128-235` one hop deeper. The resolved upstream broadcast UUID and the
transitive `siteID`/`networkID` are persisted in `status`, so teardown does not depend on the
reference chain still resolving.

Deletion ordering is recursive. Modify `UnifiNetwork.reconcileDelete`
(`unifinetwork_controller.go:312`) to list owned `UnifiWifiBroadcast` children
(`metav1.IsControlledBy(child, network)`), delete any not already deleting, and keep the network
finalizer until none remain. The network's upstream network is deleted after its children are gone.

- **Shared drain helper.** The site drain (`unifisite_controller.go:149-210`) is generalized into a
  reusable "list owned children of kind K, initiate deletion, count remaining" helper that both the
  site and network reconcilers call, rather than a third hand-written copy. The site helper keeps
  its two kinds (networks, firewall zones); the network adds `UnifiWifiBroadcast`.
- **Status-persisted cleanup.** The broadcast's `reconcileDelete` resolves the console from the
  `status`-recorded `siteID` (falling back to the live reference chain only when `status` is empty),
  mirroring `deleteUpstream` (`unifinetwork_controller.go:330-367`). A dangling `networkRef` on an
  already-provisioned broadcast therefore still cleans up; an unresolvable chain with a
  `status.wifiBroadcastID` blocks the finalizer rather than silently orphaning upstream state.
- **Why:** if the network were removed first, a broadcast's finalizer could not resolve the site
  and would strand upstream state; the recursive drain keeps the chain resolvable. Persisting the
  ids keeps cleanup correct even if the reference chain is edited or breaks.
- **Alternatives:** give the broadcast its own `siteRef` (rejected — redundant references, deviates
  from D4); rely on Kubernetes GC (rejected — GC only starts after the owner is gone, which is the
  deadlock the site finalizer already exists to avoid, `docs/crd-conventions.md:86-91`).
- **Ownership:** the broadcast reconciler sets `controllerutil.SetControllerReference(network, ...)`
  to the network, not the site.

### D3 - Secrets are same-namespace `Secret` references, read uncached

`passphrase` and each `presharedKeys[].passphrase` are `corev1.SecretKeySelector` values resolved
with the existing `resolveAPIKey`-style path (`connection.go:71-95`), reading through the
uncached `APIReader` so no cluster-wide Secret informer starts. A missing Secret or key is a
terminal `Ready=False` (`SecretNotFound`/`SecretKeyMissing`); the value is never logged or written
to `status`.

- **Why:** `docs/security.md` requires reference-not-inline and fail-closed.
- **Alternatives:** inline `passphrase` (rejected — leaks into etcd backups).

### D4 - Enterprise security references a RADIUS profile and fails closed

Enterprise security is fully modeled, but its `radiusConfiguration.profileId` becomes
`radiusProfileRef` (`CoreRef`). Until `UnifiRadiusProfile` is implemented, the reconciler treats an
unresolvable/absent profile as a terminal `Ready=False` and performs **no** upstream mutation —
the same fail-closed posture `UnifiNetwork` uses for `SWITCH` below the Official API minimum and
for an unresolved device tag (`unifinetwork_controller.go:208-251`).

- **Why:** preserves the contract’s completeness now without shipping a broken write path.
- **Alternatives:** omit enterprise variants (rejected by scope decision); expose the raw profile
  UUID (rejected — Rule 1).

### D5 - Device broadcast scope is name-based

`spec.deviceTags[]` names read-only device tags; the reconciler resolves them with
`client.ListDeviceTags` (already in `operator/internal/unifi/device_tags.go`) and requires each to
resolve to exactly one device, matching `resolveSwitchDevice` (`unifinetwork_controller.go:514-527`).
An unknown or ambiguous tag fails closed. Omitting the list sends no filter.

### D6 - Nested unions use discriminator + variant structs + CEL

Each union (`type`, `securityConfiguration.type`, `mdns.mode`, `mdns policy.action`,
`multicast.action`, `dns.mode`, `hotspot.type`, `blackout day.type`, `radius nasId.type`,
`macAuthentication.macAddressFormat` is a plain enum) is an enum field plus optional variant
sub-structs, validated with `+kubebuilder:validation:XValidation` `has()` rules exactly like
`unifinetwork_types.go:28-29,176-177`. Variant structs are kept small; unions are shallow where the
upstream is shallow (CAPTIVE_PORTAL/PASSPORT carry no extra fields).

### D7 - Idempotency by detail comparison

The reconciler lists broadcasts (`client.ListWifiBroadcasts`) to find a candidate by `status`
ID then name, fetches the detail (`client.GetWifiBroadcast`) — the list is an overview — builds the
request, and compares authored fields before issuing a PUT, following `networkMatchesRequest`
(`unifinetwork_controller.go:655-683`). Observed-only fields (`id`, `metadata`) are ignored.

### D8 - CLI emits a passphrase placeholder

`snapshot wifi` cannot recover a passphrase from the console. It emits a `UnifiWifiBroadcast` with a
placeholder `secretRef` and the `AnnotationUnresolvedWifiSecret` annotation plus a warning, following
the `UnifiController` template precedent (`cli/internal/snapshot/snapshot.go:198-224`). Known
settings (type, network, security type/mode without the passphrase, device scope, frequency bands)
are emitted faithfully. The placeholder is written into the personal `passphrase` /
`presharedKeys[].passphrase` `SecretKeySelector` (there is no top-level `secretRef` on the
broadcast), tagged with a new `AnnotationUnresolvedWifiSecret` constant alongside the existing
`AnnotationUnresolvedDeviceTag`/`AnnotationUnresolvedNetworkRef` annotations
(`cli/internal/snapshot/snapshot.go:36-41`).

### D9 - Version gating by a new `CapabilityWifi`

The broadcast reconcile gates on a new `CapabilityWifi` capability
(`operator/internal/controller/versions.go:32-49`), pinned to the Official API minimum
(`>= 10.1.78`), and a `WiFi broadcasts` row is added to the capability table in
`docs/unifi-api.md:36-40`. Below the minimum the reconciler fails closed with `VersionUnsupported`
and performs no upstream mutation, exactly as `unifinetwork_controller.go:204` does for networks.

- **Why:** broadcast CRUD is part of the Integration v1 surface, so its floor is the overall
  Official API floor; a distinct capability keeps the gate explicit and matches the existing
  per-surface capability idiom.
- **Alternatives:** reuse `CapabilityOfficialAPI` (rejected — conflates surfaces and loses the
  named gate the spec references); pin to `10.4.57` (rejected — the spec version is not an app
  minimum and no other capability uses it).

## Risks / Trade-offs

- **~30 nested types drive CRD and comparison-test volume** → keep variant structs shallow and
  generate comparison helpers per union; the `make manifests generate` no-diff gate catches schema drift.
- **Full-fidelity PUT semantics**: a field the CR omits may be reset by the console on PUT (the same
  limitation the network design accepted for `dhcpConfiguration`) → documented in `docs/unifi-api.md`;
  the CR models the full surface so a user who wants a setting can author it.
- **First non-site drain**: a bug here strands upstream broadcasts → dedicated tests for the
  recursive drain (site→network→broadcast), mirroring the site drain tests.
- **Enterprise fails closed** until `UnifiRadiusProfile` lands → the reason is explicit and the
  behavior is covered by a test.
- **`metadata.origin`**: non-`USER_DEFINED` broadcasts are adopted read-only → fail-closed test.

## Migration Plan

No live migration: `v1alpha1` is pre-release and there is no deployed `UnifiWifiBroadcast`
population. Sequence: API types + `make manifests generate` → client → reconciler + network drain
refactor → CLI emitter → kustomize/Helm + docs → gates. Rollback reverts the change; no upstream
state exists until a CR is applied.

## Resolved Questions

- **`metadata.origin` values.** The exact live non-`USER_DEFINED` values a console returns
  (beyond `USER_DEFINED` and `DERIVED`) are not enumerated. The reconciler fails closed on **every**
  non-`USER_DEFINED` origin and never overwrites it, consistent with `UnifiNetwork`/`UnifiFirewallZone`.
  `DERIVED` is therefore read-only too; adopting derived broadcasts is explicitly out of scope, so
  the unknown value set only affects the failure message, not behavior.
- **`networkRef` immutability / cleanup chain.** `spec.networkRef` is immutable (D1), and the
  broadcast persists its resolved upstream ids in `status` so teardown does not depend on the live
  reference chain (D2).
