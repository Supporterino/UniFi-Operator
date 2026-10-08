# UniFi-Operator

A Kubernetes operator that manages a UniFi network stack declaratively through Custom
Resources. Instead of clicking through the UniFi controller UI, you declare the network you
want as CRs; the operator reconciles the controller to match. A companion CLI can snapshot an
existing controller into adoptable CRs, and this documentation is the canonical reference
published to GitHub Pages.

## Why an operator

UniFi networks are usually configured by hand. That makes the desired state implicit, changes
unreviewable, and drift invisible. UniFi-Operator makes the network a Git-managed, declarative
contract:

- **Declarative CRDs.** Every managed resource is a Custom Resource with a stable `spec`.
- **Adoption.** The CLI projects a live controller into candidate CRs so a brownfield network
  can be brought under management incrementally.
- **Observable status.** Every CR reports `conditions`, `observedGeneration`, and a summary.
- **Best-practice implementation.** kubebuilder + controller-runtime, envtest coverage, and
  both kustomize and Helm deployment paths.

## Navigate the documentation

- [Architecture](architecture.md) — modules, data flow, and the reconcile model.
- [CRD conventions](crd-conventions.md) — the rules every Custom Resource follows.
- [UniFi API contract](unifi-api.md) — the controller endpoints the operator and CLI consume.
- [Go bar](go.md) — the language-level standard for the Go code.
- [Security](security.md) — credentials, RBAC, and secret hygiene.
- [Testing](testing.md) — envtest, the fake client, and HTTP fixtures.
- [Debugging](debugging.md) — diagnosing reconciles and drift.
- [Deployment](deployment.md) — kustomize and Helm.
- [Documentation](documentation.md) — how this site is built and published.

## Status

The operator is under active development. The module layout and design are established; the
first CRDs and controllers land incrementally. See the repository `README.md` and
[Architecture](architecture.md) as the design of record.
