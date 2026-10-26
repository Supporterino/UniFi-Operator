# Design

## Context

See `proposal.md` — Why. Current state that shapes the approach:

- `operator/internal/controller/conditions.go` holds the reason taxonomy as unexported string
  constants (`reasonInvalidSpec`, `reasonSecretNotFound`, …) and `setReadyCondition` is the single
  condition writer. `setReadyCondition` knows the condition but **not** which kind is being
  reconciled.
- Every controller has its own `updateStatus` method — seventeen of them, one per reconciler
  (`unifiaclrule_controller.go:442`, `unififirewallpolicy_controller.go:458`,
  `unifisite_controller.go`, …), and `requeueStatus` helpers delegate to it. This is the only place
  where a terminal outcome and its reason are simultaneously known. Transient upstream failures
  return an error without a status write, by design.
- `operator/internal/unifi/http_client.go:267` `func (c *HTTPClient) do(ctx, method, endpoint,
  body, out any) error` is the single outbound request function for the whole client.
- The metrics server is configured in `operator/cmd/main.go` with `SecureServing: secureMetrics`
  (default `true`) plus `filters.WithAuthenticationAndAuthorization`, so scraping needs a bearer
  token and the `metrics-reader` ClusterRole. Both deployment paths already ship that ClusterRole
  (`operator/config/rbac/metrics_reader_role.yaml`, `charts/unifi-operator/templates/`).
- With no `--metrics-cert-path`, controller-runtime generates a self-signed certificate at startup.
  `operator/config/prometheus/monitor.yaml` therefore hardcodes `insecureSkipVerify: true`, and
  `operator/config/default/kustomization.yaml:27` leaves `#- ../prometheus` commented out. The chart
  has `metrics_service.yaml` but no ServiceMonitor template.
- **No reconciler emits Kubernetes Events today.** There is no `Recorder` field on any reconciler and
  no `GetEventRecorderFor` call in `operator/cmd/main.go`; `operator/config/rbac/role.yaml` has no
  `events` rule.
- **The chart's manager role is a hand-maintained copy.** `charts/unifi-operator/templates/role.yaml`
  mirrors the five generated rules in `operator/config/rbac/role.yaml` by hand, and `task crd-sync`
  compares only CRDs. Any RBAC change can silently desynchronise the two paths.
- **The CLI's stdout is a YAML stream.** `emitResources` writes `---`-separated documents to stdout
  when `--output -` (the default), while `writeDiagnostics` writes warnings and notes to stderr. A
  summary printed on stdout would corrupt the stream for `kubectl apply -f -`.
- `operator/internal/version.Version` is stamped at link time and already logged at startup.

## Goals / Non-Goals

**Goals:**

- The reason a reconcile did not reach `Ready=True` is queryable as a metric without reading CRs.
- Cardinality stays bounded and no credential or upstream identifier reaches a label.
- Both deployment paths expose the same opt-in scrape configuration, with TLS verification as the
  default and the insecure bypass impossible to apply by accident.
- `config/default` and `helm install` keep working on clusters without the Prometheus Operator.
- A state transition (failure, recovery) is visible through `kubectl describe` as a Kubernetes Event,
  without one Event per retry.
- A snapshot run's shape — what it emitted, what it warned about, how long it took — is consumable by
  CI as JSON without touching the YAML stream.

**Non-Goals:**

- No new condition reasons; the metric labels and Event reasons mirror the existing constants exactly.
- No metric retries/de-duplication work: counters are monotonic, so re-observing a reconcile is
  additive rather than a correctness problem.
- No exposure of upstream inventory (network/device/client counts) — that would require a second
  read path and is a different capability.
- No Event for every reconcile or every upstream mutation; only condition transitions (D8).
- No CLI metrics endpoint or Prometheus textfile; the summary is a run artifact (D9).

## Decisions

### D1 — Record metrics in each `updateStatus`, not in `setReadyCondition` or a Reconcile wrapper

Add `operator/internal/metrics` with `RecordReconcile(kind, ready bool, reason string)`, called at
the end of each of the seventeen `updateStatus` methods after the status write is attempted, so the
outcome is recorded even when status needed no write (a steady failing object would otherwise let
`rate()` go dark for a whole resync interval). A failed status write returns before the metric is
recorded, so an API-server failure is never counted as a reconcile outcome. Tests that exercise
status transitions therefore observe the metric without any extra hook.

*Alternatives considered:*

- **`setReadyCondition`** — one call site instead of seventeen, but it has no access to the kind, and
  it is also called for intermediate or unchanged transitions. Counting there would both mislabel
  the series and inflate it.
- **Wrapping each `Reconcile`** — seventeen wrappers that still cannot see the reason, because the
  reason is only materialized inside `updateStatus`. It would also count transient errors that are
  deliberately retried, conflating "retrying" with "terminal failure".
- **A decorator on the manager's client** — intercepts status writes generically, but couples metric
  semantics to the client's write path and cannot distinguish a status update from any other write.

Consequence: adding a reconciler requires adding one `RecordReconcile` call. That is deliberate — a
per-kind checklist is easy to review, and a missing call shows up as an absent series rather than a
silently wrong one.

### D2 — Four metrics: reconcile outcomes, upstream requests, upstream latency, build info

| Metric | Type | Labels |
|---|---|---|
| `unifi_operator_reconcile_total` | counter | `kind`, `result` (`ready` \| `not_ready`), `reason` |
| `unifi_operator_upstream_requests_total` | counter | `operation`, `outcome` (`success` \| `error`) |
| `unifi_operator_upstream_request_duration_seconds` | histogram | `operation` |
| `unifi_operator_build_info` | gauge (`1`) | `version` |

*Alternatives considered:*

- **A `Ready` gauge carrying `reason`** (the intuitive design) was rejected. A gauge must delete the
  previous series when the reason changes, so every reconciler would need `DeleteLabelValues`
  bookkeeping or the operator would report a stale reason forever. A counter of terminal outcomes
  needs no cleanup and answers the alerting question directly
  (`rate(unifi_operator_reconcile_total{result="not_ready",reason="AuthenticationFailed"}[5m]) > 0`).
- **Object name/namespace labels** — unbounded and useless for alerting at cluster scale.
- **One metric per reason** (`unifi_operator_reconcile_failed_secret_not_found_total`) — would hard-code
  forty names and drift from `conditions.go` on the next added reason.

`reason` is only populated when `result="not_ready"`; a ready reconcile reports `reason="Reconciled"`
or its kind-specific success reason, so the label is never empty.

### D3 — Derive the upstream `operation` label from the endpoint pattern, bounded by construction

`HTTPClient.do` records the request using a normalizer that strips the API base path, the site UUID,
object UUIDs, and the query string, leaving a path template such as `firewall.policies` or
`networks` (`sites` is dropped as the base prefix). The Integration v1 surface is fixed
(`docs/unifi-api.md`), so the resulting set is small and enumerable — eighteen labels, with a single
`unknown` fallback that never echoes the path.

The client reports through a small `unifi.RequestObserver` interface injected as a constructor
option, rather than importing the metrics package: `docs/go.md` fixes the dependency direction
(`controller → unifi`) and forbids `unifi` depending on anything in the Kubernetes world, which
`internal/metrics` does through the controller-runtime registry. `cmd/main.go` injects the
metrics-backed observer; the default is a no-op, so unit tests and any unobserved client are
unaffected.

*Alternatives considered:*

- **Raw URL path** — contains site UUIDs and object UUIDs, so cardinality is unbounded and upstream
  identifiers leak into metrics.
- **An explicit operation argument at every call site** — most precise, but touches every client
  method for no behavioural gain over normalization.

The normalizer gets a table-driven unit test asserting that UUID-bearing paths collapse to one label
and that the label set matches the documented endpoint inventory.

### D4 — Scrape configuration is opt-in in both paths, and `config/default` stays applyable

- **kustomize:** `config/prometheus` remains a separate resource set. It is referenced from
  `config/default/kustomization.yaml` only as the existing commented line, and the docs give the
  exact one-line composition to enable it. `monitor.yaml` itself stops hardcoding
  `insecureSkipVerify: true` (D5).
- **Helm:** new `templates/servicemonitor.yaml` rendered only when
  `metrics.serviceMonitor.enabled: true` (default `false`), with `interval`, `labels`,
  `tlsConfig`, and `selector` derived from the existing metrics Service labels.

*Alternatives considered:*

- **Enable the ServiceMonitor by default in `config/default`** (the obvious reading of "ship
  observability") was rejected: `kubectl apply -k config/default` and `task deploy` would then fail
  on any cluster without the `monitoring.coreos.com` CRDs, which is most clusters that do not run
  the Prometheus Operator. A default install must not require an optional CRD.
- **A `ServiceMonitor` in the chart's `crds/`** — wrong mechanism; `crds/` is for CRDs only and Helm
  never templates it.

### D5 — TLS verification is the default; the self-signed bypass is explicit and separately named

The metrics endpoint serves a controller-runtime self-signed certificate unless
`--metrics-cert-path` is set. Rather than defaulting to `insecureSkipVerify: true`:

- The base `config/prometheus/monitor.yaml` sets no bypass at all and references a CA (the existing
  `monitor_tls_patch.yaml` cert-manager path, or a user-supplied secret); absence of the field is the
  verifying default, so a `grep` for the bypass token finds only the insecure overlay.
- The self-signed case moves to `config/prometheus-insecure/` — a **sibling** overlay whose name
  states what it does, referenced from `docs/deployment.md` as **insecure**, per the security bar
  ("a skip-verify option must be explicit and documented as insecure"). It is a sibling rather than
  `config/prometheus/insecure/` because kustomize's default load restrictor forbids a nested
  kustomization from including a file in its own parent directory, and the only ways to keep it
  nested (duplicating the ServiceMonitor, or a symlink) would create a second source of truth.
- The chart exposes `metrics.serviceMonitor.insecureSkipVerify` defaulting to `false`, documented as
  insecure.

*Alternative considered:* keep `insecureSkipVerify: true` and document it. Rejected — `docs/security.md`
forbids a permissive default, and the scaffolded TODO in `monitor.yaml` admits it is not for
production.

### D6 — Scrape auth is documented, not automated

The metrics endpoint is authn/authz-protected, so the ServiceMonitor uses the Prometheus service
account's projected token (`bearerTokenFile`). `docs/deployment.md` states that the Prometheus
service account must be bound to the `metrics-reader` ClusterRole that both paths already ship. The
chart does not create that binding automatically: binding an arbitrary cluster-scoped subject
(the Prometheus SA lives in another namespace, usually) is a cluster-admin decision, and guessing
its name/namespace would violate least privilege.

### D7 — `build_info` is its own gauge, not a `version` label on the others

`unifi_operator_build_info{version}` is a gauge set to `1` at startup, read from the already-stamped
`operator/internal/version.Version`.

*Alternatives considered:*

- **A `version` label on the counters and histogram** — every upgrade churns every series, and the
  old version's series lingers until the scrape target is re-registered, so `rate()` over the
  upgrade boundary computes across two identities.
- **No version signal at all** — the natural question "which build produced this failure rate?" would
  need pod-spec lookups from the monitoring stack.

The `_build_info` gauge set to `1` is the conventional exposition shape; the version value is bounded
by the number of releases observed, not by workload size.

### D8 — Events fire on condition transitions only, from the recorder, with the reason as Reason

Each reconciler struct gains a `Recorder record.EventRecorder`, constructed in `operator/cmd/main.go`
with the same name passed to `Named(...)` on the builder, so the Event source matches the controller.
Emission reuses the `changed` return that `setReadyCondition` already produces: `Warning` for a
transition into a failing reason (Event `Reason` = the condition reason), `Normal` for a transition
back to `Ready=True`. No Event is emitted when the condition did not change.

*Alternatives considered:*

- **Emit on every terminal reconcile** — the recorder's correlator aggregates repeats, but the result
  is a steady trickle of Events per object per resync, and `kubectl describe` becomes unreadable
  exactly when a user is debugging.
- **Failures only** — simplest wiring, but the "it recovered on its own" signal is lost, and
  operators end up believing a failure is still active.
- **A shared recorder field on a common base struct** — the reconcilers have no shared struct and
  adding one would restructure seventeen types for no behavioural gain.

RBAC: a `+kubebuilder:rbac:groups="",resources=events,verbs=create;patch` marker is added next to the
emission helper so `task manifests` regenerates `operator/config/rbac/role.yaml`. The chart's
hand-maintained `templates/role.yaml` must be updated to match, which is why D10 adds a gate.

### D9 — The CLI summary is opt-in, JSON, and never on stdout

`--summary-json <path>` on `snapshot` and `verify` writes one JSON object (`schemaVersion: 1`) with
per-kind resource counts, per-site entries, warning/note counts, elapsed milliseconds, and — for
`verify` — the projected/missing/stale/drift/skipped numbers. `-` means stderr. The default is
disabled, so every current invocation keeps its exact bytes.

*Alternatives considered:*

- **Summary on stdout** — corrupts the `---` YAML stream that `snapshot -o -` produces and that
  `kubectl apply -f -` consumes.
- **Summary on stderr by default** — breaks callers that parse the diagnostics channel, and turns a
  human-readable warning channel into a mixed format nobody documents.
- **A Prometheus textfile** — fits cron-driven snapshots but imposes a file format plus a
  node-exporter dependency, and the chosen consumer here is CI.
- **OTLP traces** — a new dependency and a collector assumption nowhere present in the repo.

No upstream UUIDs appear in the summary, matching the existing "free of upstream identifiers"
requirement; counts and emitted names only.

### D10 — Add an RBAC parity gate rather than trusting review

The chart role is a hand-copied mirror of the generated role, and this change is the first to touch
it since the scaffold. A `task` check compares the rule sets of
`operator/config/rbac/role.yaml` and `charts/unifi-operator/templates/role.yaml` (the chart's
`{{- if .Values.rbac.create }}` wrapper aside) and fails naming the divergence, mirroring what
`crd-sync` already does for CRDs.

*Alternative considered:* template the chart role from the generated file — would require a
generation step writing into `charts/`, a bigger change to the build pipeline than the parity check
this change needs.

## Risks / Trade-offs

- **[Metric completeness depends on seventeen call sites]** → A unit test per reconciler family that
  asserts the counter increments on a terminal status write, plus review vigilance; the design makes
  a missing call visible as an absent series rather than a wrong value.
- **[`kind` label cardinality × `reason` label cardinality]** → Bounded in practice: eighteen kinds
  and about forty reasons, and the realistic combination count is far lower because most reasons
  belong to one or two kinds. No action needed, but the spec forbids adding unbounded labels later.
- **[Normalized `operation` label drifts from the real endpoint set]** → The table-driven test pins it
  against the endpoints documented in `docs/unifi-api.md`; a new client method without a new case
  fails the test.
- **[Users enable the ServiceMonitor without the `metrics-reader` binding and see 403s]** → Explicit
  documentation of the binding (D6) and an example alert rule block that makes the failure mode
  obvious.
- **[A future controller forgets `RecordReconcile`]** → Accepted; it degrades one kind's visibility,
  never correctness.
- **[Event spam on a flapping condition]** → Emission is gated on the `changed` return, so at most one
  Event per transition; reconcile backoff bounds how often a flap can happen, and the recorder's
  correlator aggregates any residual repeats.
- **[Events are cluster-readable]** → The Event message is the same clamped, credential-free condition
  message already written to status; `docs/security.md` forbids credential material in events, and
  the existing messages satisfy it.
- **[RBAC divergence between the generated role and the chart copy]** → The new parity gate (D10)
  fails the local `task verify` and CI instead of relying on review.
- **[CLI summary format churn breaks a consumer]** → `schemaVersion` is part of the object and the
  golden-file tests pin the field names; additive fields are allowed, removals are not.
- **[A missing `events` verb after upgrade leaves silent 403s]** → Call out the ordering in the
  migration plan and in `docs/deployment.md`; reconciliation is unaffected, so the failure is
  degraded diagnostics rather than an outage.

## Migration Plan

The CRD surface is untouched, so there is no conversion or upgrade ordering constraint. The
non-CRD changes do have an order:

1. Apply the regenerated RBAC (`operator/config/rbac/role.yaml`, and the matching chart role) before
   or with the new manager image — the ServiceAccount needs `create`/`patch` on `events`. Missing it
   costs Event emission only; reconciliation is unaffected.
2. Operator image: metric registration and `build_info` are automatic at startup; nothing to
   configure.
3. kustomize users: optional `config/prometheus` composition, then bind `metrics-reader`. Choose the
   `insecure/` overlay only if the console-issued self-signed metrics cert is unavoidable.
4. Helm users: `--set metrics.serviceMonitor.enabled=true` after the Prometheus Operator is present.
5. CLI users: nothing changes until `--summary-json` is passed; the flag is additive.
6. Rollback: unset the chart value or delete the ServiceMonitor, and stop passing `--summary-json`.
   The operator keeps serving metrics and Events; scrapes and summaries create no state.

## Open Questions

None. The one decision that could reasonably have gone the other way — default-on scraping — is
settled in D4 with the compatibility reason recorded.
