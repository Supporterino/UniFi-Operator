## Why

The repository defines its architecture, conventions, and gates (root `AGENTS.md`, `docs/`,
`openspec/config.yaml`), but no code exists yet. Every Go, kustomize, and Helm gate is declared
but cannot run. We need a buildable, testable foundation so that feature changes land against a
real module layout rather than an aspirational one.

## What Changes

- Initialize a kubebuilder project under `operator/` (Go module, `cmd/main.go`, `Makefile`,
  controller-runtime wiring, kustomize `config/`).
- Introduce one sample CRD kind, `UnifiNetwork` (`unifi.supporterino.de/v1alpha1`), with a
  `status` subresource, `conditions`, `observedGeneration`, and a `Ready` printer column — a
  concrete reference implementation of the CRD golden rule.
- Add a reconciler for `UnifiNetwork` that is idempotent, writes `status` on every terminal
  path, and does not call a live UniFi controller (a stubbed `internal/unifi` client returns
  fixture data).
- Initialize a separate Go module under `cli/` with a cobra root command and a `snapshot`
  subcommand stub that emits a placeholder `UnifiNetwork` CR.
- Add a Helm chart under `charts/unifi-operator/` that renders the manager Deployment, RBAC,
  and CRDs consistently with the kustomize output.
- Make all declared gates runnable: `go build/vet/lint/test` for both modules,
  `make -C operator manifests generate` (no diff), `kustomize build operator/config/default`,
  and `helm lint`/`helm template`.
- Add unit + envtest tests for the sample controller and CLI, using an `httptest` fixture for
  the UniFi client.

**BREAKING**: none — this is the initial scaffold.

## Capabilities

### New Capabilities

- `project-scaffold`: The workspace builds, tests, and packages the operator and CLI, exposes a
  conformant sample CRD, and produces consistent kustomize and Helm deployment output.

### Modified Capabilities

<!-- none: openspec/specs/ is empty; this introduces the first capability. -->

## Impact

- **New Go modules**: `operator/` (module `github.com/Supporterino/UniFi-Operator/operator`) and
  `cli/` (module `github.com/Supporterino/UniFi-Operator/cli`).
- **New CRD**: `unifinetworks.unifi.supporterino.de/v1alpha1` — its `spec` must obey
  `docs/crd-conventions.md` (no opaque IDs), and its `status` carries `conditions`,
  `observedGeneration`, and an upstream correlation field.
- **New manifests**: `operator/config/**` (generated) and `charts/unifi-operator/**`.
- **Docs**: `docs/architecture.md` and `docs/unifi-api.md` remain the contract; no contract
  change is needed for the stub (the `internal/unifi` client returns fixtures).
- **Gates**: turns the declared operator/CLI/kustomize/Helm gates into runnable checks.

## Non-Goals

- No real UniFi controller integration — `internal/unifi` is a stubbed client returning fixtures.
- No additional CRD kinds beyond the single `UnifiNetwork` reference sample.
- No OLM bundle or release/chart publishing pipeline.
- No authentication/RBAC enforcement beyond the generated manager RBAC markers.
