# AGENTS.md — UniFi-Operator Workspace

**UniFi-Operator** is a Kubernetes operator that manages a UniFi network stack declaratively through Custom Resources. The repository is a single Git repo with wrapped module directories:

| Directory | Role | Tech |
|-----------|------|------|
| `operator/` | Kubernetes operator — CRD API types, controllers, UniFi client, kustomize manifests (`operator/config/`) | Go, kubebuilder, controller-runtime, envtest, kustomize |
| `cli/` | Snapshot / adoption CLI — reads a live UniFi controller and emits CRs | Go, cobra, separate Go module |
| `docs/` | Documentation site | Markdown, Zensical, GitHub Pages |
| `charts/` | Helm charts (alternative deployment path) | Helm 3, Go templates |

The per-module `AGENTS.md` files (`operator/AGENTS.md`, `cli/AGENTS.md`) are the authoritative references for module-specific conventions, commands, and gotchas. **This root AGENTS.md governs only cross-module boundaries: the CRD contract, the UniFi API contract, shared conventions, documentation, and agent decision-making that spans modules.**

---

## Docs Index

Read the referenced document before touching the relevant surface. Reference material lives in `docs/` and `openspec/specs/`, not inline here — when this file grows beyond ~250 lines, excess reference content moves to `docs/`.

| Fact / surface | Canonical source |
|----------------|------------------|
| Operator architecture, data flow, module boundaries | `docs/architecture.md` |
| CRD design rules (no ambiguous IDs, references, status, finalizers) | `docs/crd-conventions.md` |
| UniFi controller API contract (endpoint → operator/CLI consumer) | `docs/unifi-api.md` |
| Go language bar | `docs/go.md` |
| Security bar (credentials, RBAC, secret hygiene) | `docs/security.md` |
| Testing workflow (envtest, fake controller) | `docs/testing.md` |
| Cross-module debugging & observability | `docs/debugging.md` |
| Deployment (kustomize + Helm, OLM) | `docs/deployment.md` |
| Docs site pipeline (Zensical → GitHub Pages) | `docs/documentation.md` |
| Capability inventory (which specs exist) | `openspec/specs/` — enumerate via `openspec list` |
| Human overview, quickstart | `README.md` |
| Module-specific conventions, commands, gotchas | `operator/AGENTS.md`, `cli/AGENTS.md` |

**Single source of truth per fact:** the UniFi API contract is `docs/unifi-api.md`, the CRD design rules are `docs/crd-conventions.md`, the capability inventory is `openspec/specs/`, and the tech stack is the root `README.md`. All other documents link to these rather than restating them; when two documents disagree, the canonical source named above is authoritative.

---

## Agent Responsibilities & Boundaries

### When to Modify Which Module

| Trigger | Module(s) | Action |
|---------|-----------|--------|
| New managed UniFi resource | `operator/` | Add CRD API type (`api/v1alpha1/`) → `make manifests generate` → controller/reconciler → wiring in `cmd/` |
| New field on an existing CR | `operator/` | Add to the API type + validation markers → `make manifests generate` → reconcile the field |
| Reconcile drift / bug | `operator/` | Fix in the controller or UniFi client |
| New snapshot/adoption capability | `cli/` | Add a cobra command under `cmd/`, reusing the UniFi client and CR emitter |
| New shared Go helper (both modules need it) | `operator/` + `cli/` | Prefer the operator's `internal/` package; the CLI may vendor/import via its module boundary or duplicate a small helper |
| New CRD also needs CLI support | `operator/` + `cli/` | Operator first (type + controller), then CLI emitter output |
| Docs content update | `docs/` | Edit the relevant page; run `zensical build --strict` |
| Deployment manifest change | `operator/config/` + `charts/` | Keep kustomize and Helm consistent |
| Docs site pipeline change | `.github/` + `zensical.toml` | Edit the workflow/config |

### Core Rules

1. **Do not invent architecture.** Infer and align with existing patterns in each module (documented in the per-module `AGENTS.md` files).
2. **Prefer modifying existing controllers/types** over creating new ones. Only create a new CRD when it is a genuinely new managed resource.
3. **Keep changes minimal, composable, and testable.** Avoid sweeping refactors when a targeted fix suffices.
4. **Always analyze all modules** when a task may involve the CRD contract or the UniFi API contract.
5. **State assumptions explicitly** when requirements are ambiguous.
6. **Ensure backward compatibility** unless the task explicitly requests a breaking change. Add fields, deprecate before removing — CRD version changes follow the hub/spoke conversion pattern.
7. **Never commit secrets or credentials.** Unifi API keys, controller passwords, and kubeconfigs stay out of the tree.
8. **Stay in OpenCode** for normal work.
9. **Respect module boundaries (operator).** Controllers SHALL NOT embed raw UniFi API calls directly; they call the typed client in `internal/unifi`. The UniFi client SHALL NOT know about Kubernetes types — it returns plain Go structs. Reconcilers SHALL be idempotent and SHALL update `status` on every terminal path.

### Cross-Module Change Checklist

When implementing a change that spans modules, read `docs/crd-conventions.md` and `docs/unifi-api.md` and run `openspec list` before starting, then:

- [ ] CRD API type updated (`operator/api/v1alpha1/*_types.go`)
- [ ] `make -C operator manifests generate` re-run; `operator/config/crd/` and `operator/api/v1alpha1/zz_generated.deepcopy.go` updated
- [ ] Kubebuilder validation markers and printer columns added where needed
- [ ] Controller / reconciler updated
- [ ] UniFi client (`operator/internal/unifi/`) updated if the upstream contract changed
- [ ] `docs/unifi-api.md` updated if the contract changed
- [ ] CLI command / CR emitter updated if the CLI consumes the resource (`cli/`)
- [ ] `docs/` pages updated if behavior or CRDs changed
- [ ] kustomize (`operator/config/`) and Helm (`charts/`) manifests updated if deployment changes
- [ ] Operator gate passes (`cd operator && go build ./... && go vet ./... && golangci-lint run && go test ./...`)
- [ ] `make -C operator manifests generate` produces no diff
- [ ] CLI gate passes (`cd cli && go build ./... && go vet ./... && golangci-lint run && go test ./...`)
- [ ] Docs build passes (`zensical build --strict`)
- [ ] CRDs tested against a real or envtest API server; UniFi client tested against an `httptest.Server` fake

---

## CRD Contract Golden Rule

The `spec` of every Custom Resource is a **stable, human-authored, declarative contract**:

- **No ambiguous IDs in `spec`.** Never expose a UniFi internal `_id` or opaque numeric identifier in `spec`. Cross-resource references link another CR by its Kubernetes identity (`name` + optional `namespace`) or a typed selector.
- **`status` may hold upstream IDs** for correlation, but only in `status`, never in `spec`. Status is the only place runtime/upstream facts belong.
- **Every CRD has a status** with `conditions` (`metav1.Condition`), `observedGeneration`, and a human-readable summary. Follow the [Kubernetes API conventions](https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md).
- **Finalizers** are required for any CRD whose deletion must clean up UniFi-side state, and must be added/removed symmetrically.
- **`spec` is the desired state; the controller makes it real.** A spec field must never be a mirror of observed state.

See `docs/crd-conventions.md` for the full rules and examples.

---

## Cross-Module Coding Conventions

### File Naming

- Go files: `snake_case` for multi-word filenames (`unifi_client.go`), `lowercase` for single words, `_test.go` suffix for tests. Kubebuilder-generated files (`*_types.go`, `zz_generated.*`) keep their generated names.
- Docs: `kebab-case.md` under `docs/`.

### Imports

- Standard library first, then third-party, then local — grouped and `goimports`-formatted.
- Module paths: `operator/` and `cli/` are separate Go modules; import across the boundary is intentional only via published/shared packages, otherwise duplicate small helpers.

### Formatting & Linting

- All Go is `gofmt`/`goimports` clean and passes `golangci-lint run` with the repo config.
- YAML (kustomize, Helm, workflows) is validated by `kustomize build`, `helm lint`/`helm template`, and `actionlint` where available.
- Conventional commits with Gitmoji are used for commit messages (see the `git-commit` skill).

---

## Testing

- **Operator:** `go test ./...`. Unit-test reconcilers with the controller-runtime `fake` client and `envtest` for anything needing a real API server (CRD apply/defaulting/validation). Test the UniFi client with `net/http/httptest` — **never** a live controller.
- **CLI:** `go test ./...`. Snapshot/adoption logic is tested against an `httptest.Server` serving recorded UniFi API fixtures; golden files capture expected CR output. No live network calls.
- **Docs:** `zensical build --strict` is the gate (broken links/nav fail the build).
- **Manifests:** `make -C operator manifests generate` must produce no diff in a clean tree.
- Do not add a test framework the module does not already use.

---

## Usage Rules for This Agent

1. **Read this file first** when starting work in this workspace.
2. **Read `operator/AGENTS.md`** before modifying operator code.
3. **Read `cli/AGENTS.md`** before modifying CLI code.
4. **Read `docs/crd-conventions.md`** before changing any CRD API type.
5. **Read `docs/unifi-api.md`** before changing any UniFi controller integration.
6. **Follow the Cross-Module Change Checklist** when implementing features that span modules.
7. **Do not run CI pipelines** — they are configured in `.github/workflows/` and trigger on push/PR. Local changes do not trigger CI.
8. **When uncertain**, explicitly state assumptions before proceeding.
9. **For vague or open-ended requests**, use the `refine-prompt` skill to produce a grounded, specific prompt before executing. Always wait for user confirmation after refinement.

## graphify

This project has a knowledge graph at graphify-out/ with god nodes, community structure, and cross-file relationships.

When the user types `/graphify`, use the installed graphify skill or instructions before doing anything else.

Rules:
- For codebase questions, first run `graphify query "<question>"` when graphify-out/graph.json exists. Use `graphify path "<A>" "<B>"` for relationships and `graphify explain "<concept>"` for focused concepts. These return a scoped subgraph, usually much smaller than GRAPH_REPORT.md or raw grep output.
- Dirty graphify-out/ files are expected after hooks or incremental updates; dirty graph files are not a reason to skip graphify. Only skip graphify if the task is about stale or incorrect graph output, or the user explicitly says not to use it.
- If graphify-out/wiki/index.md exists, use it for broad navigation instead of raw source browsing.
- Read graphify-out/GRAPH_REPORT.md only for broad architecture review or when query/path/explain do not surface enough context.
- After modifying code, run `graphify update .` to keep the graph current (AST-only, no API cost).
