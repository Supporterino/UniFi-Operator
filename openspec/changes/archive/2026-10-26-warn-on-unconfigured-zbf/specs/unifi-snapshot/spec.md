# Spec Delta

## MODIFIED Requirements

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
