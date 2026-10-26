# Proposal

## Why

A console that has not migrated to Zone-Based Firewall answers **both** `/firewall/zones` and
`/firewall/policies` with `400 api.firewall.zone-based-firewall-not-configured`. The CLI maps only
the zones read to an empty set, so `snapshot firewall-policies` and `snapshot all` abort on such a
console even though every non-firewall kind is readable — and the user gets no explanation. The
operator has the same gap: `UnifiFirewallPolicy` and `UnifiFirewallPolicyOrdering` reconcile the
code as a terminal `InvalidSpec`, which misattributes a missing upstream subsystem to a bad spec.

## What Changes

- **CLI tolerates the whole firewall family.** The ZBF-unconfigured code maps to an empty firewall
  policy set in addition to the existing empty zone set, and the three firewall kinds
  (`firewall-zones`, `firewall-policies`, `firewall-policy-ordering`) each contribute one
  site-scoped note. `snapshot all` therefore keeps exporting every other kind and exits `0`.
- **CLI explains the gap.** The note names the site, states that firewall zones, policies, and
  orderings were not snapshotted because Zone-Based Firewall is unconfigured, and says this is
  expected if the console has not migrated to ZBF yet.
- **CLI verify separates notes from skips.** `snapshot verify` prints the note but does not count
  it as a skipped object; only genuinely dropped objects remain in the "skipped" count.
- **Operator reports the subsystem distinctly.** A new terminal
  `ZoneBasedFirewallNotConfigured` condition reason replaces `InvalidSpec` for
  `UnifiFirewallPolicy`, `UnifiFirewallPolicyOrdering`, and firewall-zone writes that are rejected
  with the ZBF code. Policy and ordering reconciles also short-circuit with that reason from the
  zone read's configured flag, before resolving zone selectors, so a ZBF-off console gets the
  accurate terminal reason instead of a `BuiltInZoneNotFound`/`DependencyNotReady` requeue loop.
  Reconcile still fails closed and never mutates upstream.
- **Contract is recorded.** `docs/unifi-api.md` states that the code is returned by both the zones
  and policies endpoints, not zones alone.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `unifi-snapshot`: the existing "An unconfigured subsystem yields an empty set" requirement is
  extended to firewall policies and now requires a site-scoped warning while every other
  representable kind is still emitted.
- `unifi-firewall`: adds a requirement that an unconfigured firewall subsystem is reported as a
  distinct terminal condition (instead of `InvalidSpec`) on policy, ordering, and zone-write
  paths, while preserving the empty, non-failing zone read path.

## Impact

- **CLI:** `cli/internal/unifi/firewall_zones.go` (surface the signal), `firewall_policies.go`
  (map the code and surface the same signal), `cli/internal/cmd/snapshot.go` (typed notes channel
  and site note), `cli/internal/cmd/verify.go` (notes printed, not counted), plus client/command
  tests and the `all` regression fixture.
- **Operator:** `operator/internal/unifi/firewall_zones.go` (export the code predicate and report
  the configured flag), `operator/internal/unifi/client.go` (client interface),
  `operator/internal/controller/conditions.go` (new reason),
  `unififirewallpolicy_controller.go`, `unififirewallpolicyordering_controller.go` (zone-read gate
  plus error branch), and `unififirewallzone_controller.go` (condition branch), plus controller
  tests.
- **Docs:** `docs/unifi-api.md` (family-wide code; CLI warns, operator conditions) and
  `docs/snapshot.md` (user-facing ZBF-off note).
- **CRD contract:** no new kinds and no new `spec` or `status` schema fields. The only
  CRD-visible change is a new `metav1.Condition.Reason` value on existing status conditions, so
  `task manifests generate` must stay no-diff.

## Non-Goals

- Importing legacy (non-ZBF) firewall rules; those remain unmodeled and are not snapshotted.
- Tolerating arbitrary firewall endpoint failures or `404`s; only the machine-readable ZBF code is
  treated as an unconfigured subsystem.
- Changing the `UnifiFirewallZone` read path, which already resolves an empty zone set without a
  failing condition.
- Any new CRD kind, API version, or bulk refactor of the snapshot projection registry.
