# Spec Delta

## MODIFIED Requirements

### Requirement: Enterprise security references a RADIUS profile

Enterprise security variants (WPA2/WPA3) SHALL reference their upstream authentication
configuration (`radiusConfiguration.profileId`) through
`spec.securityConfiguration.radiusProfileRef` to a `UnifiRadiusProfile` by Kubernetes identity,
and MUST NOT expose the opaque profile UUID. The controller SHALL resolve the referenced profile
and provision it upstream, and MUST fail closed with `Ready=False` without mutating the upstream
broadcast when that reference cannot be resolved.

#### Scenario: Enterprise profile reference is not an opaque ID
- **WHEN** the generated CRD schema for an enterprise security variant is inspected
- **THEN** the schema does not include a raw RADIUS profile UUID

#### Scenario: Enterprise fails closed until the profile kind lands
- **WHEN** an enterprise-secured broadcast references a `UnifiRadiusProfile`
- **THEN** the operator resolves the referenced profile and provisions the enterprise
  configuration upstream, so the former fail-closed path no longer applies now that the profile
  kind exists

#### Scenario: Unresolvable profile fails closed
- **WHEN** an enterprise-secured broadcast references a `UnifiRadiusProfile` that cannot be resolved
- **THEN** the operator reports `Ready=False` and does not mutate the upstream broadcast
