# Proposal

## Why

The operator already computes a rich, machine-readable health signal — `conditions.go` defines
about forty stable reason codes (`SecretNotFound`, `AuthenticationFailed`,
`ZoneBasedFirewallNotConfigured`, `MembershipConflict`, …) and every reconciler writes a `Ready`
condition with one of them — but none of it escapes the Custom Resource. `operator/internal`
contains no metrics file, so the only exported series are controller-runtime defaults, which say
a reconcile happened but never why it failed.

The scrape path is half-built in both deployment routes: `operator/config/default/kustomization.yaml`
leaves `#- ../prometheus` commented out, and the Helm chart creates a metrics `Service` but has no
`ServiceMonitor` template at all. `docs/deployment.md` promises "Ship metrics … and scrape with a
`ServiceMonitor` where available", which is not true today. Meanwhile the scaffolded
`config/prometheus/monitor.yaml` ships `insecureSkipVerify: true` as a default, which the security
bar forbids.

This is the natural next step now that every kind is implemented, the CLI snapshot is complete, and
the release pipeline is in place: the declarative surface is finished, so day-2 operability is the
remaining gap. Turning the existing condition taxonomy into metrics costs almost nothing because
the taxonomy already exists — it is simply discarded at the status write.

## What Changes

- **Emit metrics from the existing condition taxonomy.** A new `operator/internal/metrics` package
  records, at the single terminal point each reconciler already reaches, a reconcile-outcome
  counter labelled with the CR kind, the outcome, and the condition reason. No new reason codes and
  no new CRD fields are introduced.
- **Export the build version as a metric.** `unifi_operator_build_info` reports the stamped version
  already available from `operator/internal/version`, so a scrape can be tied to a release without
  reading pod specs.
- **Emit Kubernetes Events on state transitions.** Each reconciler gains an event recorder and emits
  one Event when an object first reaches a failing reason and one when it recovers to `Ready`. No
  Event is emitted per retry or per unchanged resync; the Event carries the reason as its Reason
  field.
- **Give the CLI a machine-readable run summary.** `snapshot` and `verify` gain an opt-in run
  summary — per-kind resource counts, warning and note counts, and elapsed timings — written on a
  channel that cannot corrupt the YAML stream.
- **Instrument the UniFi client.** The one HTTP choke point
  (`operator/internal/unifi/http_client.go`, `func (c *HTTPClient) do`) gains a request counter and
  a duration histogram, labelled by logical operation rather than URL.
- **Make the scrape path real in kustomize.** `config/prometheus` stops shipping
  `insecureSkipVerify: true` as its default and becomes a one-step opt-in overlay, with the
  self-signed bypass split into a separately named, explicitly insecure overlay. The default
  `config/default` install stays applyable on a cluster without the Prometheus Operator CRDs.
- **Make the scrape path real in Helm.** The chart gains a `ServiceMonitor` template gated on a new
  `metrics.serviceMonitor` values block (labels, interval, TLS), alongside the existing metrics
  `Service`.
- **Document the signal.** `docs/deployment.md` and `docs/debugging.md` describe the emitted series,
  how to scrape them, and the TLS posture, and map the condition reasons to the metrics labels so
  users can alert on specific failures.
- **Fold in the docs drift found during exploration.** Root `AGENTS.md` advertises OLM in the
  deployment row although OLM is an explicit non-goal, and `operator/README.md` still carries
  kubebuilder scaffolding text (`// TODO(user): Add simple overview of use/purpose`) plus stale
  prerequisites (Go `v1.27.0+`, kubectl `v1.11.3+`, "personal registry").

## Capabilities

### New Capabilities

- `operator-observability` — what the operator must export about its own health, how both
  deployment paths must make it scrapeable, the label-cardinality and credential constraints on
  that signal, and the TLS posture required of the scrape configuration.

### Modified Capabilities

- `unifi-snapshot` — `snapshot` and `verify` gain a run-summary output contract (per-kind counts,
  warning/note counts, timings) that existing filtered/`-o -` invocations must keep working
  alongside.
- `build-and-release` — a new gate requirement, "RBAC sources stay in sync", mirroring the existing
  CRD sync gate. The chart's manager role is a hand-maintained copy of the generated one, and this
  change is the first to touch it since the scaffold.

`project-scaffold` already requires that kustomize and Helm "render the same set of core objects".
The metrics scrape object gets its own capability rather than widening that requirement, but the
Events RBAC does change the aggregated manager role in **both** paths — and the chart's role is a
hand-maintained copy with no gate comparing it to the generated one, so `build-and-release` gains
that gate rather than relying on review.

## Impact

- **CRD contract: none.** No new kinds, no `spec` or `status` field changes, no API-version change.
  Metrics and Events are derived from status the controllers already write.
- **Operator code:** `operator/internal/metrics/` (new), `operator/cmd/main.go` (registration and
  recorder construction), an event-recorder field and `Event` emission on each of the seventeen
  reconcilers, `+kubebuilder:rbac` markers for `events`, and the seventeen `updateStatus` methods
  that already funnel every terminal path (`unififirewallpolicy_controller.go`,
  `unifiaclrule_controller.go`, …), plus `operator/internal/unifi/http_client.go`.
- **Manifests:** `operator/config/prometheus/` (verifying base plus a named insecure overlay),
  `operator/config/rbac/role.yaml` (regenerated with `events` verbs),
  `charts/unifi-operator/templates/role.yaml` (hand-maintained copy — must be updated to match),
  `charts/unifi-operator/templates/servicemonitor.yaml` (new),
  `charts/unifi-operator/values.yaml`, and a new RBAC-parity gate in `Taskfile.yml`.
- **CLI:** `cli/internal/cmd/snapshot.go` and `cli/internal/cmd/verify.go` gain the summary, with
  the summary type living beside the projection result so `all` and single-kind runs share it.
- **Docs:** `docs/deployment.md`, `docs/debugging.md`, `docs/snapshot.md`, root `AGENTS.md`,
  `operator/README.md`.
- **Dependencies:** none added to the Go modules. `k8s.io/client-go/tools/record` ships with
  `k8s.io/client-go v0.34.0`, already a direct dependency. The chart's `ServiceMonitor` is inert
  unless the Prometheus Operator CRDs are installed, which is why it stays opt-in.
- **Upgrade note:** the manager's ServiceAccount gains `create`/`patch` on `events`. The RBAC must be
  applied before or with the new image, or Event emission fails with a 403 that does not affect
  reconciliation.

## Non-Goals

- Alerting rules, dashboards, or a bundled Prometheus/Thanos stack. The operator exports; the user
  monitors.
- Tracing (OpenTelemetry) and structured log-field changes. Logs already identify the reconciler.
  OTLP was offered for the CLI and rejected because it needs a collector assumption nowhere present
  in this repo.
- Events beyond reconcile transitions: no per-object mutation Events, no `Normal` Events on every
  successful reconcile, and no `warning` Events mirroring projection diagnostics.
- Metrics about UniFi *inventory* (counts of networks, clients, devices). This change reports
  operator health, not console state.
- OLM/OperatorHub packaging — still the non-goal it is today; this change only corrects the docs
  that claim otherwise.
