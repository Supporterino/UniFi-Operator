# Tasks

## 1. Operator client predicate

- [x] 1.1 Export `IsZoneBasedFirewallUnconfigured(err error) bool` from
  `operator/internal/unifi/firewall_zones.go`, keeping `zoneBasedFirewallUnconfigured` as the
  implementation. Verify: `cd operator && go test ./internal/unifi/ -run 'Zone'` passes, including
  a new case that is true for `api.firewall.zone-based-firewall-not-configured` and false for
  another `400` and for a `401`.

## 2. Operator terminal condition

- [x] 2.1 Add `reasonZoneBasedFirewallNotConfigured = "ZoneBasedFirewallNotConfigured"` to
  `operator/internal/controller/conditions.go`. Verify: `cd operator && go build ./...`.
- [x] 2.2 In `unififirewallpolicy_controller.go` `handleUpstreamError`, check
  `unifi.IsZoneBasedFirewallUnconfigured(err)` before the generic `!apiErr.Retryable()` branch and
  set the new reason with a message naming the missing subsystem. Verify:
  `unififirewallpolicy_controller_test.go` asserts `Ready=False`, reason
  `ZoneBasedFirewallNotConfigured`, and that no upstream write occurred.
- [x] 2.3 Apply the same branch in `unififirewallpolicyordering_controller.go`. Verify:
  `unififirewallpolicyordering_controller_test.go` asserts the new reason and no order write.
- [x] 2.4 Apply the same branch in `unififirewallzone_controller.go` while keeping the
  `ListZones`/`GetZone` read-path mappings unchanged. Verify: extend
  `TestUnifiFirewallZoneReconcileUnconfiguredZBFWriteFailsClosed` to assert the new reason and no
  write.
- [x] 2.5 Update the ZBF bullet in `docs/unifi-api.md` (around line 496) to record that the code is
  returned by both the zones and policies endpoints, and how each consumer responds (operator:
  empty zone read plus `ZoneBasedFirewallNotConfigured` for policy/ordering/zone writes; CLI:
  empty firewall reads plus a site-scoped warning). Verify: `zensical build --strict`.

- [x] 2.6 Make `HTTPClient.ListZones` return `(zones, zbfConfigured, err)` (false only for the
  code) and gate `UnifiFirewallPolicy` / `UnifiFirewallPolicyOrdering` on `!zbfConfigured`
  immediately after the zone read, before selector resolution, with the terminal reason. Verify:
  the policy and ordering tests cover an empty zone read with the flag false (reason, no requeue,
  no policies read or write) as well as the zones-readable variant; the zone read path still
  discards the flag and reports no failing condition.

## 3. CLI tolerate and warn

- [x] 3.1 Add `cli/testdata/firewall-policies-unconfigured.json` from the probe body, map the code
  to an empty set in `Client.ListFirewallPolicies`, and change `Client.ListZones` to return
  `(zones, zbfConfigured, err)`; update the three projection call sites to compile. Verify:
  `cd cli && go build ./... && go test ./internal/unifi/` covers both reads, with
  `zbfConfigured=false` only for the code.
- [x] 3.2 Replace the `kindProjection` tuple with
  `projectionResult{Resources, Warnings, Notes}`, thread it through every projection and both
  orchestrators, and deduplicate notes exactly as warnings are deduplicated today. Verify:
  `cd cli && go build ./... && go test ./internal/cmd/`.
- [x] 3.3 Emit `firewallUnconfiguredNote(siteName)` from `projectFirewallZones`,
  `projectFirewallPolicies`, and `projectFirewallPolicyOrdering` when `zbfConfigured` is false.
  Verify: the ZBF-off `firewall-zones` test asserts one warning on stderr, zero CRs, and exit `0`.
- [x] 3.4 Make `snapshot verify` print notes without counting them in `skipped`. Verify: a verify
  test against a ZBF-off snapshot asserts the note is printed and the skipped count stays `0`.
- [x] 3.5 Add a ZBF-off variant of `newFixtureServer` that delegates non-firewall paths to the
  existing fixture handler and answers both firewall endpoints with the code; add a `snapshot all`
  regression proving non-firewall kinds are still emitted with exactly one note per site. Verify:
  `cd cli && go test ./...`.
- [x] 3.6 Add a short ZBF-off note to `docs/snapshot.md` near the kinds table. Verify:
  `zensical build --strict`.

- [x] 3.7 Report the configured flag from `Client.ListFirewallPolicies` and OR it with the zones
  flag in the policy and ordering projections; reword the note so it is identical per site and
  true for a partial signal. Verify: a policies-only-signal test asserts one note and zero
  policies; the client tests pin false only for the code.

## 4. Integration verification

- [x] 4.1 Run the operator gate:
  `cd operator && go build ./... && go vet ./... && golangci-lint run && go test ./...`.
- [x] 4.2 Run `task manifests generate` and confirm no diff (this change has no API-type change).
- [x] 4.3 Run the CLI gate:
  `cd cli && go build ./... && go vet ./... && golangci-lint run && go test ./...`.
- [x] 4.4 Run `zensical build --strict` and `graphify update .`.

## Workflow follow-up

- Archive the change after review and verification.
