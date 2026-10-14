# Proposal

## Why

A "full snapshot" today is neither complete nor verifiable. `snapshot all` emits only
controller, sites, networks, and WiFi broadcasts, so firewall, ACL, DNS, RADIUS,
traffic-matching, switching, and VPN objects need separate invocations that are easy to forget.
Two operator CRDs — `UnifiFirewallPolicyOrdering` and `UnifiAclRuleOrdering` — have no projection
at all (`docs/unifi-api.md` shows `—` in the snapshot-consumer column for both ordering
endpoints), so an adopted snapshot can silently change firewall/ACL evaluation order. Finally,
there is no way to check that a snapshot captured everything, so completeness is asserted by
hand against a live console.

## What Changes

- Redefine `snapshot all` (**BREAKING**) to emit every projectable kind — including the newly
  projected ordering kinds — so one command is genuinely complete. `all` becomes site-agnostic and
  covers every site on the console.
- Add `snapshot firewall-policy-ordering` and `snapshot acl-rule-ordering` projections, and
  include them in `all`. References are resolved to Kubernetes identity (emitted
  `UnifiFirewallPolicy` / `UnifiAclRule` names); no upstream UUID reaches `spec`.
- Add `snapshot verify`: re-reads the live console and reports (a) coverage — upstream objects
  with no projection or objects skipped during projection — and (b) fidelity — drift between an
  emitted resource and a fresh upstream read. `verify` writes nothing and exits non-zero on
  unexplained gaps.
- Add `snapshot/` to the repository `.gitignore` as the conventional snapshot output directory.
- Sync the `docs/unifi-api.md` snapshot-consumer column for the two ordering endpoints.

## Capabilities

### New Capabilities
<!-- None: the console-read-and-project behavior is already owned by `unifi-snapshot`. -->

### Modified Capabilities
- `unifi-snapshot`: adds requirements for projecting firewall-policy and ACL-rule ordering, adds
  the requirement that `all` emit every projectable kind, and adds a coverage-and-fidelity
  verification requirement.

## Impact

**CRD kinds:** none added or changed. `UnifiFirewallPolicyOrdering` and `UnifiAclRuleOrdering`
already exist (`operator/api/v1alpha1/unififirewallpolicyordering_types.go`,
`unifiaclruleordering_types.go`); this change only teaches the CLI to emit them.

**Controllers:** none. No operator reconciler changes.

**CLI commands (`cli/`):**
- `snapshot all` — behavior change (BREAKING): every kind, every site.
- `snapshot firewall-policy-ordering` — new kind.
- `snapshot acl-rule-ordering` — new kind.
- `snapshot verify` — new subcommand.
- `cli/internal/unifi/` — new read methods for the two ordering endpoints.
- `cli/internal/snapshot/` + `cli/internal/emit/` — new projections; new golden fixtures.

**API contract (`docs/unifi-api.md`):** the snapshot-consumer column for
`GET /v1/sites/{siteId}/firewall/policies/ordering` and `GET /v1/sites/{siteId}/acl-rules/ordering`
changes from `—` to the new kind.

**Repo config:** `.gitignore` gains `snapshot/`.

**Docs (`docs/`):** the CLI/snapshot documentation gains the new kinds, the redefined `all`, and
`verify`.

## Non-Goals

- No new CRD kinds and no changes to existing API types, controllers, or RBAC.
- No write-back to the console: `verify` and `snapshot` remain read-only against the controller.
- No change to how personal passphrases or RADIUS profile UUIDs are handled: those remain
  annotated placeholders, and `verify` treats them as known, expected gaps rather than drift.
- Not fixing the fact that `device-tags` has no CRD; it stays a read-only listing.
