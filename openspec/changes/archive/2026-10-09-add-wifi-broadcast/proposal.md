## Why

The `unifi-wifi` capability is a design-only contract: `UnifiWifiBroadcast` has a spec paragraph
but no Go type, client, or reconciler. It is the highest-demand missing resource and the first
child whose strongest reference is **not** the site — it is owned by its `UnifiNetwork` — so it
also exercises the untested "child owned by a stronger reference" branch of the ownership tree
(`docs/crd-conventions.md`, design D4) and the first resource-level `Secret` read.

## What Changes

- **Implement `UnifiWifiBroadcast` end to end** against the frozen Integration v1 spec
  v10.4.57: API type, generated CRD/deepcopy, typed client, reconciler, samples, kustomize/Helm
  CRD, RBAC, and CLI `snapshot wifi`.
- **Full-fidelity `spec`.** Model the complete `Wifi broadcast create or update` union: the
  `STANDARD`/`IOT_OPTIMIZED` discriminator with both required and optional variant fields, every
  optional nested tree (basic data rates, blackout schedule, client filtering, mDNS proxy,
  multicast filtering, DNS assistance, handoff suggestions, hotspot, DTIM override, MLO), and
  all seven `securityConfiguration` variants. No field is silently dropped.
- **Typed references replace every opaque ID** per the CRD golden rule:
  - `spec.networkRef` (`UnifiNetwork`) replaces the upstream `network` `SPECIFIC.networkId`; the
    `NATIVE` variant is not exposed.
  - `spec.deviceTags[]` (name-based `DeviceTagSelector`) replaces `broadcastingDeviceFilter`
    `DEVICE_TAGS.deviceTagIds`; the raw `DEVICES.deviceIds` variant is not exposed.
  - Personal passphrases (`passphrase`, `presharedKeys[].passphrase`) are `SecretKeySelector`
    references, never inline.
  - Enterprise `radiusConfiguration.profileId` is a `radiusProfileRef` (`UnifiRadiusProfile`).
    `presharedKeys[].network` and mDNS `bridgingNetworkIds` are `UnifiNetwork` references.
- **First network-owned child (structural).** The broadcast references its network, is owned by
  it, and resolves the site transitively (`networkRef → UnifiNetwork.spec.siteRef → UnifiSite`).
  The `UnifiNetwork` reconciler gains a child-drain (generalizing the `UnifiSite` drain) so a
  network is not removed until its broadcasts have cleaned up upstream.
- **Fail closed on dependencies the operator cannot satisfy yet.** Enterprise security sets
  `Ready=False` until `UnifiRadiusProfile` exists; an unknown device tag fails closed; a
  non-`USER_DEFINED` upstream broadcast is adopted read-only.
- **CLI parity.** `snapshot wifi` emits one `UnifiWifiBroadcast` per upstream broadcast, with a
  placeholder `secretRef` plus an annotation and warning for passphrases (which cannot be
  recovered from the console).

## Capabilities

### New Capabilities

_None._

### Modified Capabilities

- `unifi-wifi`: replace the design-only requirements with the implemented `UnifiWifiBroadcast`
  contract — the `STANDARD`/`IOT_OPTIMIZED` union and its variant fields, the typed `networkRef`
  and device-tag scope, the seven security variants with `Secret`-referenced passphrases and a
  `radiusProfileRef`, and observable status/cleanup.
- `unifi-network`: add the requirement that a `UnifiNetwork` drains its owned
  `UnifiWifiBroadcast` children before its finalizer is removed, so the site's recursive drain
  terminates correctly.

## Impact

- **Operator**: new `operator/api/v1alpha1/unifiwifibroadcast_types.go` (plus the ~30 nested
  union structs), `zz_generated.deepcopy.go`, `operator/config/crd/`; new
  `operator/internal/unifi/wifi_broadcasts.go`; new
  `operator/internal/controller/unifiwifibroadcast_controller.go`; drain change in
  `operator/internal/controller/unifinetwork_controller.go`; wiring in `operator/cmd/main.go`;
  `operator/config/samples/` and RBAC.
- **CLI**: `cli/internal/unifi/wifi_broadcasts.go`, a `snapshot wifi` emitter, command wiring,
  and `testdata/` fixtures/golden files.
- **Docs**: `docs/unifi-api.md` (WiFi broadcast endpoint rows), `docs/crd-conventions.md` (the
  passphrase-from-`Secret` and `radiusProfileRef` reference notes), `docs/architecture.md`.
- **Deployment**: kustomize (`operator/config/`) and Helm (`charts/`) gain the
  `UnifiWifiBroadcast` CRD and RBAC.
- **CRD contract**: new namespaced kind `UnifiWifiBroadcast` with a status subresource and a
  finalizer; `spec.networkRef` is immutable and `spec` carries no opaque identifier; `status`
  carries `wifiBroadcastID` plus the resolved `networkID`/`siteID` and
  `conditions`/`observedGeneration`/`summary`, and is the source for upstream teardown.
  `UnifiNetwork` gains no `spec`/`status` field, only the child-drain behavior.
- **Version gate**: a new `CapabilityWifi` (Official API minimum, `>= 10.1.78`) gates the broadcast
  reconcile; the site drain is generalized into a shared helper the network reuses.

### Non-Goals

- The raw `DEVICES` (`deviceIds`) broadcast filter and the `NATIVE` network reference — both
  require exposing opaque UUIDs.
- Implementing `UnifiRadiusProfile`; enterprise security is modeled but fails closed until that
  kind lands in a later change.
- Live-console confirmation of the `X-API-Key` header casing and the exact target app version
  (both remain open items from the Integration v1 design).
- WAN interface lookup and any change to the `UnifiNetwork` IPv6 prefix-delegation gap.
