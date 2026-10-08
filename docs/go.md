# Go Bar

This is the canonical, language-level Go bar for the UniFi-Operator workspace. It is cited by
the `go-developer` and `go-reviewer` agents and is deliberately scoped to Go the language — not
to kubebuilder or controller-runtime, whose idioms live in `operator/AGENTS.md` and
`cli/AGENTS.md`.

It distils [Effective Go](https://go.dev/doc/effective_go), the [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments),
and the [Uber Go Style Guide](https://github.com/uber-go/guide/blob/master/style.md), adapted
to this repo's `golangci-lint` configuration. Where a source and the linter disagree, the
linter config and the affected module's `AGENTS.md` win.

Do not restate kubebuilder/controller-runtime mechanics here; do not restate this bar in a
module `AGENTS.md`.

## Formatting

- `gofmt`/`goimports` clean. One import group per origin: standard library, third-party, local.
- Tabs for indentation (Go's tooling enforces it). No trailing whitespace.
- Keep lines reasonable; do not align struct tags or consecutive assignments by hand beyond what
  gofmt does.
- `golangci-lint run` is part of the gate and must be clean.

## Naming

- MixedCaps, not underscores. Initialisms keep their case (`URL`, `ID`, `HTTP`, `API`).
- Short names for short scopes (`i`, `ctx`, `err`); descriptive names for package-level
  identifiers. Avoid stutter (`unifi.Client`, not `unifi.UniFiClient`).
- Interfaces are named for behavior (`Reader`, `Reconciler`), often with `-er`. Define an
  interface at the consumer, not the producer.
- Receivers are short, consistent, and never `this`/`self`.
- File names are `snake_case` (`unifi_client.go`), `_test.go` for tests; kubebuilder-generated
  files keep their generated names.

## Errors

- Errors are values. Return them; do not panic except for truly unrecoverable programmer
  errors at startup.
- Wrap with context using `%w`: `fmt.Errorf("list networks: %w", err)`. Never lose the cause.
- Check errors on every call. Ignored errors require an explicit `_ =` and a comment, or a
  `//nolint` with a reason.
- Use `errors.Is`/`errors.As` for inspection, not string matching.
- Sentinel/typed errors for conditions callers act on; annotate API errors in `internal/unifi`
  so controllers can distinguish terminal (`InvalidSpec`) from transient (retry) failures.
- Do not log an error and return it — pick one. Controller-runtime logs returned errors.

## Context

- Every function that does I/O, talks to the API server, or calls the UniFi controller takes a
  `context.Context` as its first parameter, named `ctx`.
- Never store a `context.Context` in a struct. Never pass `nil`; use `context.TODO()` only in
  tests when a context is genuinely not available, never on a hot path.
- Honor cancellation and deadlines; do not spawn goroutines that outlive `ctx`.

## Concurrency

- Every goroutine must have a defined exit. No fire-and-forget goroutines without a comment
  explaining shutdown.
- Guard shared state with a `sync.Mutex`/`RWMutex` or channels; document which. Use
  `sync.Once` for one-time initialization.
- Prefer passing data over sharing it. Do not start a goroutine from a library function the
  caller cannot control.
- `go test -race` must pass.

## Interfaces and types

- Accept interfaces, return concrete types.
- Keep interfaces small (one to three methods). Do not define an interface before there are two
  implementations or a test double.
- Avoid `interface{}`/`any` except at true boundaries; prefer generics or a concrete type.
- Struct literals use field names for all but the first couple of fields, or when positional
  reads clearly.
- Do not embed types purely to reuse methods you do not want to expose.
- Use `time.Duration` for durations and `time.Time` for instants, never bare integers/strings.

## Slices, maps, and pointers

- Preallocate slices when the length is known (`make([]T, 0, n)`). Use `append` idiomatically.
- Never return a pointer to a loop variable in older Go; in Go 1.22+ the loop variable is
  per-iteration, but still avoid surprising aliasing.
- Map iteration order is random: never rely on it for output ordering. Sort keys explicitly
  when emitting YAML or comparing.
- Return `nil` slices rather than empty ones when there is no data, unless the caller requires
  a non-nil empty slice.
- Use pointers for optional struct fields and large structs; avoid pointer-to-slice/pointer-to-map.

## Packages and layering

- Small, focused packages with a clear responsibility. `internal/` for code not meant to be
  imported outside the module.
- No import cycles. The dependency direction is `controller → unifi`; `unifi` depends on
  nothing in the Kubernetes world.
- Exported identifiers need doc comments starting with the identifier name.
- Avoid package-level mutable state; pass dependencies explicitly (constructors with the
  `NewX`/`newX` pattern).
- Do not use `init()` for anything with side effects that could fail at runtime.

## kubebuilder-generated code

- Never hand-edit `zz_generated.deepcopy.go` or files under `config/crd/`, `config/rbac/`;
  change the source marker and re-run `make manifests generate`.
- Keep `+kubebuilder:*` markers immediately above the type/field/function they annotate.

## Testing

- Prefer table-driven tests with `t.Run` subtests. Use `t.Parallel()` when state is independent.
- Assert with the standard library plus the module's chosen helper (for example
  `gotest.tools/v3/assert` or controller-runtime matchers). Do not introduce a new assertion
  framework without a reason.
- Tests must be deterministic and hermetic: envtest for API-server behavior, `httptest` for the
  UniFi client. No live network, no real clock sleeps without `Eventually`.
- Golden files live under `testdata/`; update them deliberately.

## Verification

Run the affected module's gate before declaring work done:

```
go build ./... && go vet ./... && golangci-lint run && go test ./...
```

and, for the operator, `make manifests generate` must produce no diff. See the `go-developer`
agent and the module `AGENTS.md` files for the exact commands.
