# Tasks

## 1. Ordering reads in the UniFi client

- [x] 1.1 Add `GetFirewallPolicyOrdering(ctx, siteID, sourceZoneID, destinationZoneID)` to
  `cli/internal/unifi/firewall_policies.go`, returning the ordered policy-id partition; verify with
  an `httptest` case in `firewall_policies_test.go` and `go test ./internal/unifi/...`.
- [x] 1.2 Add `ListAclRuleOrdering(ctx, siteID)` to `cli/internal/unifi/acl_rules.go`, returning the
  site-global ordered rule ids; verify with an `httptest` case in `acl_rules_test.go` and
  `go test ./internal/unifi/...`.
- [x] 1.3 Update the snapshot-consumer column for both ordering `GET`s in `docs/unifi-api.md`; verify
  the endpoints now name a `snapshot` kind rather than `—`.

## 2. Ordering projections and golden fixtures

- [x] 2.1 Make `snapshot.FirewallPolicies` return an upstream-policy-id -> emitted-name map (as
  `Networks` does) and add `snapshot.FirewallPolicyOrdering`, referencing emitted
  `UnifiFirewallPolicy` names and excluding the system-defined partition; verify with unit tests
  covering name resolution and an unresolvable policy being skipped with a warning.
- [x] 2.2 Make `snapshot.AclRules` return an upstream-rule-id -> emitted-name map and add
  `snapshot.AclRuleOrdering`, emitting `spec.orderedAclRules` in upstream order; verify with unit
  tests that no upstream rule UUID reaches `spec`.
- [x] 2.3 Add recorded fixtures and golden files (`cli/testdata/*.json`,
  `cli/testdata/golden/firewall-policy-ordering.golden.yaml`, `acl-rule-ordering.golden.yaml`);
  verify with the golden tests in `cli/internal/snapshot`.
- [x] 2.4 Wire the `firewall-policy-ordering` and `acl-rule-ordering` kinds into the `run()` dispatch
  in `cli/internal/cmd/snapshot.go`; verify each kind emits the expected golden output via the CLI
  command test.

## 3. `snapshot verify`

- [x] 3.1 Implement the coverage check (every projectable kind upstream has an emitted resource and
  no representable object is missing) reading a snapshot directory; verify with a test that reports
  a deliberately omitted object.
- [x] 3.2 Implement the fidelity check comparing each emitted `spec` to a fresh in-memory projection,
  treating known-gap annotations as expected; verify with a test asserting a placeholder Secret is
  not reported as drift.
- [x] 3.3 Add the `verify` subcommand with `-o` defaulting to `snapshot/`, warnings on stderr, and a
  non-zero exit on an unexplained gap; verify with command tests for both exit codes and that no file
  is written.
- [x] 3.4 Document `snapshot verify` (usage, exit codes, known gaps) in `docs/`; verify the
  documented command appears in the snapshot docs page.

## 4. Redefine `all` and the output directory

- [x] 4.1 Drive `all` from a single ordered kind list so it emits every projectable kind, and add a
  table test asserting `all` covers every kind the dispatch accepts; verify the test fails if a kind
  is added without updating the list.
- [x] 4.2 Make `all` span every site via `ListSites` (not only the default `--site`); verify a
  multi-site fixture emits each site's scoped resources.
- [x] 4.3 Add `snapshot/` to `.gitignore`; verify `git check-ignore snapshot/example.yaml` matches.
- [x] 4.4 Update `docs/` for the **BREAKING** `all` semantics and the `snapshot/` output convention;
  verify with `zensical build --strict`.

## 5. Integration verification

- [x] 5.1 Run the CLI gate: `cd cli && go build ./... && go vet ./... && golangci-lint run && go test ./...`.
- [x] 5.2 Run `snapshot all -o <tmp>` then `snapshot verify -o <tmp>` against a combined `httptest`
  fixture server; verify `verify` exits `0` on the faithful snapshot and non-zero after one emitted
  file is removed.
- [x] 5.3 Run `zensical build --strict` from the repo root and confirm it passes.

## Workflow follow-up

- Resolve the design open question (subcommand vs top-level verb) before release; it does not block
  implementation.
- Archive the change after review passes and `openspec validate` is clean.
