# Proposal

## Why

The operator and CLI implement the UniFi Integration v1 contract frozen to spec v10.4.57, but a
read-only probe of a real console (`applicationVersion` **10.6.106**, one site `Home`) showed the
contract has drifted and confirmed several behaviours the docs marked "pending live
confirmation". Adopting a snapshot from this console would silently drop DHCP configuration and
hard-fail when a subsystem (Zone-Based Firewall) is unconfigured.

## What Changes

- **Record the live-verified contract facts.** The API-key header is case-insensitive
  (`X-API-Key`, `X-API-KEY`, `x-api-key` all return `200`); the list envelope is
  `{offset,limit,count,totalCount,data}`; the error body is the flat
  `{statusCode,statusName,code,message,timestamp,requestPath,requestId}` shape (with a
  context-dependent `statusName`); and a live console reports `applicationVersion` 10.6.106,
  above every version gate. Update `docs/unifi-api.md` and remove the "pending live
  confirmation" note.
- **Model DHCP configuration on `UnifiNetwork`.** Expose `ipv4Configuration.dhcpConfiguration`
  (`mode`, `ipAddressRange`, `dnsServerIpAddressesOverride`, `leaseTimeSeconds`, `domainName`,
  `pingConflictDetectionEnabled`) so a snapshot is faithful and adoption does not reset a
  network's DHCP range/DNS/domain. Additive; existing objects stay valid.
- **Validate DHCP guarding entries.** Reject the empty strings a real console returns
  (`trustedDhcpServerIpAddresses: ["192.168.2.1","",""]`) so junk is never round-tripped.
  **BREAKING**: previously-accepted objects carrying empty entries are rejected by the API
  server.
- **Treat an unconfigured Zone-Based Firewall as empty.** Map the
  `api.firewall.zone-based-firewall-not-configured` `400` to an empty zone set, so
  `snapshot firewall-zones` succeeds with zero resources and the firewall controllers report no
  zones instead of a terminal failure.
- **Make snapshot skip behaviour explicit and non-silent.** A broadcast bound to the upstream
  `NATIVE` network variant (not exposed by the CRD) is skipped with a warning; unrepresented
  settings (`bandSteeringEnabled`) and unrecoverable passphrases are annotated/placeheld.
- **Refresh CLI fixtures/goldens** from the live shapes: `metadata.configurable`,
  non-`USER_DEFINED` origins, and richer `ipv4Configuration`.

## Capabilities

### New Capabilities

- `unifi-snapshot`: the CLI `snapshot` capability's projection fidelity and safe-skip
  behaviour — emit faithful, adoptable CRs, never leak an upstream ID into `spec`, and
  warn/skip/annotate upstream objects that cannot be represented losslessly.

### Modified Capabilities

- `unifi-network`: model DHCP configuration within `ipv4Configuration`; validate DHCP guarding
  entries.
- `unifi-firewall`: an unconfigured Zone-Based Firewall yields an empty zone set rather than an
  error.

## Impact

- **CRD kinds:** `UnifiNetwork` — new `ipv4Configuration.dhcpConfiguration` fields and stricter
  `dhcpGuarding` validation. No new kinds.
- **Controllers:** `UnifiNetwork` reconciler (marshal the new fields),
  `UnifiFirewallZone` controller (empty on ZBF-unconfigured), `UnifiWifiBroadcast` behavior
  surfaced through the snapshot only.
- **CLI:** `snapshot firewall-zones` and `snapshot wifi` paths; fixtures and golden files.
- **Clients:** `operator/internal/unifi` and `cli/internal/unifi` (DHCP configuration +
  ZBF-unconfigured error mapping).
- **Docs:** `docs/unifi-api.md` (header casing, envelope, error body, version note);
  `docs/crd-conventions.md` if the DHCP shape note changes.

## Non-Goals

- No new managed resources or endpoints.
- Exposing any upstream `_id`/UUID in `spec`; the `NATIVE` network variant stays unexposed.
- Supporting the raw `DEVICES` broadcast filter.
- Live-controller calls in `go test`; tests stay hermetic and fixture-driven.
- Pinning to v10.4.57 only; the client's tolerance of unknown newer fields is retained.
