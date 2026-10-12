# Tasks

## 1. CRD contract (types + manifests)

- [x] 1.1 Add a `DHCPConfiguration` type and an optional `dhcpConfiguration` field on
  `IPv4Configuration` in `operator/api/v1alpha1/unifinetwork_types.go` (mode, `ipAddressRange`
  start/stop, `dnsServerIpAddressesOverride`, `leaseTimeSeconds`, `domainName`,
  `pingConflictDetectionEnabled`). Verify: `make -C operator manifests generate` produces no diff
  and `operator/api/v1alpha1/zz_generated.deepcopy.go` is updated.
- [x] 1.2 Add per-item validation to `DHCPGuarding.TrustedDHCPServerIPAddresses` (item `MinLength`
  plus a CEL `XValidation` for IPv4 shape) in `operator/api/v1alpha1/unifinetwork_types.go`.
  Verify: `make -C operator manifests generate` produces no diff.
- [x] 1.3 Add envtest cases in `operator/internal/controller/` covering: valid guard entries
  accepted, an empty-string guard entry rejected, and `dhcpConfiguration` round-tripping through
  the API server. Verify: `go test ./...` in `operator/` passes.

## 2. Operator client, controller, and contract doc

- [x] 2.1 Model `dhcpConfiguration` in `operator/internal/unifi/client.go`'s `IPv4Configuration`
  and confirm `NetworkRequest.MarshalJSON` emits it inside `ipv4Configuration`. Verify: an
  `httptest` test in `operator/internal/unifi/http_client_test.go` asserts the create/update body
  equals the fixture.
- [x] 2.2 Map the `api.firewall.zone-based-firewall-not-configured` `400` to an empty zone set in
  `operator/internal/unifi/firewall_zones.go` (`ListZones`/`GetZone`), matching the error `code`.
  Verify: an `httptest` test asserts an empty slice and nil error.
- [x] 2.3 Update `docs/unifi-api.md` — header casing resolved, envelope and error body confirmed,
  live-observed `applicationVersion` noted with gates kept as a floor, the ZBF-unconfigured code
  added, and the `NATIVE` binding noted — plus `docs/crd-conventions.md` if the DHCP shape note
  changes. Verify: `zensical build --strict` passes.

## 3. CLI client, snapshot, fixtures

- [x] 3.1 Model `dhcpConfiguration` in `cli/internal/unifi/client.go` and project it in
  `cli/internal/snapshot/`. Verify: a unit test asserts the emitted `UnifiNetwork` carries the DHCP
  settings.
- [x] 3.2 Map the unconfigured-ZBF `400` to an empty zone set in `cli/internal/unifi/`. Verify: a
  test asserts `snapshot firewall-zones` emits zero resources and exits `0`.
- [x] 3.3 Filter empty `trustedDhcpServerIpAddresses` entries when emitting, with a warning, so the
  CR passes the tightened schema. Verify: a test asserts the emitted CR contains no empty entry.
- [x] 3.4 Recapture `cli/testdata/*.json` from the observed shapes (`metadata.configurable`,
  non-`USER_DEFINED` origin, populated `dhcpConfiguration`, empty guard entries, ZBF-off `400`) and
  regenerate goldens. Verify: `go test ./...` in `cli/` passes and the golden diff is reviewed.

## 4. Integration and gates

- [x] 4.1 Operator gate: `go build ./... && go vet ./... && golangci-lint run && go test ./...`
  and `make manifests generate` with no diff. Verify: all exit clean.
- [x] 4.2 CLI gate: `go build ./... && go vet ./... && golangci-lint run && go test ./...` in
  `cli/`. Verify: all exit clean.
- [x] 4.3 Docs gate: `zensical build --strict`. Verify: exit clean.
- [x] 4.4 Manual read-only re-probe against a live console: `snapshot all` emits no empty guard
  values and includes DHCP configuration, and `snapshot firewall-zones` exits `0`. Verify: output
  inspected; no live console is contacted by any `go test`.

## Workflow follow-up

- Archive the change after review, noting the **BREAKING** guard-validation change in the summary.
