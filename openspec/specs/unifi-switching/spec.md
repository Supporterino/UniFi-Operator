# unifi-switching Specification

## Purpose

Defines the switching Custom Resources — `UnifiSwitchingLag`, `UnifiSwitchingMcLagDomain`, and
`UnifiSwitchingSwitchStack` — on the Integration v1 API.

## Requirements

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

### Requirement: Switching status and cleanup are observable

Every switching Custom Resource SHALL expose `conditions`, `observedGeneration`, and a summary in
`status`, and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a switching resource reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`

#### Scenario: Deletion cleans up upstream
- **WHEN** a switching resource with a finalizer is deleted
- **THEN** the operator removes the upstream object and then removes the finalizer
