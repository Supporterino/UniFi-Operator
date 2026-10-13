# unifi-vpn Specification

## Purpose

Defines `UnifiVpnServer` and `UnifiSiteToSiteTunnel`, the Custom Resources representing VPN
servers and site-to-site tunnels on the Integration v1 API.

## Requirements

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

### Requirement: VPN status and cleanup are observable

Every VPN Custom Resource SHALL expose `conditions`, `observedGeneration`, and a summary in
`status`, and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a VPN resource reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`
