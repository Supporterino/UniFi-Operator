# Design

## Context

See `proposal.md` — Why. Only two things matter for the approach:

- `TestUnifiWifiBroadcastReconcileFailsClosed` declares `enterprise := newWifiBroadcast()` once and
  each `t.Parallel()` subtest returns that same pointer from its `objects()` closure. The other
  fixtures in the same test (`notProvisioned`, `ownedNetwork()`, `ownedSite()`) are either freshly
  constructed per call or not mutated by any subtest — `enterprise` is the shared mutable one.
- The fake client writes `ResourceVersion` into the objects it is given, which is what makes the
  sharing observable as a race rather than as a latent aliasing bug.

No architectural criteria in the design guidance apply: this is a test-fixture fix plus a
documentation map.

## Goals / Non-Goals

**Goals:**

- `go test -race ./internal/controller/` passes repeatedly, with the wifi-broadcast subtests
  themselves exercising the same assertions as today.
- `operator/AGENTS.md` lists every package in the module, so a new package cannot be forgotten in
  the authoritative reference.

**Non-Goals:**

- Sweeping other tests for the same pattern (see the proposal's Non-Goals).
- Changing any test's assertions, table cases, or parallelism.

## Decisions

### D1 — Construct the fixture inside each subtest, rather than `DeepCopy()` at use sites

Two ways to stop the sharing: build a fresh `enterprise` per subtest inside its `objects()` closure,
or keep the shared value and `DeepCopy()` it before returning it to the fake builder.

Constructing per subtest is chosen because the fixture is a plain literal built by `newWifiBroadcast()`
plus a security block: duplicating the construction through a small helper keeps the table readable
and removes the shared state entirely, so no future subtest can reintroduce the race by forgetting a
copy. `DeepCopy()` fixes only the call sites that use it and leaves the shared value in scope for the
next subtest someone adds.

### D2 — Fix the fixture, not the fake client's write

The alternative "don't hand the builder objects you still hold" is the same fix expressed as a rule.
Making the test own its fixture is enforceable by reading the test; a rule in a doc is not.

### D3 — Add the missing layout rows without restructuring the block

`operator/AGENTS.md`'s layout block gets two rows in the existing style, in dependency order
(`metrics` beside `controller`, `version` beside them). Do not reorganise the block: a layout map is
most useful when its shape is stable.

## Risks / Trade-offs

- **[A shared fixture remains somewhere the `-race` run does not reach]** → The post-fix verification
  runs the controller package under `-race` several times; anything that still trips is reported as a
  new finding rather than silently fixed (Non-Goals).
- **[Duplicating the fixture construction drifts from the original literal]** → The helper is the
  same `newWifiBroadcast()` call the test already used, plus the security block moved into it, so the
  first subtest to run still proves the original shape.

## Migration Plan

None. Test-only and documentation-only; no deployment, no rollback surface.
