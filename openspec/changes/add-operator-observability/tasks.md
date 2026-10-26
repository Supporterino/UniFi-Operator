# Tasks

## 1. Metrics package and reconcile instrumentation

- [ ] 1.1 Add `operator/internal/metrics/metrics.go` registering
  `unifi_operator_reconcile_total`, `unifi_operator_upstream_requests_total`,
  `unifi_operator_upstream_request_duration_seconds`, and `unifi_operator_build_info` on the
  controller-runtime registry, with `RecordReconcile(kind string, ready bool, reason string)` and an
  init-time `build_info{version}` set from `operator/internal/version.Version`. Constrain the value
  sets: `result` is `ready`|`not_ready`, `outcome` is `success`|`error`. Verify:
  `cd operator && go build ./...`, plus unit tests asserting the counter increments for
  `(kind, ready=false, reason=AuthenticationFailed)`, that `build_info` reports `version.Version`,
  and that double registration does not panic.

- [ ] 1.2 Call `metrics.RecordReconcile` at the end of `updateStatus` in the eleven writable
  reconcilers (`unifiaclrule`, `unifiaclruleordering`, `unifidnspolicy`, `unififirewallpolicy`,
  `unififirewallpolicyordering`, `unififirewallzone`, `unifinetwork`, `unifitrafficmatchinglist`,
  `unifiwifibroadcast`, `unifisite`, `unificontroller`), passing the reason actually written to
  status rather than re-deriving it. Verify: `cd operator && go build ./... && go vet ./...` and
  `grep -c 'metrics.RecordReconcile' operator/internal/controller/*.go` shows exactly one call per
  wired method.

- [ ] 1.3 Call `metrics.RecordReconcile` in the six adoption-kind reconcilers
  (`unifiradiusprofile`, `unifiswitchinglag`, `unifiswitchingmclagdomain`,
  `unifiswitchingswitchstack`, `unifivpnserver`, `unifisitetositetunnel`), which report terminal
  conditions without writing upstream. Verify: `cd operator && go build ./...` and the wired count
  equals `grep -rn 'func (r \*.*) updateStatus' operator/internal/controller/*.go | wc -l` (17).

- [ ] 1.4 Add reconciler tests proving the metric tracks status: reuse the fake-client setups in
  `unififirewallpolicy_controller_test.go` and `unifiradiusprofile_controller_test.go` to assert the
  counter increments with the expected `kind`, `result`, and `reason` on a `Ready=False` terminal
  path, and does not increment when a transient error is returned. Verify:
  `cd operator && go test ./internal/controller/ -run 'Metric|RecordReconcile'` passes, and a control
  run with the `RecordReconcile` call removed fails.

- [ ] 1.5 Add a label-hygiene test: assert every produced label value is either a known kind, a known
  reason constant, `ready`/`not_ready`, `success`/`error`, an operation label, or the build version —
  and that no value matches a UUID pattern or contains `/`, an object name, or a namespace. Verify:
  `cd operator && go test ./internal/metrics/`.

## 2. Upstream client instrumentation

- [ ] 2.1 Instrument `HTTPClient.do` (`operator/internal/unifi/http_client.go:267`) to record the
  request counter and latency histogram, with the operation label produced by a normalizer that
  strips the API base path, site UUID, object UUIDs, and query string (yielding labels such as
  `firewall.policies`). Verify: `cd operator && go build ./...` and a table-driven test over every
  endpoint listed in `docs/unifi-api.md` asserts a stable, non-empty operation label.

- [ ] 2.2 Pin the bounded label set: assert two different site UUIDs for one endpoint collapse to a
  single label value, that no produced label contains a UUID or `/`, and that an API error increments
  `outcome="error"` while a success increments `success`. Verify:
  `cd operator && go test ./internal/unifi/ -run 'Metric|Operation'` and `go test ./internal/unifi/`
  (full package green against the `httptest` fixtures).

## 3. Kubernetes Events on transitions

- [ ] 3.1 Add `operator/internal/controller/events.go` with a `+kubebuilder:rbac:groups="",resources=events,verbs=create;patch`
  marker and a helper that, given the recorder, the object, the `changed` return from
  `setReadyCondition`, the new ready state, the reason, and the message, records a `Warning` Event on
  a transition to `False` (Event reason = condition reason) or a `Normal` Event on a transition to
  `True`, and records nothing when unchanged. Verify: `cd operator && go build ./...` and a unit test
  with `record.NewFakeRecorder` asserting one Event per transition and none for an unchanged
  condition.

- [ ] 3.2 Add a `Recorder record.EventRecorder` field to each of the seventeen reconciler structs and
  construct it in `operator/cmd/main.go` with the same name passed to `Named(...)`, so the Event
  source matches the controller. Verify: `cd operator && go build ./... && go vet ./...` and
  `grep -c 'Recorder:' operator/cmd/main.go` equals 17.

- [ ] 3.3 Emit through the helper from each `updateStatus` alongside the metric call, gated on the
  `changed` return, for all seventeen reconcilers. Verify: `cd operator && go test ./internal/controller/`
  and a test asserting that a failure transition records exactly one Event, a repeated identical
  failure records none, and a recovery records one `Normal` Event.

- [ ] 3.4 Add a test proving Event text carries no credential material: drive a `SecretKeyMissing`
  failure and assert the recorded Event message names at most the Secret name and key, with no
  credential value from the fixture. Verify:
  `cd operator && go test ./internal/controller/ -run 'Event'`.

- [ ] 3.5 Regenerate RBAC with `task manifests`, confirm `operator/config/rbac/role.yaml` gains the
  `events` rule, and update the hand-maintained `charts/unifi-operator/templates/role.yaml` to match.
  Verify: `git diff operator/config/rbac/role.yaml charts/unifi-operator/templates/role.yaml` shows the
  same rule addition in both, and `task kustomize && task chart` pass.

## 4. RBAC parity gate

- [ ] 4.1 Add an `rbac-sync` task to `Taskfile.yml` that compares the rule sets of
  `operator/config/rbac/role.yaml` and `charts/unifi-operator/templates/role.yaml` (ignoring the
  chart's `{{- if .Values.rbac.create }}` wrapper) and fails naming the diverging rules, then add it
  to the `verify` aggregate so the existing pull-request workflow runs it. Verify: `task rbac-sync`
  passes on the synchronized pair; deleting one rule from the chart role makes it fail and name that
  rule; `task verify` runs it.

## 5. Kustomize scrape path

- [ ] 5.1 Remove the hardcoded `insecureSkipVerify: true` from
  `operator/config/prometheus/monitor.yaml`, leaving `scheme: https` with a CA reference, and keep
  `monitor_tls_patch.yaml` as the documented verifying route. Verify: `task kustomize` passes and
  `grep -n insecureSkipVerify operator/config/prometheus/monitor.yaml` returns nothing.

- [ ] 5.2 Move the self-signed bypass into an explicitly named overlay
  (`operator/config/prometheus/insecure/`) whose comments state that it disables verification, and
  keep `config/default/kustomization.yaml`'s reference commented so a default apply needs no
  `monitoring.coreos.com` CRDs. Verify: the default render contains no `ServiceMonitor`; the
  documented scrape composition renders exactly one, and the `insecure/` composition is the only one
  containing `insecureSkipVerify: true`.

## 6. Helm scrape path

- [ ] 6.1 Add `charts/unifi-operator/templates/servicemonitor.yaml` gated on
  `metrics.serviceMonitor.enabled` (default `false`), selecting the existing metrics Service on
  `scheme: https`, port `https`, `/metrics`, with `bearerTokenFile`, and configurable `interval`,
  `labels`, `tlsConfig`, and `insecureSkipVerify` (default `false`); document every key in
  `charts/unifi-operator/values.yaml`. Verify: `task chart` passes;
  `helm template ... | grep -c ServiceMonitor` is `0` by default and `1` with
  `--set metrics.serviceMonitor.enabled=true`.

- [ ] 6.2 Record the parity check between the two paths: assert the enabled Helm render and the
  enabled kustomize render agree on path, port, scheme, and bearer-token source, and note the
  comparison in the template's comment block. Verify: `task chart` plus the kustomize scrape render
  both show `scheme: https`, `/metrics`, and a projected-token source; the check is described in
  `docs/deployment.md` (task 8.1).

## 7. CLI run summary

- [ ] 7.1 Add a `runSummary` type beside `projectionResult` in `cli/internal/cmd/snapshot.go` and a
  `--summary-json <path>` flag (`-` = stderr, default disabled). Populate per-kind resource counts,
  per-site entries, warning and note counts, and elapsed milliseconds from the existing orchestration
  paths for both single-kind and `all` runs. Verify: `cd cli && go build ./... && go test ./internal/cmd/`
  with a golden-file test for the JSON, and a test asserting the default invocation's stdout and
  stderr bytes are unchanged.

- [ ] 7.2 Emit the same summary from `snapshot verify`, adding the projected, missing, unexpected,
  drifted, and skipped counts, with skipped still excluding subsystem notes. Verify:
  `cd cli && go test ./internal/cmd/ -run 'Verify.*Summary'` asserts the coverage numbers and that a
  note does not increment skipped.

- [ ] 7.3 Guarantee the stream contract and the no-identifier rule: assert that requesting a summary
  while writing resources to stdout still yields a parseable `---` YAML stream, that the summary
  contains no UUID-shaped value, and that `schemaVersion` is present. Verify:
  `cd cli && go test ./internal/cmd/`.

- [ ] 7.4 Document the flag and the JSON fields in `docs/snapshot.md`, including that it is opt-in and
  never written to stdout. Verify: `zensical build --strict`.

## 8. Documentation and the folded docs drift

- [ ] 8.1 Rewrite the Observability section of `docs/deployment.md`: the four metrics and their
  labels, which transitions produce Events, the `events` RBAC verb the manager needs, the
  `metrics-reader` binding for the scraping service account, the kustomize composition and Helm
  values, the TLS posture including the insecure overlay, and one working alert expression keyed on
  `reason`. Verify: `zensical build --strict` passes and every metric name matches
  `operator/internal/metrics/metrics.go`.

- [ ] 8.2 Add a metrics-and-Events-first pointer to `docs/debugging.md` mapping common condition
  reasons to the query or Event that surfaces them, without restating the deployment section. Verify:
  `zensical build --strict`.

- [ ] 8.3 Fold in the exploration's docs drift: correct the `OLM` claim in the root `AGENTS.md` docs
  index (OLM remains a non-goal) and replace the kubebuilder scaffolding in `operator/README.md`
  (`// TODO(user):` at lines 2, 5, and 114, plus the stale Go/kubectl prerequisites and the
  "personal registry" note) with a short, accurate module overview. Verify: no `TODO(user)` remains
  in `operator/README.md` and `zensical build --strict` passes.

## 9. Integration verification

- [ ] 9.1 Run the full local gate: `task verify` (generated-manifest no-diff, lint, both modules'
  tests, CRD sync, the new RBAC sync) plus `task kustomize && task chart`. Verify: all exit 0.
- [ ] 9.2 Run `zensical build --strict` and `graphify update .`. Verify: both succeed and
  `git status --short` shows only intended changes.

## Workflow follow-up

- Run the Go bar review over the operator and CLI changes, then the generic review over the
  manifests and docs.
- Archive the change once review and verification pass.
