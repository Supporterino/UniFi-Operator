# unifi-traffic-matching Specification

## Purpose

Defines `UnifiTrafficMatchingList`, the Custom Resource representing a reusable list of IP
addresses, IPv6 addresses, or ports on the Integration v1 API.

## Requirements

### Requirement: Matching list type is validated

`UnifiTrafficMatchingList.spec.type` SHALL be one of `IPV4_ADDRESSES`, `IPV6_ADDRESSES`, or
`PORTS`, and entries MUST conform to the selected type.

#### Scenario: Invalid entry rejected
- **WHEN** a `PORTS` list contains a non-numeric entry
- **THEN** the API server rejects the object

### Requirement: Matching list entries are declarative

`UnifiTrafficMatchingList.spec` SHALL express its entries as typed values and ranges, and MUST NOT
contain a UniFi `_id` or opaque list identifier.

#### Scenario: List applied
- **WHEN** a matching list defines a set of ports or address ranges
- **THEN** the operator creates the list upstream and reports `Ready=True`

### Requirement: Matching lists are site-scoped

`UnifiTrafficMatchingList.spec` SHALL reference its parent `UnifiSite` through `spec.siteRef` and
SHALL be owned by that site.

#### Scenario: List scoped to a site
- **WHEN** a matching list sets `spec.siteRef`
- **THEN** the operator reconciles the list through the site's controller and owns the list from
  the site

### Requirement: Matching list is referenceable by other resources

Other firewall Custom Resources SHALL be able to reference a `UnifiTrafficMatchingList` by
Kubernetes identity, and the controller MUST resolve that reference to the upstream list.

#### Scenario: Referenced by a firewall policy
- **WHEN** a firewall policy references a matching list by name
- **THEN** the operator resolves the reference and applies the list's contents in the policy

### Requirement: Matching list status and cleanup are observable

`UnifiTrafficMatchingList` SHALL expose `conditions`, `observedGeneration`, and a summary in
`status`, and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a matching list reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`
