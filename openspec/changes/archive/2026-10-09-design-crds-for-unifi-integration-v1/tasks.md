## 1. Contract and docs sync (Integration v1)

- [x] 1.1 Rewrite `docs/unifi-api.md` to the Integration v1 base path frozen at spec **v10.4.57**:
  API-key auth, the JSON page envelope, the **flat** `Error Message` body, `GET /v1/info`
  (`applicationVersion`), and the minimum app version per capability (Official API ≥10.1.78,
  networks CRUD ≥10.0.162, firewall/DNS ≥10.1.84). Remove the legacy REST rows and add the
  endpoint -> operator method -> CLI map for `UnifiController`, `UnifiSite`, and `UnifiNetwork`.
  Confirm the API-key header casing against the target console and record it. Verify the doc
  builds with `zensical build --strict`.
- [x] 1.2 Extend `docs/crd-conventions.md` with the same-namespace reference chain (`siteRef` ->
  `controllerRef`), ownerReference ownership (strongest-reference parent; the site drains
  children with a finalizer), membership ownership (zone owns networks), and the typed-selector
  idiom (device, DPI, country, built-in firewall zone). Verify `zensical build --strict` passes.

## 2. Foundation API types (UnifiController, UnifiSite)

- [x] 2.1 Add `UnifiControllerSpec`/`Status` and `UnifiSiteSpec`/`Status` in
  `operator/api/v1alpha1/` with kubebuilder markers, status subresource, printer columns, and a
  same-namespace Secret-based credential reference. `UnifiController.status.applicationVersion`
  reports the detected version from `GET /v1/info` and gates capability availability. Verify
  `go build ./...` in `operator/` succeeds.
- [x] 2.2 Register the new kinds in the scheme and run `make -C operator manifests generate`;
  verify no diff remains and the new CRDs appear under `operator/config/crd/`.
- [x] 2.3 Add envtest coverage for defaulting/validation of `insecureSkipVerify` and the required
  `secretRef`/`controllerRef`; verify `go test ./...` passes.

## 3. Rework UnifiNetwork API type

- [x] 3.1 Replace `UnifiNetworkSpec` with the pinned v10.4.57 shape: `siteRef`, the `management`
  discriminator, and per-variant structs (`GATEWAY`/`SWITCH`/`UNMANAGED`) with CEL `XValidation`
  (D7). No `zoneId`/`zoneRef` (D5); the switch device binding is a typed device selector (D8).
  Ensure no opaque ID field exists. Verify `go build ./...` and `make manifests generate` produce
  no diff.
- [x] 3.2 Add envtest cases asserting a mismatched variant is rejected and an out-of-range
  `vlanId` is rejected; verify `go test ./...` passes.
- [x] 3.3 Update `operator/config/samples/` to the reworked schema and run
  `make -C operator manifests generate`; verify no diff.

## 4. Integration v1 client

- [x] 4.1 Add typed Integration v1 client code under `operator/internal/unifi/` (base path,
  `X-API-Key` header, page decoding, `{error}` mapping), returning plain structs with no
  Kubernetes imports. Verify `go build ./...` and `golangci-lint run` pass.
- [x] 4.2 Add `httptest.Server` fixture tests for `/v1/info` reachability + `applicationVersion`,
  site lookup, network read/create/update/delete (including the flat error body and the
  management-discriminated request bodies), and error/401 handling; verify `go test ./...` passes.
- [x] 4.3 Add the equivalent minimal client surface under `cli/internal/unifi/` for the CLI
  consumer; verify `go test ./...` passes in `cli/`.

## 5. Reconcilers and wiring

- [x] 5.1 Implement `unificontroller_controller.go` (resolve Secret, verify TLS policy, set
  readiness + detected app version, no credential logging). Verify with fake-client tests.
- [x] 5.2 Implement `unifisite_controller.go` (resolve `controllerRef`, adopt the upstream site by
  name and record its UUID in status, and drain owned children via a finalizer before removal).
  Each child reconciler sets its own `ownerReference` to its immediate parent. Verify with
  fake-client tests.
- [x] 5.3 Rework `unifinetwork_controller.go` (resolve `siteRef`, reconcile the gateway union,
  report zone and upstream ID in status, symmetric finalizer). Verify with fake-client tests and
  an idempotency test (second reconcile performs no writes).
- [x] 5.4 Wire the new reconcilers in `operator/cmd/main.go` and confirm RBAC markers are added
  next to the calls; run `make -C operator manifests generate` and verify no diff.

## 6. Deployment artifacts

- [x] 6.1 Ensure kustomize (`operator/config/`) and the Helm chart (`charts/`) include the new
  CRDs; verify `kustomize build operator/config/default`, `helm lint`, and `helm template` all
  exit 0 and list `UnifiController`, `UnifiSite`, and `UnifiNetwork`.

## 7. CLI snapshot parity

- [x] 7.1 Update the CLI snapshot emitter to produce the reworked `UnifiNetwork` shape, add a
  `UnifiSite` emitter (from `GET /v1/sites`), and a `UnifiController` template (`url` + a
  placeholder `secretRef`) the user fills in; verify golden-file tests pass with `go test ./...`
  in `cli/`.

## 8. Docs and verification gates

- [x] 8.1 Update `docs/architecture.md` with the new kind graph and reference chain; verify
  `zensical build --strict` passes.
- [x] 8.2 Run the full operator gate (`go build ./... && go vet ./... && golangci-lint run &&
  go test ./... && make manifests generate`) and verify no diff remains.
- [x] 8.3 Run the full CLI gate (`go build ./... && go vet ./... && golangci-lint run &&
  go test ./...`) and verify it passes.
