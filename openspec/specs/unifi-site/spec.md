# unifi-site Specification

## Purpose

Defines `UnifiSite`, the parent anchor for site-scoped resources. It binds a UniFi site to a
`UnifiController` and owns the lifecycle of the child Custom Resources that reference it.

## Requirements

### Requirement: Site binds to a controller

`UnifiSite` SHALL reference a `UnifiController` through `spec.controllerRef`. The site MUST NOT
carry connection details of its own.

#### Scenario: Controller resolved
- **WHEN** a `UnifiSite` references an existing `UnifiController`
- **THEN** the operator connects through that controller and reports `Ready=True`

#### Scenario: Missing controller prevents reconcile
- **WHEN** `spec.controllerRef` names a non-existent `UnifiController`
- **THEN** the site reports `Ready=False` and does not proceed to mutate upstream state

### Requirement: Site adopts an existing upstream site by name

`UnifiSite.spec.internalReference` SHALL name an existing upstream site (`GET /v1/sites`). The
operator SHALL adopt that site and MUST NOT create or delete upstream sites. The upstream site
UUID MUST be recorded in `status` only, never in `spec`.

#### Scenario: Existing site adopted
- **WHEN** `spec.internalReference` matches an upstream site
- **THEN** the operator records the site UUID in `status` and reports `Ready=True`

#### Scenario: Missing upstream site fails closed
- **WHEN** `spec.internalReference` matches no upstream site
- **THEN** the site reports `Ready=False` and no upstream state is changed

#### Scenario: Upstream ID recorded in status
- **WHEN** the site is resolved against the console
- **THEN** its UUID appears under `status` and no opaque identifier appears under `spec`

### Requirement: Site owns its child resources

Child Custom Resources whose strongest reference is `spec.siteRef` to a `UnifiSite` SHALL be owned
by that site via an `ownerReference`, set by the child's own reconciler.

#### Scenario: Owner reference set
- **WHEN** a child resource references a `UnifiSite`
- **THEN** the operator sets an `ownerReference` from the child to the site

### Requirement: Site deletion drains children before removal

`UnifiSite` SHALL use a finalizer that blocks site removal until every owned child has finished
its own finalizer (which needs `spec.siteRef` to reach the controller). Once no owned children
remain, the finalizer is removed and the adoption binding disappears. Deleting the `UnifiSite`
SHALL NOT delete the upstream site.

#### Scenario: Children drained before site removal
- **WHEN** a `UnifiSite` with owned children is deleted
- **THEN** the site remains until each child's finalizer completes, then the site is removed

#### Scenario: Upstream site is not deleted
- **WHEN** a `UnifiSite` is deleted
- **THEN** the upstream UniFi site is left intact

### Requirement: Site status is observable

`UnifiSite` SHALL expose `conditions`, `observedGeneration`, and a human-readable summary in
`status`.

#### Scenario: Readiness reported
- **WHEN** the site resolves successfully
- **THEN** `status.conditions` includes `Ready=True` and `observedGeneration` equals
  `metadata.generation`

### Requirement: Resource is namespaced

`UnifiSite` SHALL be a namespaced resource, and `spec.controllerRef` MUST resolve within the same
namespace. No `namespace` field is exposed on references.

#### Scenario: Same-namespace resolution
- **WHEN** a `UnifiSite` and its `UnifiController` share a namespace
- **THEN** the reference resolves without an explicit namespace
