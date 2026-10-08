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
   `make manifests` diff show whether the generated CRD matches the type.

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
- Manifest drift: run `make -C operator manifests generate` and inspect the diff.

## Related

- [Testing](testing.md)
- [Architecture](architecture.md)
- [CRD conventions](crd-conventions.md)
