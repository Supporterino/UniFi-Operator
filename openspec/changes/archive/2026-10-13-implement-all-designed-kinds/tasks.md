# Tasks

Build order is dependency-driven (design.md D1). Each group lands its own tests, CLI projection,
deployment manifests, and docs; the final group is integration checks only. Every operator task
ends with `make -C operator manifests generate` producing no diff.

Groups 1 (radius), 6 (switching), and 7 (VPN) implement **adoption kinds**: the frozen v10.4.57
surface exposes those objects read-only, so their client is list/get only, their reconciler adapts an
existing upstream object and reports its UUID in `status`, and they carry no upstream-electing
finalizer. Their `spec` carries `siteRef` plus an identity: `name` for all of them except
`UnifiSwitchingLag`, which the upstream exposes without a name and which therefore adopts by a typed
composite selector (`type` + member device-tag/port selector). Groups 2-5 are writable kinds with the
full create/update/delete slice.

## 1. UnifiRadiusProfile (unifi-radius) — adoption kind

- [x] 1.1 Add `UnifiRadiusProfile` API types + kubebuilder markers (`operator/api/v1alpha1/unifiradiusprofile_types.go`) for the adoption shape (`spec.siteRef` + `spec.name`; no authored servers/ports/secret) with `status` carrying the resolved upstream profile UUID + `conditions`/`observedGeneration`/summary; run `make -C operator manifests generate` and verify the CRD is generated and the diff is generated files only
- [x] 1.2 Add the RADIUS client read method (`ListRadiusProfiles`) to `operator/internal/unifi/` (the frozen surface exposes no write endpoint); verify an `httptest` test covers it and asserts no secret material is parsed into logs
- [x] 1.3 Implement the `UnifiRadiusProfile` reconciler (site resolution, adopt the upstream profile named by `spec.name`, `status.conditions`, version gate; no finalizer — adoption has no upstream effect); verify fake-client tests for adoption plus an unknown-profile fail-closed case
- [x] 1.4 Extend `UnifiSite` recursive drain (`operator/internal/controller/drain.go`) to own `UnifiRadiusProfile`; verify an envtest that site deletion drains the profile before the site finalizer is removed
- [x] 1.5 Add `snapshot radius` to the CLI with a fixture-backed projection and golden file; verify `cd cli && go test ./...` passes and the golden matches
- [x] 1.6 Add the CRD + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` both render the kind
- [x] 1.7 Add the RADIUS endpoint row (read-only `GET /v1/sites/{siteId}/radius/profiles`) to `docs/unifi-api.md`; verify `zensical build --strict` passes
- [x] 1.8 Resolve the enterprise path: wire `UnifiWifiBroadcast` enterprise security to resolve `radiusProfileRef` to a `UnifiRadiusProfile` and provision the enterprise configuration (replacing the fail-closed branch per `specs/unifi-wifi/spec.md`); verify the resolution scenario passes and an unresolvable profile still sets `Ready=False`

## 2. UnifiTrafficMatchingList (unifi-traffic-matching)

- [x] 2.1 Add `UnifiTrafficMatchingList` API types with the `type` discriminator (`IPV4_ADDRESSES`/`IPV6_ADDRESSES`/`PORTS`) and entry validation markers; run `make -C operator manifests generate` and verify the CRD is generated with no unexpected diff
- [x] 2.2 Add the traffic-matching-list client methods to `operator/internal/unifi/`; verify `httptest` tests cover each method
- [x] 2.3 Implement the `UnifiTrafficMatchingList` reconciler (site resolution, finalizer, `status.conditions`, version gate); verify fake-client tests including a malformed-entry rejection case
- [x] 2.4 Extend the `UnifiSite` recursive drain to own `UnifiTrafficMatchingList`; verify an envtest drain scenario
- [x] 2.5 Add `snapshot traffic-matching` to the CLI with fixture + golden; verify `cd cli && go test ./...` passes
- [x] 2.6 Add the CRD + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` render the kind
- [x] 2.7 Add the traffic-matching endpoint row to `docs/unifi-api.md`; verify `zensical build --strict` passes

## 3. Firewall completion — UnifiFirewallPolicy + UnifiFirewallPolicyOrdering (unifi-firewall)

- [x] 3.1 Add `UnifiFirewallPolicy` and `UnifiFirewallPolicyOrdering` API types (configurable zone refs, built-in-zone selector, typed filters, before/after-system lists) with CEL validation; run `make -C operator manifests generate` and verify both CRDs generate with no unexpected diff
- [x] 3.2 Add firewall policy client methods to `operator/internal/unifi/`; verify `httptest` tests cover each method
- [x] 3.3 Implement the typed built-in-zone selector resolver (configurable zone refs + read-only system zones); verify unit tests for a configurable target, a built-in target, and an unresolvable filter failing closed
- [x] 3.4 Implement the `UnifiFirewallPolicy` reconciler (zone ref + `UnifiTrafficMatchingList` filter resolution, finalizer, `status.conditions`, version gate); verify fake-client tests for apply and for the unresolvable-filter `Ready=False` path
- [x] 3.5 Implement the shared ordering primitive (duplicate-scope conflict detection + drift reporting, system policies anchored) and the `UnifiFirewallPolicyOrdering` reconciler; verify tests for order applied, duplicate-scope conflict, and drift reported
- [x] 3.6 Extend the `UnifiSite` recursive drain to own both new kinds; verify an envtest drain scenario
- [x] 3.7 Add `snapshot firewall-policies` to the CLI with fixture + golden; verify `cd cli && go test ./...` passes
- [x] 3.8 Add both CRDs + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` render both kinds
- [x] 3.9 Add firewall policy/ordering endpoint rows to `docs/unifi-api.md`, document the ordering/ownership norm in `docs/crd-conventions.md` if needed; verify `zensical build --strict` passes

## 4. ACL — UnifiAclRule + UnifiAclRuleOrdering (unifi-acl)

- [x] 4.1 Add `UnifiAclRule` (typed `IPV4`/`MAC` filters, action, enforcing-device selector) and site-global `UnifiAclRuleOrdering` API types with CEL validation; run `make -C operator manifests generate` and verify both CRDs generate with no unexpected diff
- [x] 4.2 Add ACL client methods to `operator/internal/unifi/`; verify `httptest` tests cover each method
- [x] 4.3 Implement the `UnifiAclRule` reconciler (typed filters, device-tag selector resolution, finalizer, `status.conditions`, version gate); verify fake-client tests for apply and an unknown-tag fail-closed case
- [x] 4.4 Implement the `UnifiAclRuleOrdering` reconciler reusing the group-3 ordering primitive (site-global scope); verify tests for order applied, duplicate-scope conflict, and drift reported
- [x] 4.5 Extend the `UnifiSite` recursive drain to own both ACL kinds; verify an envtest drain scenario
- [x] 4.6 Add `snapshot acl` to the CLI with fixture + golden; verify `cd cli && go test ./...` passes
- [x] 4.7 Add both CRDs + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` render both kinds
- [x] 4.8 Add ACL endpoint rows to `docs/unifi-api.md`; verify `zensical build --strict` passes

## 5. UnifiDnsPolicy (unifi-dns)

- [x] 5.1 Add `UnifiDnsPolicy` API types with the record/forward-domain `type` union and variant validation; run `make -C operator manifests generate` and verify the CRD generates with no unexpected diff
- [x] 5.2 Add DNS client methods to `operator/internal/unifi/`; verify `httptest` tests cover each method
- [x] 5.3 Implement the `UnifiDnsPolicy` reconciler (site resolution, variant mapping, finalizer, `status.conditions`, version gate); verify fake-client tests including a mismatched-variant rejection case
- [x] 5.4 Extend the `UnifiSite` recursive drain to own `UnifiDnsPolicy`; verify an envtest drain scenario
- [x] 5.5 Add `snapshot dns` to the CLI with fixture + golden; verify `cd cli && go test ./...` passes
- [x] 5.6 Add the CRD + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` render the kind
- [x] 5.7 Add the DNS endpoint row to `docs/unifi-api.md`; verify `zensical build --strict` passes

## 6. Switching — UnifiSwitchingLag + UnifiSwitchingMcLagDomain + UnifiSwitchingSwitchStack (unifi-switching) — adoption kinds

- [x] 6.1 Add the three switching API types for the adoption shape with validation markers: `UnifiSwitchingMcLagDomain`/`UnifiSwitchingSwitchStack` adopt by `spec.siteRef` + `spec.name`; `UnifiSwitchingLag` (no upstream name) adopts by a typed composite selector (`spec.type` + member device-tag/port selector). Observed members/mode/hashing live in `status`; no opaque ID in `spec`; run `make -C operator manifests generate` and verify all three CRDs generate with no unexpected diff
- [x] 6.2 Add the switching client read methods (`ListLags`/`GetLag`, `ListMcLagDomains`/`GetMcLagDomain`, `ListSwitchStacks`/`GetSwitchStack`) to `operator/internal/unifi/`; verify `httptest` tests cover each method
- [x] 6.3 Implement the three switching reconcilers (site resolution; MC-LAG/stack adopt by `spec.name`; LAG adopt by matching the typed composite selector against the site's LAGs — exactly one match or `Ready=False`; observed configuration in `status`; `status.conditions`; version gate; no finalizer); verify fake-client tests for LAG selector adoption, an ambiguous/no-match LAG fail-closed case, switch-stack adoption, and an opaque-ID-free spec assertion
- [x] 6.4 Extend the `UnifiSite` recursive drain to own all three switching kinds; verify an envtest drain scenario
- [x] 6.5 Add `snapshot switching` to the CLI with fixture + golden; verify `cd cli && go test ./...` passes
- [x] 6.6 Add the three CRDs + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` render all three kinds
- [x] 6.7 Add switching endpoint rows (read-only `GET`/`GET {id}`) to `docs/unifi-api.md`; verify `zensical build --strict` passes

## 7. VPN — UnifiVpnServer + UnifiSiteToSiteTunnel (unifi-vpn) — adoption kinds

- [x] 7.1 Add `UnifiVpnServer` and `UnifiSiteToSiteTunnel` API types for the adoption shape (`spec.siteRef` + `spec.name`; observed `type`/`enabled`/metadata in `status`; no authored variant fields, PSK, or opaque ID in `spec`); run `make -C operator manifests generate` and verify both CRDs generate with no unexpected diff
- [x] 7.2 Add the VPN client read methods (`ListVpnServers`, `ListSiteToSiteTunnels`) to `operator/internal/unifi/`; verify `httptest` tests cover each method
- [x] 7.3 Implement the VPN reconcilers (site resolution, adopt by `spec.name`, observed configuration in `status`, `status.conditions`, version gate; no finalizer, no secret read — the read-only surface exposes no PSK); verify fake-client tests for server adoption, tunnel adoption, and no secret in `status`
- [x] 7.4 Extend the `UnifiSite` recursive drain to own both VPN kinds; verify an envtest drain scenario
- [x] 7.5 Add `snapshot vpn` to the CLI with fixture + golden; verify `cd cli && go test ./...` passes
- [x] 7.6 Add both CRDs + RBAC to `operator/config/` and the Helm chart; verify `kustomize build` and `helm template` render both kinds
- [x] 7.7 Add VPN endpoint rows (read-only `GET`) to `docs/unifi-api.md`; verify `zensical build --strict` passes

## 8. Integration and gates

- [x] 8.1 Run the full operator gate (`cd operator && go build ./... && go vet ./... && golangci-lint run && go test ./...`) and verify it passes green
- [x] 8.2 Run `make -C operator manifests generate` on a clean tree and verify it produces no diff across all 12 new kinds
- [x] 8.3 Run the CLI gate (`cd cli && go build ./... && go vet ./... && golangci-lint run && go test ./...`) and verify it passes green
- [x] 8.4 Flip `docs/architecture.md` "Implemented vs designed kinds" to all-implemented and remove the "designed as contracts only" note; verify `zensical build --strict` passes
- [x] 8.5 Verify kustomize (`operator/config/`) and the Helm chart (`charts/`) are consistent for all 12 kinds (CRDs, RBAC, samples); verify `kustomize build operator/config/default` and `helm template` succeed
- [x] 8.6 Walk the root `AGENTS.md` Cross-Module Change Checklist for the change and verify every item is satisfied

## Workflow follow-up

- The `unifi-switching` spec inconsistency (its requirement still said "or `UnifiDeviceTag`
  references", but no such kind exists) is resolved **in this change** by the `unifi-switching`
  adoption delta, which drops device-membership authoring entirely.
- Archive the change once the project's review requirements are satisfied, then verify
  `openspec/specs/unifi-wifi/spec.md` reflects the enterprise modification and that
  `openspec/specs/{unifi-radius,unifi-switching,unifi-vpn}/spec.md` reflect the adoption
  corrections.
