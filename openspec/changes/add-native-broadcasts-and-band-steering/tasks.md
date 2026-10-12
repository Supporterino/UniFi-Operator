# Tasks

## 1. CRD contract (types + manifests)

- [ ] 1.1 Make `UnifiWifiBroadcast.spec.networkRef` optional and add optional `spec.nativeNetwork`
  (bool) and `spec.siteRef` (`CoreRef`) in `operator/api/v1alpha1/unifiwifibroadcast_types.go`, with
  CEL requiring exactly one of `networkRef`/`nativeNetwork` and `siteRef` present iff
  `nativeNetwork` is true (and absent for a `SPECIFIC` binding). Keep the `networkRef` immutability
  rule. Verify: `make -C operator manifests generate` produces no diff; envtest rejects both-set,
  neither-set, and `SPECIFIC` + `siteRef`; accepts `NATIVE` + `siteRef`.
- [ ] 1.2 Promote band steering: add top-level `spec.bandSteeringEnabled *bool` and remove
  `StandardWifiOptions.BandSteeringEnabled`. Verify: `make -C operator manifests generate` produces
  no diff; envtest accepts `spec.bandSteeringEnabled` for `STANDARD` and `IOT_OPTIMIZED` and rejects
  the removed `spec.standard.bandSteeringEnabled` path.
- [ ] 1.3 Add the envtest cases from 1.1–1.2 in `operator/internal/controller/unifiwifibroadcast_types_test.go`
  and confirm `go test ./...` in `operator/` passes.

## 2. Operator client and controller

- [ ] 2.1 Add a `NATIVE` network-reference constant and confirm `operator/internal/unifi/wifi_broadcasts.go`
  models both variants (`WifiNetworkReference`). Verify: a client test round-trips a `NATIVE`
  reference (no `networkId`) through `httptest`.
- [ ] 2.2 Branch `resolveDependencies` in `operator/internal/controller/unifiwifibroadcast_controller.go`:
  a `NATIVE` broadcast resolves its `UnifiSite` from `spec.siteRef` (nil network); a `SPECIFIC`
  broadcast keeps the transitive `networkRef -> UnifiNetwork -> siteRef` resolution. Update
  `ensureOwnership` to set no `UnifiNetwork` ownerReference for a `NATIVE` broadcast. Verify:
  controller tests cover both resolutions and that a `NATIVE` broadcast reconciles with no
  ownerReference.
- [ ] 2.3 Build the request and drift comparison: `buildWifiBroadcastRequest` writes
  `network: {type: NATIVE}` when `spec.nativeNetwork` is set (else `SPECIFIC`), and sets
  `BandSteeringEnabled` from the common field for every type; confirm `broadcastMatchesRequest`
  handles the `NATIVE` reference and common band steering. Verify: controller tests assert the
  `NATIVE` create/update body and that a band-steering-only change triggers one update while an
  unchanged object writes nothing.
- [ ] 2.4 Ensure teardown of a `NATIVE` broadcast uses `status.siteID`/`status.wifiBroadcastID`
  (no live network reference). Verify: a controller test deletes a `NATIVE` broadcast with no
  network and asserts the upstream delete uses the status-recorded site.

## 3. CLI client and snapshot

- [ ] 3.1 Project the new shapes in `cli/internal/snapshot/snapshot.go`: `broadcastNetworkName`
  returns the `NATIVE` binding; `buildWifiBroadcastSpec` emits `spec.nativeNetwork: true` +
  `spec.siteRef` (emitted `UnifiSite` name) instead of skipping, and projects
  `bandSteeringEnabled` into the common field, removing it from `UnrepresentedSettings`. Verify:
  unit tests assert a `NATIVE`-bound broadcast is emitted (no warning, no UUID in `spec`) and that
  band steering lands in `spec.bandSteeringEnabled` with no unrepresented annotation.
- [ ] 3.2 Recapture `cli/testdata/wifi-*.json` to include a `NATIVE`-bound broadcast and
  `bandSteeringEnabled` on a `STANDARD` broadcast, and regenerate `cli/testdata/golden/wifi.golden.yaml`.
  Verify: `go test ./...` in `cli/` passes and the golden diff is reviewed.

## 4. Contract documentation

- [ ] 4.1 Update `docs/unifi-api.md`: replace the "only the `SPECIFIC` binding is exposed" paragraph
  with the modeled `NATIVE` binding (broadcast-level `siteRef`), and note that `bandSteeringEnabled`
  is a common broadcast field. Verify: `zensical build --strict` passes.

## 5. Integration and gates

- [ ] 5.1 Operator gate: `go build ./... && go vet ./... && golangci-lint run && go test ./...` and
  `make manifests generate` with no diff. Verify: all exit clean.
- [ ] 5.2 CLI gate: `go build ./... && go vet ./... && golangci-lint run && go test ./...` in `cli/`.
  Verify: all exit clean.
- [ ] 5.3 Docs gate: `zensical build --strict`. Verify: exit clean.
- [ ] 5.4 Manual read-only re-probe against a live console: `snapshot wifi` emits the `NATIVE`-bound
  broadcast (with `nativeNetwork` + `siteRef`) and the band-steering value, and no `go test`
  contacts a live console. Verify: output inspected.

## Workflow follow-up

- Archive the change after review, noting the **BREAKING** `bandSteeringEnabled` field move in the
  summary.
