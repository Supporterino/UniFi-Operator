# unifi-device-tags Specification

## Purpose

Defines `UnifiDeviceTag`, the Custom Resource representing a named device tag that other
resources can use to select devices by typed selector.

## Requirements

### Requirement: Device tag is declarative

`UnifiDeviceTag.spec` SHALL define a tag by its name and MUST NOT contain an opaque upstream tag
identifier. The upstream identifier MUST be recorded in `status` only.

#### Scenario: Tag applied
- **WHEN** a device tag is created
- **THEN** the operator creates the tag upstream and reports `Ready=True`

#### Scenario: Spec exposes no opaque identifier
- **WHEN** the generated CRD schema for `UnifiDeviceTag.spec` is inspected
- **THEN** it contains no field representing an opaque upstream identifier

### Requirement: Device tags are site-scoped

`UnifiDeviceTag.spec` SHALL reference its parent `UnifiSite` through `spec.siteRef` and SHALL be
owned by that site.

#### Scenario: Tag scoped to a site
- **WHEN** a device tag sets `spec.siteRef`
- **THEN** the operator reconciles the tag through the site's controller and owns the tag from the
  site

### Requirement: Device tags are usable as selectors

Other Custom Resources SHALL be able to select devices by referencing `UnifiDeviceTag` objects,
and the controller MUST resolve such selectors to the upstream device set.

#### Scenario: Tag referenced by a selector
- **WHEN** a firewall or switching resource selects devices by a device-tag reference
- **THEN** the operator resolves the tag to its member devices upstream

### Requirement: Device tag status and cleanup are observable

`UnifiDeviceTag` SHALL expose `conditions`, `observedGeneration`, and a summary in `status`, and
SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Deletion cleans up upstream
- **WHEN** a device tag with a finalizer is deleted
- **THEN** the operator removes the upstream tag and then removes the finalizer
