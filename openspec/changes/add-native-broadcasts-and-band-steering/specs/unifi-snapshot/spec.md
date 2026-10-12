# Spec Delta

## MODIFIED Requirements

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

### Requirement: Unrepresented settings are annotated

When an upstream object carries settings the CRD does not model, the snapshot command SHALL emit
the resource with an annotation naming the unrepresented settings rather than dropping them
silently.

#### Scenario: An unmodeled setting is surfaced
- **WHEN** a broadcast sets an upstream setting the CRD does not model (for example `mloEnabled`)
- **THEN** the emitted broadcast carries the unrepresented-settings annotation naming that setting

## ADDED Requirements

### Requirement: Native-bound broadcasts are emitted

The snapshot command SHALL emit a broadcast bound to the upstream `NATIVE` network variant as a
`UnifiWifiBroadcast` with `spec.nativeNetwork: true` and a `spec.siteRef` naming the emitted
`UnifiSite`, instead of skipping it, and MUST NOT place an upstream network identifier in `spec`.

#### Scenario: A NATIVE-bound broadcast is emitted
- **WHEN** a WiFi broadcast is bound to the upstream `NATIVE` network variant
- **THEN** the emitted `UnifiWifiBroadcast` sets `spec.nativeNetwork: true` and `spec.siteRef`
  names the emitted `UnifiSite`, with no upstream network UUID anywhere in `spec`

### Requirement: Band steering is represented

The snapshot command SHALL project a broadcast's `bandSteeringEnabled` into the common
`spec.bandSteeringEnabled` field and MUST NOT list it as an unrepresented setting.

#### Scenario: Band steering is emitted
- **WHEN** a broadcast sets `bandSteeringEnabled`
- **THEN** the emitted `UnifiWifiBroadcast` sets `spec.bandSteeringEnabled` and its
  unrepresented-settings annotation does not name `bandSteeringEnabled`
