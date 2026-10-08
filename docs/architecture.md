# Architecture

This document is the canonical architecture reference for the UniFi-Operator workspace. It
describes the modules, their boundaries, the reconcile model, and how the CLI and docs relate
to the operator. The CRD design rules themselves live in [CRD conventions](crd-conventions.md);
the upstream contract lives in [UniFi API contract](unifi-api.md).

## Modules

| Module | Role | Tech |
|--------|------|------|
| `operator/` | Kubernetes operator — CRD API types, controllers, UniFi client | Go, kubebuilder, controller-runtime, envtest |
| `cli/` | Snapshot / adoption CLI | Go, cobra, separate Go module |
| `docs/` | This reference library | Markdown, Zensical, GitHub Pages |
| `config/` | Kustomize manifests | kustomize (kubebuilder default) |
| `charts/` | Helm charts (alternative path) | Helm 3, Go templates |

The per-module `AGENTS.md` files (`operator/AGENTS.md`, `cli/AGENTS.md`) hold module-specific
conventions. The root `AGENTS.md` governs cross-module boundaries.

## Operator data flow

```
        kubectl apply                reconcile loop                 UniFi controller
  ┌───────────────────┐        ┌──────────────────────┐        ┌────────────────────┐
  │  Custom Resources │  ───▶  │  manager (cmd/main)   │  ───▶  │  https://<host>    │
  │  (spec = desired) │        │   → reconciler        │  HTTP  │  /proxy/network/…  │
  └───────────────────┘        │   → unifi client      │  ◀───  └────────────────────┘
           ▲                   │   → status update     │
           │                   └──────────────────────┘
           │                              │
           └──────── status ◀─────────────┘
```

1. A user applies CRs. The `spec` is the desired state.
2. The manager watches each kind and calls its reconciler with a `reconcile.Request`.
3. The reconciler reads the CR, calls the typed UniFi client in `internal/unifi/`, and creates,
   updates, or deletes the corresponding controller objects.
4. The reconciler writes `status` (conditions, observed generation, upstream correlation IDs)
   back to the CR, then requeues only when needed.

**Boundaries.**

- Controllers never build raw HTTP requests; they call `internal/unifi`.
- `internal/unifi` never imports Kubernetes types — it returns plain Go structs.
- `spec` is authored by humans; `status` is written only by the controller.

See [CRD conventions](crd-conventions.md) for the contract rules these boundaries enforce.

## Reconcile model

Reconcilers are level-based, not edge-based. On each invocation the reconciler:

1. Fetches the object; on `NotFound`, cleans up if a finalizer is present, else returns.
2. Ensures the finalizer if deletion cleanup is needed.
3. Reads upstream state from the UniFi controller.
4. Applies the difference between `spec` and upstream (create/update).
5. Handles deletion when `deletionTimestamp` is set and the finalizer is present.
6. Updates `status` on every terminal path.

Every step must be safe to run repeatedly with no side effects when nothing changed. Transient
UniFi errors are returned (not swallowed) so controller-runtime applies exponential backoff.

## CLI data flow

```
  UniFi controller  ──HTTP──▶  cli/snapshot  ──▶  emit/*.yaml (candidate CRs)
                                       │
                                       └──▶ operator (kubectl apply) ──▶ reconcile
```

The CLI reads the same endpoints as the operator (see [UniFi API contract](unifi-api.md)),
projects controller objects onto CR shapes, and emits YAML. The emitted CRs follow the same
[CRD conventions](crd-conventions.md): no upstream `_id`s leak into `spec`.

## Contract synchronization

The UniFi controller API is the upstream source of truth. **`docs/unifi-api.md` is the
workspace's canonical mapping** of each consumed endpoint to the operator method and CLI
consumer. When the upstream contract changes, that document, the operator client, and the CLI
consumer change together. The root `AGENTS.md` Cross-Module Change Checklist drives this.

## Related

- [CRD conventions](crd-conventions.md)
- [UniFi API contract](unifi-api.md)
- [Testing](testing.md)
- [Deployment](deployment.md)
