## 1. Contract and docs sync

- [x] 1.1 Add the device-tags and firewall-zones rows to the `docs/unifi-api.md` endpoint map
  (`ListDeviceTags`, `ListZones`, `GetZone`, `CreateZone`, `UpdateZone`, `DeleteZone`) with the
  CLI consumers; verify `zensical build --strict` passes.
- [x] 1.2 Extend `docs/crd-conventions.md` with the read-only device-tag selector idiom, the
  zone membership single-writer rule (keyed on `(siteRef, networkRef)`), and a Rule 5 carve-out for
  pre-release in-place breaking changes; verify `zensical build --strict` passes.

## 2. API types

- [x] 2.1 Add `DeviceTagSelector` (name, `MinLength=1`) to
  `operator/api/v1alpha1/reference_types.go` and change `UnifiNetworkSpec.Switch` to use it as
  `deviceTag` instead of `deviceTagRef CoreRef`; verify `go build ./...` succeeds.
- [x] 2.2 Add `UnifiFirewallZoneSpec`/`Status`, the `UnifiFirewallZoneFinalizer`, status
  subresource, and printer columns in `operator/api/v1alpha1/unififirewallzone_types.go`; verify
  `go build ./...` succeeds.
- [x] 2.3 Register the new kind in the scheme and run `make -C operator manifests generate`; verify
  no diff remains and the CRD appears under `operator/config/crd/`.
- [x] 2.4 Add envtest coverage for zone defaulting/validation (`name` `MinLength`, optional
  `networkRefs`) and the `deviceTag` selector `MinLength`; verify `go test ./...` passes.

## 3. Integration v1 client

- [x] 3.1 Add `DeviceTag` (with `DeviceIDs`) and `ListDeviceTags`, plus firewall-zone structs and
  `ListZones`/`GetZone`/`CreateZone`/`UpdateZone`/`DeleteZone`, under `operator/internal/unifi/`
  (plain structs, no Kubernetes imports); verify `go build ./...` and `golangci-lint run` pass.
- [x] 3.2 Add `httptest.Server` fixtures for device-tags list, firewall-zone list/get/create/
  update/delete, page decoding, and flat-error/401 handling; verify `go test ./...` passes.
- [x] 3.3 Mirror the read-only device-tags list and firewall-zone list in
  `cli/internal/unifi/`; verify `go test ./...` passes in `cli/`.

## 4. Firewall zone reconciler

- [x] 4.1 Implement `operator/internal/controller/unififirewallzone_controller.go` (resolve the
  site and controller, gate on `CapabilityFirewallDNS`, resolve the managed upstream zone by
  `status.zoneID` then name, resolve `networkRefs` via `UnifiNetwork.status.networkID`, add/remove
  the finalizer symmetrically, set `status.zoneID`/`origin`/conditions); verify with fake-client
  tests.
- [x] 4.2 Implement the custom-vs-system write policy (write only `USER_DEFINED`; adopt any other
  origin read-only with `Ready=False`) and verify with fake-client tests.
- [x] 4.3 Implement runtime uniqueness detection keyed on `(siteRef, networkRef)` for membership
  (`MembershipConflict`, no membership written while a network has more than one claimant in the
  same site), `(siteRef, spec.name)` for zone names (`ZoneNameConflict`, neither zone written), and
  fail-closed handling of a cross-site `networkRef` (`CrossSiteReference`); add an idempotency test
  asserting a second reconcile performs no writes; verify `go test ./...` passes.
- [x] 4.4 Add the new condition reasons and wire the reconciler in `operator/cmd/main.go` with RBAC
  markers next to the calls; run `make -C operator manifests generate` and verify no diff.
- [x] 4.5 Add a `UnifiFirewallZone` watch to the `UnifiNetwork` controller (reusing
  `networksInNamespace`) so a membership write re-reconciles the network and refreshes
  `status.zoneID`; verify with a fake-client/envtest test.

## 5. Network SWITCH device-tag resolution

- [x] 5.1 Replace `switchUnsupportedMessage` in
  `operator/internal/controller/unifinetwork_controller.go` with an explicit
  `CheckCapability(CapabilityOfficialAPI)` gate followed by tag resolution (`ListDeviceTags`,
  require exactly one `DeviceIDs` entry) and new reasons `DeviceTagNotFound`/`DeviceTagAmbiguous`;
  do not verify the resolved device's hardware type (Non-Goals); verify with fake-client tests for
  resolved, missing, and ambiguous tags and for a below-minimum console.
- [x] 5.2 Update the `UnifiNetwork` sample(s) and existing switch tests to the `deviceTag` selector,
  run `make -C operator manifests generate`, and verify no diff and `go test ./...` passes.

## 6. Deployment artifacts

- [x] 6.1 Ensure kustomize (`operator/config/`) and the Helm chart (`charts/`) include the
  `UnifiFirewallZone` CRD and its RBAC roles; verify `kustomize build operator/config/default`,
  `helm lint`, and `helm template` all exit 0 and list `UnifiFirewallZone`.

## 7. CLI snapshot parity

- [x] 7.1 Add the `snapshot firewall-zones` emitter (emit a `UnifiFirewallZone` per upstream zone,
  building `networkRefs` from the shared upstream-network-id -> emitted-name map rather than
  re-deriving names) and the read-only `snapshot device-tags` listing (stdout: tag names and device
  counts); reverse-map the switch `deviceId` to a tag name and keep the annotation fallback when it
  is not uniquely resolvable; verify golden-file tests pass with `go test ./...` in `cli/`.

## 8. Docs and verification gates

- [x] 8.1 Update `docs/architecture.md` with the new kind and the device-tag selector; verify
  `zensical build --strict` passes.
- [x] 8.2 Run the operator gate (`go build ./... && go vet ./... && golangci-lint run &&
  go test ./... && make manifests generate`) and verify no diff remains.
- [x] 8.3 Run the CLI gate (`go build ./... && go vet ./... && golangci-lint run && go test ./...`).
