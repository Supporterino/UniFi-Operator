# Design

## Context

See `proposal.md` — Why. Constraints that shape the approach:

- The CRD type is `operator/api/v1alpha1/unifiwifibroadcast_types.go`; `spec.networkRef` is a
  required `CoreRef` and the request body is built in
  `operator/internal/controller/unifiwifibroadcast_controller.go` (`buildWifiBroadcastRequest`,
  line ~763). The upstream reference is written as `SPECIFIC` (constant
  `wifiBroadcastNetworkReferenceSpecific`).
- The broadcast reconciler resolves dependencies transitively: `spec.networkRef -> UnifiNetwork ->
  UnifiNetwork.spec.siteRef -> UnifiSite -> status.siteID` (`resolveDependencies`, line ~137), and
  `ensureOwnership` (line ~196) sets the ownerReference to the parent `UnifiNetwork`.
- The upstream `Wifi network reference` union is `{type: SPECIFIC, networkId}` or `{type: NATIVE}`
  (no `networkId`). A live probe showed a STANDARD broadcast with `network: {"type":"NATIVE"}`.
- `bandSteeringEnabled` is returned by the console as a **top-level** broadcast field. The operator
  request struct already carries it (`WifiBroadcastRequest.BandSteeringEnabled`,
  `operator/internal/unifi/wifi_broadcasts.go` line ~296) and the drift comparison already checks it
  (`broadcastMatchesRequest` -> `optionalBoolMatches`), but the CRD models it only inside
  `StandardWifiOptions`. The CLI decodes it as a raw field and lists it in
  `UnrepresentedSettings` (`cli/internal/unifi/wifi_broadcasts.go`), so the snapshot never emits it.
- The snapshot projects broadcasts in `cli/internal/snapshot/snapshot.go`
  (`buildWifiBroadcastSpec`, `broadcastNetworkName`); a `NATIVE` reference currently returns
  `ok=false` and the broadcast is skipped.
- Tests are hermetic (`docs/testing.md`): envtest for CRDs, `httptest` for clients, recorded
  fixtures + goldens for the CLI.

## Goals / Non-Goals

**Goals:**

- Model the upstream `NATIVE` binding without an opaque identifier, and resolve its site from a
  broadcast-level `siteRef`.
- Make band steering a first-class, common broadcast setting on both variants.
- Keep the snapshot faithful: emit `NATIVE`-bound broadcasts and band steering instead of
  skipping/annotating them.

**Non-Goals:**

- The raw `DEVICES` device filter (still not exposed).
- A `NATIVE` binding on `UnifiNetwork`; only the broadcast reference gains it.
- A CRD version bump; stays `v1alpha1` (the project is pre-1.0).

## Decisions

### D1: Model the binding additively — optional `networkRef`, `nativeNetwork`, `siteRef`

Add to `UnifiWifiBroadcastSpec`:
- `networkRef *CoreRef` (becomes optional),
- `nativeNetwork *bool` (optional),
- `siteRef *CoreRef` (optional; the site source for a `NATIVE` binding).

CEL (`+kubebuilder:validation:XValidation`) enforces exactly one of `networkRef`/`nativeNetwork`,
and `siteRef` present iff `nativeNetwork` is true. `networkRef` keeps its immutability CEL rule.

- **Rationale:** additive and backward-compatible for existing `SPECIFIC` objects; no upstream
  identifier is introduced (the `NATIVE` variant carries none).
- **Alternatives:** (a) replace `networkRef` with a discriminated `spec.network` union — rejected as
  a larger breaking change for the same behavior; (b) snapshot-only — rejected, it leaves the CRD
  unable to adopt or author the binding.

### D2: Site resolution branches on the binding

`resolveDependencies` returns `(network, site, …)`. For a `NATIVE` broadcast there is no network:
resolve the `UnifiSite` from `spec.siteRef` directly (and return a nil network). For `SPECIFIC`,
keep the existing transitive resolution unchanged.

- **Rationale:** the site is required to address the upstream endpoint; the broadcast carries no
  site today only because the network supplied it.
- **Alternatives:** auto-select the console's single site — rejected; it hides multi-site consoles
  and adds an implicit dependency.

### D3: Ownership and teardown for `NATIVE`

`ensureOwnership` sets the ownerReference only for a `SPECIFIC` broadcast (its parent
`UnifiNetwork`). A `NATIVE` broadcast has no parent network, so it gets no ownerReference; teardown
relies on the existing finalizer plus `status.siteID`/`status.wifiBroadcastID` (the reconciler
already deletes from `status` when the live reference chain is broken, see `resolveCleanupClient`).

- **Rationale:** reuses the established status-recorded teardown path; avoids an ownerReference to
  the site, which would couple broadcast lifecycle to site deletion semantics that do not own it.

### D4: Promote `bandSteeringEnabled` to a common field (**BREAKING**)

Add `spec.bandSteeringEnabled *bool` and remove `StandardWifiOptions.BandSteeringEnabled`.

- **Rationale:** the console returns it top-level and both variants can carry it, so it is a common
  broadcast field; the operator request struct already treats it as such a field.
- **Alternatives:** (a) keep it `STANDARD`-only and add `iotOptimized.bandSteeringEnabled` —
  rejected, duplicates the setting and diverges from the flat upstream shape; (b) leave it
  `STANDARD`-only — rejected, it does not match the observed response.
- **Migration:** see Migration Plan — existing objects must move the field.

### D5: Request build and drift comparison

`buildWifiBroadcastRequest` writes `Network: {type: NATIVE}` when `spec.nativeNetwork` is set, else
`{type: SPECIFIC, networkId}` as today, and sets `BandSteeringEnabled` from the common field for
every type. `broadcastMatchesRequest` needs no new logic: `wifiNetworkReferenceMatches` already
compares `Type` and `NetworkID` (a `NATIVE` reference has an empty `NetworkID`), and
`optionalBoolMatches` already compares band steering.

### D6: Snapshot projection

`broadcastNetworkName` returns the binding: for `SPECIFIC` it maps the network UUID to an emitted
name (as today); for `NATIVE` it signals a native binding. `buildWifiBroadcastSpec` then sets
`nativeNetwork: true` and `siteRef` to the emitted `UnifiSite` name (the snapshot always emits the
site), and no longer skips. `bandSteeringEnabled` is projected into the common field and removed
from `UnrepresentedSettings` (so it is no longer annotated).

- **Fixture/goldens:** recapture `cli/testdata/wifi-*.json` to include a `NATIVE`-bound broadcast
  and `bandSteeringEnabled` on a `STANDARD` broadcast, then regenerate goldens deliberately.

### D7: Docs

`docs/unifi-api.md`: replace the "only the SPECIFIC binding is exposed" paragraph with the modeled
`NATIVE` binding (broadcast-level `siteRef`), and note band steering is a common field.

## Risks / Trade-offs

- **The console may reject `bandSteeringEnabled` on an `IOT_OPTIMIZED` write** → the field is
  common in the CRD per D4; if upstream rejects it, the reconciler surfaces a terminal spec error
  (fail-closed) rather than silently dropping it. Confirm with a live write when one is available;
  if it is variant-specific after all, follow up with per-variant modeling.
- **Breaking field move rejects existing objects that set `standard.bandSteeringEnabled`** →
  documented **BREAKING**; migration moves the value to `spec.bandSteeringEnabled`.
- **A `NATIVE` broadcast has no ownerReference** → teardown uses `status.siteID`; the existing
  "deletion survives a broken reference" path already covers it, and a controller test pins it.
- **`siteRef` becomes required for `NATIVE`** → CEL rejects a `NATIVE` broadcast without it, so a
  malformed object fails at the API server rather than mid-reconcile.

## Migration Plan

1. Land the CRD, client, controller, and CLI changes; run `make -C operator manifests generate` and
   confirm no diff.
2. Migrate existing `UnifiWifiBroadcast` objects: move `spec.standard.bandSteeringEnabled` to
   `spec.bandSteeringEnabled`. No managed `NATIVE`-bound object exists yet (the snapshot skipped
   them), so no `nativeNetwork` migration is needed; a fresh snapshot emits them.
3. Rollback: reverting the CRD markers restores prior acceptance; no CRD version bump is involved.

## Open Questions

- None blocking. Whether `bandSteeringEnabled` is accepted on an `IOT_OPTIMIZED` write is recorded
  as a risk above and can be answered with a live write without changing the specs or the task
  breakdown.
