## Purpose

Defines `UnifiAclRule` and the site-global `UnifiAclRuleOrdering` Custom Resource for access
control rules on the Integration v1 API.

## ADDED Requirements

### Requirement: ACL rule type is validated

`UnifiAclRule.spec.type` SHALL be one of `IPV4` or `MAC`, and the corresponding source and
destination filter shape MUST match the selected type.

#### Scenario: Mismatched filters rejected
- **WHEN** a rule sets MAC filters while `spec.type` is `IPV4`
- **THEN** the API server rejects the object

### Requirement: ACL rule expresses typed filters

`UnifiAclRule.spec` SHALL express actions (`ALLOW`/`BLOCK`), source and destination filters, and
protocol filters through typed structures rather than opaque identifiers.

#### Scenario: Rule applied
- **WHEN** a rule defines a typed source filter and a `BLOCK` action
- **THEN** the operator applies the rule upstream and reports `Ready=True`

### Requirement: Enforcing device scope uses a typed selector

The devices that enforce an ACL rule SHALL be expressed through a typed selector, not a list of
opaque device identifiers.

#### Scenario: Default enforcement
- **WHEN** no enforcing-device selector is set
- **THEN** the rule is provisioned to all enforcing-capable devices upstream

### Requirement: ACL ordering is site-global

`UnifiAclRuleOrdering.spec` SHALL reference its parent `UnifiSite` and SHALL expose the flat
ordered ACL-rule list for the whole site (mirroring `orderedAclRuleIds[]`). Because CRD markers
cannot enforce cross-object uniqueness, the controller MUST detect duplicate site ordering
objects, set `Ready=False` on all of them, and apply no order until a single unambiguous object
remains.

#### Scenario: Order applied site-wide
- **WHEN** a single ordering object lists ACL rules in order
- **THEN** the operator applies that order for the site

#### Scenario: Duplicate ordering reports conflict
- **WHEN** two ordering objects target the same site
- **THEN** both report `Ready=False` and no order is applied

#### Scenario: Ordering reports drift
- **WHEN** an upstream ACL rule is present but absent from the ordering object
- **THEN** the drift is reported in the ordering object's `status`

### Requirement: ACL resources are namespaced with finalizers

Every ACL Custom Resource SHALL be namespaced, SHALL reference its parent `UnifiSite`, and SHALL
use a finalizer when deletion has upstream effects.

#### Scenario: Deletion cleans up upstream
- **WHEN** an ACL rule with a finalizer is deleted
- **THEN** the operator removes the upstream rule and then removes the finalizer
