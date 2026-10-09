# unifi-wifi Specification

## Purpose

Defines `UnifiWifiBroadcast`, the Custom Resource representing a WiFi broadcast (SSID) and its
security configuration on the Integration v1 API.

## Requirements

### Requirement: Broadcast references a network

`UnifiWifiBroadcast.spec` SHALL reference its underlying `UnifiNetwork` by Kubernetes identity and
MUST NOT contain an opaque upstream network identifier.

#### Scenario: Network resolved
- **WHEN** a broadcast references a network by name
- **THEN** the operator resolves the reference and applies the broadcast upstream

#### Scenario: Broadcast owned by its network
- **WHEN** a broadcast references a `UnifiNetwork`
- **THEN** the operator sets an `ownerReference` from the broadcast to the network

### Requirement: Broadcast type is validated

`UnifiWifiBroadcast.spec.type` SHALL be one of `STANDARD` or `IOT_OPTIMIZED`, and
variant-specific fields MUST only be accepted for the matching value.

#### Scenario: Mismatched variant rejected
- **WHEN** a broadcast sets fields for the wrong `type`
- **THEN** the API server rejects the object

### Requirement: Secrets are referenced, never inline

WiFi security configuration SHALL express passphrases and enterprise secrets through references
to Kubernetes `Secret` objects. Secrets MUST NOT appear in `spec` inline, in `status`, in events,
or in logs.

#### Scenario: Passphrase from Secret
- **WHEN** a personal security configuration references a `Secret`
- **THEN** the operator reads the passphrase at reconcile time and never records it in status

#### Scenario: Secret not logged
- **WHEN** the operator applies a security configuration
- **THEN** no secret material appears in logs or events

### Requirement: Broadcasting scope uses typed selectors

The set of devices that broadcast the WiFi network SHALL be expressed through a typed selector
rather than a list of opaque device identifiers.

#### Scenario: Selector scopes broadcast
- **WHEN** a broadcast sets a device scope selector
- **THEN** the operator resolves the scope and applies it upstream

### Requirement: Broadcast status and cleanup are observable

`UnifiWifiBroadcast` SHALL expose `conditions`, `observedGeneration`, and a summary in `status`,
and SHALL use a finalizer when deletion has upstream effects.

#### Scenario: Readiness reported
- **WHEN** a broadcast reconciles successfully
- **THEN** `status.conditions` includes `Ready=True`
