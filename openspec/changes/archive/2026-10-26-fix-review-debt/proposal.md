# Proposal

## Why

The `add-operator-observability` review passes found two problems that fell outside that change's
scope. Both are small, and leaving them open has a real cost:

- **A flaky race in the test suite.** `operator/internal/controller/unifiwifibroadcast_controller_test.go`
  builds one `*UnifiWifiBroadcast` fixture and hands the same pointer to every `t.Parallel()`
  subtest. `fake.ClientBuilder.Build()` writes `ResourceVersion` into the object it is given, so the
  subtests race. `go test -race ./internal/controller/` fails intermittently (reproduced against
  pristine `HEAD`, so this predates the observability work). `docs/go.md` treats a clean `-race` run
  as part of the Go bar, so today the bar is not reliably enforceable and a genuine race introduced
  later would be easy to dismiss as "that known flake".
- **A stale module map.** `operator/AGENTS.md`'s layout block enumerates the module's packages but
  omits `internal/metrics/` — a package the observability change added and `operator/README.md` now
  advertises — and has never listed `internal/version/`. The per-module `AGENTS.md` is the
  authoritative reference for module layout, so a missing package there is worse than a missing row
  in a README.

## What Changes

- **Make the wifi-broadcast test fixtures per-subtest.** Construct the fixture object inside each
  subtest (or `DeepCopy()` before handing it to the fake builder) so no two parallel subtests share a
  mutable object. No production code changes and no assertion changes.
- **Complete the layout block in `operator/AGENTS.md`** by adding `internal/metrics/` and
  `internal/version/`, described in the same style as the existing rows.
- **Verify the race is actually gone** by running the controller suite under `-race` repeatedly, and
  keep the fix small enough that it cannot mask a different race.

## Capabilities

None. This change alters no observable behaviour: it fixes a test's fixture sharing and corrects a
documentation map. `openspec` therefore records `skip_specs: true` for this change rather than
inventing a requirement that no consumer can observe.

## Impact

- **Test code:** `operator/internal/controller/unifiwifibroadcast_controller_test.go` only.
- **Docs:** `operator/AGENTS.md`.
- **CRD contract:** none. **Manifests:** none. **CLI:** none.
- **Gate:** `go test -race ./internal/controller/` must pass repeatedly; `task verify` unchanged.

## Non-Goals

- Auditing the rest of the suite for the same shared-fixture pattern. If the `-race` runs after this
  fix expose another one, that is a separate change — this one does not expand to a sweep.
- Changing the test framework, adding `t.Parallel()` anywhere, or restructuring the wifi-broadcast
  tests beyond what the race requires.
- Any behaviour change to the operator, the chart, or the CLI.
