# operator

The Kubernetes operator module of [UniFi-Operator](../README.md). It reconciles the workspace's
Custom Resources into a UniFi Network controller through the Integration v1 API, and records the
outcome of every reconcile in the object's `status`.

## What it owns

- `api/v1alpha1/` — the CRD API types (`UnifiController`, `UnifiSite`, `UnifiNetwork`,
  `UnifiFirewallPolicy`, …) and their generated deepcopy code.
- `internal/controller/` — one reconciler per kind. Every terminal path writes a `Ready`
  condition with a stable reason and a human-readable summary.
- `internal/unifi/` — the typed UniFi client. It returns plain Go structs and imports no
  Kubernetes types.
- `internal/metrics/` — the Prometheus series the operator exports about itself.
- `config/` — the kustomize manifests: generated CRDs and RBAC, the manager Deployment, the
  `default` overlay, and the opt-in Prometheus scrape overlays.

Credentials never live in a Custom Resource: each `UnifiController` names a same-namespace
`Secret` through `spec.secretRef`, and the client resolves the key at reconcile time. Upstream
`_id` values appear in `status` only. The design rules are in
[docs/crd-conventions.md](../docs/crd-conventions.md).

## Requirements

- Go 1.27.1 or newer — the version in `go.mod`.
- [Task](https://taskfile.dev/) — the root `Taskfile.yml` is the only entrypoint. `kubebuilder`
  re-emits an `operator/Makefile` when you scaffold a new API; delete it after scaffolding,
  because it is not part of the build.
- `kubectl` and a cluster reachable from your current kubeconfig, for the install tasks.
- Docker, for the image tasks only.

## Commands

Run these from the repository root.

| Task | Does |
|------|------|
| `task manifests generate` | Regenerate CRDs, RBAC, and deepcopy code |
| `task test` | Unit and envtest suites (downloads the envtest binaries on first run) |
| `task lint` | `go vet` plus `golangci-lint`, both modules |
| `task build` | Build the manager and CLI binaries into `bin/` |
| `task run` | Run the manager from the host against the current kubeconfig |
| `task install` | Install the CRDs into the cluster |
| `task deploy` | Install the CRDs and deploy the manager |
| `task undeploy` `task uninstall` | Remove the manager, then the CRDs |
| `task kustomize` `task chart` | Render the kustomize overlay and lint/render the Helm chart |
| `task verify` | The local gate: generation no-diff, lint, both modules' tests, CRD and RBAC sync |

`task test` excludes the e2e suite; `task test-e2e` runs it against a Kind cluster.

Deepcopy, CRD, and RBAC output is generated — edit the API type or the `+kubebuilder` marker and
re-run `task manifests generate`, then confirm the tree is unchanged.

The manager image is `ghcr.io/supporterino/unifi-operator`; the deployment path and its overrides
are documented in [docs/deployment.md](../docs/deployment.md).

## Docs

- [`AGENTS.md`](AGENTS.md) — this module's layout, conventions, and verification gate.
- [Deployment](../docs/deployment.md) — kustomize and Helm, including observability.
- [UniFi API contract](../docs/unifi-api.md) — the endpoints each kind consumes.
- [Architecture](../docs/architecture.md) — where this module sits in the workspace.

## License

Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
