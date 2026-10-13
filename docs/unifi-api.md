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

> The rows above are the consumed surface for the implemented kinds (`UnifiController`,
> `UnifiSite`, `UnifiNetwork`, `UnifiFirewallZone`, `UnifiWifiBroadcast`) plus the read-only
> device-tag selector. The frozen v10.4.57 document also exposes devices, clients, firewall
> policies, ACL rules, DNS policies, traffic matching lists, switching, VPN, and RADIUS; a row is
> added here when each kind is implemented. The authoritative full endpoint list is the upstream
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
