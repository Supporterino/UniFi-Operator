# unifi-switching Specification

## Purpose

Defines the switching Custom Resources — `UnifiSwitchingLag`, `UnifiSwitchingMcLagDomain`, and
`UnifiSwitchingSwitchStack` — on the Integration v1 API.

## Requirements

### Requirement: Switching resources reference sites and devices by identity

Every switching Custom Resource SHALL reference its parent `UnifiSite`, and any device membership
SHALL be expressed through typed selectors or `UnifiDeviceTag` references rather than opaque
device identifiers.

#### Scenario: Device selector resolves
- **WHEN** a switch stack lists member devices through a selector
- **THEN** the operator resolves the selector and applies the stack upstream

#### Scenario: No opaque identifiers in spec
- **WHEN** the generated CRD schemas for the switching kinds are inspected
- **THEN** none contains a field representing an opaque upstream identifier

### Requirement: LAG and MC-LAG configuration is declarative

`UnifiSwitchingLag` and `UnifiSwitchingMcLagDomain` SHALL expose their configurable attributes
(e.g. member ports, mode, hashing) as declarative `spec` fields.

#### Scenario: LAG applied
- **WHEN** a LAG defines its member ports and mode
- **THEN** the operator creates the LAG upstream and reports `Ready=True`

### Requirement: Switching status and cleanup are observable

Every switching Custom Resource SHALL expose `conditions`, `observedGeneration`, and a summary in
`status`, and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a switching resource reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`

#### Scenario: Deletion cleans up upstream
- **WHEN** a switching resource with a finalizer is deleted
- **THEN** the operator removes the upstream object and then removes the finalizer
