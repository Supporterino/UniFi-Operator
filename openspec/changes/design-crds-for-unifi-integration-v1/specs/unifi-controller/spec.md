## Purpose

Defines `UnifiController`, the Custom Resource that represents a connection from the operator to
a single UniFi Network console over the Integration v1 API, and the credential and TLS handling
that connection requires.

## ADDED Requirements

### Requirement: Credentials come from a Secret

`UnifiController` SHALL read its UniFi API key from a Kubernetes `Secret` referenced by
`spec.secretRef`. It MUST NOT accept an inline API key in `spec`, and the API key MUST NOT
appear in `status`, events, or logs.

#### Scenario: API key resolved from Secret
- **WHEN** a `UnifiController` references an existing `Secret` containing the API key
- **THEN** the operator authenticates to the console and reports a reachable condition

#### Scenario: Missing Secret does not leak
- **WHEN** the referenced `Secret` is absent
- **THEN** `status.conditions` reports `Ready=False` with a non-credential reason and message

### Requirement: TLS verification is on by default

The operator SHALL verify controller TLS by default. Skipping verification MUST be opt-in via an
explicit `spec.insecureSkipVerify` field documented as insecure.

#### Scenario: Default verifies TLS
- **WHEN** `spec.insecureSkipVerify` is unset
- **THEN** a connection using an untrusted certificate fails with a certificate error condition

#### Scenario: Opt-out is explicit
- **WHEN** `spec.insecureSkipVerify` is `true`
- **THEN** the connection proceeds without certificate verification

### Requirement: Connection status is observable

`UnifiController` SHALL expose a `status` subresource with `conditions`, `observedGeneration`, a
human-readable summary, and the detected application version of the console.

#### Scenario: Reachability reported
- **WHEN** the controller queries `GET /proxy/network/integration/v1/info`
- **THEN** `status.conditions` includes `Ready=True` and `status.observedGeneration` equals
  `metadata.generation`

#### Scenario: App version recorded
- **WHEN** the operator reads `applicationVersion` from the application info response
- **THEN** the detected version is recorded in `status`

### Requirement: Capability availability is gated by the detected app version

`UnifiController` SHALL treat the Integration v1 surface as version-gated. A capability whose
minimum app version is unmet (Official API ≥10.1.78, networks CRUD ≥10.0.162, firewall/DNS
≥10.1.84) MUST fail closed with `Ready=False` rather than proceed.

#### Scenario: Unsupported capability fails closed
- **WHEN** the detected `applicationVersion` is below a capability's minimum
- **THEN** `status.conditions` reports `Ready=False` with a version reason and the operator does
  not attempt that capability

### Requirement: Resource is namespaced

`UnifiController` SHALL be a namespaced resource, and references to it MUST resolve within the
same namespace. No `namespace` field is exposed on references.

#### Scenario: Same-namespace resolution
- **WHEN** a dependent object references a `UnifiController` in its own namespace
- **THEN** the reference resolves without a namespace field
