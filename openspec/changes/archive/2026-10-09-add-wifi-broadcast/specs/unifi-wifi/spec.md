## MODIFIED Requirements

### Requirement: Broadcast references a network

`UnifiWifiBroadcast.spec` SHALL reference its underlying `UnifiNetwork` by Kubernetes identity
through `spec.networkRef` (name only, same namespace) and MUST NOT contain an opaque upstream
network identifier. The controller SHALL resolve the network's upstream network UUID and SHALL
resolve the site transitively through the network's `spec.siteRef`. The upstream `network`
`NATIVE`/`SPECIFIC` union SHALL be written as the `SPECIFIC` variant; the `NATIVE` variant MUST
NOT be exposed in `spec`. `spec.networkRef` SHALL be immutable: once set, a change MUST be
rejected by the API server rather than re-parenting the broadcast or leaving its prior upstream
network unstyled.

#### Scenario: Network resolved
- **WHEN** a broadcast references a network by name
- **THEN** the operator resolves the reference and applies the broadcast upstream

#### Scenario: Broadcast owned by its network
- **WHEN** a broadcast references a `UnifiNetwork`
- **THEN** the operator sets an `ownerReference` from the broadcast to the network

#### Scenario: Site resolved transitively
- **WHEN** a broadcast is reconciled and its referenced network's site has adopted an upstream site
- **THEN** the operator resolves the site through the network reference and applies the broadcast

#### Scenario: Native network variant not exposed
- **WHEN** the generated CRD schema for `UnifiWifiBroadcast.spec` is inspected
- **THEN** it contains no field representing a raw network UUID and no `NATIVE` selector

#### Scenario: Repointing the network is rejected
- **WHEN** an existing broadcast changes `spec.networkRef` to a different `UnifiNetwork`
- **THEN** the API server rejects the update

### Requirement: Broadcast type is validated

`UnifiWifiBroadcast.spec.type` SHALL be one of `STANDARD` or `IOT_OPTIMIZED`, and variant-specific
fields MUST only be accepted for the matching value. The `STANDARD` variant SHALL require its
variant fields (`advertiseDeviceName`, `arpProxyEnabled`, `broadcastingFrequenciesGHz`,
`bssTransitionEnabled`); the `IOT_OPTIMIZED` variant SHALL accept only the common fields. The
common broadcast fields SHALL be modeled faithfully, including the optional nested configuration
trees the v10.4.57 union defines.

#### Scenario: Mismatched variant rejected
- **WHEN** a broadcast sets fields for the wrong `type`
- **THEN** the API server rejects the object

#### Scenario: Standard variant applied
- **WHEN** a `STANDARD` broadcast sets its broadcasting frequencies and variant flags
- **THEN** the operator applies them upstream and reports `Ready=True`

#### Scenario: IoT-optimized variant applied
- **WHEN** an `IOT_OPTIMIZED` broadcast sets only common fields
- **THEN** the operator applies it upstream and reports `Ready=True`

### Requirement: Secrets are referenced, never inline

WiFi security configuration SHALL express passphrases through references to Kubernetes `Secret`
objects: the personal `passphrase` and every `presharedKeys[].passphrase` SHALL be a
same-namespace `Secret` key selector. Secrets MUST NOT appear in `spec` inline, in `status`, in
events, or in logs.

#### Scenario: Passphrase from Secret
- **WHEN** a personal security configuration references a `Secret`
- **THEN** the operator reads the passphrase at reconcile time and never records it in status

#### Scenario: Preshared key from Secret
- **WHEN** a broadcast lists per-network preshared keys and each references a `Secret`
- **THEN** the operator reads each passphrase at reconcile time and never records it in status

#### Scenario: Secret not logged
- **WHEN** the operator applies a security configuration
- **THEN** no secret material appears in logs or events

### Requirement: Broadcasting scope uses typed selectors

The set of devices that broadcast the WiFi network SHALL be expressed through typed, name-based
`DeviceTagSelector` entries (`spec.deviceTags`) resolved against the read-only device-tags list
(`GET /v1/sites/{siteId}/device-tags`) rather than a list of opaque device identifiers. The raw
`DEVICES` (`deviceIds`) filter MUST NOT be exposed. Omitting the scope SHALL broadcast on all
AP-capable devices. The controller MUST fail closed with `Ready=False` and MUST NOT mutate
upstream when a named tag does not resolve.

#### Scenario: Selector scopes broadcast
- **WHEN** a broadcast sets a device scope selector
- **THEN** the operator resolves the scope through the read-only device-tags list and applies it upstream

#### Scenario: Omitted scope broadcasts everywhere
- **WHEN** a broadcast sets no device scope selector
- **THEN** the operator applies the broadcast with no device filter (all AP-capable devices)

#### Scenario: Unknown tag fails closed
- **WHEN** a device scope selector names a tag that does not exist on the site
- **THEN** `status.conditions` reports `Ready=False` and no upstream broadcast is modified

### Requirement: Broadcast status and cleanup are observable

`UnifiWifiBroadcast` SHALL expose `conditions`, `observedGeneration`, a human-readable summary,
the upstream broadcast UUID, and the resolved transitive `networkID`/`siteID` in `status`, and
SHALL use a finalizer when deletion has upstream effects. The resolved `siteID`/`networkID`
recorded in `status` SHALL be the source the controller uses to delete the upstream broadcast, so
teardown does not depend on `spec.networkRef` still resolving. A broadcast that resolves to a
non-`USER_DEFINED` upstream object SHALL be adopted read-only with `Ready=False` and MUST NOT be
created, updated, or deleted.

#### Scenario: Readiness reported
- **WHEN** a broadcast reconciles successfully
- **THEN** `status.conditions` includes `Ready=True` and `observedGeneration` equals `metadata.generation`

#### Scenario: Upstream identifier in status only
- **WHEN** the operator resolves the upstream broadcast
- **THEN** its UUID appears only under `status`, never under `spec`

#### Scenario: Resolved ids recorded
- **WHEN** the operator resolves a broadcast's network and site
- **THEN** it records the upstream broadcast UUID and the resolved `networkID`/`siteID` in `status`

#### Scenario: Deletion cleans up upstream
- **WHEN** a broadcast with the finalizer is deleted
- **THEN** the operator deletes the upstream broadcast recorded in `status` and then removes the
  finalizer

#### Scenario: Deletion survives a broken reference
- **WHEN** a broadcast with a recorded `status.wifiBroadcastID` is deleted after its
  `spec.networkRef` no longer resolves
- **THEN** the operator uses the `status`-recorded site to delete the upstream broadcast rather
  than blocking on the reference

#### Scenario: System object is read-only
- **WHEN** an upstream broadcast resolves to a `metadata.origin` other than `USER_DEFINED`
- **THEN** the operator reports `Ready=False` and does not overwrite it

## ADDED Requirements

### Requirement: Broadcast security variant is validated

`UnifiWifiBroadcast.spec.securityConfiguration.type` SHALL be one of `OPEN`, `WPA2_PERSONAL`,
`WPA2_WPA3_PERSONAL`, `WPA3_PERSONAL`, `WPA2_ENTERPRISE`, `WPA2_WPA3_ENTERPRISE`, or
`WPA3_ENTERPRISE`, and variant-specific fields MUST only be accepted for the matching value. The
variant fields the v10.4.57 security union defines (for example `encryption`, `passphrase`,
`presharedKeys`, `pmfMode`, `saeConfiguration`, `fastRoamingEnabled`, `groupRekeyIntervalSeconds`,
`coaEnabled`, `securityMode`, and `radiusConfiguration`) SHALL be modeled.

#### Scenario: Mismatched security variant rejected
- **WHEN** a personal security configuration sets an enterprise-only field
- **THEN** the API server rejects the object

#### Scenario: Open security applied
- **WHEN** a broadcast sets `securityConfiguration.type` to `OPEN`
- **THEN** the operator applies an open broadcast upstream and reports `Ready=True`

### Requirement: Enterprise security references a RADIUS profile

Enterprise security variants (WPA2/WPA3) SHALL reference their upstream authentication
configuration (`radiusConfiguration.profileId`) through `spec.securityConfiguration.radiusProfileRef`
to a `UnifiRadiusProfile` by Kubernetes identity, and MUST NOT expose the opaque profile UUID.
Because `UnifiRadiusProfile` is not yet implemented, the controller MUST fail closed with
`Ready=False` and MUST NOT mutate the upstream broadcast until the referenced profile can be
resolved and provisioned.

#### Scenario: Enterprise profile reference is not an opaque ID
- **WHEN** the generated CRD schema for an enterprise security variant is inspected
- **THEN** the schema does not include a raw RADIUS profile UUID

#### Scenario: Enterprise fails closed until the profile kind lands
- **WHEN** an enterprise-secured broadcast references a `UnifiRadiusProfile`
- **THEN** the operator reports `Ready=False` and does not mutate the upstream broadcast

### Requirement: Broadcast reconciliation is idempotent

The broadcast reconciler SHALL be safe to run repeatedly: a second reconcile of an unchanged
object MUST perform no upstream write. It SHALL read the full broadcast detail (the list response
is an overview only) before deciding, and SHALL record the upstream correlation identifier in
`status`.

#### Scenario: Second reconcile performs no writes
- **WHEN** the same broadcast is reconciled twice with unchanged input
- **THEN** the second reconcile issues no upstream create or update

### Requirement: Broadcast reconcile is version-gated

The controller SHALL gate broadcast reconciliation on a WiFi capability (a distinct capability,
not a reuse of the overall Official API one) whose minimum console application version is the
Official API floor (`>= 10.1.78`), read from `UnifiController.status.applicationVersion`. Below
the minimum, or when the version cannot be determined, the controller MUST fail closed with
`Ready=False` and MUST NOT mutate the upstream broadcast.

#### Scenario: Console below the WiFi minimum
- **WHEN** a broadcast reconciles against a console whose detected version is below the WiFi
  capability minimum
- **THEN** `status.conditions` reports `Ready=False` and no upstream broadcast is created or updated

#### Scenario: Console at or above the minimum
- **WHEN** a broadcast reconciles against a console meeting the WiFi capability minimum
- **THEN** the operator proceeds to apply the broadcast upstream
