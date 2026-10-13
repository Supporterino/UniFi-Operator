# Proposal

## Why

Seven declarative capabilities of the Integration v1 CRD set are **designed as contracts
only** — their requirements already live in `openspec/specs/`, but they have no Go types, CRD
manifests, or reconcilers. That leaves the operator visibly half-built: firewalls that own zones
but cannot write a single policy, and an already-shipped `UnifiWifiBroadcast` whose enterprise
security requirement documents a `UnifiRadiusProfile` reference that does not exist, so enterprise
WiFi fails closed forever. This change implements the 12 remaining kinds so the declared contract
becomes a shipped surface.

Verified against the frozen v10.4.57 OpenAPI document, three of those capabilities — RADIUS,
switching, and VPN — are exposed **read-only** (list/get only, no write endpoint), so their kinds
ship as **adoption kinds** rather than writers, and their specs of record are corrected by the
deltas below rather than implemented against an upstream surface that does not exist.

## What Changes

- **Implement the 12 remaining kinds across 7 capabilities** (`unifi-radius`,
  `unifi-traffic-matching`, `unifi-firewall`, `unifi-acl`, `unifi-dns`, `unifi-switching`,
  `unifi-vpn`), each as a full vertical slice: API type + kubebuilder validation markers →
  `make manifests generate` (deepcopy + CRD) → typed Integration v1 client methods → reconciler
  with finalizer, `status`, conflict detection, and version gating → envtest + `httptest` tests →
  CLI snapshot projection → kustomize and Helm CRDs/RBAC → docs. The four **writable**
  capabilities (`unifi-traffic-matching`, `unifi-firewall`, `unifi-acl`, `unifi-dns`) get the full
  create/update/delete slice; the three **read-only** capabilities (`unifi-radius`,
  `unifi-switching`, `unifi-vpn`) get the adoption slice: `spec.siteRef` + `spec.name`, a
  read-only client (list/get), a reconciler that resolves the upstream object and reports its UUID
  in `status`, and no upstream-electing finalizer.
- **Correct the three read-only specs of record.** `unifi-radius`, `unifi-switching`, and
  `unifi-vpn` are **MODIFIED** to adoption requirements (references by site + name, configuration
  observed in `status`, no authored secrets or opaque IDs), so the contract matches the frozen
  surface.
- **Complete the firewall capability.** `UnifiFirewallZone` already ships; add
  `UnifiFirewallPolicy` (referencing configurable source/destination zones plus a typed
  built-in-zone selector) and `UnifiFirewallPolicyOrdering` (per-zone-pair, before/after-system
  partition, drift reported rather than silently reordered).
- **Add `unifi-traffic-matching`.** `UnifiTrafficMatchingList` provides the typed, reusable
  filter target that firewall policies and ACL rules resolve against.
- **Unblock enterprise WiFi.** Add `UnifiRadiusProfile` as an **adoption kind** (the frozen surface
  exposes RADIUS profiles read-only; the CR adopts an existing upstream profile by name and records
  its UUID, authoring no secret) and **MODIFY** `unifi-wifi` so the enterprise requirement resolves
  the referenced profile instead of failing closed.
- **Add ACL, DNS, switching, and VPN capabilities** as independent, additive kinds:
  `UnifiAclRule` + `UnifiAclRuleOrdering` (site-global ordering) and `UnifiDnsPolicy` as writable
  kinds; `UnifiSwitchingLag` / `UnifiSwitchingMcLagDomain` / `UnifiSwitchingSwitchStack` and
  `UnifiVpnServer` / `UnifiSiteToSiteTunnel` as **adoption kinds** (read-only upstream surface).
- **Introduce two shared primitives** (detailed in design.md) rather than reinventing them per
  kind: an ordering + drift + duplicate-conflict reconciler primitive (firewall per-zone-pair,
  ACL site-global), and a typed-filter selector resolving firewall-policy and ACL filters against
  `UnifiTrafficMatchingList`.
- **Sync the contract docs** as each endpoint is consumed: add the `docs/unifi-api.md` endpoint
  rows, and flip `docs/architecture.md` "Implemented vs designed kinds" to all-implemented.

## Capabilities

### New Capabilities

None. Every affected capability already exists as a spec of record under `openspec/specs/`, authored
as a forward contract by the archived `design-crds-for-unifi-integration-v1` change. This change
implements those specs; it introduces no new capability and invents no new requirement.

### Modified Capabilities

- `unifi-wifi`: **MODIFIED** requirement *Enterprise security references a RADIUS profile* — it
  changes from "the controller MUST fail closed ... until the referenced profile can be resolved
  and provisioned" to resolving the referenced `UnifiRadiusProfile` and provisioning the
  enterprise configuration; the "fails closed until the profile kind lands" scenario is replaced
  by a resolution scenario.
- `unifi-radius`: **MODIFIED** to an adoption contract — the writable-secret and declarative-servers
  requirements are replaced by "adopted by name + references its site" and "authors no credentials
  and never leaks secrets", because v10.4.57 exposes `GET .../radius/profiles` read-only.
- `unifi-switching`: **MODIFIED** to an adoption contract — device-membership-by-selector and
  declarative LAG/MC-LAG configuration are replaced by "adopted by name + references its site" and
  "configuration observed in `status`", because v10.4.57 exposes the switching objects read-only.
- `unifi-vpn`: **MODIFIED** to an adoption contract — type-variant validation, secret references,
  and peer-by-identity are replaced by "adopted by name + references its site" and "authors no
  credentials and never leaks secrets", because v10.4.57 exposes the VPN objects read-only.

## Impact

- **CRD kinds — 12 new (all namespaced; each with `status.conditions`, `observedGeneration`, and a
  human-readable summary; a finalizer only where deletion has upstream effects — the four writable
  capabilities):** `UnifiRadiusProfile`, `UnifiTrafficMatchingList`, `UnifiFirewallPolicy`,
  `UnifiFirewallPolicyOrdering`, `UnifiAclRule`, `UnifiAclRuleOrdering`, `UnifiDnsPolicy`,
  `UnifiSwitchingLag`, `UnifiSwitchingMcLagDomain`, `UnifiSwitchingSwitchStack`,
  `UnifiVpnServer`, `UnifiSiteToSiteTunnel`.
- **Read-only adoption kinds.** 6 of the 12 kinds (`UnifiRadiusProfile`, the three switching kinds,
  and the two VPN kinds) adopt an existing upstream object by name; they expose no write surface, no
  authored device membership/secrets, and no upstream-electing finalizer, matching the frozen
  v10.4.57 `GET`-only surface.
- **CRD contract impact.** New kinds only; no `spec` field on the five shipped kinds changes and
  no opaque upstream `_id`/UUID is exposed. Upstream IDs continue to live in `status` only.
  Cross-resource links use Kubernetes identity (`spec.siteRef`, typed zone and device-tag
  selectors) or references to other CRs (`UnifiTrafficMatchingList`, `UnifiRadiusProfile`).
- **Modified kind (behavior, not spec).** The `UnifiWifiBroadcast` enterprise path becomes
  functional: `spec.securityConfiguration.<variant>.radiusConfiguration.radiusProfileRef`
  resolves and provisions.
- **Controllers.** 12 new reconcilers; the `UnifiSite` recursive drain is extended to own the new
  site-scoped children; ordering reconcilers detect duplicate objects and report upstream drift.
- **Clients.** `operator/internal/unifi` and `cli/internal/unifi` gain the methods the consumed
  endpoints expose: list/get/create/update/delete for the writable capabilities, and list/get only
  for the read-only adoption capabilities (radius, switching, VPN).
- **CLI.** `snapshot` subcommands (radius, traffic-matching, firewall-policies, acl, dns,
  switching, vpn), plus fixtures and golden files following the `unifi-snapshot` safe-skip norm.
- **Deployment.** `operator/config/crd/`, RBAC, and `config/samples/`, and the Helm chart
  CRDs/RBAC, for all 12 kinds.
- **Docs.** `docs/unifi-api.md` endpoint rows; `docs/architecture.md` "Implemented vs designed
  kinds"; `docs/crd-conventions.md` only if the selector/ordering norms warrant it.
- **Gates.** `make -C operator manifests generate` produces no diff; operator and CLI
  build/vet/lint/test pass; `zensical build --strict` passes.

## Non-Goals

- No new spec-level capabilities and no invented requirements: the contract is the pre-existing
  `openspec/specs/`.
- No upstream surface beyond the frozen Integration v1 v10.4.57 endpoint map.
- No `UnifiDeviceTag` Custom Resource; device tags remain read-only and are consumed through the
  name-based device-tag selector.
- No opaque upstream `_id`/UUID in any `spec`; IDs remain `status`-only.
- No live-controller calls in tests; tests stay hermetic (`httptest` fixtures, envtest).
- No breaking change to the five shipped kinds beyond the `unifi-wifi` requirement flip.
