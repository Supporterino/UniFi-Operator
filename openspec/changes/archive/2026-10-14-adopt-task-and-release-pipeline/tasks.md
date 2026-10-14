# Tasks

## 1. Local entrypoint (Taskfile)

- [x] 1.1 Add a root `Taskfile.yml` with the project variables (image repository, chart path, version) and the task groups; verify `task --list` exits 0 and lists build, test, lint, fmt, generate, manifests, verify, image, crd-sync, docs, and chart tasks.
- [x] 1.2 Implement the generation tasks (`generate` for deepcopy, `manifests` for CRD and RBAC) using `go run sigs.k8s.io/controller-tools/cmd/controller-gen@v0.19.0`, replacing the Makefile's `go-install-tool` step; verify each task run twice in a row leaves `git status` clean.
- [x] 1.3 Implement the build and test tasks for both modules, including the envtest bootstrap via `go run sigs.k8s.io/controller-runtime/tools/setup-envtest@release-0.22`; verify `task test` exits 0 for the operator and the CLI.
- [x] 1.4 Declare the container tool and Helm with Task `requires`; verify a task needing Helm fails before execution with a message naming Helm as missing.
- [x] 1.5 Implement `lint` (golangci-lint with the operator config plus `go vet`) over both modules; add a `cli/.golangci.yml` so the CLI lints under the same bar; verify `task lint` exits 0 for both modules.
- [x] 1.6 Add `docs/building.md` describing every local task, the required tools, and a table mapping each former `make` target to its replacement; add the page to `nav` in `zensical.toml` and verify `zensical build --strict` exits 0.

## 2. Toolchain unification

- [x] 2.1 Bump the `operator/go.mod` `go` directive to 1.27.x; verify `task test` and `task build` exit 0 for the operator.
- [x] 2.2 Update `operator/Dockerfile` to build on `golang:1.27` and the devcontainer image to match; verify the Dockerfile's Go version satisfies both modules' directives and, where a container tool is available, that the image builds.
- [x] 2.3 Confirm one toolchain satisfies both modules by running `task verify` after the bump; verify it exits 0.

## 3. Version stamping

- [x] 3.1 Add a `cli/internal/version` package with a default development value and wire it to cobra's `Version` field; verify a built CLI prints a non-empty version for `--version`.
- [x] 3.2 Add a version variable to the operator and log it once at startup beside the existing `setupLog`; verify a unit test asserts the reported value, or start the manager and observe the version line.
- [x] 3.3 Inject the version into both binaries from the Taskfile build tasks with `-ldflags -X`; verify a `task build` binary reports the injected value and that an unset version still yields a non-empty local build identifier.
- [x] 3.4 Document version reporting and how to override it in `docs/building.md`; verify the documented command produces the version it claims.

## 4. Manifests and chart defaults

- [x] 4.1 Replace the `controller:latest` placeholder in `operator/config/manager/manager.yaml` with the real registry repository and an annotated tag; verify `task` kustomize rendering shows the real repository and not `controller`.
- [x] 4.2 Update `charts/unifi-operator/values.yaml` `repository` and `tag` to the real registry, and annotate `charts/unifi-operator/Chart.yaml` `version` and `appVersion` for the release tooling; verify `helm lint` and `helm template` render the real repository.
- [x] 4.3 Update `docs/deployment.md` to the published image and chart references and to task-based commands; verify `zensical build --strict` exits 0.

## 5. CRD synchronization gate

- [x] 5.1 Add a `crd-sync` task diffing `operator/config/crd/bases/*.yaml` against `charts/unifi-operator/crds/*.yaml` and naming any divergent or missing file; verify it passes on the current tree.
- [x] 5.2 Verify the gate fails as intended by editing one CRD copy, running the task, and confirming it reports the divergent file; restore the file afterward.
- [x] 5.3 Wire `crd-sync` into the `verify` task; verify `task verify` fails when CRD drift is present and passes when it is not.

## 6. release-please configuration

- [x] 6.1 Add `.release-please-config.json` (simple release type, `bump-minor-pre-major`, no component in tag, `extra-files` targeting `charts/unifi-operator/Chart.yaml` `version` and `appVersion`) and seed `.release-please-manifest.json` at `0.1.0`; verify both files parse and the referenced chart paths exist.
- [x] 6.2 Add `.github/workflows/release-please.yml` with `contents: write` and `pull-requests: write`; verify the workflow lints (actionlint where available) and its configuration is valid YAML.

## 7. Release workflow

- [x] 7.1 Add `.goreleaser.yaml` building the CLI from `cli/` with the version injected, producing per-platform archives and a checksum file, and using `release.mode: append`; verify `goreleaser check` passes and a snapshot build produces the expected archives.
- [x] 7.2 Add a release workflow triggered on release publication that builds and pushes the multi-architecture image to the project registry tagged with the release version and its minor series; verify the buildx configuration targets `linux/amd64` and `linux/arm64` and the workflow lints.
- [x] 7.3 Extend the release workflow to package and push the Helm chart to the project OCI registry and to build and attach the consolidated install manifest with the image pinned to the release tag; verify the workflow lints and a dry run references the release tag consistently across all four artifacts.
- [x] 7.4 Add `docs/releasing.md` covering the tag flow, what publishes where, the difference between the annotated chart version and the injected image tag, and rollback; add the page to `nav` and verify `zensical build --strict` exits 0.

## 8. Pull-request CI

- [x] 8.1 Add `.github/workflows/ci.yml` running `task verify` and `zensical build --strict` on pull requests; verify it invokes only task targets (no duplicated command lines) and lints.
- [x] 8.2 Dry-run the same two commands locally in a clean tree and confirm they match what CI runs; verify `task verify` and `zensical build --strict` both exit 0.

## 9. Migrate references and remove Make

- [x] 9.1 Migrate the `make` references in `AGENTS.md`, `operator/AGENTS.md`, `cli/AGENTS.md`, `README.md`, `docs/{testing,go,debugging,security,crd-conventions}.md`, and `openspec/config.yaml` to task targets; verify no documented build command still references `make` and `zensical build --strict` exits 0.
- [x] 9.2 Delete `operator/Makefile` and record in `operator/AGENTS.md` that the file is not part of the build and is removed again if `kubebuilder create api` regenerates it; verify the repository's gates pass with no Makefile present.

## 10. Integration verification

- [x] 10.1 Run `task verify` (generation no-diff, lint, both modules' tests, CRD sync) followed by `zensical build --strict` in a clean tree and verify both exit 0 with `git status` clean afterward.
- [x] 10.2 Verify no placeholder remains and no stale reference survives: `grep -rn 'controller:latest' operator charts` and `grep -rn '\bmake ' AGENTS.md README.md docs operator/AGENTS.md cli/AGENTS.md openspec/config.yaml` return no build instructions.

## Workflow follow-up

- Archive the change after review, syncing the `build-and-release` capability into `openspec/specs/` and applying the `project-scaffold` delta.
- Verify the archived result, including that the new spec and the modified spec both read correctly.
- Confirm the release workflows run green on the first real tagged release before announcing artifacts.
