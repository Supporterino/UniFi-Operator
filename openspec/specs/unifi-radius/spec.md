# unifi-radius Specification

## Purpose

Defines `UnifiRadiusProfile`, the Custom Resource representing a RADIUS authentication profile on
the Integration v1 API.

## Requirements

### Requirement: RADIUS profile is adopted by name and references its site

`UnifiRadiusProfile.spec` SHALL reference its parent `UnifiSite` through `spec.siteRef` (name only,
same namespace) and SHALL name the existing upstream profile to adopt through `spec.name`. It MUST
NOT contain a UniFi `_id`/UUID or any opaque identifier; the resolved upstream UUID belongs in
`status` only.

#### Scenario: Profile adopted by name
- **WHEN** a profile sets `spec.siteRef` and `spec.name` naming an existing upstream profile
- **THEN** the controller records the resolved upstream profile UUID in `status` and reports `Ready=True`

#### Scenario: No opaque identifier in spec
- **WHEN** the generated CRD schema for `UnifiRadiusProfile.spec` is inspected
- **THEN** it contains no field representing an opaque upstream identifier

#### Scenario: Unknown profile fails closed
- **WHEN** `spec.name` names no upstream profile in the site
- **THEN** the operator reports `Ready=False` and mutates nothing upstream

### Requirement: RADIUS profiles author no credentials and never leak secrets

Because the frozen surface exposes RADIUS profiles read-only, the operator SHALL author no shared
secret or credential on the CR. No secret material MUST appear in `spec`, in `status`, in events, or
in logs.

#### Scenario: No credential field in spec
- **WHEN** the generated CRD schema for `UnifiRadiusProfile.spec` is inspected
- **THEN** it contains no shared-secret or credential field

#### Scenario: Secret material not logged
- **WHEN** the operator adopts a RADIUS profile
- **THEN** no secret material appears in logs or events

### Requirement: Profile status and cleanup are observable

`UnifiRadiusProfile` SHALL expose `conditions`, `observedGeneration`, and a summary in `status`,
and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a profile reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`
