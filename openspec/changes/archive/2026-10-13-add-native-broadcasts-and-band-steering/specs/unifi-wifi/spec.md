# Spec Delta

## MODIFIED Requirements

### Requirement: Broadcast references a network

`UnifiWifiBroadcast.spec` SHALL express the upstream `network` reference (the `SPECIFIC`/`NATIVE`
union) without an opaque upstream identifier. A `SPECIFIC` broadcast SHALL reference a
`UnifiNetwork` by name in `spec.networkRef` and resolve its site through the network's
`spec.siteRef`; it MUST NOT set `spec.siteRef`. A `NATIVE` broadcast SHALL set
`spec.nativeNetwork: true` and resolve its site from `spec.siteRef`, and MUST NOT set
`spec.networkRef`. Exactly one of `spec.networkRef`/`spec.nativeNetwork` MUST be set.
`spec.networkRef` SHALL be immutable: once set, a change MUST be rejected by the API server. The
binding SHALL be immutable in both directions: switching an existing object between the `SPECIFIC`
and `NATIVE` variants (setting or clearing `spec.networkRef`) and re-pointing a `NATIVE` broadcast's
`spec.siteRef` MUST be rejected by the API server.

#### Scenario: Network resolved
- **WHEN** a broadcast references a network by name
- **THEN** the operator resolves the reference and applies the broadcast upstream

#### Scenario: Broadcast owned by its network
- **WHEN** a broadcast references a `UnifiNetwork`
- **THEN** the operator sets an `ownerReference` from the broadcast to the network

#### Scenario: Site resolved transitively
- **WHEN** a `SPECIFIC`-bound broadcast is reconciled and its referenced network's site has adopted
  an upstream site
- **THEN** the operator resolves the site through the network reference and applies the broadcast

#### Scenario: Native network variant is modeled
- **WHEN** a broadcast sets `spec.nativeNetwork: true` and `spec.siteRef`
- **THEN** the operator writes the upstream `network` reference as the `NATIVE` variant, resolves
  the site from `spec.siteRef`, and the generated CRD schema contains no field representing a raw
  network UUID

#### Scenario: Native broadcast is owned by its site
- **WHEN** a broadcast sets `spec.nativeNetwork: true`
- **THEN** the operator sets no `UnifiNetwork` ownerReference, sets a `UnifiSite` ownerReference
  (its strongest reference) so the site drains the broadcast before it is removed, and deletion
  uses the site recorded in `status`

#### Scenario: Native network variant not exposed
- **WHEN** the generated CRD schema for `UnifiWifiBroadcast.spec` is inspected
- **THEN** it contains no field representing a raw network UUID: the `NATIVE` binding is a boolean
  (`nativeNetwork`) carrying no upstream identifier

#### Scenario: Ambiguous or absent binding is rejected
- **WHEN** a broadcast sets both `spec.networkRef` and `spec.nativeNetwork`, or neither
- **THEN** the API server rejects the object

#### Scenario: Repointing the network is rejected
- **WHEN** an existing broadcast changes `spec.networkRef` to a different `UnifiNetwork`
- **THEN** the API server rejects the update

#### Scenario: Switching the network binding is rejected
- **WHEN** an existing broadcast switches between binding variants (sets or clears
  `spec.networkRef`, or re-points a `NATIVE` broadcast's `spec.siteRef`)
- **THEN** the API server rejects the update

### Requirement: Broadcast type is validated

`UnifiWifiBroadcast.spec.type` SHALL be one of `STANDARD` or `IOT_OPTIMIZED`, and variant-specific
fields MUST only be accepted for the matching value. The `STANDARD` variant SHALL require its
variant fields (`advertiseDeviceName`, `arpProxyEnabled`, `broadcastingFrequenciesGHz`,
`bssTransitionEnabled`); the `IOT_OPTIMIZED` variant SHALL accept only the common fields. The
common broadcast fields SHALL be modeled faithfully, including the top-level
`bandSteeringEnabled` (usable by both variants) and the optional nested configuration trees the
v10.4.57 union defines.

#### Scenario: Mismatched variant rejected
- **WHEN** a broadcast sets fields for the wrong `type`
- **THEN** the API server rejects the object

#### Scenario: Standard variant applied
- **WHEN** a `STANDARD` broadcast sets its broadcasting frequencies and variant flags
- **THEN** the operator applies them upstream and reports `Ready=True`

#### Scenario: IoT-optimized variant applied
- **WHEN** an `IOT_OPTIMIZED` broadcast sets only common fields
- **THEN** the operator applies it upstream and reports `Ready=True`

#### Scenario: Band steering is a common field
- **WHEN** a `STANDARD` or `IOT_OPTIMIZED` broadcast sets the top-level `spec.bandSteeringEnabled`
- **THEN** the API server accepts the object and the controller sends the setting on create and
  update
