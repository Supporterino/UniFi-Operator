## 1. Contract and docs sync

- [x] 1.1 Add the WiFi broadcast endpoint rows to `docs/unifi-api.md` (`ListWifiBroadcasts`,
  `GetWifiBroadcast`, `CreateWifiBroadcast`, `UpdateWifiBroadcast`, `DeleteWifiBroadcast`) with
  their operator methods and CLI consumers, noting the list is an overview and the passphrase is
  inline upstream but carried by a `Secret` in the CR; add the `WiFi broadcasts` row to the
  capability table (`docs/unifi-api.md:36-40`) pinned to the Official API minimum (`>= 10.1.78`);
  verify `zensical build --strict` passes.
- [x] 1.2 Extend `docs/crd-conventions.md` with the network-owned-child (strongest-reference) drain
  idiom and the passphrase-from-`Secret` and `radiusProfileRef` reference notes; verify
  `zensical build --strict` passes.

## 2. Broadcast API types

- [x] 2.1 Add `UnifiWifiBroadcastSpec`/`Status`, the `UnifiWifiBroadcastFinalizer`, status
  subresource, printer columns, and the root/List types in
  `operator/api/v1alpha1/unifiwifibroadcast_types.go`, including `status.wifiBroadcastID` and the
  resolved `status.networkID`/`status.siteID`; verify `go build ./...` succeeds.
- [x] 2.2 Add the broadcast variant structs (common required/optional fields plus
  `StandardWifiOptions`/`IotOptimizedWifiOptions`) including the optional trees (basic data rates,
  blackout schedule, client filtering, mDNS proxy, multicast filtering, DNS assistance, handoff
  suggestions, hotspot, DTIM override, MLO) with discriminator CEL rules; verify `go build ./...`.
- [x] 2.3 Add the seven-variant `securityConfiguration` union with `SecretKeySelector` passphrases
  (`passphrase`, `presharedKeys[].passphrase`) and `radiusProfileRef`, plus the RADIUS
  NAS-ID/mac-authentication sub-unions, with discriminator CEL rules; verify `go build ./...`.
- [x] 2.4 Add `networkRef` (with an immutability CEL rule on the spec), `deviceTags[]`, and the
  `UnifiNetwork` refs (`presharedKeys[].network`, mDNS `bridgingNetworkIds`), register the kind in
  the scheme, and run `make -C operator manifests generate`; verify no diff and the CRD appears
  under `operator/config/crd/`.
- [x] 2.5 Add envtest coverage for broadcast defaulting/validation (`type` mismatch, security
  `type` mismatch, `deviceTags` `MinLength`, group-rekey and DTIM ranges, basic-data-rate and
  mac-format enums, and rejection of a `networkRef` change); verify `go test ./...` passes.
- [x] 2.6 Add `operator/config/samples/` examples for `STANDARD` and `IOT_OPTIMIZED` broadcasts
  (passphrase via `Secret`); verify `make manifests generate` produces no diff.

## 3. Integration v1 WiFi client

- [x] 3.1 Add the plain-struct broadcast types and `ListWifiBroadcasts`/`GetWifiBroadcast`/
  `CreateWifiBroadcast`/`UpdateWifiBroadcast`/`DeleteWifiBroadcast` under
  `operator/internal/unifi/` (no Kubernetes imports); verify `go build ./...` and
  `golangci-lint run` pass.
- [x] 3.2 Add `httptest.Server` fixtures for the broadcast list page (overview), detail, create,
  update, delete, page decoding, flat-error/401 handling, and unknown-field tolerance; verify
  `go test ./...` passes.

## 4. Reconciler and recursive drain

- [x] 4.1 Add `CapabilityWifi` (Official API minimum, `>= 10.1.78`) to
  `operator/internal/controller/versions.go` and implement
  `operator/internal/controller/unifiwifibroadcast_controller.go`: resolve
  `spec.networkRef` → network → `spec.siteRef` → site → controller transitively, gate on
  `CapabilityWifi`, set the `ownerReference` to the network, and add/remove the finalizer
  symmetrically; verify with fake-client tests including a below-minimum `VersionUnsupported` case.
- [x] 4.2 Implement create/update/delete with detail comparison for idempotency and the
  non-`USER_DEFINED` read-only fail-closed path, recording `status.wifiBroadcastID` and the
  resolved `status.networkID`/`status.siteID`; delete from the `status`-recorded site so teardown
  survives a broken `networkRef`; verify with fake-client tests including a second-reconcile
  no-write assertion and a delete-after-broken-reference case.
- [x] 4.3 Implement device-tag scope resolution (`ListDeviceTags`, require exactly one member
  device) with `DeviceTagNotFound`/`DeviceTagAmbiguous` fail-closed reasons; verify with tests for
  resolved, missing, and ambiguous tags.
- [x] 4.4 Implement passphrase reads for `passphrase` and `presharedKeys[].passphrase` through the
  uncached `APIReader` with terminal `SecretNotFound`/`SecretKeyMissing` reasons; verify with
  tests asserting the value never appears in `status` or logs.
- [x] 4.5 Implement the enterprise `radiusProfileRef` fail-closed path (`Ready=False`, no upstream
  mutation); verify with a fake-client test.
- [x] 4.6 Extract the site's child-drain loop (`unifisite_controller.go:149-210`) into a reusable
  helper and have `UnifiNetwork.reconcileDelete` (`unifinetwork_controller.go:312`) drain owned
  `UnifiWifiBroadcast` children before removing its finalizer; verify with a test covering the
  recursive site → network → broadcast drain and a regression test that the site still drains
  networks and firewall zones.
- [x] 4.7 Wire the reconciler in `operator/cmd/main.go` with `+kubebuilder:rbac` markers and
  dependency watches, then run `make -C operator manifests generate`; verify no diff.

## 5. CLI snapshot parity

- [x] 5.1 Mirror the read-only WiFi broadcast list/detail in `cli/internal/unifi/`; verify
  `go test ./...` passes in `cli/`.
- [x] 5.2 Add the `snapshot wifi` emitter emitting one `UnifiWifiBroadcast` per upstream broadcast,
  building `networkRef` from the shared upstream-network-id → emitted-name map (`snapshot.Networks`,
  keyed by `network.ID`), reverse-mapping the device scope to tag names, and emitting a placeholder
  personal `passphrase`/`presharedKeys[].passphrase` `SecretKeySelector` plus a new
  `AnnotationUnresolvedWifiSecret` constant (alongside `AnnotationUnresolvedDeviceTag`/
  `AnnotationUnresolvedNetworkRef`) and a warning; verify golden-file tests.
- [x] 5.3 Wire `snapshot wifi` into the cobra command tree and into `snapshot all` (it exists today):
  add `wifi` to the kind switch and the help text (`cli/internal/cmd/snapshot.go:69,103`); verify
  `go build ./...`, `go vet ./...`, `golangci-lint run`, and `go test ./...` pass in `cli/`.

## 6. Deployment artifacts

- [x] 6.1 Add the `UnifiWifiBroadcast` CRD and RBAC to kustomize (`operator/config/`) and the Helm
  chart (`charts/`); verify `kustomize build operator/config/default`, `helm lint`, and
  `helm template` all exit 0 and list `UnifiWifiBroadcast`.

## 7. Architecture docs

- [x] 7.1 Update `docs/architecture.md` with the new kind, the network-owned-child recursive drain,
  and the `Secret`/`radiusProfileRef` reference patterns; verify `zensical build --strict` passes.

## 8. Verification gates

- [x] 8.1 Run the operator gate (`go build ./... && go vet ./... && golangci-lint run &&
  go test ./... && make manifests generate`) and verify no diff remains.
- [x] 8.2 Run the CLI gate (`go build ./... && go vet ./... && golangci-lint run && go test ./...`).
- [x] 8.3 Run `zensical build --strict` and verify it passes.
