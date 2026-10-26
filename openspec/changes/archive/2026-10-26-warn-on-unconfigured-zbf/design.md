# Design

## Context

See `proposal.md` — Why. Constraints and observed facts that shape the approach:

- A live probe (2026-10-26) of a ZBF-off console returned the **same** body from both
  `GET /proxy/network/integration/v1/sites/{id}/firewall/zones` and
  `GET .../firewall/policies`: `400`, code `api.firewall.zone-based-firewall-not-configured`,
  message `Zone Based Firewall is not configured`. The earlier probe recorded in
  `openspec/changes/archive/2026-10-12-verify-unifi-live-contract/design.md` only covered zones.
- `cli/internal/unifi/firewall_zones.go:42` already maps the code to `[]FirewallZone{}, nil` for
  `ListZones`, but discards the fact that it did so. `cli/internal/unifi/firewall_policies.go:117`
  has no mapping, so `ListFirewallPolicies` returns the `*APIError`.
- In the CLI, `projectFirewallPolicies` (`cli/internal/cmd/snapshot.go:421`) calls `ListZones`
  and then `ListFirewallPolicies`; `projectAll` (`:259`) returns on the first projection error.
  `snapshot all` therefore aborts on a ZBF-off console at the policies call.
- `projectFirewallPolicyOrdering` (`:444`) only calls `GetFirewallPolicyOrdering` for pairs
  derived from user-defined policies (`snapshot.FirewallPolicyZonePairs`), so it never reaches
  `/ordering` when the policy list is empty.
- Projections already return `[]string` warnings (`kindProjection`, `:200`), `run` prints them to
  stderr, and `snapshot verify` counts them as `skipped` (`cli/internal/cmd/verify.go:234`).
- The operator's `HTTPClient.ListFirewallPolicies` (`operator/internal/unifi/firewall_policies.go:121`)
  has no mapping; `UnifiFirewallPolicy` and `UnifiFirewallPolicyOrdering` both call it
  (`unififirewallpolicy_controller.go:266`, `unififirewallpolicyordering_controller.go:262`) and
  map a terminal `4xx` to `InvalidSpec` (`:403`, `:329`).
- `UnifiFirewallZone` already treats the read as empty (`ListZones`) and treats a ZBF-off
  `GetZone` as `ErrNotFound`, but its write path deliberately fails closed
  (`unififirewallzone_controller.go:255`).

## Goals / Non-Goals

**Goals:**

- Make the CLI's firewall family uniformly degrade on a ZBF-off console with one explanation
  per site, without changing behaviour on a healthy or partially broken console.
- Give the operator an accurate, machine-readable reason for the same condition without changing
  the fail-closed write contract.
- Keep the change hermetic and testable with recorded fixtures only.

**Non-Goals:**

- Importing legacy firewall rules, or modelling a "pre-ZBF" firewall surface.
- Turning arbitrary firewall `4xx`/`404` responses into an empty subsystem.
- Changing `UnifiFirewallZone`'s read-path empty set or its write-path fail-closed behaviour.
- Reordering or restructuring the snapshot projection registry.

## Decisions

### D1: `ListZones` returns the configured flag instead of swallowing it

Change the CLI client to:

```go
func (c *Client) ListZones(ctx context.Context, siteID string) (zones []FirewallZone, zbfConfigured bool, err error)
```

`zbfConfigured` is `false` only when the response is the ZBF-unconfigured code; every other
outcome (including an empty `200`) leaves it `true`. The three firewall projections turn
`!zbfConfigured` into a note. The operator client adopts the same flag (D5) so a reconciler can
tell an empty-but-configured zone page from a ZBF-off console before it resolves selectors.

- **Why not infer from `len(zones) == 0`:** a ZBF-on console always has system zones, but a
  filtered/empty page is not a contract guarantee; inferring would also conflate a future
  pagination bug with an unconfigured subsystem.
- **Why not a sentinel error:** callers that must continue would have to special-case it at every
  call site; the boolean records the fact without making the absence an error.
- **Alternative:** return a `ZoneSet{Zones, Configured}` struct. Rejected only for diff size; a
  documented boolean is adequate and keeps the existing `zones` variable shape.

### D2: Map the code in the CLI policy read; leave ordering unmapped

`ListFirewallPolicies` returns `[]FirewallPolicy{}, nil` for the code and reports the same
configured flag as `ListZones`, so a policies-only signal is not indistinguishable from an empty
policy list and can still raise the site note (D3).
`GetFirewallPolicyOrdering` is deliberately **not** mapped: the ZBF-off path never calls it
because no user-defined policies exist, so mapping it would encode an unobserved assumption.
If a future call path reaches it, the `*APIError` still surfaces rather than being hidden.

- **Alternative:** map every firewall read. Rejected as speculative; the probe covered zones and
  policies only.

### D3: Typed notes channel in the CLI projection layer

Replace the three-value `kindProjection` result with:

```go
type projectionResult struct {
    Resources []emit.Resource
    Warnings  []string // objects that could not be represented (existing meaning)
    Notes     []string // subsystem-level notices; not a skipped object
}
```

Every projection returns the struct. The three firewall projections append
`firewallUnconfiguredNote(siteName)` when their configured flag is false: the zone projection uses
the zones flag, while the policy and ordering projections use the conjunction of the zones and
policies flags because both reads feed them. `projectAll` and `projectKind`
deduplicate notes exactly as they already deduplicate warnings (`snapshot.go:284`), preserving
first-seen order. Because the text is site-scoped and kind-agnostic, the three firewall kinds
collapse to one line per site; a per-kind invocation prints one line.

`run` prints both warnings and notes to stderr with the existing `warning:` prefix. The note text
is:

```
site "<name>": Zone-Based Firewall is not configured; firewall resources could not be fully
snapshotted. This is expected if the console has not migrated to ZBF yet.
```

The text deliberately does not enumerate the three kinds: a partial signal (one read reports the
code, the other succeeds) must not claim kinds it did not lose, so "fully" is the strongest true
claim.

- **Why not prefix-tag a single warnings slice:** verify's semantics would then depend on string
  matching, which is brittle and easy to drift.
- **Why not a client-cached flag:** it hides state on the `Client` and couples note emission to
  call order.
- **Cost:** every projection signature changes; the compiler enumerates the call sites, and the
  existing tests exercise them.

### D4: `verify` prints notes but keeps `skipped` for real skips

`compareSnapshot` continues to receive only `warnings`, so `skipped: len(warnings)` is unchanged.
The verify command prints notes alongside warnings to stderr. No report-line format change is
needed; the distinction is the channel, not the message.

### D5: Operator surfaces a distinct terminal condition before selector resolution

- Export the existing code check from `operator/internal/unifi/firewall_zones.go` as
  `IsZoneBasedFirewallUnconfigured(err error) bool` (the unexported helper stays the implementation).
- Make the operator client's `ListZones` report the same configured flag as the CLI (D1):
  `(zones []FirewallZone, zbfConfigured bool, err error)`, `false` only for the code. The empty-set
  mapping itself is unchanged.
- Add `reasonZoneBasedFirewallNotConfigured = "ZoneBasedFirewallNotConfigured"` in
  `operator/internal/controller/conditions.go`.
- In `UnifiFirewallPolicy` and `UnifiFirewallPolicyOrdering`, gate on `!zbfConfigured` **immediately
  after the zone read and before any `resolveZoneSelector` call**, writing the new reason via
  `updateStatus` (terminal: no error returned, no requeue). Without that gate the condition is
  unreachable on the console this change targets: a ZBF-off zone read returns an empty set with no
  error, so selector resolution fails first — `BuiltInZoneNotFound` (retry) or
  `DependencyNotReady` (retry) — and hot-requeues instead of reporting the absent subsystem.
- In `handleUpstreamError` for `UnifiFirewallPolicy`, `UnifiFirewallPolicyOrdering`, and
  `UnifiFirewallZone`, check `unifi.IsZoneBasedFirewallUnconfigured(err)` **before** the generic
  `!apiErr.Retryable()` branch and write the new reason with a message naming the missing
  subsystem. This is the second variant: the zone read succeeded, but a policy/ordering read or
  write, or a rejected zone *write*, carries the code.

The operator client's `ListFirewallPolicies` is left as-is: remapping the read to empty would let
the policy controller reach `CreateFirewallPolicy` and fail one call later with the same
condition, adding a pointless write attempt and blurring the signal. The zone read path keeps its
empty mapping and discards the flag; only a rejected *write* gets the new reason, which is
consistent with the existing comment at `unififirewallzone_controller.go:255`.

- **Why not a new CRD status field:** `metav1.Condition.Reason` already carries the machine-readable
  outcome; a field would duplicate it and change the contract.
- **Why not reuse `InvalidSpec`:** the spec is valid; the console's subsystem is absent. `InvalidSpec`
  sends users to edit a correct CR.

### D6: Record the family-wide code in the contract, and the CLI warn in the snapshot docs

`docs/unifi-api.md:496` currently says the operator and CLI map the code to an "empty zone set".
Update it to name both endpoints and describe the two consumers' distinct responses. Add a short
ZBF-off note to `docs/snapshot.md` near the kinds table.

### D7: Hermetic fixtures and regression coverage

- Add `cli/testdata/firewall-policies-unconfigured.json` with the 2026-10-26 probe body (same
  code and shape as the older zones recording, whose message wording differs; matching is by
  `code`, never message).
- Extend `newZBFOffServer` (`cli/internal/cmd/snapshot_test.go:316`) to answer
  `/firewall/policies` with the code, and add a `snapshot all` case proving non-firewall kinds are
  still emitted and exactly one note per site is printed.
- Assert the existing `firewall-zones` ZBF-off test now sees the note on stderr.
- Add a verify test that a ZBF-off note does not increment the skipped count.
- Operator: cover the new reason in the policy, ordering, and zone controller tests using the
  existing fake/`httptest` pattern.
- Operator: pin the `ListZones` flag (false only for the code; true for an empty `200`, another
  `400`, and a `401`), and cover both reaching variants per controller — the zone-read gate
  (empty zones plus a false flag: terminal reason, no requeue, no policies read or write) and the
  zones-readable variant (populated zones, code on the policies read/write).

## Risks / Trade-offs

- **The note only appears when a firewall kind is projected** → accepted: `snapshot networks`
  should not warn about a subsystem it did not read. A user running `all` sees it once per site.
- **Deduplication depends on identical text across three projections** → the text is built by one
  helper from the site name only, so it is identical by construction; a test pins one line per
  site.
- **`ListZones` has no other production caller** → now false: the signature changes in both
  modules (CLI and operator clients). The change is still contained — the compiler enumerates the
  call sites (three projections, three reconcilers, the client interfaces, and tests) — and the
  zone reconciler discards the flag.
- **A partial signal would otherwise be silent** → the policy read reports the flag too, and the
  note text stops enumerating kinds, so a policies-only signal still warns without overclaiming.
- **The operator gate duplicates the CLI selector-path fix** → both clients now expose the same
  `(value, configured, err)` shape; that symmetry is deliberate, so the two consumers cannot drift
  on what "unconfigured" means.
- **A future firewall endpoint returns the code and is not mapped** → the code is now documented as
  family-wide; `GetFirewallPolicyOrdering` is the only unmapped read, and it is unreachable on the
  ZBF-off path today.
- **New condition reason is a status-only change** → no CRD schema or version bump; Kubernetes
  clients must tolerate unknown reasons, and `task manifests generate` stays no-diff.
- **Probe evidence is a point-in-time console** → the fixtures pin the observed body; matching is
  by `code`, never message text, so wording drift does not break the mapping.

## Migration Plan

1. Land the CLI client change, projections, and tests; the CLI has no persisted state.
2. Land the operator predicate, reason, controller branches, and tests.
3. Update the docs and run the docs gate.
4. Rollback: revert the code; conditions return to `InvalidSpec` on the next reconcile, and the
   CLI returns to its current empty/failing behaviour. No CRD migration is involved.

## Open Questions

- None blocking. Whether `GetFirewallPolicyOrdering` should also map the code can be decided if a
  call path reaches it on a ZBF-off console.
