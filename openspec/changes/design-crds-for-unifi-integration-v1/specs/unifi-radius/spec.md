## Purpose

Defines `UnifiRadiusProfile`, the Custom Resource representing a RADIUS authentication profile on
the Integration v1 API.

## ADDED Requirements

### Requirement: RADIUS secrets are referenced, never inline

RADIUS shared secrets and credentials SHALL be expressed through references to Kubernetes
`Secret` objects. Secrets MUST NOT appear inline in `spec`, in `status`, in events, or in logs.

#### Scenario: Shared secret from Secret
- **WHEN** a profile references a `Secret` for its shared secret
- **THEN** the operator reads the secret at reconcile time and never records it in status

#### Scenario: Secret not logged
- **WHEN** the operator applies a RADIUS profile
- **THEN** no secret material appears in logs or events

### Requirement: Profile references its site and servers declaratively

`UnifiRadiusProfile.spec` SHALL reference its parent `UnifiSite` and SHALL declare its
authentication and accounting servers through typed structures rather than opaque identifiers.

#### Scenario: Profile applied
- **WHEN** a profile declares its servers and server ports
- **THEN** the operator creates the profile upstream and reports `Ready=True`

### Requirement: Profile status and cleanup are observable

`UnifiRadiusProfile` SHALL expose `conditions`, `observedGeneration`, and a summary in `status`,
and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a profile reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`
