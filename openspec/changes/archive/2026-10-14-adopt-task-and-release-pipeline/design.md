# Design

## Context

See `proposal.md` — Why. The constraints that shape the approach:

- `operator/` is a kubebuilder v4 project (`operator/PROJECT`, `cliVersion: 4.9.0`) whose
  generated `operator/Makefile` pins controller-gen `v0.19.0`, kustomize `v5.7.1`,
  golangci-lint `v2.4.0`, and bootstraps envtest via `setup-envtest`.
- `cli/` is a second Go module with no Makefile, no Dockerfile, and no lint config.
- `.github/workflows/docs.yml` is the only workflow; it builds Zensical and deploys Pages.
- Version facts today: `charts/unifi-operator/Chart.yaml` is `0.1.0` / `appVersion 0.1.0`, and
  the image is a placeholder — `controller:latest` in `operator/config/manager/manager.yaml`
  and `charts/unifi-operator/values.yaml` (`repository: controller`, `tag: latest`).
- CRDs exist twice and are hand-synced: `operator/config/crd/bases/*.yaml` and
  `charts/unifi-operator/crds/*.yaml` are currently byte-identical.
- The Go toolchain is split: `operator/go.mod` declares `go 1.24.5`, `cli/go.mod` declares
  `go 1.27.1`, and `operator/Dockerfile` builds on `golang:1.24`.

## Goals / Non-Goals

**Goals:**

- One local entrypoint that both developers and CI use, so local and CI gates cannot drift.
- A single version that is true in the tag, both binaries, the chart, and the release bundle.
- Reproducible, tool-pinned builds that do not depend on pre-installed or stale binaries.

**Non-Goals:**

- Signing, SBOM, and provenance (the release workflow is shaped so these slot in later).
- Making the CLI `go install`-able or the operator API a supported Go library (both need
  submodule tags and would break the single bundle tag).
- Changing the CRD API version; the generated CRD content is unchanged.
- Versioned documentation.

## Decisions

### 1. Taskfile replaces Make as the only entrypoint

`operator/Makefile` is deleted and every target it exposed is re-expressed as a named task.
Task's `sources`/`generates` give content-based incrementality, which matters because Make's
mtime checks thrash on regenerated CRDs; `requires` turns "this tool is missing" into a clear
pre-execution error; `includes` namespaces `docs:*` and `chart:*`; and a single `verify` task
becomes the one gate both locally and in CI.

Alternatives considered: a root Makefile aggregating per-module includes (rejected — weaker
incrementality, no declarative tool requirements, terser expressions); keeping Make with Task
as a wrapper (rejected — two runners, ambiguous "which do I call").

**Consequence:** `kubebuilder create api` re-emits `operator/Makefile`. `operator/AGENTS.md`
must record that the file is not part of the build and is deleted after scaffolding.

### 2. Tools resolved with `go run pkg@version`

Generation and test tooling is invoked as `go run <package>@<version>`, reusing the versions
already pinned in the Makefile: controller-gen `v0.19.0`, kustomize `v5.7.1`, setup-envtest at
the controller-runtime release branch. Nothing is installed into a `bin/` directory.

Alternatives: `go tool` directives in `go.mod` (rejected — pulls the tools into the module
graph and `go.sum`); installing into `operator/bin` as today (rejected — stale-binary
management). Container tooling and Helm cannot be resolved this way, so they are declared with
Task `requires`.

### 3. Unify the Go toolchain on 1.27.x

Bump the `operator/go.mod` `go` directive, the `operator/Dockerfile` base image, and the
devcontainer to 1.27.x. The CLI's `1.27.1` floor wins; a single image can then build both
modules, and the operator already compiles under the local 1.27.2 toolchain.

Alternative: two build images or a matrix (rejected — more moving parts for a single
repository).

### 4. One bundle version, driven by release-please

`release-please` runs in `simple` mode at the repository root, because there is no single root
`go.mod` and the `go` release type does not apply to the two-module layout.
`.release-please-config.json` sets `bump-minor-pre-major: true` (stay in `0.x`),
`include-component-in-tag: false` (tags are `vX.Y.Z`), and `extra-files` entries that rewrite
`charts/unifi-operator/Chart.yaml` `$.version` and `$.appVersion`.
`.release-please-manifest.json` is seeded at the current chart version, `0.1.0`.

Alternatives: release-please `go` type (rejected — no single root module); manual tagging
(rejected — the user chose release-please); semantic-release (equivalent but less
GitHub-native).

### 5. Version reaches the binaries through `-ldflags -X`

A version variable is added to both modules, defaulting to a development marker so a local
build still reports something non-empty. The CLI surfaces it through cobra's `Version` field
(which wires `--version`); the operator logs it once at startup next to the existing
`setupLog`. This requires no runtime dependency and no new endpoint.

### 6. Build and publish on `release: published`

A workflow separate from release-please does the packaging, keyed off the tag `vX.Y.Z`:

- **Image:** `docker buildx build --platform linux/amd64,linux/arm64 --push` to
  `ghcr.io/supporterino/unifi-operator`, tagged both the release version and its minor series.
- **CLI:** goreleaser with `builds[0].dir: cli`, injecting the version, producing archives and
  a checksum file. `release.mode: append` attaches the assets to the release release-please
  already created instead of creating a second one.
- **Chart:** `helm package charts/unifi-operator`, then
  `helm push … oci://ghcr.io/supporterino/charts`.
- **Install bundle:** `kustomize build operator/config/default` with the image pinned to the
  release tag, uploaded as a release asset.

Alternative for the CLI: a hand-rolled `go build` matrix over `GOOS`/`GOARCH` (rejected — the
user chose goreleaser, which also handles archives and checksums).

### 7. Annotate the chart, inject the image tag

`Chart.yaml` `version`/`appVersion` are rewritten by release-please, so any checked-out tree
states its version truthfully. The image tag in `operator/config/manager/manager.yaml` and
`charts/unifi-operator/values.yaml` is instead *injected at build time* from the tag: the
image does not exist until the release workflow runs, so annotating a committed tag there
would be circular. The committed defaults become the real image repository with an annotated
placeholder tag, and `docs/releasing.md` explains why the two mechanisms differ.

### 8. CRD sync is gated, not yet single-sourced

A `crd-sync` task (and CI step) diffs `operator/config/crd/bases/*.yaml` against
`charts/unifi-operator/crds/*.yaml` and fails, naming the divergent files. `docs/deployment.md`
currently only asks humans to keep the two copies aligned. Single-sourcing the chart CRDs from
`operator/config/crd` would be stronger but is a larger refactor; the gate is the minimal
enforcement and does not preclude it.

### 9. CI runs the same tasks as local

The pull-request workflow runs `task verify` and `zensical build --strict` — the same entry
points a developer runs. This removes the possibility of a gate that exists in CI but not
locally, or vice versa.

## Risks / Trade-offs

- [kubebuilder re-emits `operator/Makefile` on `create api`] → documented in
  `operator/AGENTS.md`; a re-emitted Makefile is treated as generated noise and deleted.
- [release-please and goreleaser both manage the GitHub Release] → goreleaser runs with
  `release.mode: append`; the release workflow must verify no duplicate release is created.
- [Bumping the operator to Go 1.27 may surface new vet/lint findings] → the toolchain bump is
  its own task, gated by the full operator test run before anything else depends on it.
- [Deleting `make` breaks muscle memory and any external scripts] → `docs/building.md` carries
  a table mapping each old `make` target to its replacement.
- [The two CRD copies may already be drifting in a future tree] → the `crd-sync` gate catches
  it; any pre-existing divergence is fixed in this change.
- [Chart `version` equalling the bundle version means chart-only changes still bump the app] →
  accepted consequence of the one-bundle model.

## Migration Plan

1. Add `Taskfile.yml` covering every current `make` target; confirm each target has a task.
2. Bump the toolchain in `operator/go.mod`, `operator/Dockerfile`, `.devcontainer`; run both
   module gates.
3. Add the version variables and the CLI `--version` flag; confirm binaries report a version.
4. Add release-please config, manifest, and workflow.
5. Add the release workflow (image, goreleaser, chart OCI, install bundle).
6. Add the pull-request CI workflow running `task verify` and the strict docs build.
7. Migrate `make` references in docs and agent instructions to task targets; add
   `docs/building.md` and `docs/releasing.md` and their nav entries.
8. Delete `operator/Makefile`.

**Rollback:** revert the change. Tags and published artifacts are additive and can be deleted
independently; `operator/Makefile` and the old manifests restore from git history.

## Open Questions

- Whether to later single-source the chart CRDs from `operator/config/crd` rather than gating a
  copy. Deferrable — the gate is correct either way and single-sourcing is its own change.
