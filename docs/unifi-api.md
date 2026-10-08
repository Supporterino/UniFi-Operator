# UniFi API Contract

This is the canonical map of the UniFi controller API surface that UniFi-Operator consumes.
It is the workspace's source of truth for the upstream contract: if the operator or CLI needs
a new endpoint or field, it is added here first, then implemented in both consumers. When the
code and this document disagree, this document is authoritative and the code is wrong.

The contract is consumed by:

- `operator/internal/unifi/` — the typed client controllers call.
- `cli/internal/unifi/` — the client the snapshot command uses.

Both return plain Go structs and never import Kubernetes types.

## Authentication

The operator authenticates to the UniFi Network application. Support both models, preferring
API keys:

- **API key (preferred).** An API key is sent on every request (for example an
  `X-API-Key` header) against the integration API base path. The key is read from a Kubernetes
  `Secret` referenced by the operator's configuration and never logged or written to `status`.
- **Local account session.** For controllers without API-key support, the client logs in with
  a credential from a `Secret`, receives a session cookie/token, and re-authenticates
  transparently on expiry. Credentials are never logged.

The exact header name, base path, and login endpoint are confirmed against the target
controller version during integration and recorded in the table below before the client is
written.

## Base paths

| Concern | Base path | Notes |
|---------|-----------|-------|
| Login / session | `/api/login` (legacy) | Local-account auth; session cookie. |
| Site-scoped REST | `/proxy/network/api/s/{site}/...` | Most object CRUD under `rest/`. |
| Integration API | `/proxy/network/integration/v1/...` | API-key auth; newer controllers. |
| Self / meta | `/proxy/network/api/self`, `/status` | Identity and health. |

Base path and site name are configurable. The site is a `spec`/config input, never a hard-coded
constant.

## Endpoint map

Every consumed endpoint is listed here as `endpoint → operator method → CLI consumer`. Fill a
row before implementing either consumer.

| Method | Endpoint | Purpose | Operator | CLI |
|--------|----------|---------|----------|-----|
| POST | `/api/login` | Establish a session | `client.Login` | `client.Login` |
| GET | `/proxy/network/api/s/{site}/rest/networkconf` | List networks | `client.ListNetworks` | `snapshot networks` |
| GET | `/proxy/network/api/s/{site}/rest/wlanconf` | List WLANs | `client.ListWLANs` | `snapshot wlans` |
| GET | `/proxy/network/api/s/{site}/rest/firewallrule` | List firewall rules | `client.ListFirewallRules` | `snapshot firewallrules` |
| GET | `/proxy/network/api/s/{site}/rest/user` | List clients | `client.ListClients` | `snapshot clients` |
| GET | `/proxy/network/api/s/{site}/rest/networkconf/{id}` | Read one network | `client.GetNetwork` | — |
| PUT | `/proxy/network/api/s/{site}/rest/networkconf/{id}` | Update a network | `client.UpdateNetwork` | — |
| POST | `/proxy/network/api/s/{site}/rest/networkconf` | Create a network | `client.CreateNetwork` | — |
| DELETE | `/proxy/network/api/s/{site}/rest/networkconf/{id}` | Delete a network | `client.DeleteNetwork` | — |

> The rows above are the intended surface, not yet verified against a specific controller
> version. Each row is confirmed and adjusted as the corresponding client method is
> implemented; unknown or unstable endpoints stay out of the contract until verified.

## Response envelope

Legacy REST endpoints return `{ "meta": { "rc": "ok" }, "data": [ ... ] }`. The client parses
the envelope and returns the `data` payload as typed structs. Any non-`ok` `rc` becomes a typed
`*unifi.APIError`. The integration API's error shape is mapped to the same Go error type.

## Error handling

- Transient failures (network, `5xx`, timeouts) return an error that the reconciler propagates
  so controller-runtime backs off.
- A `4xx` that indicates bad input maps to a terminal status condition (`Ready=False`,
  reason `InvalidSpec`) rather than an infinite retry.
- `401`/session expiry triggers one re-authentication attempt, then surfaces the error.

## Rules

- The client takes a `context.Context` and honors its deadline.
- Responses are parsed into plain structs; no Kubernetes types appear in `internal/unifi`.
- New fields are added server-side before the CR exposes them — never expose a `spec` field the
  controller cannot actually reconcile.
- Upstream `_id`s are returned to callers but are stored only in `status`, per
  [CRD conventions](crd-conventions.md).

## Related

- [Architecture](architecture.md)
- [CRD conventions](crd-conventions.md)
- [Security](security.md)
- [Testing](testing.md)
