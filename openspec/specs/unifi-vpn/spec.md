# unifi-vpn Specification

## Purpose

Defines `UnifiVpnServer` and `UnifiSiteToSiteTunnel`, the Custom Resources representing VPN
servers and site-to-site tunnels on the Integration v1 API.

## Requirements

### Requirement: VPN server type is validated

`UnifiVpnServer.spec.type` SHALL be one of the server types the upstream API supports (for
example `WIREGUARD`, `OPENVPN`, `L2TP`, `PPTP`, or `UID`), and variant-specific fields MUST only
be accepted for the matching value.

#### Scenario: Mismatched variant rejected
- **WHEN** a server sets fields for the wrong `type`
- **THEN** the API server rejects the object

### Requirement: VPN secrets are referenced, never inline

Pre-shared keys and other VPN credentials SHALL be expressed through references to Kubernetes
`Secret` objects. Secrets MUST NOT appear inline in `spec`, in `status`, in events, or in logs.

#### Scenario: Pre-shared key from Secret
- **WHEN** a tunnel references a `Secret` for its pre-shared key
- **THEN** the operator reads the key at reconcile time and never records it in status

#### Scenario: Secret not logged
- **WHEN** the operator applies VPN configuration
- **THEN** no secret material appears in logs or events

### Requirement: VPN resources reference their site and peers by identity

VPN Custom Resources SHALL reference their parent `UnifiSite`, and site-to-site tunnels SHALL
reference their peer endpoints declaratively rather than by opaque identifier.

#### Scenario: Tunnel applied
- **WHEN** a site-to-site tunnel defines its local and remote endpoints
- **THEN** the operator creates the tunnel upstream and reports `Ready=True`

### Requirement: VPN status and cleanup are observable

Every VPN Custom Resource SHALL expose `conditions`, `observedGeneration`, and a summary in
`status`, and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a VPN resource reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`
