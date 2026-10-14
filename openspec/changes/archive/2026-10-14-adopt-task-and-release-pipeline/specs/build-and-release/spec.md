# Spec Delta

## Purpose

Gives the workspace one reproducible local build entrypoint and a single-version release
pipeline, so the operator, CLI, and Helm chart are versioned together and published as
verifiable artifacts from a tagged release.

## ADDED Requirements

### Requirement: Single local build entrypoint

The workspace SHALL provide one root task runner that exposes the local build, test, lint,
generation, image, and documentation tasks. Those tasks SHALL be the only documented way to
run them, and a task that generates source or manifests SHALL be idempotent.

#### Scenario: Entrypoint lists the tasks
- **WHEN** the task runner is invoked with its list flag from the repository root
- **THEN** it exits 0 and lists the build, test, lint, generate, manifests, verify, image, and
  documentation tasks

#### Scenario: Generation is idempotent
- **WHEN** a generation task runs twice with no intervening source change
- **THEN** the second run leaves the working tree with no diff

#### Scenario: Documentation names only the task runner
- **WHEN** a contributor follows the repository's build instructions
- **THEN** every documented command is a task target and no `make` target is referenced

### Requirement: Pinned, self-provisioning tools

Build tasks SHALL pin the versions of the generator and test tools they use and SHALL run them
without a pre-installed copy by resolving a module version at invocation. Build tools that
cannot be resolved this way, such as container and Helm tooling, SHALL be declared as
requirements that fail with a message naming the missing tool.

#### Scenario: Generator runs from a clean checkout
- **WHEN** the repository is cloned with no cached tool binaries and a generation task runs
- **THEN** the pinned tool version is fetched and used without a separate installation step

#### Scenario: Missing binary tool is reported
- **WHEN** a task requiring an external binary runs while that binary is absent
- **THEN** the task fails before execution and names the missing tool

### Requirement: Binaries report their version

The operator and the CLI SHALL embed the release version at build time and SHALL report it. The
CLI SHALL expose it through a version flag; the operator SHALL emit it in its startup log.

#### Scenario: CLI reports its version
- **WHEN** the CLI is built with a version and run with its version flag
- **THEN** it exits 0 and prints that version

#### Scenario: Local build still reports a version
- **WHEN** a developer builds without supplying a release version
- **THEN** the binary still reports a non-empty version identifying the build

### Requirement: One bundle version

The operator, CLI, and Helm chart SHALL version and release as a single bundle identified by one
semantic version tag. On each release the release manifest, the chart `version` and `appVersion`,
and the changelog SHALL all carry that same version.

#### Scenario: Release proposal carries the version
- **WHEN** a releasable change lands on the release branch
- **THEN** the release tooling proposes a version and updates the release manifest, the chart
  `version` and `appVersion`, and the changelog to agree

#### Scenario: Tag matches the chart
- **WHEN** a release is tagged with version `X.Y.Z`
- **THEN** the packaged chart's `appVersion` is `X.Y.Z`

### Requirement: Container image is published

A release SHALL publish a multi-architecture manager image to the project registry, carrying the
release version as a tag and recorded immutably by digest.

#### Scenario: Versioned image exists
- **WHEN** a release completes
- **THEN** the registry serves the image under the release version tag and under a tag for its
  minor series

#### Scenario: Image is multi-architecture
- **WHEN** the published image manifest is inspected
- **THEN** it resolves for both `linux/amd64` and `linux/arm64`

### Requirement: CLI binaries are published

Each release SHALL publish cross-compiled CLI binaries as release assets for the supported
operating systems and architectures, together with a checksum file, and SHALL attach them to the
release that already exists for the tag.

#### Scenario: Assets attach to the existing release
- **WHEN** a release completes
- **THEN** the CLI binaries and their checksum file are attached to the release for that tag and
  no second release is created

#### Scenario: Checksums verify
- **WHEN** a published CLI archive is checksummed against the published checksum file
- **THEN** the checksum matches

### Requirement: Helm chart is published

Each release SHALL package the Helm chart and push it to the project OCI chart registry under the
release version, and the published chart SHALL reference the released manager image.

#### Scenario: Released chart is pullable
- **WHEN** the released chart version is pulled from the OCI registry and rendered
- **THEN** it produces a manager Deployment referencing the released image

#### Scenario: OCI push does not repackage
- **WHEN** the chart is published for a release
- **THEN** the pushed chart's `appVersion` equals the release version

### Requirement: Install bundle is published

Each release SHALL attach a consolidated install manifest built from the project's deployment
overlay, so a cluster can be brought to the released state with a single apply.

#### Scenario: Install bundle brings up the release
- **WHEN** the released install bundle is applied to a cluster
- **THEN** it creates the custom resource definitions and the manager Deployment referencing the
  released image

### Requirement: CRD sources stay in sync

The operator's generated custom resource definitions and the Helm chart's bundled definitions
SHALL remain byte-identical, and a gate SHALL fail and name the diverging files when they differ.

#### Scenario: Drift fails the gate
- **WHEN** one of the two CRD sources is changed without the other
- **THEN** the synchronization gate fails and reports the divergent files

### Requirement: Gates run on every change

Every pull request SHALL run the same gates as the local verification task: generated manifests
produce no diff, lint and tests pass in both Go modules, the CRD sources are in sync, and the
documentation builds in strict mode.

#### Scenario: Pull request is gated
- **WHEN** a pull request is opened
- **THEN** the continuous integration runs the verification gates and reports their result

#### Scenario: Out-of-sync generated output blocks the pull request
- **WHEN** a pull request leaves generated output or the CRD sources out of sync
- **THEN** the continuous integration fails
