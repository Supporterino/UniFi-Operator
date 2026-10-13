# Design

## Context

See `proposal.md` — Why. The design constraint that shapes everything here: **the contract
already exists.** The archived `design-crds-for-unifi-integration-v1` change authored forward specs
for every remaining kind into `openspec/specs/` (`unifi-radius`, `unifi-traffic-matching`,
`unifi-firewall`, `unifi-acl`, `unifi-dns`, `unifi-switching`, `unifi-vpn`), and the current
implementation covers only `UnifiController`, `UnifiSite`, `UnifiNetwork`,
`UnifiFirewallZone`, and `UnifiWifiBroadcast`. So this is an **implementation change**: the spec
deltas are the `unifi-wifi` enterprise flip (`specs/unifi-wifi/spec.md`) plus corrections to the
three read-only capabilities (`specs/unifi-radius`, `specs/unifi-switching`, `specs/unifi-vpn`),
because the frozen v10.4.57 surface exposes RADIUS, switching, and VPN read-only and their specs of
record assumed a write surface. Every other requirement is already the spec of record.

The existing code establishes every primitive the new kinds reuse:

- Reference chain and site resolution — `operator/internal/controller/unifinetwork_controller.go`,
  `operator/internal/controller/connection.go` (`ConnectionResolver`, `readerOrCached`).
- Recursive drain — `operator/internal/controller/drain.go` (`drainOwnedChildren`).
- Duplicate-conflict detection — `operator/internal/controller/unififirewallzone_controller.go`
  (`zoneConflict`, `zoneNameClaimants`, `conflictedNetworks`).
- Status/conditions — `operator/internal/controller/conditions.go`.
- Variant unions — `operator/api/v1alpha1/unifiwifibroadcast_types.go`,
  `operator/api/v1alpha1/unifinetwork_types.go`.
- Version gating — `operator/internal/controller/versions.go` (`CheckCapability`).
- Secret reads via uncached reader — the `UnifiWifiBroadcast` passphrase path in
  `operator/internal/controller/unifiwifibroadcast_controller.go`.
- Test doubles — `operator/internal/controller/stub_client_test.go`;
  CLI projection — `cli/internal/snapshot/snapshot.go`.

## Goals / Non-Goals

**Goals:**

- Ship all 12 remaining kinds as full vertical slices (type → generated CRD → client →
  reconciler → tests → CLI → deployment → docs).
- Introduce the two shared primitives once and reuse them, rather than per-kind copies.
- Keep every new `spec` free of opaque upstream identifiers and every reference same-namespace,
  name-only, resolving by Kubernetes identity.

**Non-Goals:**

- Authoring new spec capabilities or requirements (the contract exists).
- Extending beyond the frozen Integration v1 v10.4.57 endpoint map.
- Reworking the five shipped kinds beyond the `unifi-wifi` requirement flip.

## Decisions

### D1 — Build in dependency waves inside one change

Even as a single change, the build order is fixed by dependencies, so tasks are phased
P1→P5 (see `tasks.md`). Rationale: firewall policy and ACL cannot compile without the
`UnifiTrafficMatchingList` filter target, and the `unifi-wifi` enterprise flip cannot land before
`UnifiRadiusProfile`. *Alternative considered:* per-capability changes (rejected by the user in
favour of one mega-change). *Consequence:* the large `make manifests generate` diff lands once;
per-phase gates compensate.

### D2 — Shared ordering primitive: duplicate-conflict + drift

`UnifiFirewallPolicyOrdering` (per source/destination zone pair) and `UnifiAclRuleOrdering`
(site-global) share the same behavior: one authoritative object per scope; duplicates set
`Ready=False` on all claimants and apply no order; upstream user objects absent from the ordering
object are **reported as drift** in `status`, never silently reordered; system-defined policies are
anchored and never reordered. Model the conflict half on the existing
`unififirewallzone_controller.go` claimant pattern, and factor the shared logic into one helper used
by both reconcilers rather than duplicating it. *Alternative considered:* an admission webhook to
enforce uniqueness — rejected because the repo has no webhook infrastructure and CRD markers cannot
express cross-object uniqueness; runtime detection matches the existing zone-name precedent.

### D3 — Typed-filter strategy

Firewall policies and ACL rules express filters through typed structures, never opaque IDs. Where
the spec names a reusable list (`unifi-traffic-matching` — "Matching list is referenceable by other
resources"), a policy references a `UnifiTrafficMatchingList` by Kubernetes identity and the
controller resolves it to the upstream list; an unresolvable reference fails closed
(`Ready=False`, no upstream mutation) per the firewall policy "Unresolvable filter fails closed"
scenario. ACL rules carry their own inline typed `IPV4`/`MAC` filters per `unifi-acl`. *Alternative
considered:* inline-only filters everywhere — rejected because `unifi-traffic-matching` exists
precisely to be referenced.

### D4 — Reference and ownership strategy (uniform across new kinds)

- Every new kind is **namespaced** and references its parent site through `spec.siteRef` (name-only,
  same namespace), exactly like `UnifiFirewallZone`. The `UnifiSite` recursive drain
  (`drain.go`) is extended to list and drain the new site-scoped children before the site finalizer
  is removed.
- Cross-CR references use Kubernetes identity: firewall policy → `UnifiFirewallZone` (configurable
  zones) or a typed **built-in-zone selector** (system zones); firewall policy → traffic matching
  list; enterprise broadcast → `UnifiRadiusProfile`.
- Device membership for the **writable** kinds (ACL enforcing scope) uses the existing name-based
  device-tag selector resolved against the read-only device-tags list; there is no
  `UnifiDeviceTag` kind. For the **adoption** kinds (switch stacks, MC-LAG domains) device
  membership is read-only upstream and is observed in `status`, never authored in `spec`. The one
  exception is `UnifiSwitchingLag`'s composite adoption selector: because the frozen surface exposes
  LAGs without a name, it authors a typed member selector (device tags + port indexes) used only to
  **match** the single upstream LAG — never to write device membership upstream.
- **Secrets** (the WiFi passphrase path) are same-namespace `Secret` key selectors, read through
  the uncached `APIReader` and never written to `status`. The read-only adoption kinds
  (`UnifiRadiusProfile`, `UnifiVpnServer`, `UnifiSiteToSiteTunnel`) author **no** secret at all,
  because the frozen surface exposes no credential to write.
- **Upstream IDs** (profile/list/policy/rule/record/lag/stack/server/tunnel UUIDs) appear only in
  `status`, never in `spec`.

### D5 — `spec`/`status` shape per kind

Each kind follows the established shape: `spec.type`/variant discriminator (where the upstream
union has one) modeled as a discriminator field + variant structs + CEL validation, mirroring
`unifiwifibroadcast_types.go`; `status` carries `conditions`, `observedGeneration`, a
human-readable summary, resolvable upstream IDs, and resolved zone/site IDs.

| Kind | Site ref | Discriminator | Notable `spec` | Key `status` |
|------|----------|---------------|----------------|--------------|
| `UnifiRadiusProfile` | `siteRef` | — | **adoption:** `name` (no authored config/secret) | profile UUID, conditions |
| `UnifiTrafficMatchingList` | `siteRef` | `type`: `IPV4_ADDRESSES`/`IPV6_ADDRESSES`/`PORTS` | typed values/ranges | list UUID, conditions |
| `UnifiFirewallPolicy` | `siteRef` | — | source/destination zone refs (configurable + built-in-zone selector); action; typed filters | policy UUID, conditions |
| `UnifiFirewallPolicyOrdering` | `siteRef` | — | zone-pair ref; `beforeSystemDefined[]`/`afterSystemDefined[]` | drift set, conditions |
| `UnifiAclRule` | `siteRef` | `type`: `IPV4`/`MAC` | action; typed source/destination + protocol filters; enforcing-device selector | rule UUID, conditions |
| `UnifiAclRuleOrdering` | `siteRef` | — | site-global ordered rule refs | drift set, conditions |
| `UnifiDnsPolicy` | `siteRef` | `type`: record/forward-domain | variant-specific fields | record UUID, conditions |
| `UnifiSwitchingLag` | `siteRef` | `type`: `LOCAL`/`MULTI_CHASSIS`/`SWITCH_STACK` | **adoption:** typed member selector (members/mode observed) | lag UUID, conditions |
| `UnifiSwitchingMcLagDomain` | `siteRef` | — | **adoption:** `name` (peers observed) | domain UUID, conditions |
| `UnifiSwitchingSwitchStack` | `siteRef` | — | **adoption:** `name` (members observed) | stack UUID, conditions |
| `UnifiVpnServer` | `siteRef` | — | **adoption:** `name` (observed `type`/`enabled`) | server UUID, conditions |
| `UnifiSiteToSiteTunnel` | `siteRef` | — | **adoption:** `name` (endpoints observed) | tunnel UUID, conditions |

**Adoption kinds** (RADIUS, the three switching kinds, and the two VPN kinds) carry only
`spec.siteRef` plus an identity — `spec.name` for MC-LAG domains, switch stacks, RADIUS profiles, and
VPN servers/tunnels, and a **typed composite selector** (`type` + member device-tag/port selector)
for `UnifiSwitchingLag`, which the frozen surface exposes without a name — and a read-only list/get
client; their upstream configuration and any peer/member identity are reported in `status`, never
authored in `spec`.

### D6 — Version gating extends the capability table

New capabilities are gated through the existing `versions.go` `CheckCapability` mechanism with
their own minimums (notably firewall/DNS ≥ 10.1.84 per `docs/unifi-api.md`) rather than reusing the
overall Official-API floor. Below the minimum, or when the version is unknown, the reconciler fails
closed with `Ready=False` and mutates nothing — the established pattern.

### D7 — Contract sync in the same change

Every consumed endpoint gets its `docs/unifi-api.md` row (`endpoint → operator method → CLI
consumer`) before or with its implementation, per the golden rule; `docs/architecture.md`
"Implemented vs designed kinds" flips to all-implemented; kustomize
(`operator/config/crd/`, RBAC, samples) and Helm (`charts/`) stay consistent.

## Risks / Trade-offs

- **[Mega-change is hard to review]** → Enforce per-phase gates (build/vet/lint/test,
  `make manifests generate` no-diff, `zensical build --strict`) at the end of each phase, not only
  at the end of the change; keep tasks small and dependency-ordered.
- **[Ordering drift semantics can be read as license to reorder]** → The ordering reconciler never
  writes order for a scope with duplicates, and never moves a system-defined policy; drift is
  surfaced only through `status`.
- **[Built-in zones are read-only]** → The built-in-zone selector resolves to a system zone for
  policy targeting only; it never triggers a create/update/delete of a zone, consistent with the
  `UnifiFirewallZone` read-only system-zone rule.
- **[Client grows broad quickly]** → One method per endpoint, mirrored in both
  `operator/internal/unifi` and `cli/internal/unifi`, each covered by an `httptest` test; no shared
  cross-module package.
- **[CLI golden churn]** → New `snapshot` subcommands add fixtures/goldens; the `unifi-snapshot`
  safe-skip/annotate norm governs unrepresentable upstream objects.

## Open Questions

- ~~`openspec/specs/unifi-switching/spec.md` says device membership is "typed selectors **or
  `UnifiDeviceTag` references**", but no `UnifiDeviceTag` kind exists.~~ **Resolved:** the frozen
  v10.4.57 switching surface is read-only, so the `unifi-switching` delta in this change replaces
  that requirement with adoption-by-name and observed membership in `status`; the `UnifiDeviceTag`
  wording is removed.
- The upstream `v10.4.57` surface exposes RADIUS, switching, and VPN **read-only** (verified against
  `openapi.json`); their kinds ship as adoption kinds and their specs are corrected by the deltas in
  this change (`specs/unifi-radius`, `specs/unifi-switching`, `specs/unifi-vpn`). If a later console
  version adds write endpoints, a follow-up change can promote them to writers.
- The exact upstream request/response shapes for the writable kinds are pinned when each is
  implemented and recorded in `docs/unifi-api.md`; this does not change the specs, approach, or task
  breakdown.
