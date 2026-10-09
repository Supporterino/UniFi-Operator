## Why

The Integration v1 change landed `UnifiController`, `UnifiSite`, and `UnifiNetwork`, then froze the
remaining kinds as contracts only. Two of those contracts are now known to be more than mechanical:
`UnifiFirewallZone` is fully writable upstream (custom zones) and is the designated owner of
network membership, while `UnifiDeviceTag` turns out to be **list-only** in the frozen v10.4.57
surface — it cannot be created, updated, or deleted — so the contract that has the operator create a
tag is unsatisfiable. Both sit at the center of the reference model: `UnifiNetwork` reports zone
membership in `status` but nothing ever writes it, and a `SWITCH`-managed network fails closed
because its device binding references a `UnifiDeviceTag` that cannot exist. Implementing the zone
and replacing the phantom device-tag CR with a read-only selector closes both holes and unblocks the
firewall and switch device-scoping paths.

## What Changes

- **Implement `UnifiFirewallZone`.** A namespaced CR with `spec.siteRef`, `spec.name`, and
  `spec.networkRefs` (references to `UnifiNetwork` by Kubernetes name). The controller creates,
  updates, and deletes **custom** (user-defined) zones and sets their upstream network membership.
- **System zones are adopted read-only.** A system-defined upstream zone is never created, updated,
  or deleted; a `UnifiFirewallZone` that resolves to one reports `Ready=False` and does not mutate
  it. Only user-defined zones are written.
- **Network-membership uniqueness is enforced at runtime.** CRD markers cannot enforce cross-object
  uniqueness; when two `UnifiFirewallZone` objects claim the same `UnifiNetwork`, all such objects
  report `Ready=False` and no membership is written until a single writer remains.
- **Replace the `UnifiDeviceTag` CR with a device-tag selector (BREAKING within pre-release
  `v1alpha1`).** Device tags are read-only upstream, so `UnifiNetwork.spec.switch` drops the
  `deviceTagRef` reference to a `UnifiDeviceTag` kind and instead carries a name-based typed
  selector (the `WANSelector` idiom). The controller resolves the tag name through the read-only
  `GET /v1/sites/{siteId}/device-tags` list.
- **Unblock `SWITCH`-managed networks.** When the resolved tag yields exactly one device, the
  controller uses its UUID; when it yields zero or more than one, it fails closed with `Ready=False`.
  `UnifiNetwork` no longer reports `SwitchManagedUnsupported` for a resolvable binding.
- **Drop the `UnifiDeviceTag` CR from the implemented surface.** No `*_types.go`, CRD manifest,
  controller, finalizer, or CLI emitter for it. The `unifi-device-tags` capability is retained only
  as the shared home for the read-only selector behavior that WiFi, ACL, and switching will reuse.
- **Docs and deployment sync.** Add the device-tags and firewall-zones rows to `docs/unifi-api.md`,
  the zone/selector reference rules to `docs/crd-conventions.md`, and the new kind to
  `docs/architecture.md`; ship the CRD via kustomize and Helm.

### Non-Goals

- Firewall policies, firewall-policy ordering, ACL rules/ordering, DNS policies, traffic matching
  lists, switching kinds, VPN, and RADIUS remain contracts only — not implemented here.
- No attempt to author device tags upstream (no create/update/delete); no device-tag CR.
- No change to the selected UniFi API version (still frozen to Integration v1 spec v10.4.57) and no
  legacy REST fallback.
- No membership writes to system-defined zones; no probing-ahead capability for the undefined
  `configurable` flag (fail closed until a live console signal exists).

## Capabilities

### New Capabilities

- _None._

### Modified Capabilities

- `unifi-firewall`: implements `UnifiFirewallZone` — custom-zone create/update/delete, read-only
  adoption of system zones, and runtime membership uniqueness.
- `unifi-network`: the `SWITCH` device binding becomes a name-based device-tag selector resolved
  through the read-only device-tags list instead of a reference to a `UnifiDeviceTag` CR; a
  resolvable single-device binding no longer fails closed.
- `unifi-device-tags`: repurposed from a declarative Custom Resource (create/delete with a
  finalizer) to the shared read-only device-tag selector idiom (name-based, resolved from
  `GET /v1/sites/{siteId}/device-tags`), with no CR, no upstream writes, and no finalizer.

## Impact

- **CRD kinds**: add `UnifiFirewallZone`; change `UnifiNetwork.spec.switch` (remove `deviceTagRef`,
  add a device-tag selector); no `UnifiDeviceTag` kind is generated.
- **API types**: `operator/api/v1alpha1/unififirewallzone_types.go` (new),
  `operator/api/v1alpha1/unifinetwork_types.go` (switch selector),
  `operator/api/v1alpha1/reference_types.go` (shared selector struct),
  `operator/api/v1alpha1/zz_generated.deepcopy.go`.
- **Manifests**: `operator/config/crd/`, `operator/config/rbac/`, `operator/config/samples/`, and
  `charts/` gain the zone CRD and RBAC.
- **Client**: `operator/internal/unifi/` gains device-tag list and firewall-zone CRUD methods
  (plain structs, no Kubernetes imports); `cli/internal/unifi/` mirrors the read-only tag list.
- **Controllers**: `operator/internal/controller/unififirewallzone_controller.go` (new),
  `unifinetwork_controller.go` (resolve the selector, unblock `SWITCH`), `conditions.go`
  (new reasons), `versions.go` (gate zones on the existing `CapabilityFirewallDNS`),
  `operator/cmd/main.go` (wiring).
- **CLI**: a `snapshot firewall-zones` emitter (from `GET /v1/sites/{siteId}/firewall/zones`) and a
  `snapshot device-tags` read-only emitter.
- **Docs**: `docs/unifi-api.md` (endpoint rows), `docs/crd-conventions.md` (selector idiom),
  `docs/architecture.md` (kind graph).
- **API compatibility**: pre-release `v1alpha1`; the `UnifiNetwork` switch-shape change is
  **BREAKING** and accepted in place (no conversion webhook), consistent with the prior reshape.
