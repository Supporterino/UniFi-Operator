# UniFi API Contract

This is the canonical map of the UniFi controller API surface that UniFi-Operator consumes.
It is the workspace's source of truth for the upstream contract: if the operator or CLI needs
a new endpoint or field, it is added here first, then implemented in both consumers. When the
code and this document disagree, this document is authoritative and the code is wrong.

The operator and CLI consume only the **Integration v1** API, frozen to the controller's own
OpenAPI document at **spec version v10.4.57**. The legacy REST surface
(`/proxy/network/api/s/{site}/rest/...`) and local-account session authentication are no longer
part of the contract.

The contract is consumed by:

- `operator/internal/unifi/` — the typed client controllers call.
- `cli/internal/unifi/` — the client the snapshot command uses.

Both return plain Go structs and never import Kubernetes types.

## Surface and versioning

| Concern | Value |
|---------|-------|
| Base path (on the console) | `/proxy/network/integration/v1` |
| Frozen spec version | `v10.4.57` — the controller's OpenAPI document |
| Authentication | API key header (see [Authentication](#authentication)) |
| Identity | UUIDs on every resource; lists are pages |
| Transport | HTTPS to the console |

Site-scoped paths are of the form `/v1/sites/{siteId}/...`, where `siteId` is the site UUID
resolved from `GET /v1/sites`.

The surface is **version-gated**. A capability is only usable when the console's detected
`applicationVersion` meets its minimum:

| Capability | Minimum app version |
|------------|---------------------|
| Official API (overall surface) | ≥ 10.1.78 |
| Networks CRUD | ≥ 10.0.162 |
| Firewall / DNS | ≥ 10.1.84 |
| WiFi broadcasts | ≥ 10.1.78 |

`UnifiController.status.applicationVersion` records the detected version and gates which
capabilities are available. Below a capability's minimum the controller fails closed with
`Ready=False` rather than guessing. These thresholds are product-level — they are not encoded in
the OpenAPI document — so they are pinned by the design.

A read-only probe of a live console observed `applicationVersion` **10.6.106**, above every
minimum above. The gates stay a **floor**, not a pinned version: a console newer than the
thresholds remains usable, and the client keeps tolerating unknown response fields.

## Authentication

- **API key.** Every request carries the API key in the `X-API-Key` header. The key is read from
  a Kubernetes `Secret` referenced by `UnifiController.spec.secretRef`, resolved at reconcile
  time, and never logged or written to `status`.
- **No session auth.** Local-account login (`/api/login`) and session cookies are not part of
  this contract.

The header **casing is irrelevant**: a live console returned `200` for `X-API-Key`, `X-API-KEY`,
and `x-api-key`, so this contract records no canonical spelling and no casing-sensitive behavior
is required. Go's `net/http` canonicalizes header names on the wire (writing `X-API-Key` sends
`X-Api-Key`), which the console accepts.

## Endpoint map

Every consumed endpoint is listed here as `endpoint → operator method → CLI consumer`. Add a row
before implementing either consumer.

### `UnifiController`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/info` | Reachability + detected `applicationVersion` | `client.GetInfo` | `snapshot controller` (template) |

`GET /v1/info` is the reachability probe and the input to
`UnifiController.status.applicationVersion` that gates capability availability. The CLI's
`snapshot controller` emits a `UnifiController` template (a `url` plus a placeholder
`secretRef`); it does not read the console.

### `UnifiSite`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites` | List sites; map `internalReference` → UUID | `client.ListSites` | `snapshot sites` |

### `UnifiNetwork`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/networks` | List networks | `client.ListNetworks` | `snapshot networks` |
| POST | `/v1/sites/{siteId}/networks` | Create a network | `client.CreateNetwork` | — |
| GET | `/v1/sites/{siteId}/networks/{networkId}` | Read one network | `client.GetNetwork` | `snapshot networks` |
| PUT | `/v1/sites/{siteId}/networks/{networkId}` | Update a network | `client.UpdateNetwork` | — |
| DELETE | `/v1/sites/{siteId}/networks/{networkId}` | Delete a network | `client.DeleteNetwork` | — |

### `UnifiFirewallZone`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/firewall/zones` | List firewall zones | `client.ListZones` | `snapshot firewall-zones` |
| GET | `/v1/sites/{siteId}/firewall/zones/{id}` | Read one firewall zone | `client.GetZone` | — |
| POST | `/v1/sites/{siteId}/firewall/zones` | Create a custom firewall zone | `client.CreateZone` | — |
| PUT | `/v1/sites/{siteId}/firewall/zones/{id}` | Update a custom firewall zone | `client.UpdateZone` | — |
| DELETE | `/v1/sites/{siteId}/firewall/zones/{id}` | Delete a custom firewall zone | `client.DeleteZone` | — |

Writes are limited to custom (`USER_DEFINED`) zones and carry `{name, networkIds}` only; a
system-defined upstream zone is adopted read-only and never created, updated, or deleted.
`UnifiFirewallZone` is the single writer of a network's zone membership — see
[CRD conventions](crd-conventions.md).

### Device tags

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/device-tags` | List device tags (read-only) | `client.ListDeviceTags` | `snapshot device-tags` (read-only listing) |

The frozen v10.4.57 surface exposes device tags **read-only**: there is no create, update, or
delete endpoint and no tag-assignment endpoint. A consuming resource selects devices through a
typed, name-based device-tag selector resolved against this list (see
[CRD conventions](crd-conventions.md)); there is no `UnifiDeviceTag` Custom Resource.

### `UnifiWifiBroadcast`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/wifi/broadcasts` | List WiFi broadcasts (overview) | `client.ListWifiBroadcasts` | `snapshot wifi` |
| POST | `/v1/sites/{siteId}/wifi/broadcasts` | Create a WiFi broadcast | `client.CreateWifiBroadcast` | — |
| GET | `/v1/sites/{siteId}/wifi/broadcasts/{broadcastId}` | Read one WiFi broadcast | `client.GetWifiBroadcast` | `snapshot wifi` |
| PUT | `/v1/sites/{siteId}/wifi/broadcasts/{broadcastId}` | Update a WiFi broadcast | `client.UpdateWifiBroadcast` | — |
| DELETE | `/v1/sites/{siteId}/wifi/broadcasts/{broadcastId}` | Delete a WiFi broadcast | `client.DeleteWifiBroadcast` | — |

The list endpoint (`WiFi broadcast overview page`) returns only a broadcast's overview fields; the
variant configuration — the `STANDARD`/`IOT_OPTIMIZED` options and every nested tree — is returned
only by the detail endpoint `GET /v1/sites/{siteId}/wifi/broadcasts/{broadcastId}`
(`WiFi broadcast details`). `snapshot wifi` reads the detail endpoint to emit a faithful
`UnifiWifiBroadcast`.

Personal security passphrases (`passphrase`, `presharedKeys[].passphrase`) are inline in the
upstream API, but the CR carries them as same-namespace `Secret` key selectors, never inline; the
controller reads each passphrase at reconcile time and never records it in `status`. See
[Security](security.md) and [CRD conventions](crd-conventions.md).

A broadcast carries a `network` reference — the upstream `Wifi network reference` union — whose
`type` discriminator is `SPECIFIC` (carrying the target network UUID) or `NATIVE`. The CRD models
both variants. A `SPECIFIC` broadcast references its `UnifiNetwork` by name in `spec.networkRef` and
resolves its site transitively through the network's `spec.siteRef`. A `NATIVE` broadcast — the
upstream `{"type": "NATIVE"}` reference, which carries no network UUID — sets
`spec.nativeNetwork: true` and resolves its site from a broadcast-level `spec.siteRef` naming a
`UnifiSite`; it sets no `networkRef` and gets no `UnifiNetwork` ownerReference — its `UnifiSite`
owns it instead — and its teardown uses the site and broadcast IDs recorded in `status`.
`spec.networkRef` and `spec.nativeNetwork` are mutually exclusive (exactly one is set):
`spec.siteRef` is required for `NATIVE` and forbidden for `SPECIFIC`, enforced by CEL. No upstream
network UUID ever appears in `spec`.

Band steering is a **common** broadcast field, not a `STANDARD`-only one: the console returns
`bandSteeringEnabled` at the top level, and the CRD models it as the top-level
`spec.bandSteeringEnabled`, usable by both the `STANDARD` and `IOT_OPTIMIZED` variants. `snapshot
wifi` emits a `NATIVE`-bound broadcast as `nativeNetwork: true` with a `siteRef` to the emitted
`UnifiSite` (no warning), and projects `bandSteeringEnabled` into `spec.bandSteeringEnabled` rather
than annotating it as an unrepresented setting.

### `UnifiRadiusProfile`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/radius/profiles` | List RADIUS profiles (read-only) | `client.ListRadiusProfiles` | `snapshot radius` |

The frozen v10.4.57 surface exposes RADIUS profiles **read-only**: the list endpoint above is the
only RADIUS endpoint — there is no create, update, or delete endpoint, and the profile schema is
`{id, metadata, name}` with no server, port, or shared-secret field. `UnifiRadiusProfile` is
therefore an **adoption kind**: it adopts an existing upstream profile by name (`spec.siteRef` +
`spec.name`) and records the resolved profile UUID in `status`. It authors no servers, ports, or
shared secret, and there is no write endpoint to reconcile.

### `UnifiTrafficMatchingList`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/traffic-matching-lists` | List traffic matching lists | `client.ListTrafficMatchingLists` | `snapshot traffic-matching` |
| POST | `/v1/sites/{siteId}/traffic-matching-lists` | Create a traffic matching list | `client.CreateTrafficMatchingList` | — |
| GET | `/v1/sites/{siteId}/traffic-matching-lists/{trafficMatchingListId}` | Read one traffic matching list | `client.GetTrafficMatchingList` | — |
| PUT | `/v1/sites/{siteId}/traffic-matching-lists/{trafficMatchingListId}` | Update a traffic matching list | `client.UpdateTrafficMatchingList` | — |
| DELETE | `/v1/sites/{siteId}/traffic-matching-lists/{trafficMatchingListId}` | Delete a traffic matching list | `client.DeleteTrafficMatchingList` | — |

Unlike the read-only RADIUS section above, `UnifiTrafficMatchingList` is a **writable** kind: the
full create/update/delete slice exists, so deletion has upstream effects and the CR carries a
finalizer (see [CRD conventions](crd-conventions.md)). The list is site-scoped through
`spec.siteRef`; its upstream UUID is recorded in `status` only.

`type` is the list discriminator and is one of `IPV4_ADDRESSES`, `IPV6_ADDRESSES`, or `PORTS`;
every entry must conform to the selected type. Writes carry only the authored fields
`{type, name, items}` — the list id is response-only. Entries are a flattened typed union:

| List `type` | Allowed item variants |
|-------------|-----------------------|
| `IPV4_ADDRESSES`, `IPV6_ADDRESSES` | `IP_ADDRESS` (`value`), `IP_ADDRESS_RANGE` (`start`, `stop`), `SUBNET` (`value`, CIDR) |
| `PORTS` | `PORT_NUMBER` (`value`), `PORT_NUMBER_RANGE` (`start`, `stop`) |

A `PORTS` entry's `value`/`start`/`stop` is a port number in `1..65535`. Upstream `items` carry
`PORT_NUMBER`/`PORT_NUMBER_RANGE` values as JSON **numbers**, and the CRD's typed `items` are the
same; a client must tolerate numeric values, and the CLI also tolerates stringified values
defensively. Firewall policies and ACL rules reference a matching list by Kubernetes identity
rather than an upstream id, and the controller resolves the reference to the upstream list.

### `UnifiFirewallPolicy` / `UnifiFirewallPolicyOrdering`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/firewall/policies` | List firewall policies | `client.ListFirewallPolicies` | `snapshot firewall-policies` |
| POST | `/v1/sites/{siteId}/firewall/policies` | Create a firewall policy | `client.CreateFirewallPolicy` | — |
| GET | `/v1/sites/{siteId}/firewall/policies/{firewallPolicyId}` | Read one firewall policy | `client.GetFirewallPolicy` | — |
| PUT | `/v1/sites/{siteId}/firewall/policies/{firewallPolicyId}` | Update a firewall policy | `client.UpdateFirewallPolicy` | — |
| DELETE | `/v1/sites/{siteId}/firewall/policies/{firewallPolicyId}` | Delete a firewall policy | `client.DeleteFirewallPolicy` | — |
| GET | `/v1/sites/{siteId}/firewall/policies/ordering` | Read the policy ordering for a zone pair | `client.GetFirewallPolicyOrdering` | — |
| PUT | `/v1/sites/{siteId}/firewall/policies/ordering` | Replace the policy ordering for a zone pair | `client.UpdateFirewallPolicyOrdering` | — |

The ordering endpoint is scoped by the required `sourceFirewallZoneId` and
`destinationFirewallZoneId` query parameters, and its body is the before/after-system-defined
partition `{orderedFirewallPolicyIds: {beforeSystemDefined[], afterSystemDefined[]}}`.

`UnifiFirewallPolicy` is a **writable** kind: the full create/update/delete slice exists, so
deletion has upstream effects and the CR carries a finalizer (see
[CRD conventions](crd-conventions.md)). A system-defined upstream policy is adopted read-only and
is never created, updated, or deleted. Policy zone targeting uses **typed zone references**, never
a raw UUID: `spec.source` and `spec.destination` each carry either a configurable
`UnifiFirewallZone` by Kubernetes identity (`zoneRef`) or a typed **built-in-zone selector**
(`builtInZone.name`) for a system zone such as `Internal`/`External`. Exactly one of the two is
set; the controller resolves each to the upstream zone at reconcile time and records the resolved
zone UUIDs in `status` only.

Both endpoints may carry a **typed traffic filter** referencing a `UnifiTrafficMatchingList` by
Kubernetes identity (`filter.ipAddress.listRef` / `filter.port.listRef`) — never an opaque list
id. The discriminator `type` is `IP_ADDRESS` or `PORT`, and `matchOpposite` inverts the match. An
unresolvable reference fails closed with `Ready=False` and no upstream mutation.

`UnifiFirewallPolicyOrdering` is a writable kind authoritative for **one source/destination zone
pair**: ordering is per zone pair, mirroring the upstream before/after-system partition. System
policies are anchored and are never reordered. One authoritative ordering object exists per zone
pair — duplicate ordering objects for the same pair each set `Ready=False` and apply no order
until a single object remains, and a user policy present upstream for the pair but absent from the
ordering object is reported as drift in `status.drift`, never silently reordered. The ordering
lists reference `UnifiFirewallPolicy` objects by Kubernetes identity; no upstream policy UUID
appears in `spec`.

### `UnifiAclRule` / `UnifiAclRuleOrdering`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/acl-rules` | List ACL rules | `client.ListAclRules` | `snapshot acl` |
| POST | `/v1/sites/{siteId}/acl-rules` | Create an ACL rule | `client.CreateAclRule` | — |
| GET | `/v1/sites/{siteId}/acl-rules/{aclRuleId}` | Read one ACL rule | `client.GetAclRule` | — |
| PUT | `/v1/sites/{siteId}/acl-rules/{aclRuleId}` | Update an ACL rule | `client.UpdateAclRule` | — |
| DELETE | `/v1/sites/{siteId}/acl-rules/{aclRuleId}` | Delete an ACL rule | `client.DeleteAclRule` | — |
| GET | `/v1/sites/{siteId}/acl-rules/ordering` | Read the site-global ACL-rule order | `client.GetAclRuleOrdering` | — |
| PUT | `/v1/sites/{siteId}/acl-rules/ordering` | Replace the site-global ACL-rule order | `client.UpdateAclRuleOrdering` | — |

`UnifiAclRule` is a **writable** kind: the full create/update/delete slice exists, so deletion has
upstream effects and the CR carries a finalizer (see [CRD conventions](crd-conventions.md)). A
derived (non-user-defined) upstream rule is adopted read-only and is never created, updated, or
deleted.

`spec.type` is the rule discriminator and is `IPV4` or `MAC`; it selects the filter shape, and each
of `spec.source` and `spec.destination` is a **typed endpoint union** whose variant must match the
selected type. An `IPV4` rule uses `IP_ADDRESSES_OR_SUBNETS` (inline `addresses`, optional
`ports`), `NETWORKS` (network membership, optional `ports`), or `PORTS`, and may restrict the match
through `spec.protocolFilter` (`TCP`/`UDP`); a `MAC` rule uses `MAC_ADDRESSES` (addresses plus an
optional `prefixLength`) and additionally requires `spec.networkRef`, the `UnifiNetwork` the rule
applies to. The upstream `NETWORKS` variant carries opaque `networkIds`; the CR exposes them as
`UnifiNetwork` references by Kubernetes identity (`networkRefs`), so no raw network UUID appears in
`spec`.

Enforcing devices are scoped by a typed, name-based **device-tag selector**
(`spec.enforcingDeviceTags[].name`) resolved against the read-only device-tag list above; no opaque
device or tag identifier appears in `spec`. Omitting the selector provisions the rule to all
enforcing-capable devices upstream.

`UnifiAclRuleOrdering` is a writable kind authoritative for the **whole site's** flat order,
mirroring the upstream `orderedAclRuleIds[]` list, and references its parent `UnifiSite` and its
`UnifiAclRule` objects by Kubernetes identity; no upstream rule UUID appears in `spec`. Ordering is
**site-global** (unlike the per-zone-pair firewall ordering above). Exactly one ordering object is
authoritative per site: duplicate ordering objects for the same site each set `Ready=False` and
apply no order until a single object remains, and a user-defined upstream rule present but absent
from the ordering object is reported as drift in `status.drift`, never silently reordered.
System/derived rules are anchored and are never reordered.

### `UnifiDnsPolicy`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/dns/policies` | List DNS policies | `client.ListDnsPolicies` | `snapshot dns` |
| POST | `/v1/sites/{siteId}/dns/policies` | Create a DNS policy | `client.CreateDnsPolicy` | — |
| GET | `/v1/sites/{siteId}/dns/policies/{dnsPolicyId}` | Read one DNS policy | `client.GetDnsPolicy` | — |
| PUT | `/v1/sites/{siteId}/dns/policies/{dnsPolicyId}` | Update a DNS policy | `client.UpdateDnsPolicy` | — |
| DELETE | `/v1/sites/{siteId}/dns/policies/{dnsPolicyId}` | Delete a DNS policy | `client.DeleteDnsPolicy` | — |

`UnifiDnsPolicy` is a **writable** kind: the full create/update/delete slice exists, so deletion
has upstream effects and the CR carries a finalizer (see [CRD conventions](crd-conventions.md)).
The policy is site-scoped through `spec.siteRef`, and the upstream record UUID is recorded in
`status` only — never in `spec`. A record whose observed `metadata.origin` is not `USER_DEFINED`
is adopted read-only and is never created, updated, or deleted.

`spec.type` is the record union discriminator and is one of `A_RECORD`, `AAAA_RECORD`,
`CNAME_RECORD`, `MX_RECORD`, `SRV_RECORD`, `TXT_RECORD`, or `FORWARD_DOMAIN`. `spec.domain` and
the optional `spec.enabled` (defaulting to `true`) are common to every variant; each variant's
fields live in a type-named sub-object that is only accepted for the matching `spec.type`, so
setting fields for a different type is rejected (enforced by CEL):

| `spec.type` | Variant sub-object fields |
|-------------|---------------------------|
| `A_RECORD` | `aRecord.ipv4Address`, `aRecord.ttlSeconds` (0..86400) |
| `AAAA_RECORD` | `aaaaRecord.ipv6Address`, `aaaaRecord.ttlSeconds` (0..86400) |
| `CNAME_RECORD` | `cnameRecord.targetDomain`, `cnameRecord.ttlSeconds` (0..604800) |
| `MX_RECORD` | `mxRecord.mailServerDomain`, `mxRecord.priority` (0..65535) |
| `SRV_RECORD` | `srvRecord.port`, `srvRecord.priority`, `srvRecord.weight` (0..65535), `srvRecord.protocol`, `srvRecord.serverDomain`, `srvRecord.service` |
| `TXT_RECORD` | `txtRecord.text` (1..1024) |
| `FORWARD_DOMAIN` | `forwardDomain.ipAddress` |

Writes carry only the authored fields — the record id and `metadata` are response-only — so no
upstream identifier appears in `spec`.

### `UnifiSwitchingLag` / `UnifiSwitchingMcLagDomain` / `UnifiSwitchingSwitchStack`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/switching/lags` | List LAGs (read-only) | `client.ListLags` | `snapshot switching` |
| GET | `/v1/sites/{siteId}/switching/lags/{lagId}` | Read one LAG | `client.GetLag` | — |
| GET | `/v1/sites/{siteId}/switching/mc-lag-domains` | List MC-LAG domains (read-only) | `client.ListMcLagDomains` | `snapshot switching` |
| GET | `/v1/sites/{siteId}/switching/mc-lag-domains/{mcLagDomainId}` | Read one MC-LAG domain | `client.GetMcLagDomain` | — |
| GET | `/v1/sites/{siteId}/switching/switch-stacks` | List switch stacks (read-only) | `client.ListSwitchStacks` | `snapshot switching` |
| GET | `/v1/sites/{siteId}/switching/switch-stacks/{switchStackId}` | Read one switch stack | `client.GetSwitchStack` | — |

The frozen v10.4.57 surface exposes switching **read-only**: the list/get endpoints above are the
only switching endpoints — there is no create, update, or delete endpoint, so member ports, mode,
and hashing cannot be authored upstream. All three switching kinds are therefore **adoption kinds**:
each adopts an existing upstream object, records the resolved UUID in `status`, and carries no
finalizer — adoption has no upstream effect.

`UnifiSwitchingMcLagDomain` and `UnifiSwitchingSwitchStack` adopt by name (`spec.siteRef` +
`spec.name`); their observed peers, member devices, and member ports are reported in `status`.

`UnifiSwitchingLag` has **no upstream name** — the upstream `LAG details` schema exposes the type and
members but no name — so it adopts by a **typed composite selector** instead: `spec.type`
(`LOCAL`/`MULTI_CHASSIS`/`SWITCH_STACK`) plus `spec.members[]`, each a name-based device-tag selector
(`member.deviceTag.name`, resolved against the read-only device-tag list above) with its member
`member.portIdxs`. The controller selects exactly one upstream LAG whose type and members match
every selector; zero or more than one match fails closed with `Ready=False` and records no upstream
UUID. No upstream LAG, device, or port identifier appears in `spec`; the resolved LAG UUID and the
observed members are recorded in `status` only.

### `UnifiVpnServer` / `UnifiSiteToSiteTunnel`

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| GET | `/v1/sites/{siteId}/vpn/servers` | List VPN servers (read-only) | `client.ListVpnServers` | `snapshot vpn` |
| GET | `/v1/sites/{siteId}/vpn/site-to-site-tunnels` | List site-to-site tunnels (read-only) | `client.ListSiteToSiteTunnels` | `snapshot vpn` |

The frozen v10.4.57 surface exposes VPN **read-only**: the list endpoints above are the only VPN
endpoints — there is no create, update, or delete endpoint, and the server and tunnel schemas
carry no pre-shared key or credential field, so variant configuration and tunnel peers cannot be
authored upstream. Both VPN kinds are therefore **adoption kinds**: each adopts an existing
upstream object by name (`spec.siteRef` + `spec.name`), records the resolved UUID in `status`, and
carries no finalizer — adoption has no upstream effect.

A `UnifiVpnServer` reports its observed `type` (`WIREGUARD`, `OPENVPN`, `L2TP`, `PPTP`, or `UID`)
and `enabled` in `status` alongside the resolved UUID; a `UnifiSiteToSiteTunnel` reports its
observed `type` (`IPSEC`, `OPENVPN`, or `WIREGUARD`) and the resolved UUID. The CR authors no
pre-shared key or other credential, and the read-only surface exposes none to read: secret
material never appears in `spec`, `status`, events, or logs.

> The rows above are the consumed surface for the implemented kinds (`UnifiController`,
> `UnifiSite`, `UnifiNetwork`, `UnifiFirewallZone`, `UnifiWifiBroadcast`, `UnifiRadiusProfile`,
> `UnifiTrafficMatchingList`, `UnifiFirewallPolicy`, `UnifiFirewallPolicyOrdering`,
> `UnifiAclRule`, `UnifiAclRuleOrdering`, `UnifiDnsPolicy`, `UnifiSwitchingLag`,
> `UnifiSwitchingMcLagDomain`, `UnifiSwitchingSwitchStack`, `UnifiVpnServer`,
> `UnifiSiteToSiteTunnel`) plus the read-only device-tag selector. The frozen v10.4.57 document
> also exposes devices and clients; a row is added here when each kind is implemented. The
> authoritative full endpoint list is the upstream
> [llms.txt](https://developer.ui.com/network/v10.4.57/llms.txt) and
> [openapi.json](https://developer.ui.com/network/v10.4.57/openapi.json).

## Response envelope

The Integration v1 API returns plain JSON; there is no `{meta,data}` wrapper. This was confirmed
against a live console: lists carry the page envelope `{offset,limit,count,totalCount,data}`, and
single-object endpoints return the resource shape directly.

- **List endpoints** return a page:

  ```json
  {
    "count": 10,
    "data": [],
    "limit": 25,
    "offset": 0,
    "totalCount": 1000
  }
  ```

  `data` is an array of the endpoint's resource shape. `count`, `limit`, `offset`, and
  `totalCount` are the page metadata; all five fields are required. Lists accept `offset`
  (default `0`) and `limit` (default `25`, maximum `200`) query parameters, plus an optional
  `filter` query parameter whose filterable properties are documented per endpoint. The client
  decodes the page and returns the `data` elements to callers.
- **Single-object endpoints** return the resource shape directly (for example `Network details`),
  not a page and not an envelope.

### `GET /v1/info`

```json
{ "applicationVersion": "10.4.57" }
```

`applicationVersion` is required. Use it as the reachability probe and as the version-gating
input for `UnifiController.status`.

### `GET /v1/sites`

Returns a `Site overview page`. Each element is `{id, internalReference, name}` (all required):

- `id` — the site UUID required by every site-scoped `/v1/sites/{siteId}/...` path.
- `internalReference` — the site's internal unique name used by the older APIs; this is what
  `UnifiSite.spec.internalReference` names.
- `name` — the display name.

`UnifiSite` adopts an existing site by matching `spec.internalReference`, records the UUID in
`status`, and never creates or deletes upstream sites.

### Networks

`GET /v1/sites/{siteId}/networks` returns `Network overview` items that carry only the overview
fields `id`, `management`, `name`, `enabled`, `vlanId`, `default`, and `metadata`. The variant
configuration (`dhcpGuarding`, `ipv4Configuration`, `ipv6Configuration`, `cellularBackupEnabled`,
`internetAccessEnabled`, `isolationEnabled`, `mdnsForwardingEnabled`, `zoneId`, `deviceId`) is
**not** part of the list response; it is returned only by the single-network detail endpoint
`GET /v1/sites/{siteId}/networks/{networkId}` (`Network details`). The CLI `snapshot networks`
reads that detail endpoint to emit faithful `GATEWAY`/`SWITCH` networks.

`POST`/`PUT` `/v1/sites/{siteId}/networks` take the discriminated `Create or update Network`
union on `management`:

| `management` | Required (beyond `name`, `enabled`, `management`, `vlanId`) | Optional additions |
|--------------|------------------------------------------------------------|--------------------|
| `GATEWAY` | `cellularBackupEnabled`, `internetAccessEnabled`, `ipv4Configuration`, `isolationEnabled` | `ipv6Configuration`, `mdnsForwardingEnabled`, `dhcpGuarding`, `zoneId` |
| `SWITCH` | `cellularBackupEnabled`, `deviceId`, `ipv4Configuration`, `isolationEnabled` | `dhcpGuarding` |
| `UNMANAGED` | — | `dhcpGuarding` |

- `name`, `enabled`, `management`, and `vlanId` are required for every variant; `vlanId` is
  `1..4009`, with `1` reserved for the default network.
- `dhcpGuarding.trustedDhcpServerIpAddresses` requires at least one entry, and each entry must be
  a non-empty IPv4 address; omitting `dhcpGuarding` disables the feature.
- `ipv4Configuration.dhcpConfiguration` models the nested DHCP settings: `mode` is required, and
  `ipAddressRange` (`start`/`stop`), `dnsServerIpAddressesOverride`, `leaseTimeSeconds`,
  `domainName`, and `pingConflictDetectionEnabled` are optional — omitted fields leave the
  console's setting unchanged.
- `deviceId` is an opaque device UUID upstream; the CR exposes it as a typed device selector
  (see [CRD conventions](crd-conventions.md)), never as a raw UUID in `spec`.
- `zoneId` is accepted on writes, but per the membership-ownership rule in
  [CRD conventions](crd-conventions.md) `UnifiNetwork` is not its writer: the controller omits
  `zoneId` on writes and reports the resolved zone in `status`. `UnifiFirewallZone` owns network
  membership.

The single-network response (`Network details`) adds the observed fields `id` (UUID), `default`,
`metadata.origin`, and the union fields above (`zoneId` for `GATEWAY`, `deviceId` for `SWITCH`).
These are observed, not authored; the CR keeps upstream IDs and `metadata.origin` in `status`.

## Error handling

Errors are a **flat** JSON body (the OpenAPI `Error Message` schema), not nested under an
`error` key:

```json
{
  "statusCode": 400,
  "statusName": "UNAUTHORIZED",
  "code": "api.authentication.missing-credentials",
  "message": "Missing credentials",
  "timestamp": "2024-11-27T08:13:46.966Z",
  "requestPath": "/integration/v1/sites/123",
  "requestId": "3fa85f64-5717-4562-b3fc-2c963f66afa6"
}
```

This flat shape was confirmed against a live console. `statusName` is **context-dependent** — it
names the failing context rather than a fixed value per `statusCode` — so match on `code` and
`statusCode`, never on `statusName`.

The client maps any non-2xx response to a typed `*unifi.APIError` carrying these fields, then:

- Transient failures (network, `5xx`, timeouts) return an error that the reconciler propagates
  so controller-runtime backs off.
- A `4xx` that indicates bad input maps to a terminal status condition (`Ready=False`, reason
  `InvalidSpec`) rather than an infinite retry.
- `401`/authentication failure fails closed: the reconciler sets a failing condition and does
  not fall back to a default credential. There is no session re-authentication.
- A `400` carrying the machine-readable code `api.firewall.zone-based-firewall-not-configured` is
  not bad input: it means Zone-Based Firewall is unconfigured on that console. The operator and
  CLI clients map it to an **empty zone set** rather than an error, so firewall reconciliation
  reports no zones instead of failing.

## Rules

- The client takes a `context.Context` and honors its deadline.
- Responses are parsed into plain structs; no Kubernetes types appear in `internal/unifi`.
- New fields are added server-side before the CR exposes them — never expose a `spec` field the
  controller cannot actually reconcile.
- Upstream UUIDs are returned to callers but are stored only in `status`, per
  [CRD conventions](crd-conventions.md).
- The client tolerates unknown response fields so a console newer than v10.4.57 does not break
  decoding; it does not assume the upstream response shape is valid.

## Related

- [Architecture](architecture.md)
- [CRD conventions](crd-conventions.md)
- [Security](security.md)
- [Testing](testing.md)
