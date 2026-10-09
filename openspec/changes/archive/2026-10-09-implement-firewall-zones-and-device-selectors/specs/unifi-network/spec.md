## MODIFIED Requirements

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
