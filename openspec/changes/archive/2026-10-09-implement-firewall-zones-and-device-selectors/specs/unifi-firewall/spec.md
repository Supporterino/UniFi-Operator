## MODIFIED Requirements

### Requirement: Zone owns network membership

`UnifiFirewallZone.spec` SHALL list member networks as references to `UnifiNetwork` Custom
Resources by Kubernetes identity. Network membership MUST NOT be authored on `UnifiNetwork`. The
controller SHALL create, update, and delete only user-defined (custom) zones; a system-defined
upstream zone MUST be adopted read-only and MUST NOT be created, updated, or deleted.

#### Scenario: Membership applied to upstream zone
- **WHEN** a `UnifiFirewallZone` lists network references
- **THEN** the operator resolves each reference and sets the upstream zone's network membership

#### Scenario: Custom zone lifecycle
- **WHEN** a `UnifiFirewallZone` is created, changed, or deleted
- **THEN** the operator creates, updates, or deletes the matching custom upstream zone

#### Scenario: Default/system zones are not overwritten
- **WHEN** a `UnifiFirewallZone` resolves to a system-defined upstream zone
- **THEN** the operator does not create, update, or delete it and reports a failing condition

## ADDED Requirements

### Requirement: Network membership has a single writer

Because CRD markers cannot enforce cross-object uniqueness, when more than one
`UnifiFirewallZone` in the same site claims the same `UnifiNetwork`, the controller MUST detect the
conflict, set `Ready=False` on every conflicting zone, and MUST NOT write membership upstream until
a single zone remains.

#### Scenario: Duplicate membership reports conflict
- **WHEN** two `UnifiFirewallZone` objects list the same `UnifiNetwork`
- **THEN** both report `Ready=False` and no membership is written upstream

#### Scenario: Cross-site network reference fails closed
- **WHEN** a `UnifiFirewallZone` lists a `UnifiNetwork` whose site differs from the zone's site
- **THEN** the zone reports `Ready=False` and writes no membership upstream

### Requirement: Zone names are unique within a site

Because CRD markers cannot enforce cross-object uniqueness, when two `UnifiFirewallZone` objects in
the same site set the same `spec.name`, the controller MUST detect the conflict, set `Ready=False`
on both, and MUST NOT create or update an upstream zone until a single object remains.

#### Scenario: Duplicate zone name reports conflict
- **WHEN** two `UnifiFirewallZone` objects in the same site set the same `spec.name`
- **THEN** both report `Ready=False` and neither creates or updates an upstream zone
