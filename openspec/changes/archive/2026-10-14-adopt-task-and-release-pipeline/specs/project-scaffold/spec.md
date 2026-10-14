# Spec Delta

## MODIFIED Requirements

### Requirement: Sample Custom Resource follows the CRD golden rule

The operator SHALL serve `UnifiController`, `UnifiSite`, and `UnifiNetwork` Custom Resources in
group `unifi.supporterino.de`, version `v1alpha1`. `UnifiNetwork.spec` SHALL reference its parent
site through `spec.siteRef` and MUST contain no opaque UniFi identifiers. Each kind's `status`
SHALL be a status subresource carrying `conditions`, `observedGeneration`, and, where applicable,
an upstream correlation field.

#### Scenario: CRD is generated from markers
- **WHEN** `task manifests generate` is run in `operator/`
- **THEN** the CRD manifests and deepcopy are regenerated and no diff remains

#### Scenario: Status reports readiness
- **WHEN** a `UnifiNetwork` is created with a resolvable `spec.siteRef` and reconciled successfully
- **THEN** its `status.conditions` includes a `Ready` condition set to `True`
- **AND** `status.observedGeneration` equals `metadata.generation`

#### Scenario: Spec exposes no opaque identifier
- **WHEN** the generated CRD schema for `UnifiNetwork.spec` is inspected
- **THEN** it contains no field representing a UniFi internal `_id` or opaque numeric ID

#### Scenario: Reference chain is expressed by Kubernetes identity
- **WHEN** a `UnifiNetwork` references its site and the site references its controller
- **THEN** both references use Kubernetes names within the same namespace and no upstream
  identifier appears in `spec`
