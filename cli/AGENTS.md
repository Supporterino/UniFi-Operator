# AGENTS.md — `cli/`

The `cli/` module is a separate Go module that reads a live UniFi controller and emits
adoptable Custom Resources — the "snapshot" capability. It reuses the operator's UniFi API
knowledge (the contract in `docs/unifi-api.md`) but is a distinct binary and module. Read the
root `AGENTS.md` before changing the CR shapes it emits.

## Layout

```
cli/
  go.mod
  main.go                 (or cmd/unifi-operator-cli/main.go)
  internal/
    cmd/                  cobra command tree
    snapshot/             controller → CR projection
    unifi/                UniFi HTTP client (mirrors operator/internal/unifi)
    emit/                 YAML CR emitter
  testdata/               recorded UniFi API fixtures + golden CR output
```

## Commands (kubectl-style grammar)

```
unifi-operator-cli snapshot <kind|all>   # read a live controller, emit CRs
unifi-operator-cli snapshot --controller <url> --output <dir>
```

- Prefer verb-first, `kubectl`-style subcommands.
- Emitted CRs must be valid against the operator's CRDs and must follow the same CRD golden
  rule as the operator: **no UniFi `_id`s in `spec`** — emit stable names and reference
  other emitted CRs by name. Correlation IDs, if useful, go in `status`.

## Rules

- The CLI is a consumer of the same UniFi API contract as the operator. Keep
  `docs/unifi-api.md` authoritative: if the CLI needs a field the operator does not yet model,
  add it to the contract first.
- Emitted CRs should be something a user can `kubectl apply` and have the operator adopt.
  When a controller object cannot be represented losslessly, emit it with an annotation and a
  clear warning rather than silently dropping it.
- Golden files under `testdata/` capture expected CR output; update them deliberately when the
  emitted shape changes.

## Testing

- All snapshot logic is tested against an `httptest.Server` serving recorded fixtures from
  `testdata/`. No live controller.
- Golden-file tests for emitted CR YAML.
- `go test ./...`.

## Gate

```
go build ./... && go vet ./... && golangci-lint run && go test ./...
```
