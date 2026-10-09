## Purpose

Defines the firewall Custom Resources: `UnifiFirewallZone` (which owns network membership),
`UnifiFirewallPolicy`, and per-zone-pair `UnifiFirewallPolicyOrdering`.

## ADDED Requirements

### Requirement: Zone owns network membership

`UnifiFirewallZone.spec` SHALL list member networks as references to `UnifiNetwork` Custom
Resources by Kubernetes identity. Network membership MUST NOT be authored on `UnifiNetwork`.

#### Scenario: Membership applied to upstream zone
- **WHEN** a `UnifiFirewallZone` lists network references
- **THEN** the operator resolves each reference and sets the upstream zone's network membership

#### Scenario: Default/system zones are not overwritten
- **WHEN** an upstream zone is reported as system-defined
- **THEN** the operator does not overwrite its configuration and reports a failing condition

### Requirement: Policy references zones and typed filters

`UnifiFirewallPolicy.spec` SHALL reference configurable source and destination zones by
Kubernetes identity (`UnifiFirewallZone`) and SHALL target built-in/system zones through a typed
built-in-zone selector. Traffic filters SHALL be expressed through typed selectors rather than
opaque identifiers.

#### Scenario: Zone references resolved
- **WHEN** a policy references source and destination zones by name
- **THEN** the operator resolves both and applies the policy upstream

#### Scenario: Built-in zone targeted
- **WHEN** a policy targets a system-defined zone through the built-in-zone selector
- **THEN** the operator resolves it to the upstream zone and applies the policy

#### Scenario: Unresolvable filter fails closed
- **WHEN** a filter selector cannot be resolved to an upstream object
- **THEN** the operator sets `Ready=False` and does not mutate the policy upstream

### Requirement: Ordering is per zone pair and mirrors the before/after-system partition

`UnifiFirewallPolicyOrdering.spec` SHALL identify a source/destination zone pair by reference and
SHALL expose two ordered lists — `beforeSystemDefined` and `afterSystemDefined` — mirroring the
upstream DTO. System-defined policies are anchored and MUST NOT be reordered. Because CRD markers
cannot enforce cross-object uniqueness, the controller MUST detect duplicate ordering objects for
the same zone pair, set `Ready=False` on all of them, and apply no order until a single
unambiguous object remains.

#### Scenario: Order applied
- **WHEN** a single ordering object lists policy references for a zone pair
- **THEN** the operator applies the before/after-system order upstream for the pair

#### Scenario: Duplicate ordering reports conflict
- **WHEN** two ordering objects target the same zone pair in the same site
- **THEN** both report `Ready=False` and no order is applied

### Requirement: Ordering reports drift rather than silently reordering

Each ordering object SHALL be authoritative for its user-defined scope. User policies present
upstream but absent from the ordering object MUST be reported as drift in `status` rather than
silently reordered.

#### Scenario: Drift reported
- **WHEN** an upstream user policy for the zone pair is not listed in the ordering object
- **THEN** the ordering object's `status` reports the drift

### Requirement: Firewall resources are namespaced with finalizers

Every firewall Custom Resource SHALL be namespaced, SHALL reference its parent `UnifiSite`, and
SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Deletion cleans up upstream
- **WHEN** a firewall resource with a finalizer is deleted
- **THEN** the operator removes the upstream object and then removes the finalizer
