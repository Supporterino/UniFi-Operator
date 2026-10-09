## Context

See `proposal.md` for motivation. The current state that constrains this design:

- `operator/api/v1alpha1/unifinetwork_types.go` is a scaffold whose `spec` (`site`, `name`,
  `vlan`, `subnet`, `enabled`) mirrors the legacy REST `networkconf` object, and whose field
  comment admits the reconciler only matches by name.
- `docs/unifi-api.md` documents the legacy REST surface (`/proxy/network/api/s/{site}/rest/...`,
  `{meta,data}` envelope). It is declared the source of truth for the upstream contract.
- `docs/crd-conventions.md` fixes the CRD golden rules: no opaque IDs in `spec`, references by
  Kubernetes identity, rich `status`, symmetric finalizers, marker-based validation.
- The UniFi Integration v1 API is typed CRUD over `/proxy/network/integration/v1`, authenticated
  with an API-key header, keyed by UUIDs, paginated, and discriminates most resources with a
  `type`/`management` string. The contract is frozen to the controller's own OpenAPI document at
  **spec version v10.4.57** (fetched from `developer.ui.com`). The surface is version-gated: the
  Official API requires app ≥10.1.78, networks CRUD ≥10.0.162, and firewall/DNS ≥10.1.84.
- `GET /v1/info` returns `applicationVersion`; `GET /v1/sites` maps a site name
  (`internalReference`) to its UUID.
- Group/version is `unifi.supporterino.de/v1alpha1`.

## Goals / Non-Goals

**Goals:**

- Establish the controller -> site -> resource reference model and the ownership/membership
  rules once, so every future CRD is a mechanical extension.
- Define the `UnifiNetwork` contract against Integration v1 spec v10.4.57 and implement the
  foundation (`UnifiController`, `UnifiSite`, `UnifiNetwork`) end to end.
- Capture every remaining declarative resource as a buildable-later contract.

**Non-Goals:**

- No conversion webhooks or a new API version; `v1alpha1` is pre-release.
- No legacy REST fallback, no session auth, no cloud-connector base URL.
- No controller/client for any resource beyond the implemented slice.
- No runtime/observed resources as CRDs (devices, clients, stats, WANs, catalogs, vouchers).
- No cross-namespace references; the API-key `Secret` is same-namespace.

## Decisions

### D1 - Integration v1 is the sole API surface, frozen to spec v10.4.57

Base path `/proxy/network/integration/v1`, API-key auth, responses are plain JSON (lists are
`{count,data,limit,offset,totalCount}` pages). Errors are a **flat** body —
`{statusCode,statusName,code,message,timestamp,requestPath,requestId}` (the OpenAPI `Error
Message` schema), not a nested `{"error":{...}}`. `GET /v1/info` returns `{applicationVersion}`
and is the reachability + version probe. The contract is frozen to spec **v10.4.57**, with the
minimum app version recorded per capability (Official API ≥10.1.78, networks CRUD ≥10.0.162,
firewall/DNS ≥10.1.84); `UnifiController.status.applicationVersion` reports the detected version
and gates which capabilities are considered available.

- **Why:** the linked controller spec and Ubiquiti's investment are here; it is typed CRUD,
  which maps 1:1 onto CRDs. Legacy `networkconf` mixes read and write shapes and would freeze us
  on a dead surface.
- **Alternatives:** legacy REST only (rejected - dead end); both (rejected - doubles the client
  and the contract with no operator benefit).
- **Consequence:** `docs/unifi-api.md` is rewritten: legacy rows are removed and replaced with
  the v10.4.57 endpoint -> operator-method -> CLI map. The client in
  `operator/internal/unifi/` gains a v1 base-path/header, flat-error mapping, and page decoding;
  it still returns plain structs and never imports Kubernetes types.
- **Verify on target console:** the API-key header casing — the OpenAPI/docs say `X-API-Key`
  while community references stress exact `X-Api-Key`; confirm before implementing.

### D2 - Reference chain is `siteRef` -> `controllerRef`

```
 UnifiNetwork.spec.siteRef ---> UnifiSite.spec.controllerRef ---> UnifiController
   (name)                         (name)                           url, secretRef, TLS
```

- **Why:** one place holds the connection; children carry only site identity. It composes with
  ownership (D4) and keeps secrets out of every resource.
- **Alternatives:** flat `controllerRef` on every child (rejected - connection leaks everywhere);
  optional `siteRef` (rejected - a site is always meaningful).
- **Shape:** references are `{name, kebab}` and are **same-namespace only** (D3); no `namespace`
  field is exposed, because a cross-namespace reference cannot be an `ownerReference` (D4) and
  would complicate RBAC.

### D3 - All CRDs are namespaced with same-namespace references

- **Why:** matches `docs/crd-conventions.md`, least-privilege RBAC (namespaced `Role` where
  possible), and the existing manager layout. All references resolve in the same namespace.
- **Alternative:** cluster-scoped controller (rejected - broader RBAC, no isolation);
  optional-namespace references (rejected - illegal as ownerReferences and unnecessary).

### D4 - Ownership follows the strongest reference; the site drains its children

Ownership is a tree rooted at the site. `UnifiSite` **adopts an existing upstream site by name**
(`spec.internalReference`) and never creates or deletes it. It owns every child that references
it by `spec.siteRef`; a child whose strongest reference is another CR is owned by that CR instead
— `UnifiWifiBroadcast` references (and is owned by) its `UnifiNetwork`. Each reconciler sets its
own `ownerReference` to its immediate parent with `controllerutil.SetControllerReference`; the
site controller never reaches into child kinds.

- **Why:** each child keeps its own `status`/conditions, matches the CLI's one-CR-per-object
  snapshot model, and gets Kubernetes GC for free. The site is an anchor, not a bundle.
- **Site finalizer (deletion ordering):** because a child finalizer needs `spec.siteRef` to reach
  the controller and delete upstream state, the site gets a **finalizer that drains owned
  children first**. Blocking the site keeps it (and its `controllerRef` chain) resolvable until
  every child has finished; the site reconciler initiates deletion of its direct ownerReference
  children (GC only starts after the owner is actually gone, so a blocked owner must drive it).
  Once no children remain, the site finalizer is removed and the adoption binding disappears.
- **Alternative:** embedded lists in `UnifiSite.spec` (rejected - collapses per-object status and
  the reference idiom); background GC without a site finalizer (rejected - deadlocks the child
  finalizer once the site is gone).

### D5 - Membership is owned by `UnifiFirewallZone`; `UnifiNetwork` reports zone in status

- **Why:** `FirewallZone.networkIds` is the editable upstream membership list. The gateway
  network create/update body *also* accepts a `zoneId` (v10.4.57), but authoring both sides would
  create an unresolvable cycle and racy reconciles, so the contract deliberately picks a single
  writer.
- **Alternative:** network owns its zone (`spec.zoneRef`) (rejected - two writers); both +
  conflict detection (rejected - avoidable complexity).
- **Consequence:** `UnifiNetwork.spec` has no `zoneId`/`zoneRef`; the controller omits `zoneId`
  on writes and the resolved zone lands in `status`. This also means the implemented
  `UnifiNetwork` does not depend on the not-yet-implemented `UnifiFirewallZone`.
- **Switch binding:** the switch-managed variant accepts a `deviceId` (opaque UUID). It is
  modeled as a typed device selector (D8), never a raw ID in `spec`.

### D6 - Ordered collections get dedicated ordering CRDs

- **Why:** ordering is a separate upstream write, order-dependent, and global in scope, so a
  per-object field is racy (the ACL `index` field is even deprecated upstream: "use the dedicated
  ACL rule reordering endpoint"). An ordering CR is the single writer.
- **Shape:**
  - Firewall: one `UnifiFirewallPolicyOrdering` per `(sourceZoneRef, destinationZoneRef)` pair,
    matching the API's query parameters. Upstream the order is partitioned around system-defined
    policies — `orderedFirewallPolicyIds{ beforeSystemDefined[], afterSystemDefined[] }` — so the
    CR exposes **two ordered lists** mirroring the DTO. System-defined policies are anchored and
    never reordered.
  - ACL: one `UnifiAclRuleOrdering` per site, exposing the flat `orderedAclRuleIds[]`.
  - **Drift:** user policies/rules present upstream but absent from the ordering CR are reported
    as drift in `status`, not silently reordered.
- **Alternatives:** `priority` int per rule (rejected - conflict resolution); a single firewall
  list with a placement enum (rejected - loses the explicit before/after-system partition); defer
  ordering (rejected - "must be first" rules become inexpressible).
- **Uniqueness (runtime, no webhook):** the zone-pair triple / site is unique in intent, but CRD
  markers and CEL cannot enforce cross-object uniqueness. The controller detects duplicate
  ordering CRs, sets `Ready=False` on all of them, and applies **no** order until a single
  unambiguous CR remains. This matches the marker-first validation rule in `operator/AGENTS.md`.

### D7 - Discriminated unions become enum + variant structs + CEL

Go CRDs have no sum types. Model each union as an enum discriminator plus variant sub-structs,
validated with `+kubebuilder:validation:XValidation` (CEL).

```go
// management discriminator; one variant struct per value (spec v10.4.57)
type UnifiNetworkSpec struct {
    SiteRef    CoreRef `json:"siteRef"`
    Management string  `json:"management"` // enum GATEWAY|SWITCH|UNMANAGED
    Gateway    *GatewayNetworkOptions `json:"gateway,omitempty"`
    Switch     *SwitchNetworkOptions  `json:"switch,omitempty"`
    // +kubebuilder:validation:XValidation:rule="self.management == 'GATEWAY' ? has(self.gateway) : !has(self.gateway)",message="gateway fields require management GATEWAY"
    // +kubebuilder:validation:XValidation:rule="self.management == 'SWITCH' ? has(self.switch) : !has(self.switch)",message="switch fields require management SWITCH"
}
```

**Pinned union (spec v10.4.57):** common to every `management` value are `name`, `enabled`,
`vlanId` (1..4009; 1 is the default network), and `dhcpGuarding.trustedDhcpServerIpAddresses`.
`GATEWAY` adds `cellularBackupEnabled`, `internetAccessEnabled`, `ipv4Configuration`
(`GatewayManagedIPv4Configuration`), optional `ipv6Configuration`, `isolationEnabled`, and
`mdnsForwardingEnabled` (upstream also accepts `zoneId`, deliberately not exposed per D5).
`SWITCH` adds `cellularBackupEnabled`, `isolationEnabled`, `ipv4Configuration`
(`SwitchManagedIPv4Configuration`), and a device binding modeled as a typed device selector
(D8). `UNMANAGED` adds nothing. The gateway booleans and `ipv4Configuration` are required
upstream for `GATEWAY`, so they are required in the variant struct.

- **Why:** keeps a flat, documentable spec that mirrors the API's discriminator while the API
  server rejects mismatches before the controller sees them (per `docs/security.md` "validate at
  the API boundary").
- **Alternatives:** one flat struct with all fields (rejected - admits nonsensical combinations);
  a CRD per variant (rejected - kind explosion).

### D8 - Typed selectors for non-CR references

Devices, DPI applications/categories, countries, and built-in firewall zones have no CR and
never will (or must not be overwritten). References to them are typed selector structs resolved
by the controller, never raw UUIDs in `spec`.

- **Why:** preserves the golden rule for references that cannot use Kubernetes identity.
- **Alternatives:** raw-UUID escape hatch (rejected - leaks opaque IDs into the contract); drop
  all such filters (rejected - removes real capability the user chose to keep).
- **Detail:** the concrete selector fields are an open question (below); the contract is "typed
  selector, no opaque IDs". This covers the network switch `deviceId` (D5) and policy built-in
  zones (D13).

### D9 - Credentials and secrets are always Secret references

- `UnifiController.spec.secretRef` holds the API key; TLS verifies by default with an explicit
  `insecureSkipVerify` opt-out.
- WiFi passphrases, VPN pre-shared keys, and RADIUS shared secrets are `Secret` references.
- **Namespace:** every `Secret` resolves in the same namespace as its owner CR (D3);
  cross-namespace secret references are not supported.
- **Why:** `docs/security.md` - never inline, never logged, never in `status`; fail closed on
  resolution failure.
- **Alternatives:** inline `string` fields (rejected - leaks secrets into the object and etcd
  backups).

### D10 - Scaffold boundary: specs/docs for all, Go types for the implemented slice

- **Why:** captures the full contract design without generating ~18 kinds' worth of unmaintained
  manifests. Only `UnifiController`, `UnifiSite`, `UnifiNetwork` get `*_types.go`, deepcopy, and
  CRD manifests now; `make manifests generate` stays a clean, reviewable diff.
- **Consequence:** the design-only capabilities are contracts, not generated schemas; a later
  change implements each. Per-kind `spec`/`status` field tables in `docs/` are written when each
  kind is implemented.

### D11 - Status-only fields and fail-closed on system objects

`metadata.origin` (`USER_DEFINED`/`SYSTEM_DEFINED`/`DERIVED`/`ORCHESTRATED`), upstream `id`,
server `index`, and `default` are observed, not authored.

- **Why:** they either are opaque IDs or are computed; per the golden rule they belong in
  `status`. System/derived objects may not be fully editable, so the controller fails closed
  (`Ready=False`) rather than overwriting them.
- **Alternative:** treat `index`/`default` as authorable (rejected - mirrors observed state).

### D12 - Implementation slice and wiring

- New reconcilers under `operator/internal/controller/` (`unificontroller_controller.go`,
  `unifisite_controller.go`) plus the reworked `unifinetwork_controller.go`; wired in
  `operator/cmd/main.go`.
- Integration v1 client types under `operator/internal/unifi/` (plain structs, context-aware,
  `httptest`-tested). Endpoint map synced into `docs/unifi-api.md`.
- Samples in `operator/config/samples/`; CRDs under `operator/config/crd/`; kustomize/Helm kept
  consistent.

### D13 - Firewall policies reach built-in zones via a typed selector

Built-in firewall zones (for example the default `Internal`/`External` zones) have no CR and
`metadata.configurable=false`; they must not be overwritten. A `UnifiFirewallPolicy` therefore
references configurable zones by Kubernetes identity (`zoneRef` -> `UnifiFirewallZone`) and
built-in zones through a **typed built-in-zone selector** (an enum/name resolved by the
controller), so `spec` still carries no opaque UUID.

- **Why:** preserves the golden rule while allowing the common "any zone -> External" policies.
- **Alternatives:** read-only adopted `UnifiFirewallZone` CRs for built-ins (rejected - more
  objects and reconcile surface for data the user never authors); raw `zoneId` (rejected -
  opaque ID in `spec`).

## Risks / Trade-offs

- **Version skew / capability gating** (controller app newer/older than the frozen v10.4.57
  spec) -> `UnifiController.status.applicationVersion` records the detected version; the client
  tolerates unknown fields, and a capability below its minimum app version (Official API
  <10.1.78, networks <10.0.162, firewall/DNS <10.1.84) fails closed with a `Ready=False` reason
  rather than guessing.
- **`metadata.origin` semantics** (system-defined objects partially editable) -> fail closed;
  never overwrite system/derived objects.
- **Ordering CR conflicts** (two writers for one zone pair / site) -> runtime uniqueness check +
  a failing condition; only the authoritative CR writes.
- **CEL union validation complexity** -> keep variant sub-structs small and rule-per-discriminator;
  prefer `+optional` pointers.
- **Contract/schema drift for design-only kinds** -> specs are the source of truth; a later change
  implements each and re-runs `make manifests generate`.
- **BREAKING `UnifiNetwork` reshape** -> acceptable pre-release; update samples and the CLI
  snapshot emitter in the same change to avoid a broken tree.
- **Secret resolution failure mid-reconcile** -> set a terminal failing condition; never fall
  back to a default credential.

## Migration Plan

No live migration: `v1alpha1` is pre-release and there is no deployed population. The scaffold
`UnifiNetwork` type is replaced in place (no conversion webhook). Sequence: add `UnifiController`
and `UnifiSite` types -> rework `UnifiNetwork` -> `make manifests generate` (no diff) -> new
client + reconcilers -> update samples, `docs/unifi-api.md`, `docs/crd-conventions.md`,
`docs/architecture.md` -> update the CLI snapshot emitter. Rollback is reverting the change; no
upstream state is created until a CR is applied.

## Resolved Questions

- **Gateway union** is pinned to spec v10.4.57 (D7); `ipv4Configuration`/`ipv6Configuration`
  sub-struct depth is taken from the linked OpenAPI document at implementation time.
- **Cross-namespace `Secret`** is not supported; all references are same-namespace (D3, D9).
- **Typed-selector shapes** for device, DPI application/category, country, and built-in zones are
  deferred: the contract fixes "typed selector, no opaque ID" (D8, D13); concrete fields land
  with each design-only kind.
- **Per-kind docs field tables** are written when each kind is implemented, not now (D10).
- **Network switch device binding** is a typed device selector expressed as
  `UnifiNetwork.spec.switch.deviceTagRef` (a `CoreRef`, name-only and same-namespace) to a
  `UnifiDeviceTag` (D5/D8). Because `UnifiDeviceTag` is design-only, the network reconciler fails
  closed for `management: SWITCH` until the device-tag kind lands; the client surface is **not**
  expanded with a device lookup. This resolves the "typed-selector shapes ... deferred" open
  question for the one implemented kind that needs it.

## Open Questions

- The precise target app version to test against (the contract is frozen to v10.4.57; the exact
  console build may expose a subset — see capability gating in Risks).
