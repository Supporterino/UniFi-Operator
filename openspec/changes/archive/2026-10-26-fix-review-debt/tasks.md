# Tasks

## 1. Remove the shared wifi-broadcast fixture

- [x] 1.1 Make `enterprise` per-subtest in `operator/internal/controller/unifiwifibroadcast_controller_test.go`:
  move the fixture construction (the `newWifiBroadcast()` call plus the enterprise security block)
  into a helper that each subtest invokes, so no two `t.Parallel()` subtests share a mutable object.
  Keep every table case, assertion, and parallel marker unchanged. Verify:
  `cd operator && go test ./internal/controller/ -run TestUnifiWifiBroadcastReconcileFailsClosed -count=1`
  passes and `grep -c "enterprise :=" operator/internal/controller/unifiwifibroadcast_controller_test.go`
  shows no remaining top-level shared fixture.

- [x] 1.2 Prove the race is gone: run
  `cd operator && go test ./internal/controller/ -race -count=3 -skip '^TestControllers$'` (the
  Ginkgo envtest suite rejects `-count>1`, so skip it for the repeated run) plus
  `-race -count=1` for the full package, and confirm clean results. If another shared-fixture race
  surfaces, stop and report it rather than continuing the sweep. Verify: repeated clean `-race` runs,
  and `git diff --stat operator/internal/controller/unifiwifibroadcast_controller_test.go` showing
  only test-file changes.

## 2. Complete the module layout map

- [x] 2.1 Add `internal/metrics/` and `internal/version/` to the layout block in `operator/AGENTS.md`,
  in the existing style and position (beside `internal/controller/` and `internal/unifi/`), describing
  each in one line: metrics exports the operator's own Prometheus series and Events surface; version
  carries the link-time stamped build version. Verify: every directory under `operator/internal/` is
  named in the block (`ls -d operator/internal/*/ | xargs -n1 basename` compared against the block).

## 3. Integration verification

- [x] 3.1 Run `cd operator && go build ./... && go vet ./... && go test ./... -count=1`, `task lint`,
  and `task verify`'s non-git-dependent components (`task crd-sync`, `task rbac-sync`). Verify: all
  exit 0 and no generated manifest changes (`task manifests generate` produces no diff).

## Workflow follow-up

- Run the Go review over the test change, then the generic review over the docs change.
- Archive this change once both pass.
