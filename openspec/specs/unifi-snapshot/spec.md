# unifi-snapshot Specification

## Purpose
The `snapshot` command reads a live UniFi Network console and emits adoptable Custom Resources, so
an existing deployment can be brought under Kubernetes management without hand-authoring CRs.

## Requirements

### Requirement: Emitted resources are adoptable and free of upstream identifiers
The snapshot command SHALL emit Custom Resources that validate against the operator's CRDs and
carry no opaque upstream identifier in `spec`, referencing other emitted resources by Kubernetes
identity. An upstream correlation identifier MAY appear in `status` only.

#### Scenario: Network references its site by name
- **WHEN** a network carrying an upstream UUID is snapshotted
- **THEN** the emitted `UnifiNetwork.spec.siteRef` names the emitted `UnifiSite` and no upstream
  UUID appears anywhere in `spec`

### Requirement: Projection reads the detail endpoint
The snapshot command SHALL read the per-object detail endpoint whenever the list response carries
overview fields only, so the emitted resource includes the full variant configuration.

#### Scenario: Detail-only configuration is emitted
- **WHEN** a network or WiFi broadcast is snapshotted
- **THEN** the emitted resource includes the variant configuration returned only by the detail
  endpoint

### Requirement: Unrepresentable upstream objects are skipped with a warning
When an upstream object cannot be represented faithfully as a Custom Resource, the snapshot
command SHALL skip that object, emit a warning naming the object and the reason on stderr, and
still emit every representable object. It MUST NOT silently omit the object or emit a partially
projected resource.

#### Scenario: A NATIVE-bound broadcast is skipped, not dropped silently
- **WHEN** a WiFi broadcast is bound to the upstream `NATIVE` network variant
- **THEN** the command no longer treats it as unrepresentable: it emits it with
  `spec.nativeNetwork: true` and `spec.siteRef` rather than dropping it silently, and no upstream
  network UUID appears in `spec`

#### Scenario: An unresolved network reference is skipped, not dropped silently
- **WHEN** a WiFi broadcast references a network that cannot be resolved to an emitted
  `UnifiNetwork`
- **THEN** the command skips that broadcast, prints a warning naming it and the unresolved
  network, emits the remaining broadcasts, and exits `0`

### Requirement: Native-bound broadcasts are emitted
The snapshot command SHALL emit a broadcast bound to the upstream `NATIVE` network variant as a
`UnifiWifiBroadcast` with `spec.nativeNetwork: true` and a `spec.siteRef` naming the emitted
`UnifiSite`, instead of skipping it, and MUST NOT place an upstream network identifier in `spec`.

#### Scenario: A NATIVE-bound broadcast is emitted
- **WHEN** a WiFi broadcast is bound to the upstream `NATIVE` network variant
- **THEN** the emitted `UnifiWifiBroadcast` sets `spec.nativeNetwork: true` and `spec.siteRef`
  names the emitted `UnifiSite`, with no upstream network UUID anywhere in `spec`

### Requirement: Unrepresented settings are annotated
When an upstream object carries settings the CRD does not model, the snapshot command SHALL emit
the resource with an annotation naming the unrepresented settings rather than dropping them
silently.

#### Scenario: An unmodeled setting is surfaced
- **WHEN** a broadcast sets an upstream setting the CRD does not model (for example `mloEnabled`)
- **THEN** the emitted broadcast carries the unrepresented-settings annotation naming that setting

### Requirement: Band steering is represented
The snapshot command SHALL project a broadcast's `bandSteeringEnabled` into the common
`spec.bandSteeringEnabled` field and MUST NOT list it as an unrepresented setting.

#### Scenario: Band steering is emitted
- **WHEN** a broadcast sets `bandSteeringEnabled`
- **THEN** the emitted `UnifiWifiBroadcast` sets `spec.bandSteeringEnabled` and its
  unrepresented-settings annotation does not name `bandSteeringEnabled`

### Requirement: Unrecoverable secrets are placeholders
When a secret value (for example a personal passphrase) is not exposed by the console, the
snapshot command MUST NOT invent or transmit a value; it SHALL emit a placeholder same-namespace
`Secret` reference and an annotation stating the value must be set before applying, and no secret
value SHALL appear in the output.

#### Scenario: An unreadable passphrase becomes a placeholder
- **WHEN** a `WPA2_PERSONAL` broadcast's passphrase cannot be read from the console
- **THEN** the emitted `securityConfiguration` references a placeholder `Secret`, carries the
  unresolved-secret annotation, and contains no passphrase value

### Requirement: An unconfigured subsystem yields an empty set
When an upstream subsystem is not configured, the snapshot command SHALL treat the condition as
an empty result and exit successfully rather than failing.

#### Scenario: Zone-Based Firewall is off
- **WHEN** `snapshot firewall-zones` runs against a console whose Zone-Based Firewall is not
  configured and the API reports the condition as an error
- **THEN** the command emits no resources and exits with status `0`
