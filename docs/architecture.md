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

## Kind graph and reference chain

The declarative CRD set is built around one connection kind and a reference chain. The
foundation is:

| Kind | Role |
|------|------|
| `UnifiController` | The connection to one UniFi Network console: `spec.url`, the API-key `spec.secretRef`, and the TLS opt-out. It is the only kind that holds connection details, so credentials never leak into children. |
| `UnifiSite` | Adopts an existing upstream site by name (`spec.internalReference`) and points at its controller (`spec.controllerRef`). It is the ownership-root anchor and never creates or deletes upstream sites. |
| `UnifiNetwork` | A network on Integration v1, discriminated by `spec.management` (`GATEWAY`/`SWITCH`/`UNMANAGED`); it points at its site through `spec.siteRef`. |

References are name-only and resolve in the same namespace:

```text
UnifiNetwork.spec.siteRef ──▶ UnifiSite.spec.controllerRef ──▶ UnifiController
```

The full reference and ownership contract — including the rationale for name-only,
same-namespace references — lives in [CRD conventions](crd-conventions.md); this page fixes
only the graph.

### Ownership tree

Ownership is a tree rooted at the `UnifiSite`:

- The site owns every child whose **strongest** reference is `spec.siteRef`.
- A child with a stronger parent reference is owned by that parent instead — for example a WiFi
  broadcast references (and is owned by) its network, not the site.
- The site carries a **finalizer that drains its owned children before the site is removed**, so
  a child finalizer can still resolve `spec.siteRef` to reach the controller.

See [CRD conventions](crd-conventions.md#ownership-follows-the-strongest-reference) for the
ownership rules in full.

### Integration v1 data flow

The operator and CLI consume only the UniFi **Integration v1** API:

- Base path `/proxy/network/integration/v1` on the console.
- Every request authenticates with the API key in the `X-API-Key` header, read from the
  `Secret` referenced by `UnifiController.spec.secretRef`.
- `GET /v1/info` is the reachability probe and reports `applicationVersion`, which gates
  capability availability.
- Resources are identified by UUIDs and lists are pages; upstream UUIDs are recorded in
  `status` only, never in `spec`.

The endpoint → operator method → CLI map is [UniFi API contract](unifi-api.md).

### Implemented vs designed kinds

This change implements `UnifiController`, `UnifiSite`, and `UnifiNetwork` end to end — API
types, generated CRD manifests, the Integration v1 client, and reconcilers. The remaining
declarative kinds (WiFi broadcasts, firewall zones/policies/ordering, ACL rules/ordering, DNS
policies, traffic matching lists, switching, VPN, RADIUS profiles, and device tags) are
**designed as contracts only**: they have no Go types or CRD manifests yet and are implemented
by later changes. Treat those designs as intent, not as a shipped surface.

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
