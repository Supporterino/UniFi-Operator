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
When an upstream subsystem is not configured, the snapshot command SHALL treat that subsystem's
readable kinds as an empty result, emit one site-scoped warning naming the subsystem and site,
still emit every other representable resource, and exit `0`. It MUST NOT treat the condition as a
validation error, and MUST NOT count the warning as a skipped object in `snapshot verify`.

#### Scenario: Zone-Based Firewall is off
- **WHEN** `snapshot firewall-zones` runs against a console whose Zone-Based Firewall is not
  configured and the API reports the condition as an error
- **THEN** the command emits no resources, prints one warning naming the site, the unconfigured
  subsystem, and that this is expected if the console has not migrated to ZBF yet, and exits with
  status `0`

#### Scenario: Firewall policies are empty on a ZBF-off console
- **WHEN** `snapshot firewall-policies` runs against a console whose firewall policies endpoint
  reports the unconfigured-subsystem error
- **THEN** the command emits no policies, prints the same site-scoped warning, and exits with
  status `0`

#### Scenario: `all` still exports the non-firewall kinds
- **WHEN** `snapshot all` runs against a ZBF-off console that holds networks, WiFi broadcasts, DNS
  policies, and other non-firewall objects
- **THEN** no firewall resource is emitted, one warning per site is printed, and every
  non-firewall object is still emitted

#### Scenario: verify does not count the note as a skipped object
- **WHEN** `snapshot verify` re-projects a ZBF-off console against a snapshot taken from it
- **THEN** the note is printed, the skipped-object count remains `0`, and verify exits `0` when no
  other gap exists

### Requirement: `all` emits every projectable kind
`snapshot all` SHALL emit one resource for every kind the snapshot command can project,
including firewall-policy ordering and ACL-rule ordering, across every site on the console,
so a single invocation is complete. It MUST NOT omit a projectable kind, and it MUST NOT remain
limited to the four kinds and single site it previously covered.

#### Scenario: Every kind is present in `all`
- **WHEN** `snapshot all` runs against a console that holds objects of every projectable kind
- **THEN** the output contains resources of every projectable kind, including
  `UnifiFirewallPolicyOrdering` and `UnifiAclRuleOrdering`, and no projectable kind is absent

#### Scenario: `all` spans every site
- **WHEN** the console has more than one site carrying site-scoped objects
- **THEN** `all` emits the site-scoped resources of every site, not only the default site

### Requirement: Firewall-policy ordering is emitted
`snapshot` SHALL project the upstream firewall-policy ordering for each source/destination zone
pair into a `UnifiFirewallPolicyOrdering`, referencing emitted `UnifiFirewallPolicy` resources by
Kubernetes identity and expressing the system-defined partition as the anchored boundary. No
upstream zone or policy UUID SHALL appear in `spec`.

#### Scenario: Ordering references policies by name
- **WHEN** a zone pair has an upstream policy ordering over user-defined policies
- **THEN** the emitted `UnifiFirewallPolicyOrdering.spec.beforeSystemDefined` and `afterSystemDefined`
  name emitted `UnifiFirewallPolicy` resources, and no upstream policy or zone UUID appears in `spec`

#### Scenario: System-defined policies are anchored, not listed
- **WHEN** the upstream ordering includes system-defined policies in its partition
- **THEN** those policies are not emitted into `spec.beforeSystemDefined` or `spec.afterSystemDefined`;
  the system boundary is represented by the anchored partition instead

### Requirement: ACL-rule ordering is emitted
`snapshot` SHALL project the upstream site-global ACL-rule order into a `UnifiAclRuleOrdering`,
referencing emitted `UnifiAclRule` resources by Kubernetes identity in upstream order. No upstream
rule UUID SHALL appear in `spec`.

#### Scenario: ACL order references rules by name
- **WHEN** a site has an upstream ACL-rule order over user-defined rules
- **THEN** the emitted `UnifiAclRuleOrdering.spec.orderedAclRules` names emitted `UnifiAclRule`
  resources in upstream order, and no upstream rule UUID appears in `spec`

### Requirement: Snapshot completeness can be verified
`snapshot verify` SHALL re-read the live console and report, writing no resource, (a) coverage —
every projectable kind present upstream has a projection and no representable object was skipped —
and (b) fidelity — any field drift between an emitted resource and a fresh upstream read. It SHALL
exit non-zero when an explained-by-nothing gap is found and MUST NOT create, update, or delete any
upstream object.

#### Scenario: A skipped object is reported
- **WHEN** an upstream object that should be representable is absent from the emitted set
- **THEN** `verify` reports the object and exits with a non-zero status

#### Scenario: Known placeholders are not drift
- **WHEN** an emitted resource carries an annotated placeholder such as an unreadable passphrase Secret
- **THEN** `verify` classifies it as a known, expected gap and does not report it as fidelity drift

#### Scenario: Verify writes nothing
- **WHEN** `snapshot verify` runs against a console
- **THEN** it writes no file and performs no upstream mutation

### Requirement: Snapshot runs can report a machine-readable summary

`snapshot` and `snapshot verify` SHALL support an opt-in run summary describing what the run did:
per-kind emitted resource counts, per-site entries, the number of warnings and subsystem notes, and
elapsed time. `verify` SHALL include its coverage and fidelity counts in the same summary. The
summary MUST be a single JSON object carrying a schema version, MUST NOT contain any upstream UUID,
and MUST NOT be written to the resource stream.

#### Scenario: Default invocation is unchanged

- **WHEN** a snapshot runs without requesting a summary
- **THEN** no summary is produced and the emitted resources and diagnostics are byte-identical to
  the current behaviour

#### Scenario: The summary describes the run

- **WHEN** a snapshot for one site requests a summary and emits resources of several kinds
- **THEN** the summary reports fewer-or-equal counts per kind matching the emitted resources, the
  warning and note counts, and a non-negative elapsed time

#### Scenario: Requesting a summary does not corrupt the resource stream

- **WHEN** a summary is requested while resources are written to standard output
- **THEN** standard output still parses as a `---`-separated YAML stream containing only resources,
  and the summary appears on the requested destination instead

#### Scenario: Verify reports coverage in the summary

- **WHEN** `snapshot verify` runs with a summary requested against a matching snapshot
- **THEN** the summary reports the projected, missing, unexpected, drifted, and skipped counts, and
  the skipped count does not include subsystem notes

#### Scenario: The summary carries no upstream identifiers

- **WHEN** a summary is produced for a console holding several sites and objects
- **THEN** no field of the summary contains an upstream UUID or `_id`, only Kubernetes identities and
  counts
