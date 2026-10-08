# AGENTS.md — `operator/`

The `operator/` module is the kubebuilder project that reconciles UniFi CRDs. It owns all
CRD API types, controllers, and the typed UniFi client. Read the root `AGENTS.md` for the CRD
golden rule and the cross-module checklist before changing anything that a CR exposes.

## Layout (canonical kubebuilder)

```
operator/
  api/v1alpha1/         CRD API types (*_types.go) + zz_generated.deepcopy.go
  cmd/main.go           manager entrypoint — scheme, controllers, flags
  internal/
    controller/         reconcilers
    unifi/              typed UniFi controller client (no k8s types)
  config/
    crd/                generated CRDs
    rbac/               generated RBAC
    manager/            manager Deployment
    samples/            example CRs
    default/            kustomize root
  test/                 envtest helpers and e2e
  Makefile              kubebuilder targets
```

## Commands

| Task | Command |
|------|---------|
| Generate CRDs + RBAC + deepcopy | `make manifests generate` |
| Build | `go build ./...` |
| Vet | `go vet ./...` |
| Lint | `golangci-lint run` |
| Unit + envtest | `make test` (or `go test ./...`) |
| Run against a cluster | `make run` |
| Install CRDs into the cluster | `make install` |

`make test` bootstraps `envtest` via `setup-envtest`; the first run downloads the API server
and etcd binaries. If the download is blocked, set `KUBEBUILDER_ASSETS` to a pre-fetched
`setup-envtest use -p path` directory.

## API type rules

- API types live in `api/v1alpha1/`. Group/version is `unifi.supporterino.de/v1alpha1`
  (confirm the exact group in `PROJECT` before adding a new kind).
- Every kind has a `Status` with `conditions []metav1.Condition`, `observedGeneration int64`,
  and a human-readable summary field. Add a `+kubebuilder:subresource:status` marker.
- Add printer columns with `+kubebuilder:printcolumn` for the fields an operator user needs
  (`Ready`/`Synced` condition, the key spec field, age).
- Validation is expressed with `+kubebuilder:validation:*` markers, not hand-written webhooks,
  unless cross-field validation genuinely requires a webhook.
- **Never put a UniFi `_id` in `spec`.** Reference other CRs by `name`/`namespace` or a typed
  selector; put upstream IDs in `status` only. See `docs/crd-conventions.md`.
- After any type change: `make manifests generate`, then verify no diff remains.

## Controller rules

- One reconciler per kind under `internal/controller/`, wired in `cmd/main.go`.
- A reconciler is idempotent: re-running it on an unchanged object produces no writes. Use
  `controllerutil.CreateOrUpdate` / server-side apply and compare before updating status.
- Always `defer` status updates on terminal paths; set `observedGeneration` to
  `obj.Generation` once reconciled.
- Add/remove finalizers symmetrically in the same reconcile path that creates/deletes the
  UniFi-side object.
- Return transient UniFi errors so controller-runtime backs off; do not swallow them.
- Set owner references on child Kubernetes objects and use predicates to drop no-op events.
- Add the RBAC markers (`+kubebuilder:rbac`) next to the k8s calls that need them and re-run
  `make manifests`.

## UniFi client rules (`internal/unifi/`)

- The client returns **plain Go structs** and never imports Kubernetes types.
- All calls take a `context.Context` and honor its deadline.
- Talk to the controller only over HTTP; tests use `net/http/httptest`. Never hit a live
  controller in a test.
- Keep the endpoint → method mapping in sync with `docs/unifi-api.md`; a contract change
  updates both the code and that document.

## Testing

- Unit-test reconcilers with the controller-runtime `fake` client for logic that does not need
  validation/defaulting.
- Use `envtest` for CRD defaulting, validation, subresource status, and finalizers.
- Use `httptest.Server` fixtures for the UniFi client. No live network calls.
- Table-driven tests with `t.Run` subtests; use `t.Parallel()` where safe.

## Gate

```
go build ./... && go vet ./... && golangci-lint run && go test ./... && make manifests generate
```

`make manifests generate` must produce no diff.
