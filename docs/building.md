# Building

This is the canonical local-build reference for the UniFi-Operator workspace. Every local
build, test, lint, generation, image, and packaging command is a named task in the root
`Taskfile.yml`; the task runner is the sole entrypoint for contributors and for CI, so local
and automated gates cannot drift. The former `operator/Makefile` is retired — this page maps
each of its targets to the task that replaces it.

## Prerequisites

- **Go 1.27.x.** Both modules (`operator/go.mod` and `cli/go.mod`) declare `go 1.27.1`; the
  tasks use whichever `go` is on `PATH`. See the [Go bar](go.md).
- **Task** ([taskfile.dev](https://taskfile.dev/installation/)). Install it however you prefer,
  for example:

  ```bash
  go install github.com/go-task/task/v3/cmd/task@latest
  ```

- **External tools for the tasks that need them** — a container runtime, Helm, kubectl, Kind,
  and zensical. See [Required tools](#required-tools).

Generation and test tooling is **not** installed into a `bin/` directory. Each task resolves the
exact version it needs at invocation with `go run <pkg>@<version>`, so a clean checkout needs no
separate installation step. A task whose tool cannot be resolved this way — the container
runtime, Helm, kubectl, Kind, and zensical — declares a precondition that **fails before the
task runs and names the missing binary**.

## Task variables

Variables have working defaults. Override any of them as a CLI variable
(`task build VERSION=1.2.3`). `VERSION`, `IMAGE_REPOSITORY`, and `IMAGE_TAG`
also accept an environment override (`VERSION=1.2.3 task build`); a CLI variable
outranks the environment, which outranks the default.

| Variable | Default | Used by |
|----------|---------|---------|
| `VERSION` | `dev` | `IMAGE_TAG`; stamped into the built binaries by the build tasks |
| `IMAGE_REPOSITORY` | `ghcr.io/supporterino/unifi-operator` | `image`, `docker-push`, `docker-buildx`, `build-installer`, `deploy` |
| `IMAGE_TAG` | `{{.VERSION}}` | `image`, `docker-push`, `docker-buildx`, `build-installer`, `deploy` |
| `CONTAINER_TOOL` | `docker` | the container tasks |
| `PLATFORMS` | `linux/amd64,linux/arm64` | `docker-buildx` |
| `KIND_CLUSTER` | `operator-test-e2e` | the e2e tasks |
| `IGNORE_NOT_FOUND` | `false` | `uninstall`, `undeploy` |
| `ENVTEST_K8S_VERSION` | `1.34.x` | `setup-envtest`, `test` |

The pinned tool versions (`CONTROLLER_GEN_VERSION` `v0.19.0`, `KUSTOMIZE_VERSION` `v5.7.1`,
`ENVTEST_VERSION` `release-0.22`, `GOLANGCI_LINT_VERSION` `v2.14.0`) are set in `Taskfile.yml`
and changed there, not per invocation.

## Tasks

Run `task` (or `task --list`) from the repository root to see the same groups interactively.

### General

| Task | What it does | Overrides |
|------|--------------|-----------|
| `default` | Lists the available tasks. | — |
| `help` | Lists the available tasks. | — |
| `all` | Builds every module; equivalent to `build`. | — |

### Generation

| Task | What it does | Overrides |
|------|--------------|-----------|
| `manifests` | Generates the CRD, RBAC, and webhook manifests with the pinned controller-gen. | — |
| `generate` | Generates the deepcopy methods with the pinned controller-gen. | — |
| `controller-gen` | Resolves and prints the pinned controller-gen version. | — |
| `kustomize` | Renders the default kustomize overlay to stdout with the pinned kustomize. | — |

`manifests` and `generate` are idempotent: they declare their inputs (`sources`) and outputs
(`generates`), so a second run with no intervening source change leaves the tree clean.

### Formatting, vetting, and linting

| Task | What it does | Overrides |
|------|--------------|-----------|
| `fmt` | Runs `gofmt` over both Go modules. | — |
| `vet` | Runs `go vet` over both Go modules. | — |
| `lint` | Runs golangci-lint in each module under its own config, plus `vet`. | — |
| `lint-fix` | Runs golangci-lint over both modules and applies available fixes. | — |
| `lint-config` | Verifies the golangci-lint configuration in both modules. | — |
| `golangci-lint` | Resolves and prints the pinned golangci-lint version. | — |

### Testing

| Task | What it does | Overrides |
|------|--------------|-----------|
| `setup-envtest` | Downloads the envtest API server and etcd for the pinned Kubernetes version. Alias: `envtest`. | `ENVTEST_K8S_VERSION` |
| `test` | Runs the unit and envtest suites for both Go modules. | `ENVTEST_K8S_VERSION` |
| `setup-test-e2e` | Creates the Kind cluster used by the e2e tests if it does not exist. | `KIND_CLUSTER` |
| `test-e2e` | Runs the e2e tests against an isolated Kind cluster, then tears it down. | `KIND_CLUSTER` |
| `cleanup-test-e2e` | Tears down the Kind cluster used by the e2e tests. | `KIND_CLUSTER` |

`test` bootstraps envtest inline: it fetches the API server and etcd through the pinned
`setup-envtest` before running. See [Testing](testing.md) for the strategy.

### Build

| Task | What it does | Overrides |
|------|--------------|-----------|
| `build` | Builds the manager and CLI binaries with the bundle version stamped in. | `VERSION` |
| `run` | Runs the operator manager from the host against the current kubeconfig, with the version stamped in. | `VERSION` |
| `image` | Builds the manager container image with the container tool. | `VERSION`, `IMAGE_REPOSITORY`, `IMAGE_TAG`, `CONTAINER_TOOL` |
| `docker-build` | Builds the manager container image; alias for `image`. | as `image` |
| `docker-push` | Pushes the manager container image with the container tool. | `VERSION`, `IMAGE_REPOSITORY`, `IMAGE_TAG`, `CONTAINER_TOOL` |
| `docker-buildx` | Builds and pushes a multi-platform manager image with the container tool. | `VERSION`, `IMAGE_REPOSITORY`, `IMAGE_TAG`, `CONTAINER_TOOL`, `PLATFORMS` |
| `build-installer` | Builds a consolidated install bundle at `dist/install.yaml` with the image pinned. | `VERSION`, `IMAGE_REPOSITORY`, `IMAGE_TAG` |

### Deployment

| Task | What it does | Overrides |
|------|--------------|-----------|
| `install` | Installs the CRDs into the cluster in `~/.kube/config`. | — |
| `uninstall` | Uninstalls the CRDs from the cluster. | `IGNORE_NOT_FOUND` |
| `deploy` | Deploys the manager to the cluster with the built image pinned. | `VERSION`, `IMAGE_REPOSITORY`, `IMAGE_TAG` |
| `undeploy` | Undeploys the manager from the cluster. | `IGNORE_NOT_FOUND` |

Deployment specifics — which path to use and how credentials are wired — live in
[Deployment](deployment.md).

### CRD synchronization

| Task | What it does | Overrides |
|------|--------------|-----------|
| `crd-sync` | Fails if the generated CRDs and the Helm-bundled CRDs are not byte-identical, naming the divergent files. | — |

### Documentation and chart

| Task | What it does | Overrides |
|------|--------------|-----------|
| `docs` | Builds the documentation site in strict mode; broken links fail the build. | — |
| `chart` | Lints and renders the Helm chart. | `CHART_PATH`, `CHART_NAME` |

### Aggregate gate

| Task | What it does | Overrides |
|------|--------------|-----------|
| `verify` | Runs the local gate: generation no-diff, both modules' lint and tests, and CRD sync. | — |

`task verify` is the single gate a contributor and CI both run. The docs build
(`task docs`) is run alongside it, not inside it.

## Version reporting

Both binaries embed the bundle version at link time. `task build` passes the `VERSION` variable
to the Go linker (`-ldflags -X`) for each module, so the version the release pipeline knows is
the version the binary reports:

| Binary | Reports the version through | Plain `task build` reports |
|--------|-----------------------------|----------------------------|
| `cli/bin/unifi-operator-cli` | the `--version` flag | `dev` |
| `operator/bin/manager` | a `starting` log line carrying a `version` field, logged once at startup | `dev` |

`VERSION` defaults to the `dev` development marker, so a build that supplies no version still
reports the non-empty `dev` identifier rather than an empty string. Override it inline to stamp
a specific version:

```bash
VERSION=v9.9.9-test task build
./cli/bin/unifi-operator-cli --version
# unifi-operator-cli version v9.9.9-test
```

The same variable reaches the container image: `task image` and `task docker-buildx` pass it as
the `VERSION` build argument, and `operator/Dockerfile` forwards it to `go build -ldflags`. A
released image therefore reports the release version rather than `dev`. `task run` stamps the
same value into the manager it starts from the host.

The release pipeline derives `VERSION` from the tag `vX.Y.Z`, so the tag, the image tag, the
CLI archives, and both binaries all report one version.

## Required tools

Tasks that depend on a tool Task cannot resolve through `go run` declare it as a precondition.
When the binary is absent the task fails immediately and names the tool.

| Tool | Needed by | How it is handled |
|------|-----------|-------------------|
| Go toolchain | every task | Assumed on `PATH` |
| Container runtime (`CONTAINER_TOOL`, default `docker`) | `image`, `docker-build`, `docker-push`, `docker-buildx` | Precondition names the missing tool |
| Helm | `chart` | Precondition names the missing tool |
| kubectl | `install`, `uninstall`, `deploy`, `undeploy` | Precondition names the missing tool |
| Kind | `setup-test-e2e`, `cleanup-test-e2e` | Precondition names the missing tool |
| zensical | `docs` | Precondition names the missing tool |

The generator and test tools — controller-gen, kustomize, setup-envtest, and golangci-lint — are
not external requirements: each task pins its version and resolves it with `go run pkg@version`
at invocation, so no `bin/` install and no version skew.

## Former `make` targets

The `operator/Makefile` is retired. Every target it exposed maps to a task:

| Former `make` target | Replacement | Notes |
|----------------------|-------------|-------|
| `make all` | `task all` | Builds every module. |
| `make help` | `task help` | `task` and `task --list` do the same. |
| `make manifests` | `task manifests` | Pinned controller-gen, no `go-install-tool` step. |
| `make generate` | `task generate` | Pinned controller-gen. |
| `make fmt` | `task fmt` | Now runs over both Go modules. |
| `make vet` | `task vet` | Now runs over both Go modules. |
| `make test` | `task test` | envtest bootstrapped inline; both modules. |
| `make setup-envtest` | `task setup-envtest` | Also aliasable as `task envtest`. |
| `make envtest` | `task setup-envtest` | Alias `envtest`; resolved via `go run`, no `bin/`. |
| `make setup-test-e2e` | `task setup-test-e2e` | `KIND_CLUSTER` unchanged. |
| `make test-e2e` | `task test-e2e` | |
| `make cleanup-test-e2e` | `task cleanup-test-e2e` | |
| `make lint` | `task lint` | Both modules, each under its own config. |
| `make lint-fix` | `task lint-fix` | Both modules. |
| `make lint-config` | `task lint-config` | Both modules. |
| `make build` | `task build` | Now builds the CLI too. |
| `make run` | `task run` | |
| `make docker-build` | `task docker-build` | Alias for `image`. |
| `make docker-push` | `task docker-push` | |
| `make docker-buildx` | `task docker-buildx` | `PLATFORMS` unchanged. |
| `make build-installer` | `task build-installer` | |
| `make install` | `task install` | `kubectl` guarded by a precondition. |
| `make uninstall` | `task uninstall` | `ignore-not-found=true` → `IGNORE_NOT_FOUND=true`. |
| `make deploy` | `task deploy` | |
| `make undeploy` | `task undeploy` | `ignore-not-found=true` → `IGNORE_NOT_FOUND=true`. |
| `make kustomize` | `task kustomize` | Pinned kustomize. |
| `make controller-gen` | `task controller-gen` | Pinned controller-gen. |
| `make golangci-lint` | `task golangci-lint` | Pinned golangci-lint. |

Argument mapping: the old `IMG=<registry>/<image>:<tag>` becomes `IMAGE_REPOSITORY=<registry>/<image>`
with `VERSION=<tag>` (or an explicit `IMAGE_TAG`); `CONTAINER_TOOL`, `PLATFORMS`, and
`KIND_CLUSTER` keep their names. Pass the mapped variables either in the environment or as CLI
variables — `IMAGE_REPOSITORY=<repo> VERSION=<tag> task <task>` and
`task <task> IMAGE_REPOSITORY=<repo> VERSION=<tag>` are equivalent, with the CLI form winning
when both are set.

## Related

- [Testing](testing.md) — the test layers and what envtest covers.
- [Deployment](deployment.md) — kustomize and Helm deployment paths.
- [Documentation](documentation.md) — how this site is built and published.
- [Go bar](go.md) — the language-level standard the lint and vet tasks enforce.
