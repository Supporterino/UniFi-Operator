# Spec Delta

## REMOVED Requirements

### Requirement: Switching resources reference sites and devices by identity
**Reason**: The frozen v10.4.57 surface exposes switching objects read-only
(`GET .../switching/lags`, `/mc-lag-domains`, `/switch-stacks`), so device membership and
configuration cannot be authored upstream; they are observed in `status` only. This also removes the
non-existent `UnifiDeviceTag` reference.
**Migration**: Not applicable — the kinds had not shipped. Adopt an existing upstream object;
device membership is reported, not authored.

### Requirement: LAG and MC-LAG configuration is declarative
**Reason**: No create/update endpoint exists for LAGs, MC-LAG domains, or switch stacks, so member
ports, mode, and hashing cannot be authored; they are observed upstream.
**Migration**: Not applicable — the kinds had not shipped. Adopt an existing upstream object;
configuration is reported in `status`.

## ADDED Requirements

### Requirement: Switching resources reference their site and adopt upstream objects by identity

Every switching Custom Resource SHALL reference its parent `UnifiSite` through `spec.siteRef` (name
only, same namespace). `UnifiSwitchingMcLagDomain` and `UnifiSwitchingSwitchStack` SHALL adopt an
existing upstream object by `spec.name`. No switching `spec` MUST contain a UniFi `_id`/UUID or
opaque identifier; resolved upstream UUIDs belong in `status` only.

#### Scenario: MC-LAG domain or switch stack adopted by name
- **WHEN** an MC-LAG domain or switch stack sets `spec.siteRef` and `spec.name` naming an existing upstream object
- **THEN** the controller records the resolved upstream UUID in `status` and reports `Ready=True`

#### Scenario: No opaque identifiers in spec
- **WHEN** the generated CRD schemas for the switching kinds are inspected
- **THEN** none contains a field representing an opaque upstream identifier

### Requirement: UnifiSwitchingLag is adopted by a typed composite selector

Because the frozen surface exposes LAGs without a name, `UnifiSwitchingLag` SHALL adopt by a typed
composite selector: `spec.type` (`LOCAL`/`MULTI_CHASSIS`/`SWITCH_STACK`) plus a member selector
expressed through device-tag selectors and port indexes. The operator MUST select exactly one
matching upstream LAG or fail closed.

#### Scenario: LAG adopted by typed composite selector
- **WHEN** a LAG sets `spec.type` and a member selector matching exactly one upstream LAG
- **THEN** the controller records the resolved upstream UUID in `status` and reports `Ready=True`

#### Scenario: LAG selector matches nothing or is ambiguous
- **WHEN** a LAG's typed selector matches no upstream LAG, or more than one
- **THEN** the operator reports `Ready=False` and records no upstream UUID

### Requirement: Switching configuration is observed in status, not authored

Because the frozen surface exposes switching read-only, the switching Custom Resources SHALL report
their upstream member ports, mode, and hashing only in `status`. Member devices MUST NOT be authored
except as the typed device-tag selector used to adopt a `UnifiSwitchingLag`.

#### Scenario: Configuration observed
- **WHEN** a LAG is adopted
- **THEN** its member ports and mode are reported in `status`, not read from `spec`
