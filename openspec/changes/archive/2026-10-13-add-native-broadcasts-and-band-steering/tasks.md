# Tasks

## 1. CRD contract (types + manifests)

- [x] 1.1 Make `UnifiWifiBroadcast.spec.networkRef` optional and add optional `spec.nativeNetwork`
  (bool) and `spec.siteRef` (`CoreRef`) in `operator/api/v1alpha1/unifiwifibroadcast_types.go`, with
  CEL requiring exactly one of `networkRef`/`nativeNetwork` and `siteRef` present iff
  `nativeNetwork` is true (and absent for a `SPECIFIC` binding). Keep the `networkRef` immutability
  rule. Verify: `make -C operator manifests generate` produces no diff; envtest rejects both-set,
  neither-set, and `SPECIFIC` + `siteRef`; accepts `NATIVE` + `siteRef`.
- [x] 1.2 Promote band steering: add top-level `spec.bandSteeringEnabled *bool` and remove
  `StandardWifiOptions.BandSteeringEnabled`. Verify: `make -C operator manifests generate` produces
  no diff; envtest accepts `spec.bandSteeringEnabled` for `STANDARD` and `IOT_OPTIMIZED` and rejects
  the removed `spec.standard.bandSteeringEnabled` path.
- [x] 1.3 Add the envtest cases from 1.1–1.2 in `operator/internal/controller/unifiwifibroadcast_types_test.go`
  and confirm `go test ./...` in `operator/` passes.
- [x] 1.4 Make the network binding fully immutable: replace the one-directional rule with
  `has(self.networkRef) == has(oldSelf.networkRef)` and add a field-level `self == oldSelf` rule to
  `spec.siteRef`, so switching a binding variant or re-pointing a `NATIVE` `spec.siteRef` is
  rejected. Defensively, surface a terminal `Ready=False` status if an object still carries a
  foreign controller owner instead of looping on `AlreadyOwnedError`. Verify: envtest rejects
  NATIVE→SPECIFIC and a `siteRef` re-point; `make manifests generate` produces no diff.

## 2. Operator client and controller

- [x] 2.1 Add a `NATIVE` network-reference constant and confirm `operator/internal/unifi/wifi_broadcasts.go`
  models both variants (`WifiNetworkReference`). Verify: a client test round-trips a `NATIVE`
  reference (no `networkId`) through `httptest`.
- [x] 2.2 Branch `resolveDependencies` in `operator/internal/controller/unifiwifibroadcast_controller.go`:
  a `NATIVE` broadcast resolves its `UnifiSite` from `spec.siteRef` (nil network); a `SPECIFIC`
  broadcast keeps the transitive `networkRef -> UnifiNetwork -> siteRef` resolution. Update
  `ensureOwnership` so a `NATIVE` broadcast is owned by its `UnifiSite` (it sets no
  `UnifiNetwork` ownerReference). Verify: controller tests cover both resolutions and that a
  `NATIVE` broadcast is owned by its site.
- [x] 2.3 Build the request and drift comparison: `buildWifiBroadcastRequest` writes
  `network: {type: NATIVE}` when `spec.nativeNetwork` is set (else `SPECIFIC`), and sets
  `BandSteeringEnabled` from the common field for every type; confirm `broadcastMatchesRequest`
  handles the `NATIVE` reference and common band steering. Verify: controller tests assert the
  `NATIVE` create/update body and that a band-steering-only change triggers one update while an
  unchanged object writes nothing.
- [x] 2.4 Ensure teardown of a `NATIVE` broadcast uses `status.siteID`/`status.wifiBroadcastID`
  (no live network reference). Verify: a controller test deletes a `NATIVE` broadcast with no
  network and asserts the upstream delete uses the status-recorded site.
- [x] 2.5 Own a `NATIVE` broadcast by its `UnifiSite`: `ensureOwnership` sets a controller
  ownerReference to the `UnifiSite` for a `NATIVE` broadcast (a `SPECIFIC` broadcast keeps its
  `UnifiNetwork` ownerReference), and the `UnifiSite` drain in `unifisite_controller.go` includes
  `UnifiWifiBroadcast` (with the matching RBAC) so a `NATIVE` broadcast is deleted before the site
  is removed and its teardown resolves a live site. Verify: controller tests assert the `UnifiSite`
  ownerReference and that the site drain covers `UnifiWifiBroadcast`.

## 3. CLI client and snapshot

- [x] 3.1 Project the new shapes in `cli/internal/snapshot/snapshot.go`: `broadcastNetworkName`
  returns the `NATIVE` binding; `buildWifiBroadcastSpec` emits `spec.nativeNetwork: true` +
  `spec.siteRef` (emitted `UnifiSite` name) instead of skipping, and projects
  `bandSteeringEnabled` into the common field, removing it from `UnrepresentedSettings`. Verify:
  unit tests assert a `NATIVE`-bound broadcast is emitted (no warning, no UUID in `spec`) and that
  band steering lands in `spec.bandSteeringEnabled` with no unrepresented annotation.
- [x] 3.2 Recapture `cli/testdata/wifi-*.json` to include a `NATIVE`-bound broadcast and
  `bandSteeringEnabled` on a `STANDARD` broadcast, and regenerate `cli/testdata/golden/wifi.golden.yaml`.
  Verify: `go test ./...` in `cli/` passes and the golden diff is reviewed.

## 4. Contract documentation

- [x] 4.1 Update `docs/unifi-api.md`: replace the "only the `SPECIFIC` binding is exposed" paragraph
  with the modeled `NATIVE` binding (broadcast-level `siteRef`), and note that `bandSteeringEnabled`
  is a common broadcast field. Verify: `zensical build --strict` passes.
- [x] 4.2 Update `docs/architecture.md` and `docs/crd-conventions.md` for the NATIVE binding and its
  ownership: a NATIVE broadcast resolves its site directly from broadcast-level `spec.siteRef` (no
  transitive network) and is owned by that `UnifiSite` (its strongest reference), so the general
  "the site owns its strongest-reference children" rule holds as written; the site drain now also
  covers `UnifiWifiBroadcast`, and teardown also uses `status`. Verify: `zensical build --strict`
  passes.

## 5. Integration and gates

- [x] 5.1 Operator gate: `go build ./... && go vet ./... && golangci-lint run && go test ./...` and
  `make manifests generate` with no diff. Verify: all exit clean.
- [x] 5.2 CLI gate: `go build ./... && go vet ./... && golangci-lint run && go test ./...` in `cli/`.
  Verify: all exit clean.
- [x] 5.3 Docs gate: `zensical build --strict`. Verify: exit clean.
- [x] 5.4 Manual read-only re-probe against a live console: `snapshot wifi` emits the `NATIVE`-bound
  broadcast (with `nativeNetwork` + `siteRef`) and the band-steering value, and no `go test`
  contacts a live console. Verify: output inspected.
- [x] 5.5 Sync the Helm chart CRD copy: `charts/unifi-operator/crds/unifi.supporterino.de_unifiwifibroadcasts.yaml`
  must match the regenerated `operator/config/crd/bases/` file (every other chart CRD is already
  byte-identical). Verify: the two files are byte-identical and the chart still lints/renders.

## Workflow follow-up

- Archive the change after review, noting the **BREAKING** `bandSteeringEnabled` field move in the
  summary.
