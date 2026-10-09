# Testing

This is the canonical testing workflow for the UniFi-Operator workspace. Tests are hermetic:
no live UniFi controller, no network. The module `AGENTS.md` files hold the exact commands;
this document holds the strategy.

## Layers

| Layer | Tool | What it covers |
|-------|------|----------------|
| Pure unit | `go test` + `testing` | Helper logic, projections, client parsing |
| Reconciler unit | controller-runtime `fake` client | Reconcile branches that need no API-server validation |
| API-server integration | `envtest` | CRD defaulting/validation, status subresource, finalizers |
| UniFi client | `net/http/httptest` | Request shape, response parsing, error mapping, retry |
| CLI end-to-end | `httptest` + golden files | Snapshot → emitted CR YAML |

## Operator

- **Fake client** for reconciler logic that does not depend on API-server defaulting or
  validation. Register the scheme with `runtime.NewScheme()` + the API group. Assert the
  resulting objects and `status` conditions.
- **`envtest`** for anything the API server must enforce: CRD apply/default/validation, the
  status subresource, and finalizer-driven deletion. `make test` bootstraps envtest via
  `setup-envtest`.
- Reconciler tests assert **idempotency**: a second reconcile with unchanged input performs no
  writes and preserves conditions.
- Status assertions check `observedGeneration`, the `Ready` condition, and the summary.

## UniFi client

- Start an `httptest.Server` that serves recorded Integration v1 fixtures.
- Assert the outgoing request (method, path, headers, body) and the parsed result.
- Cover the error paths: a flat `Error Message` body maps to `*unifi.APIError`, a `404` matches
  `unifi.ErrNotFound`, `Retryable()` is `true` for `5xx`/`429` and `false` for the terminal `4xx`
  class, and `401`/`403` fail closed (no fallback, no retry).
- Cover list pagination: decode the page envelope (`count`/`data`/`limit`/`offset`/`totalCount`)
  and follow `offset` to completion.
- **Never** point a test at a real controller or the public internet.

## CLI

- Serve recorded UniFi API fixtures from `httptest` and run the snapshot projection.
- Compare emitted CR YAML against golden files in `testdata/`.
- Golden files are updated deliberately (`go test ./... -update`) and reviewed in the diff.

## Docs

- `zensical build --strict` is the gate. A broken internal link or nav entry fails the build.
- Keep every cross-reference in the reference library resolving.

## Manifests

- `make -C operator manifests generate` must produce no diff in a clean tree. Run it after any
  API-type or RBAC-marker change and commit the generated output.

## Rules

- No test framework beyond what a module already uses.
- Tests are deterministic; use `Eventually`/polling instead of sleeps.
- `go test -race` must pass.
- A bug fix lands with a regression test that fails before and passes after.

## Related

- [Architecture](architecture.md)
- [UniFi API contract](unifi-api.md)
- [Debugging](debugging.md)
- [Go bar](go.md)
