## Why

The CRD surface is currently a single scaffolded `UnifiNetwork` kind whose fields (`vlan`,
`subnet`, `enabled`) mirror the legacy REST `networkconf` object, and `docs/unifi-api.md` targets
the legacy REST API. Ubiquiti's actively-developed surface is the **Integration v1** API
(`/proxy/network/integration/v1`) — the API behind the controller's own linked API spec — which
provides typed CRUD, UUID identities, and per-resource endpoints for networks, WiFi, firewall,
ACL, DNS, switching, and VPN. To grow past the scaffold without baking in legacy assumptions we
must commit to Integration v1, fix the controller/site/resource reference model, and capture the
full declarative CRD set as contracts before writing controllers.

## What Changes

- **Adopt Integration v1 as the sole UniFi API surface,** frozen to the controller's OpenAPI
  document at **spec v10.4.57**. Base path `/proxy/network/integration/v1`; API-key
  authentication read from a `Secret`; flat error body; `GET /v1/info` for reachability and the
  detected `applicationVersion`. The surface is version-gated (Official API ≥10.1.78, networks
  CRUD ≥10.0.162, firewall/DNS ≥10.1.84). The legacy REST contract is dropped.
- **New foundation CRDs.** `UnifiController` (connection: `url`, `secretRef` API key,
  `insecureSkipVerify`; `status.applicationVersion` gates capability availability) and
  `UnifiSite` (adopts an existing upstream site by name; parent anchor that owns child CRs via
  `ownerReferences` and drains them with a finalizer on deletion). Both namespaced.
- **Rework `UnifiNetwork` onto Integration v1** with the full gateway union (management
  discriminator, `vlanId`, gateway IPv4/IPv6, isolation, mDNS, internet access, cellular
  backup). Network→zone membership is reported in `status`; `UnifiFirewallZone` owns membership
  in its `spec`. **BREAKING** to the scaffolded `UnifiNetwork` `spec` shape.
- **Reference chain (same-namespace).** `child.spec.siteRef` -> `UnifiSite.spec.controllerRef`
  -> `UnifiController`; references carry no `namespace` field. Ownership follows the strongest
  reference: the site owns `siteRef` children and drains them with a finalizer before it is
  removed, while a child with a stronger parent reference (e.g. a WiFi broadcast -> network) is
  owned by that parent. Upstream IDs never appear in `spec`.
- **Design the remaining declarative CRDs as contracts only** (no Go types yet):
  `UnifiWifiBroadcast`; `UnifiFirewallZone`, `UnifiFirewallPolicy`,
  `UnifiFirewallPolicyOrdering` (one per source/destination zone pair); `UnifiAclRule`,
  `UnifiAclRuleOrdering` (site-global); `UnifiDnsPolicy`; `UnifiTrafficMatchingList`;
  `UnifiSwitchingLag`, `UnifiSwitchingMcLagDomain`, `UnifiSwitchingSwitchStack`;
  `UnifiVpnServer`, `UnifiSiteToSiteTunnel`; `UnifiRadiusProfile`; `UnifiDeviceTag`.
- **Typed selectors** for references to non-CR objects (device, DPI application/category,
  country, and built-in firewall zones) so `spec` stays free of opaque IDs.
- **Implementation slice is only** `UnifiController`, `UnifiSite`, and `UnifiNetwork`
  (API types, generated manifests/deepcopy, typed client, reconcilers, httptest/envtest tests).
  All other kinds are designed but not implemented in this change.
- **Docs sync.** Repoint `docs/unifi-api.md` to Integration v1 and extend
  `docs/crd-conventions.md` with the reference chain, ownership, and membership-ownership rules.

### Non-Goals

- Implementing controllers/clients for any resource other than `UnifiController`, `UnifiSite`,
  and `UnifiNetwork`.
- Modeling observed-or-runtime resources as CRDs: sites list, devices, clients, statistics,
  pending devices, WANs, DPI/country catalogs, hotspot vouchers, `/info`.
- Supporting legacy REST or local-account session auth.
- Multi-controller-per-namespace beyond what `controllerRef` implies; cloud-connector base URLs;
  cross-namespace references (all references and `Secret` lookups are same-namespace).
- Managing firewall-policy or ACL ordering in the implemented slice (contracts only).

## Capabilities

### New Capabilities

- `unifi-controller`: the connection CRD — `url`, `secretRef` (API key), TLS opt-out, and a
  `status` reporting reachability and detected app version.
- `unifi-site`: the site parent CRD — `controllerRef`, site identity, and ownership of child
  CRs via `ownerReferences`.
- `unifi-network`: the `UnifiNetwork` CRD on Integration v1 — full gateway union, membership
  reported in `status`, no opaque IDs in `spec`.
- `unifi-firewall`: firewall zones (owning network membership), policies, and per-zone-pair
  ordering contracts.
- `unifi-wifi`: WiFi broadcast contracts (including security configuration and secret
  references).
- `unifi-acl`: ACL rule contracts and site-global ordering.
- `unifi-dns`: DNS policy contracts (record and forward-domain variants).
- `unifi-traffic-matching`: traffic matching list contracts.
- `unifi-switching`: switching contracts (LAGs, MC-LAG domains, switch stacks).
- `unifi-vpn`: VPN server and site-to-site tunnel contracts.
- `unifi-radius`: RADIUS profile contracts.
- `unifi-device-tags`: device tag contracts.

### Modified Capabilities

- `project-scaffold`: the `UnifiNetwork` contract requirement changes from the legacy
  `vlan`/`subnet`/`enabled` shape to the Integration v1 shape with `siteRef`, and the deployment
  requirement gains the new CRDs in the rendered manifest set.

## Impact

- **Operator**: `operator/api/v1alpha1/` (new `UnifiController`/`UnifiSite` types; reworked
  `UnifiNetwork`), `operator/internal/unifi/` (Integration v1 client), `operator/internal/controller/`
  (new reconcilers), `operator/cmd/main.go` (wiring), `operator/config/crd/`,
  `operator/config/samples/`.
- **CLI**: `cli/` snapshot emitter output for the reworked `UnifiNetwork` and new kinds.
- **Docs**: `docs/unifi-api.md` (Integration v1 fork), `docs/crd-conventions.md` (reference chain
  and ownership), `docs/architecture.md`.
- **Deployment**: `operator/config/` kustomize and `charts/` Helm gain the new CRDs.
- **API compatibility**: `UnifiNetwork` is pre-release `v1alpha1`; the spec reshape is accepted
  as a **BREAKING** change rather than a new version.
