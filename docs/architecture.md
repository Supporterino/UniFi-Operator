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
| `UnifiNetwork` | A network on Integration v1, discriminated by `spec.management` (`GATEWAY`/`SWITCH`/`UNMANAGED`); it points at its site through `spec.siteRef`. A `SWITCH` network binds its managing device through a name-based `spec.switch.deviceTag` selector, never a raw device UUID. |
| `UnifiFirewallZone` | A firewall zone on the site; it points at its site through `spec.siteRef` and declares member networks through `spec.networkRefs`. It is the single writer of upstream zone network membership. |
| `UnifiWifiBroadcast` | A WiFi broadcast (SSID) on Integration v1, discriminated by `spec.type` (`STANDARD`/`IOT_OPTIMIZED`); it references its owning `UnifiNetwork` through `spec.networkRef` and resolves the site transitively. Personal passphrases are `Secret` references and enterprise security references a `UnifiRadiusProfile`. |

References are name-only and resolve in the same namespace:

```text
UnifiNetwork.spec.siteRef ──▶ UnifiSite.spec.controllerRef ──▶ UnifiController
UnifiFirewallZone.spec.siteRef ──▶ UnifiSite
UnifiFirewallZone.spec.networkRefs ──▶ UnifiNetwork
UnifiWifiBroadcast.spec.networkRef ──▶ UnifiNetwork ──▶ UnifiSite ──▶ UnifiController
```

The full reference and ownership contract — including the rationale for name-only,
same-namespace references — lives in [CRD conventions](crd-conventions.md); this page fixes
only the graph.

### Firewall zones and device selection

`UnifiFirewallZone` is the **single writer** of upstream zone network membership: it declares
member networks through `spec.networkRefs`, and `UnifiNetwork.spec` carries no zone reference.
Only user-defined (`USER_DEFINED`) upstream zones are created, updated, or deleted; a
system-defined zone is adopted **read-only** and fails closed rather than being mutated.
Membership and zone-name uniqueness are enforced at runtime, because CRD markers cannot express
cross-object uniqueness. The rules live in [CRD conventions](crd-conventions.md); the zone write
surface is in the [UniFi API contract](unifi-api.md).

`UnifiNetwork.spec.switch` binds a switch-managed network to its managing device through a
name-based `deviceTag` selector, resolved against the read-only device-tags list. Device tags
are read-only upstream, so there is **no `UnifiDeviceTag` kind** — no API type, CRD manifest, or
controller is generated for it. An unknown tag, or a tag that does not resolve to exactly one
device, fails closed. A zone membership write re-reconciles the affected networks, which report
the resolved zone in `UnifiNetwork.status.zoneID`.

### WiFi broadcast references and fail-closed dependencies

`UnifiWifiBroadcast` references its owning `UnifiNetwork` through `spec.networkRef` (name-only,
same namespace) and resolves the site **transitively** — `networkRef` → `UnifiNetwork.spec.siteRef`
→ `UnifiSite.spec.controllerRef` — so the broadcast carries no `siteRef` of its own. It is owned
by the network, not the site. Raw upstream identifiers are replaced by references rather than
exposed in `spec`:

- **Device scope** uses `spec.deviceTags`, name-based `DeviceTagSelector` entries resolved against
  the read-only device-tags list; the raw `DEVICES` (`deviceIds`) filter is not exposed. An
  unknown or ambiguous tag fails closed.
- **Personal passphrases** (`passphrase`, `presharedKeys[].passphrase`) are same-namespace
  `Secret` key selectors, never inline. The controller reads each `Secret` through the uncached
  `APIReader` and never records the value in `status`; a missing secret or key fails closed.
- **Enterprise security** (`WPA2`/`WPA3` enterprise variants) references its RADIUS profile
  through `spec.securityConfiguration.<variant>.radiusConfiguration.radiusProfileRef` (for example
  `spec.securityConfiguration.wpa2Enterprise.radiusConfiguration.radiusProfileRef`) to a
  `UnifiRadiusProfile`, never the opaque profile UUID. Because `UnifiRadiusProfile` has no Go type
  yet, an enterprise broadcast fails closed (`Ready=False`) with no upstream mutation until that
  kind lands.

### Ownership tree

Ownership is a tree rooted at the `UnifiSite`:

- The site owns every child whose **strongest** reference is `spec.siteRef`.
- A child with a stronger parent reference is owned by that parent instead — for example a WiFi
  broadcast references (and is owned by) its network, not the site.
- The site carries a **finalizer that drains its owned children before the site is removed**, so
  a child finalizer can still resolve `spec.siteRef` to reach the controller.

The drain is recursive and uses one shared helper (`drainOwnedChildren`): a parent lists exactly
the child kinds it owns, initiates their deletion, and keeps its finalizer until none remain. A
`UnifiSite` drains its `UnifiNetwork` and `UnifiFirewallZone` children; a `UnifiNetwork` drains
the `UnifiWifiBroadcast` children it owns; the site finalizer is removed last so each child's
reference chain stays resolvable while its own finalizer deletes upstream state. The unwind is
`UnifiSite` → `UnifiNetwork` → `UnifiWifiBroadcast`.

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

The implemented kinds are `UnifiController`, `UnifiSite`, `UnifiNetwork`,
`UnifiFirewallZone`, and `UnifiWifiBroadcast` — API types, generated CRD manifests, the
Integration v1 client, and reconcilers. The remaining declarative kinds (firewall
policies/ordering, ACL rules/ordering, DNS policies, traffic matching lists, switching kinds, VPN,
and RADIUS profiles) are **designed as contracts only**: they have no Go types or CRD manifests
yet and are implemented by later changes. Treat those designs as intent, not as a shipped
surface.

Device tags are not a kind at all: the upstream surface is read-only, so **no `UnifiDeviceTag`
Custom Resource is generated**. Consumers select devices through the name-based device-tag
selector resolved from the read-only list (see [CRD conventions](crd-conventions.md)).

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
