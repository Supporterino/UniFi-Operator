# Spec Delta

## ADDED Requirements

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
