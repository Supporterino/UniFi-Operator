# Design

## Context

See `proposal.md` — Why. Constraints that shape the approach:

- Two Go modules mirror the same Integration v1 contract: `operator/internal/unifi/` (typed client
  with create/update/delete) and `cli/internal/unifi/` (reads only — verified: the sole HTTP verb
  under `cli/` is `http.MethodGet`).
- CRD types live in `operator/api/v1alpha1/unifinetwork_types.go`; the request body is
  management-discriminated and flattened in `NetworkRequest.MarshalJSON`
  (`operator/internal/unifi/client.go`). That client's `IPv4Configuration` comment states the
  upstream `dhcpConfiguration` union is deliberately unmodeled.
- A read-only probe of a live console (`applicationVersion` 10.6.106, one site) established:
  header casing is irrelevant; the list envelope is `{offset,limit,count,totalCount,data}`; the
  error body is the flat `Error Message`; a network's `ipv4Configuration` carries a
  `dhcpConfiguration` object; a ZBF-off console returns `400
  api.firewall.zone-based-firewall-not-configured`; and a broadcast may be bound to the `NATIVE`
  network variant.
- Tests are hermetic (`docs/testing.md`): fixtures plus `httptest` only, never a live console.

## Goals / Non-Goals

**Goals:**

- Make a snapshot faithful enough to adopt without resetting a network's DHCP settings.
- Make present-but-unrepresentable upstream objects visible (warn), never silently dropped.
- Model DHCP configuration inside the existing management-discriminated union without exposing any
  upstream identifier.
- Keep tests hermetic by recapturing fixtures from the observed shapes.

**Non-Goals:**

- Live-console calls in CI; making a `NATIVE`-bound broadcast authorable; the raw `DEVICES` filter;
  new CRD kinds or a CRD version bump.

## Decisions

### D1: Model `dhcpConfiguration` as an optional nested struct on `IPv4Configuration`

Add a `DHCPConfiguration` type in `operator/api/v1alpha1/unifinetwork_types.go` — `mode`,
`ipAddressRange{start,stop}`, `dnsServerIpAddressesOverride []string`, `leaseTimeSeconds`,
`domainName`, `pingConflictDetectionEnabled` — referenced as an optional field on
`IPv4Configuration`. Mirror the field in both clients and rely on the existing flattening in
`NetworkRequest.MarshalJSON` (it already marshals `ipv4Configuration` whole).

- **spec shape:** `spec.gateway.ipv4Configuration.dhcpConfiguration` (and the switch variant),
  optional; no UUIDs, no `_id` — it is pure configuration.
- **Status:** unchanged; the values are desired state, not observed identifiers.
- **Alternatives:** (a) leave unmodeled and only annotate — rejected, the observed adoption risk is
  that a PUT without the block resets DHCP; (b) a top-level `spec.dhcpConfig` — rejected, the CR
  mirrors the upstream union, which nests it under `ipv4Configuration`.

### D2: Validate DHCP-guarding entries at the schema level, and filter on emit

Add per-item validation to `DHCPGuarding.TrustedDHCPServerIPAddresses` (an item `MinLength` plus a
CEL `XValidation` for IPv4 shape — `format: ipv4` is not portable across OpenAPI v3 tooling).
Because a live console can return `["192.168.2.1","",""]`, the **snapshot must strip empty entries
(with a warning)** so the emitted `UnifiNetwork` still passes the tightened schema.

- **Alternative:** validate only in the controller — rejected; the API server is the contract
  boundary, and the golden rule wants invalid input rejected there.

### D3: Map the unconfigured-ZBF error to an empty list by machine-readable code

In both clients, when `*APIError.Code == "api.firewall.zone-based-firewall-not-configured"`,
return an empty zone slice and no error. Match the `code`, not the message text.

- **Alternatives:** match the message string — rejected as brittle; treat any `400` on the endpoint
  as empty — rejected, that hides genuine bad requests.

### D4: Snapshot skip/annotation behavior stays, and becomes a documented capability

The existing warnings for a `NATIVE`-bound broadcast (skipped) and for unrepresented settings
(`bandSteeringEnabled`) are correct; they are captured as the new `unifi-snapshot` capability
rather than re-implemented.

### D5: Recapture fixtures from the observed shapes

Replace the hand-authored `cli/testdata/*.json` with captures that include
`metadata.configurable`, a non-`USER_DEFINED` origin, a populated `dhcpConfiguration`, empty
guarding entries, and the ZBF-off `400`. Redact identity the way the existing fixtures do
(placeholder UUIDs/names), and update golden files deliberately.

### D6: Docs

`docs/unifi-api.md`: drop the "pending live confirmation" note on the `X-API-Key` spelling and
record that casing is irrelevant; note the live-observed `applicationVersion` while keeping the
version gates a floor; add the ZBF-unconfigured error code and the `NATIVE` binding note.

## Risks / Trade-offs

- **Tightened guard validation rejects existing objects** → documented **BREAKING**; migration
  removes the empty entries, and the snapshot filters them so emitted CRs stay valid.
- **A dropped DHCP field on write could still reset DHCP** → every observed field is modeled, and
  an `httptest` round-trip asserts the marshaled body equals the fixture.
- **Unknown future `dhcpConfiguration` modes** → the field is modeled flat and decoded tolerantly;
  a discriminated union can follow when a non-`SERVER` mode appears.
- **A network whose `dhcpConfiguration` the CRD cannot represent (an empty `mode`) is skipped
  whole, not emitted with the block dropped** → deliberate: the CRD requires a non-empty `mode`
  when the block is present, so emitting the network without it would be a *partially projected
  resource*, which the `unifi-snapshot` capability forbids ("MUST NOT ... emit a partially
  projected resource"). The network is therefore skipped with a warning (mirroring the
  unrepresentable-object rule) rather than annotated as an unrepresented setting; an empty `mode`
  has not been observed on a live console.
- **Live shapes drift again** → the pinned version stays a floor and fixtures are a point-in-time
  capture, refreshed deliberately.
- **`code`-based ZBF mapping could mask a genuine 400 with the same code** → accepted; the code is
  specific.

## Migration Plan

1. Land schema, client, and controller changes; run `make -C operator manifests generate` and
   confirm no diff.
2. Migrate: an existing `UnifiNetwork` with empty guard entries is rejected on next apply; remove
   the empties (a fresh snapshot already omits them).
3. Rollback: reverting the guard markers restores prior acceptance; no CRD version bump is
   involved (stays `v1alpha1`).
4. Recapture fixtures and run the module and docs gates.

## Open Questions

- None blocking. Whether to model non-`SERVER` `dhcpConfiguration` modes as a discriminated union
  can be decided when such a mode is observed.
