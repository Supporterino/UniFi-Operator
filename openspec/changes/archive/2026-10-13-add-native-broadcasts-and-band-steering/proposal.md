# Proposal

## Why

A read-only probe of a live console (applicationVersion 10.6.106) exposed two fidelity gaps in
`UnifiWifiBroadcast` that the current contract cannot close:

- A STANDARD broadcast bound to the upstream `NATIVE` network variant (the `network` reference
  carries no `networkId`) cannot be represented. `spec.networkRef` is required and the operator
  always writes the `SPECIFIC` variant, so `snapshot wifi` skips such a broadcast and the operator
  can never author or adopt it.
- Band steering (`bandSteeringEnabled`) is a **top-level** broadcast field upstream, but the CRD
  models it only inside the `STANDARD` variant. The CLI therefore reports it as unrepresented and
  annotates the emitted CR instead of emitting the setting.

Both make a real deployment non-adoptable or lossy, so they should be closed together.

## What Changes

- **Model the `NATIVE` network binding.** Make `UnifiWifiBroadcast.spec.networkRef` optional, add an
  optional `spec.nativeNetwork` (bool) and an optional `spec.siteRef`, and add a CEL rule requiring
  exactly one of `networkRef`/`nativeNetwork`. A `NATIVE`-bound broadcast resolves its site from
  `spec.siteRef` (it has no network to resolve transitively); a `SPECIFIC`-bound broadcast keeps
  today's transitive `networkRef -> UnifiNetwork -> siteRef` resolution and MUST NOT set `siteRef`.
  The operator writes `network: {type: NATIVE}` for the NATIVE variant. Additive for existing
  `SPECIFIC` objects; the `NATIVE` variant carries no upstream identifier.
- **Promote band steering to a common broadcast field.** **BREAKING**: move `bandSteeringEnabled`
  from `spec.standard.bandSteeringEnabled` to a top-level optional `spec.bandSteeringEnabled`,
  usable by both `STANDARD` and `IOT_OPTIMIZED` broadcasts. Existing objects that set the STANDARD
  field must move it; the API server rejects the old path after the schema change.
- **Snapshot emits instead of skipping.** `snapshot wifi` emits a `NATIVE`-bound broadcast as
  `nativeNetwork: true` with a `siteRef` to the emitted `UnifiSite` (no network resolution, no
  warning), and projects `bandSteeringEnabled` into the common field rather than annotating it as
  unrepresented.
- **Contract docs.** Record that the `NATIVE` variant is now modeled and that band steering is a
  common field in `docs/unifi-api.md`.

## Capabilities

### New Capabilities

- None.

### Modified Capabilities

- `unifi-wifi`: the broadcast network reference models the `NATIVE` variant with a broadcast-level
  `siteRef` (the "NATIVE is not exposed" rule is replaced); band steering becomes a common broadcast
  field rather than a `STANDARD`-only one.
- `unifi-snapshot`: a `NATIVE`-bound broadcast is emitted (no longer skipped), and band steering is
  represented in `spec` (no longer annotated as unrepresented).

## Impact

- **CRD kinds:** `UnifiWifiBroadcast` — `spec.networkRef` becomes optional; new `spec.nativeNetwork`
  and `spec.siteRef`; `spec.bandSteeringEnabled` added and `spec.standard.bandSteeringEnabled`
  removed. No new kind; no `status` change; stays `v1alpha1`.
- **Controllers:** `unifiwifibroadcast_controller` — dependency/site resolution, request build, and
  drift comparison.
- **Clients:** `operator/internal/unifi` and `cli/internal/unifi` `wifi_broadcasts.go` (network
  reference; band steering is already modeled in the operator request struct).
- **CLI:** `cli/internal/snapshot` (`buildWifiBroadcastSpec`, `broadcastNetworkName`).
- **Docs:** `docs/unifi-api.md`.

## Non-Goals

- No support for the raw `DEVICES` (`deviceIds`) broadcast filter.
- No change to how a `SPECIFIC`-bound broadcast resolves its site (still transitive through the
  network).
- No `NATIVE` support on `UnifiNetwork` itself; only the broadcast's network reference gains it.
- No CRD version bump or hub/spoke conversion (stays `v1alpha1`).
