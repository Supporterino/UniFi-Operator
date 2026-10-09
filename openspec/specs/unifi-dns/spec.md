# unifi-dns Specification

## Purpose

Defines `UnifiDnsPolicy`, the Custom Resource representing a UniFi DNS policy or record on the
Integration v1 API.

## Requirements

### Requirement: DNS policy variant is validated

`UnifiDnsPolicy.spec.type` SHALL be one of `A_RECORD`, `AAAA_RECORD`, `CNAME_RECORD`,
`MX_RECORD`, `SRV_RECORD`, `TXT_RECORD`, or `FORWARD_DOMAIN`, and variant-specific fields MUST
only be accepted for the matching value.

#### Scenario: Mismatched variant rejected
- **WHEN** a record sets fields for a different `type`
- **THEN** the API server rejects the object

#### Scenario: Record content is declarative
- **WHEN** a `A_RECORD` policy sets a hostname and address
- **THEN** the operator creates the record upstream and reports `Ready=True`

### Requirement: DNS policy is scoped to a site and references no opaque IDs

`UnifiDnsPolicy.spec` SHALL reference its parent `UnifiSite` and MUST NOT contain a UniFi `_id`
or opaque record identifier.

#### Scenario: Spec exposes no opaque identifier
- **WHEN** the generated CRD schema for `UnifiDnsPolicy.spec` is inspected
- **THEN** it contains no field representing an opaque upstream identifier

### Requirement: DNS policy status and cleanup are observable

`UnifiDnsPolicy` SHALL expose `conditions`, `observedGeneration`, and a summary in `status`, and
SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a DNS policy reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`

#### Scenario: Deletion cleans up upstream
- **WHEN** a DNS policy with a finalizer is deleted
- **THEN** the operator removes the upstream policy and then removes the finalizer
