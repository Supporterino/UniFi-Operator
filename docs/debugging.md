# Debugging

This is the canonical cross-module debugging workflow for UniFi-Operator — how to find out why
a CR is not converging and where the fault lies.

## First stop: the CR status

```bash
kubectl get <kind> <name> -o yaml
kubectl describe <kind> <name>
```

A well-formed CR tells you where it is stuck:

- `status.observedGeneration` vs `metadata.generation` — if observed is behind, the controller
  has not reconciled the latest spec yet.
- `status.conditions` — read the `Ready` condition's `reason` and `message`. A terminal
  `InvalidSpec` reason means the spec was rejected; a transient reason means retry.
- Events (in `describe`) show reconcile failures the controller emitted.

## Second stop: metrics and Events

The condition tells you what is wrong with one object. The metric labelled with the same reason
tells you how often it failed, and the Event tells you when it changed. Use them together: the
series, labels, and scrape setup are in [Deployment](deployment.md#observability); this section is
how to triage with them.

```bash
# Transitions and their timestamps
kubectl describe <kind> <name> | sed -n '/Events:/,$p'

# The live series, through the API server proxy. Needs read access to /metrics —
# the same permission the scraper gets from the metrics-reader ClusterRole — plus
# get on services/proxy in operator-system, which that ClusterRole does not grant:
# a forbidden from this hop is the missing proxy permission, not broken operator
# RBAC. Names are the kustomize install's; a Helm release prefixes its fullname.
kubectl get --raw \
  "/api/v1/namespaces/operator-system/services/https:operator-controller-manager-metrics-service:8443/proxy/metrics" \
  | grep unifi_operator_reconcile_total
```

In Prometheus, the same question over a window — a non-zero value means a failure was recorded, and a
series that stops incrementing means it is no longer being retried:

```promql
sum by (kind, reason) (increase(unifi_operator_reconcile_total{result="not_ready"}[15m])) > 0
```

| Reason | What it means | What surfaces it |
|--------|---------------|------------------|
| `SecretNotFound` | The `Secret` named by `spec.secretRef` is not in the object's namespace | `Warning` Event with this reason; `unifi_operator_reconcile_total{reason="SecretNotFound"}` |
| `SecretKeyMissing` | The `Secret` exists but has no key named by `spec.secretRef.key` | `Warning` Event; same counter with `reason="SecretKeyMissing"` |
| `AuthenticationFailed` | The console rejected the API key | `Warning` Event; also `unifi_operator_upstream_requests_total{outcome="error"}` for the `operation` that failed |
| `CertificateError` | TLS verification of the console failed | `Warning` Event; check the console certificate, or `spec.insecureSkipVerify` if it is genuinely self-signed |
| `InvalidSpec` | The spec was rejected — a bad URL or a terminal 4xx from the console | `Warning` Event; terminal, so fix the spec rather than waiting for a retry |
| `ZoneBasedFirewallNotConfigured` | The console has not migrated to Zone-Based Firewall, so the three firewall kinds cannot reconcile | `Warning` Event; expected on a pre-ZBF console, not a fault |
| `MembershipConflict`, `ZoneNameConflict` | Two CRs claim the same zone network membership, or the same zone name | `Warning` Event; the conflict is between objects, so compare their specs |
| `ControllerNotFound`, `SiteNotFound`, `*RefNotFound` | A cross-CR reference does not resolve | `Warning` Event; find the missing object by the name in the message |
| `DependencyNotReady` | The referenced object exists but is not `Ready=True` yet | `Warning` Event; debug the dependency first |

Three properties of the signal matter when reading it:

- **Events fire on change only.** An object that has been failing for an hour has one Event from
  the start of the failure and none since, so silence in `describe` is not health.
- **A counter is activity, not state.** A failure that is not being retried stops incrementing while
  its `Ready=False` condition stays put, so an alert over the counter fires when a failure happens,
  not for as long as it lasts. `status.conditions` is what is true now.
- **A missing series is not a failure.** The counter is recorded when a reconcile reaches its
  terminal status path, whether or not that path needed to write status, so a kind with no series at
  all has either never reconciled or is not being scraped.

If an object's `Ready` condition changes but no Events appear, the manager is probably missing the
`events` RBAC verbs — see [Deployment](deployment.md#observability). Slow reconciles point at the
console rather than the operator:

```promql
histogram_quantile(0.9,
  sum by (le, operation) (rate(unifi_operator_upstream_request_duration_seconds_bucket[5m])))
```

## Operator logs

```bash
kubectl -n <operator-namespace> logs deploy/unifi-operator -f
```

- Look for the reconcile key (`namespace/name`) and the error returned by the reconciler.
- Controller-runtime logs the error it will retry; the backoff interval grows, so a repeating
  error is expected to slow down.
- A panic/stacktrace points at a nil or type-assertion fault in the controller.

## Isolate the module

Work from the inside out:

1. **UniFi client** — reproduce the failing call against a fixture with `go test`. If the
   client cannot reach or parse the controller, the fault is there, not in the reconciler.
2. **Reconciler** — run the envtest/fake-client test for that branch. Confirm the desired
   request and the status written.
3. **CRD schema** — if the API server rejects the CR, `kubectl apply --dry-run=server` and
   `task manifests` diff show whether the generated CRD matches the type.

## Drift

Drift is when the controller and the CR disagree with the UniFi controller.

```bash
# Compare spec to upstream manually via the CLI
unifi-operator-cli snapshot <kind> --controller <url>
```

- If the CR never reconciles a field, check that the reconciler reads it and that it is not
  masked by a default.
- If the controller object changes but the CR does not update, check the watch/predicate — a
  misconfigured predicate can drop the update event.
- If the operator re-creates a deleted object, that is expected; if it cannot, check finalizers
  and owner references.

## Common causes

| Symptom | Likely cause |
|---------|--------------|
| `Ready=False`, reason `InvalidSpec` | Bad spec value; CRD marker or controller validation rejected it |
| `observedGeneration` never advances | Controller crash-loop, or reconcile error returned before status update |
| Object stuck terminating | Finalizer not removed on the deletion path |
| Frequent churn / requeue storm | Status updated unconditionally, or predicate not filtering no-op events |
| Auth failures | Wrong/expired API key or session; verify the `Secret` and controller version |

## Docs and builds

- Docs failures (`zensical build --strict`) are almost always a broken internal link — the build
  output names the source file and target.
- Manifest drift: run `task manifests generate` and inspect the diff.

## Related

- [Testing](testing.md)
- [Deployment](deployment.md) — the observability contract, scrape setup, and alert expressions.
- [Architecture](architecture.md)
- [CRD conventions](crd-conventions.md)
- [CLI snapshot](snapshot.md)
