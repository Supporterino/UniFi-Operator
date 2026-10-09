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

`UnifiController.status.applicationVersion` records the detected version and gates which
capabilities are available. Below a capability's minimum the controller fails closed with
`Ready=False` rather than guessing. These thresholds are product-level — they are not encoded in
the OpenAPI document — so they are pinned by the design and confirmed against a live console
when one is available.

## Authentication

- **API key.** Every request carries the API key in the `X-API-Key` header. The key is read from
  a Kubernetes `Secret` referenced by `UnifiController.spec.secretRef`, resolved at reconcile
  time, and never logged or written to `status`.
- **No session auth.** Local-account login (`/api/login`) and session cookies are not part of
  this contract.

Header casing: HTTP header names are case-insensitive, and `X-API-Key` is the documented contract
value here. Go's `net/http` canonicalizes header names on the wire (so writing `X-API-Key`
produces `X-Api-Key`), which is valid and independent of casing. The upstream v10.4.57
documentation renders the header as `X-API-KEY`; the exact console behavior has not been
confirmed against a live target console in this environment, so **live-console confirmation of
the spelling remains pending**.

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

> The rows above are the consumed surface for the implemented kinds (`UnifiController`,
> `UnifiSite`, `UnifiNetwork`, `UnifiFirewallZone`) plus the read-only device-tag selector. The
> frozen v10.4.57 document also exposes devices, clients, WiFi broadcasts, firewall policies, ACL
> rules, DNS policies, traffic matching lists, switching, VPN, and RADIUS; a row is added here when
> each kind is implemented. The authoritative full endpoint list is the upstream
> [llms.txt](https://developer.ui.com/network/v10.4.57/llms.txt) and
> [openapi.json](https://developer.ui.com/network/v10.4.57/openapi.json).

## Response envelope

The Integration v1 API returns plain JSON; there is no `{meta,data}` wrapper.

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
- `dhcpGuarding.trustedDhcpServerIpAddresses` requires at least one entry; omitting
  `dhcpGuarding` disables the feature.
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

The client maps any non-2xx response to a typed `*unifi.APIError` carrying these fields, then:

- Transient failures (network, `5xx`, timeouts) return an error that the reconciler propagates
  so controller-runtime backs off.
- A `4xx` that indicates bad input maps to a terminal status condition (`Ready=False`, reason
  `InvalidSpec`) rather than an infinite retry.
- `401`/authentication failure fails closed: the reconciler sets a failing condition and does
  not fall back to a default credential. There is no session re-authentication.

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
