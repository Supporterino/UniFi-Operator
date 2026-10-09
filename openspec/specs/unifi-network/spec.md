# unifi-network Specification

## Purpose

Defines `UnifiNetwork`, the Custom Resource representing a UniFi network on the Integration v1
API, including its gateway-managed configuration and its observed firewall-zone membership.

## Requirements

### Requirement: Network references its site

`UnifiNetwork` SHALL reference its parent `UnifiSite` through `spec.siteRef`. The `spec` MUST NOT
contain a UniFi `_id` or any opaque identifier.

#### Scenario: Site reference drives reconcile
- **WHEN** a `UnifiNetwork` sets `spec.siteRef` to a ready `UnifiSite`
- **THEN** the operator reconciles the network through the site's controller

#### Scenario: Spec exposes no opaque identifier
- **WHEN** the generated CRD schema for `UnifiNetwork.spec` is inspected
- **THEN** it contains no field representing a UniFi internal `_id` or opaque numeric ID

### Requirement: Management discriminator is validated

`UnifiNetwork.spec.management` SHALL be one of `GATEWAY`, `SWITCH`, or `UNMANAGED`, and
variant-specific fields MUST only be accepted for the matching value.

#### Scenario: Mismatched variant rejected
- **WHEN** a `UnifiNetwork` sets gateway-only configuration while `spec.management` is `UNMANAGED`
- **THEN** the API server rejects the object

### Requirement: Gateway configuration is declarative

For `management: GATEWAY`, `UnifiNetwork.spec` SHALL expose the v10.4.57 gateway union: `vlanId`,
`enabled`, `dhcpGuarding`, `ipv4Configuration`, optional `ipv6Configuration`,
`isolationEnabled`, `mdnsForwardingEnabled`, `internetAccessEnabled`, and
`cellularBackupEnabled`. For `management: SWITCH`, it SHALL expose `cellularBackupEnabled`,
`isolationEnabled`, `ipv4Configuration`, and a device binding. For `management: UNMANAGED`, it
SHALL expose only the common fields (`name`, `enabled`, `vlanId`, `dhcpGuarding`).

#### Scenario: VLAN validated
- **WHEN** `spec.vlanId` is outside the accepted range
- **THEN** the API server rejects the object

#### Scenario: Enabling isolation
- **WHEN** `spec.isolationEnabled` is set to `true`
- **THEN** the operator makes the upstream network isolated and reports `Ready=True`

### Requirement: Switch device binding uses a typed selector

For `management: SWITCH`, a network's device binding SHALL be expressed through a typed,
name-based device-tag selector and MUST NOT expose a raw device UUID and MUST NOT reference a
device-tag Custom Resource. The controller SHALL resolve the selector through the read-only
device-tags list (`GET /v1/sites/{siteId}/device-tags`). The binding SHALL resolve to exactly one
device; when the selector matches no device or more than one device the controller MUST fail closed
with `Ready=False` and MUST NOT modify the upstream network.

#### Scenario: Switch device binding
- **WHEN** a switch-managed network binds a device through the device-tag selector
- **THEN** `spec` contains the selected tag name and no opaque device identifier

#### Scenario: Single-device tag resolves
- **WHEN** the selected device tag resolves to exactly one upstream device
- **THEN** the operator provisions the switch-managed network with that device and reports `Ready=True`

#### Scenario: Ambiguous tag fails closed
- **WHEN** the selected device tag resolves to zero devices or more than one device
- **THEN** `status.conditions` reports `Ready=False` and no upstream network is modified

### Requirement: Zone membership is observed, not authored

A network's firewall-zone membership SHALL NOT be authorable on `UnifiNetwork`. The controller
SHALL omit `zoneId` on writes and SHALL report the resolved zone in `status` only.

#### Scenario: Zone reported in status
- **WHEN** the operator reads the network from the console
- **THEN** the associated zone is reported under `status` and no zone field exists under `spec`

### Requirement: Network status is observable

`UnifiNetwork` SHALL expose `conditions`, `observedGeneration`, a human-readable summary, and the
upstream correlation identifier in `status`.

#### Scenario: Readiness reported
- **WHEN** a `UnifiNetwork` reconciles successfully
- **THEN** `status.conditions` includes `Ready=True` and `observedGeneration` equals
  `metadata.generation`

### Requirement: Deletion cleans up upstream state

`UnifiNetwork` SHALL use a finalizer to remove its upstream network before the object is
finalized, adding and removing the finalizer symmetrically.

#### Scenario: Finalizer removes upstream network
- **WHEN** a `UnifiNetwork` with the finalizer set is deleted
- **THEN** the operator deletes the upstream network and then removes the finalizer
