# project-scaffold Specification

## Purpose

Establishes the buildable foundation for UniFi-Operator: a kubebuilder operator module, a
separate Go CLI module, a conformant sample Custom Resource, and consistent kustomize and Helm
deployment output, with every declared gate runnable.

## Requirements

### Requirement: Operator module builds

The workspace SHALL contain a Go module under `operator/` that builds with the standard Go
tooling and exposes a controller-runtime manager entrypoint.

#### Scenario: Operator compiles
- **WHEN** `go build ./...` is run in `operator/`
- **THEN** it exits 0 and the manager package compiles

#### Scenario: Operator gate passes
- **WHEN** `go vet ./...` and `golangci-lint run` are run in `operator/`
- **THEN** both exit 0

### Requirement: Sample Custom Resource follows the CRD golden rule

The operator SHALL serve a `UnifiNetwork` Custom Resource in group `unifi.supporterino.de`,
version `v1alpha1`, whose `spec` contains no opaque UniFi identifiers and whose `status` is a
status subresource carrying `conditions`, `observedGeneration`, and an upstream correlation
field.

#### Scenario: CRD is generated from markers
- **WHEN** `make manifests generate` is run in `operator/`
- **THEN** the CRD manifests and deepcopy are regenerated and no diff remains

#### Scenario: Status reports readiness
- **WHEN** a `UnifiNetwork` is created and reconciled successfully
- **THEN** its `status.conditions` includes a `Ready` condition set to `True`
- **AND** `status.observedGeneration` equals `metadata.generation`

#### Scenario: Spec exposes no opaque identifier
- **WHEN** the generated CRD schema for `UnifiNetwork.spec` is inspected
- **THEN** it contains no field representing a UniFi internal `_id` or opaque numeric ID

### Requirement: Reconciliation is idempotent and observable

The `UnifiNetwork` reconciler SHALL be safe to run repeatedly and SHALL update status on every
terminal path without contacting a live UniFi controller in tests.

#### Scenario: Second reconcile performs no writes
- **WHEN** the same `UnifiNetwork` is reconciled twice with unchanged input
- **THEN** the second reconcile produces no object or status mutation beyond condition
  timestamps and idempotent rewrites

#### Scenario: Upstream identifier is recorded in status only
- **WHEN** the reconciler resolves the upstream object through the UniFi client
- **THEN** any upstream identifier it records appears only under `status`, never under `spec`

#### Scenario: Events and requeue are not spurious
- **WHEN** a reconcile succeeds
- **THEN** the controller does not return a requeue solely due to an unconditional status write

### Requirement: CLI module builds and runs

The workspace SHALL contain a separate Go module under `cli/` providing a cobra command tree
whose root command reports usage and whose `snapshot` subcommand emits Custom Resources.

#### Scenario: CLI help
- **WHEN** the CLI is invoked with `--help`
- **THEN** it exits 0 and prints usage for the root command and the `snapshot` subcommand

#### Scenario: Snapshot emits a CR
- **WHEN** `snapshot` runs against a fake UniFi endpoint
- **THEN** it writes a `UnifiNetwork` Custom Resource whose `spec` contains no opaque
  upstream identifier

#### Scenario: CLI gate passes
- **WHEN** `go build ./...`, `go vet ./...`, and `go test ./...` are run in `cli/`
- **THEN** all exit 0

### Requirement: Deployment manifests are consistent

The operator SHALL be deployable by both kustomize (canonical) and a Helm chart, and the two
paths SHALL render the same set of core objects.

#### Scenario: Kustomize renders
- **WHEN** `kustomize build operator/config/default` is run
- **THEN** it exits 0 and the output includes the manager Deployment, its ServiceAccount/RBAC,
  and the `UnifiNetwork` CRD

#### Scenario: Helm chart renders
- **WHEN** `helm lint` and `helm template` are run against the chart
- **THEN** both exit 0 and the rendered output includes the manager Deployment and RBAC

### Requirement: Tests are hermetic

Operator and CLI tests SHALL run without a live UniFi controller or external network access.

#### Scenario: Operator tests use envtest and fakes
- **WHEN** `go test ./...` is run in `operator/`
- **THEN** API-server-dependent behavior uses `envtest` and controller logic uses the fake
  client, with no live controller contacted

#### Scenario: UniFi client tests use HTTP fixtures
- **WHEN** the UniFi client is exercised in a test
- **THEN** it is served by an `httptest.Server` fixture and no external network call is made
