# UniFi-Operator

A Kubernetes operator that manages a UniFi network stack declaratively through Custom
Resources. Point it at a UniFi controller, declare the network you want as CRs, and the
operator reconciles the controller to match — with a CLI that can snapshot an existing
controller into adoptable CRs and a documentation site published to GitHub Pages.

## Modules

| Directory | Role | Tech |
|-----------|------|------|
| `operator/` | Kubernetes operator — CRD API types, controllers, UniFi client, kustomize manifests | Go, kubebuilder, controller-runtime, envtest, kustomize |
| `cli/` | Snapshot / adoption CLI — reads a live UniFi controller and emits CRs | Go, cobra, separate Go module |
| `docs/` | Reference library published as a docs site | Markdown, Zensical, GitHub Pages |
| `charts/` | Helm charts (alternative deployment path) | Helm 3, Go templates |

## Design goals

- **Declarative CRDs.** Every managed UniFi resource is a Custom Resource whose `spec` is a
  stable, human-authored contract. No opaque UniFi `_id`s leak into `spec`; cross-resource
  links use Kubernetes identity.
- **Rich status.** Every CR reports `conditions`, `observedGeneration`, and a human-readable
  summary so drift and progress are visible from `kubectl`.
- **Adoption by snapshot.** The CLI reads a live controller and emits a candidate set of CRs,
  turning a brownfield deployment into a declarative one.
- **Documentation as a first-class artifact.** The reference library in `docs/` is canonical
  and published to GitHub Pages.
- **Best-practice operator.** Built with kubebuilder and controller-runtime, tested with
  envtest and HTTP fakes, deployable via kustomize and Helm.

## Getting started

> The operator is under active development; the module layout is established but the first
> CRDs and controllers are landing incrementally. See `README.md` and `docs/architecture.md`
> as the design of record.

```bash
# Operator
cd operator
make manifests generate
make test

# CLI
cd cli
go build ./...
go test ./...

# Docs
cd docs   # or repo root
zensical serve
```

## Documentation

- **Root `AGENTS.md`** — cross-module rules, boundaries, CRD golden rule, change checklist
- **`docs/`** — reference library: architecture, CRD conventions, UniFi API contract, Go and
  security bars, testing, debugging, deployment, docs pipeline
- **`operator/AGENTS.md`, `cli/AGENTS.md`** — per-module conventions and commands
- **`openspec/specs/`** — canonical specifications for all capabilities (enumerate via
  `openspec list`)

## Documentation site

The docs are built with [Zensical](https://zensical.org/) and published to GitHub Pages by
`.github/workflows/docs.yml`. Local gate: `zensical build --strict`.

## License

See [LICENSE](LICENSE).
