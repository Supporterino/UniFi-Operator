# Proposal

## Why

The workspace can build each module ad hoc, but it cannot produce, version, or publish
anything: there are no tags, no release workflow, no version stamped into either binary, and
both the kustomize and Helm manifests still point at the placeholder `controller:latest`.
Contributors also juggle three separate entrypoints (`make`, `go`, `zensical`) and depend on
tools that may not be installed. This change gives the project one local entrypoint and a
single-version release pipeline so the operator, CLI, and Helm chart ship together,
reproducibly and with a version that is true in every artifact.

## What Changes

- **Adopt a root `Taskfile.yml` as the sole local entrypoint.** **BREAKING** for contributors:
  remove `operator/Makefile` and migrate every documented `make` command to a named Task.
  kubebuilder-specific generators (CRDs, RBAC, deepcopy, envtest bootstrap) move into Task.
- **Unify the Go toolchain on 1.27.x.** Bump the `operator` module's `go` directive from
  `1.24.5` and the build image from `golang:1.24`, so one toolchain satisfies both modules.
- **Stamp a version into both binaries.** Add a version variable to the operator and the CLI,
  injected at build time with `-ldflags`; expose it via the CLI `--version` flag and an
  operator startup log line.
- **Adopt a single bundle version.** `release-please` drives `vX.Y.Z`; it updates
  `.release-please-manifest.json`, `charts/unifi-operator/Chart.yaml` (`version` and
  `appVersion`), and `CHANGELOG.md`.
- **Publish artifacts on release.** A release workflow pushes the multi-arch image to
  `ghcr.io/supporterino/unifi-operator`, cross-compiles the CLI with goreleaser (appending to
  the release release-please already created), publishes the chart to
  `oci://ghcr.io/supporterino/charts`, and attaches the kustomize-built `dist/install.yaml`.
- **Enforce the gates in CI.** Add a pull-request workflow running the same checks as
  `task verify`: generated-manifest no-diff, lint, tests, and a new `crd-sync` check that
  proves the kustomize CRDs and the chart's `crds/` are byte-identical.
- **Provision pinned tools through `go run pkg@version`** (controller-gen, kustomize,
  setup-envtest) and declare binary requirements (docker, helm) with Task `requires`.
- **Document the workflows.** Add `docs/building.md` and `docs/releasing.md`, and point
  `docs/deployment.md` at the published image and chart.

### Non-Goals

- Artifact signing and SBOM/attestation (named as a follow-up; the release workflow is shaped
  so these slot in without restructuring).
- Per-component versioning; the operator, CLI, and chart release as one bundle.
- Making the CLI `go install`-able or the operator API a supported Go library (both would
  require submodule tags and break the single bundle tag).
- Versioned documentation and any change to the CRD API version (`v1alpha1` stays).
- OLM/OperatorHub packaging and non-GitHub distribution channels.

## Capabilities

### New Capabilities

- `build-and-release`: one local build/test entrypoint (Taskfile with pinned tool
  provisioning), the single-version release model, version stamping into the operator and CLI,
  and the packaging and publishing of the container image, CLI binaries, Helm chart, and
  consolidated install bundle, with the CI gates that protect them.

### Modified Capabilities

- `project-scaffold`: the CRD-generation gate is exercised through the Taskfile
  (`task manifests` / `task generate`) rather than `make manifests generate`.

## Impact

- **CRD contract:** none. No new kinds, no `spec` or `status` fields, no API-version change;
  the generated CRDs are unchanged in content.
- **Operator:** `operator/go.mod`, `operator/Dockerfile`, `operator/.devcontainer`,
  `operator/cmd/main.go` (version stamp); `operator/Makefile` removed.
- **CLI:** `cli/internal/cmd/root.go` plus a version package; new `--version`.
- **Manifests:** `operator/config/manager/manager.yaml` and
  `charts/unifi-operator/values.yaml` move off the `controller:latest` placeholder;
  `charts/unifi-operator/Chart.yaml` gains release-please annotations. Generated CRDs and RBAC
  are unaffected.
- **Automation:** new `.github/workflows/ci.yml`, `.github/workflows/release-please.yml`,
  `.github/workflows/release.yml`; new `Taskfile.yml`, `.release-please-config.json`,
  `.release-please-manifest.json`, `.goreleaser.yaml`.
- **Docs and agent instructions:** `AGENTS.md`, `operator/AGENTS.md`, `cli/AGENTS.md`,
  `README.md`, `docs/{deployment,testing,go,debugging,security,crd-conventions}.md`,
  `openspec/config.yaml`, and the `project-scaffold` spec.
- **Dependencies:** no new Go dependencies for the shipped binaries; goreleaser and Task are
  new build-time tools.
