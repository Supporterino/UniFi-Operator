# Spec Delta

## REMOVED Requirements

### Requirement: RADIUS secrets are referenced, never inline
**Reason**: The frozen Integration v1 v10.4.57 surface exposes RADIUS profiles read-only
(`GET /v1/sites/{siteId}/radius/profiles`, schema `{id, metadata, name}`); no writable secret is
exposed, so the CR authors no credential.
**Migration**: Not applicable — `UnifiRadiusProfile` had not shipped. Adopt an existing upstream
profile by name and author no shared secret.

### Requirement: Profile references its site and servers declaratively
**Reason**: v10.4.57 exposes no RADIUS server or port fields and no create/update endpoint, so
servers and ports can be neither authored nor observed.
**Migration**: Not applicable — the kind had not shipped. Set `spec.siteRef` and `spec.name` to
adopt an existing upstream profile.

## ADDED Requirements

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
