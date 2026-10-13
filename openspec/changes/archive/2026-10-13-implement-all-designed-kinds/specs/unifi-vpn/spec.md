# Spec Delta

## REMOVED Requirements

### Requirement: VPN server type is validated
**Reason**: The frozen v10.4.57 surface exposes VPN servers read-only
(`GET .../vpn/servers`, overview `{enabled, id, metadata, name, type}`); there are no authored
variant-specific fields to validate, and `type` is observed.
**Migration**: Not applicable — the kind had not shipped. Adopt an existing upstream server by
name; its `type` is reported in `status`.

### Requirement: VPN secrets are referenced, never inline
**Reason**: The read-only VPN surface exposes no pre-shared key or other credential, so no secret
is authored or read.
**Migration**: Not applicable — the kinds had not shipped. Adopt an existing upstream object by
name; author no credential.

### Requirement: VPN resources reference their site and peers by identity
**Reason**: The read-only surface exposes no write endpoint, so tunnel peer endpoints and
local/remote configuration cannot be authored; they are observed upstream.
**Migration**: Not applicable — the kinds had not shipped. Set `spec.siteRef` and `spec.name` to
adopt an existing upstream object.

## ADDED Requirements

### Requirement: VPN resources are adopted by name and reference their site

`UnifiVpnServer` and `UnifiSiteToSiteTunnel` SHALL reference their parent `UnifiSite` through
`spec.siteRef` (name only, same namespace) and SHALL name the existing upstream object to adopt
through `spec.name`. Neither MUST contain a UniFi `_id`/UUID or any opaque identifier; resolved
upstream UUIDs and the observed `type`/`enabled` belong in `status` only.

#### Scenario: Tunnel adopted by name
- **WHEN** a tunnel sets `spec.siteRef` and `spec.name` naming an existing upstream tunnel
- **THEN** the controller records the resolved upstream UUID in `status` and reports `Ready=True`

#### Scenario: Server adopted by name
- **WHEN** a VPN server sets `spec.siteRef` and `spec.name` naming an existing upstream server
- **THEN** the controller records the resolved upstream UUID and its observed `type` in `status`

#### Scenario: No opaque identifier in spec
- **WHEN** the generated CRD schemas for the VPN kinds are inspected
- **THEN** none contains a field representing an opaque upstream identifier

### Requirement: VPN adoption authors no credentials and never leaks secrets

Because the frozen surface exposes VPN resources read-only, the operator SHALL author no pre-shared
key or credential on the CR. No secret material MUST appear in `spec`, in `status`, in events, or in
logs.

#### Scenario: No credential field in spec
- **WHEN** the generated CRD schemas for the VPN kinds are inspected
- **THEN** none contains a pre-shared-key or credential field

#### Scenario: Secret material not logged
- **WHEN** the operator adopts VPN configuration
- **THEN** no secret material appears in logs or events
