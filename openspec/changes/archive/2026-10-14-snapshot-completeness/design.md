# Design

## Context

See `proposal.md` for motivation. The relevant current state:

- `cli/internal/cmd/snapshot.go` dispatches kinds in `run()` via a growing `if kind == ...` chain.
  `snapshot all` currently appends only `controller`, `sites`, and the `networks`/`wifi` block for a
  single `--site`. Output is written by `emitResources` to `-` (stdout) or an output directory.
- Projections live in `cli/internal/snapshot/snapshot.go`. `snapshot.Networks` already returns an
  upstream-id -> emitted-name map precisely so cross-resource references (e.g. firewall zone member
  networks) resolve by Kubernetes identity; `FirewallPolicies` reuses `ListZones` +
  `ListTrafficMatchingLists` and resolves references by name.
- The typed client is `cli/internal/unifi/`: one small file per endpoint group (`firewall_policies.go`,
  `acl_rules.go`, `firewall_zones.go`, ...), plain Go structs, no Kubernetes imports, pagination via
  the shared `listAll` in `client.go`.
- The ordering CRDs already exist and are authoritative in the operator:
  `operator/api/v1alpha1/unififirewallpolicyordering_types.go` (spec: `siteRef`, `source`,
  `destination` zone selectors, `BeforeSystemDefined`/`AfterSystemDefined []CoreRef`) and
  `unifiaclruleordering_types.go` (spec: `siteRef`, `OrderedAclRules []CoreRef`). Both reference
  other CRs by name and keep upstream UUIDs in `status` only.
- `docs/unifi-api.md` maps every endpoint to its snapshot consumer and currently shows `—` for the
  two ordering `GET`s.
- `.gitignore` already ignores `*.env` but has no snapshot-output pattern.

## Goals / Non-Goals

**Goals:**
- One command (`snapshot all`) captures every projectable kind across every site.
- The two ordering kinds are projected with Kubernetes-identity references only.
- A `snapshot verify` that mechanically answers "did we capture everything, and is the capture
  faithful?" for a previously written snapshot directory.

**Non-Goals:**
- No CRD or controller changes; the ordering types are consumed as-is.
- No upstream mutation; `verify` and `snapshot` stay read-only.
- No resolution of passphrase/RADIUS-UUID placeholders (they remain annotated gaps).

## Decisions

### D1: Redefine `all` rather than add a second verb

`all` becomes the complete projection: every projectable kind × every site. The alternative — a new
`full` command or a `--complete` flag — was rejected because two overlapping "everything" verbs
invite the exact omission this change exists to fix. The site span changes: `all` iterates
`ListSites` and emits each site's site-scoped objects, so `--site` no longer restricts `all`
(it still restricts the per-kind commands). This is the **BREAKING** part and is called out in
`docs/`. Kinds are driven by a single ordered list so a future kind cannot be added to the dispatch
chain but forgotten by `all`; a table test asserts `all` covers every dispatchable kind.

### D2: Ordering projection reuses the id -> name maps already built for references

`snapshot.FirewallPolicies` already receives `zones` and returns name-resolved resources; the
ordering projection needs the reverse: the emitted name for a given upstream policy id. Rather than
re-deriving names (which disagrees after collision suffixes), the design follows the existing
`Networks`/`FirewallZones` pattern: the policy projection returns an upstream-policy-id ->
emitted-name map (as `Networks` does for `siteRef`), and `FirewallPolicyOrdering` consumes it.
For ACLs, `AclRules` already exists; give it the same id -> name return so `AclRuleOrdering` resolves
rule ids to names. The upstream ordering list is then mapped position-for-position.

### D3: Firewall ordering is enumerated per zone pair from the policy set

The upstream ordering endpoint is scoped by `sourceFirewallZoneId` + `destinationFirewallZoneId`.
The projection derives the zone pairs from the policies it already listed (distinct source/destination
zone pairs among user-defined policies), then reads `GetFirewallPolicyOrdering` per pair. Each pair
yields one `UnifiFirewallPolicyOrdering`. The system-defined partition is represented by the CRD's
anchored boundary, so system-defined policies are excluded from
`BeforeSystemDefined`/`AfterSystemDefined`; a policy that cannot be resolved to an emitted name is
skipped with a warning and an annotation rather than emitting an unresolvable reference — the same
fail-soft-with-annotation rule the CLI applies to unresolved device tags and network refs today.

### D4: `verify` compares a written snapshot directory against a fresh console read

`snapshot verify -o <dir>` (default `snapshot/`) reads the CRs a prior snapshot wrote, re-reads the
console, and reports:
- **Coverage:** every projectable kind present upstream has at least one emitted resource, and no
  representable upstream object is missing from the directory. Missing objects and skipped objects
  are listed with the reason.
- **Fidelity:** each emitted resource's `spec` is compared to a fresh in-memory projection of the
  same upstream object; field-level differences are reported.

The comparison reuses the projection functions directly, so "fidelity" means "the projection of live
state equals what is on disk" — a change in upstream state since the snapshot is reported as drift,
which is the intended meaning. `verify` treats anything carrying a known-gap annotation
(`AnnotationUnresolvedWifiSecret`, `AnnotationUnresolvedRadiusProfile`,
`AnnotationUnrepresentedWifiSettings`, `AnnotationUnresolvedDeviceTag`,
`AnnotationUnresolvedNetworkRef`, `AnnotationUnrepresentedIPv6`) as expected, not drift. It writes
nothing and performs no upstream mutation. Exit codes: `0` when only expected gaps remain, non-zero
when an unexplained gap exists. Warnings go to stderr, matching `snapshot`'s existing convention.

### D5: New reads follow the one-file-per-endpoint-group client pattern

Two read methods are added alongside their existing siblings: `GetFirewallPolicyOrdering` in
`cli/internal/unifi/firewall_policies.go` and `ListAclRuleOrdering`/`GetAclRuleOrdering` in
`cli/internal/unifi/acl_rules.go`, both via the shared `listAll`/`do`. `docs/unifi-api.md`'s
snapshot-consumer column is updated in the same change (contract golden rule).

### D6: `snapshot/` becomes the conventional output directory

`.gitignore` gains `/snapshot/`, anchored to the repo root so it matches the conventional output
directory without also matching the `cli/internal/snapshot/` source package. The `-o` default stays
`-` (stdout) so nothing writes to disk by accident; `verify` defaults to reading `snapshot/` because
that is the documented "full snapshot" destination.

### D7: Site-scoped names are site-qualified in `all`/`verify`

Making `all` span every site exposes a name collision that a single-site snapshot never hit: two
sites routinely share site-scoped object names ("Default"/"Guest" networks, `Internal`/`External`
zones, identical zone pairs), while every site-scoped projection names its resource from the
upstream object name alone. Emitting `all` per site would therefore produce duplicate
`(kind, metadata.name)` values — a silent overwrite in the directory output and an identity
collision a user cannot distinguish — and `verify` (which keys by `kind/name`) could not detect it.

`all` and `verify` therefore **site-qualify** every site-scoped resource name (`<site>-<name>`), so
`all` emits no duplicate `(kind, name)` and the write-to-directory path cannot lose one. The
qualification is applied where each projection allocates names, so the id→name maps that feed
cross-resource references carry the qualified names and references keep resolving. Per-kind
commands project a single site, so they keep their existing upstream-derived names unchanged; the
qualifier is set only by `snapshot all` (and by `verify`, which reuses the same projection path).
`AclRuleOrdering` (new in this change) follows the same rule instead of hard-coding a site prefix.

## Risks / Trade-offs

- **BREAKING `all` semantics** -> documented in `docs/`; the release notes call it out; the per-kind
  commands retain `--site` for users who want the old scope.
- **Older consoles may not expose the ordering endpoints** -> the reads happen only when a
  user-defined policy (or ACL rule) exists to order, and an endpoint error fails the run closed with
  the actionable `APIError` message rather than being skipped. `all` therefore fails loudly on such a
  console instead of emitting a silently incomplete snapshot; this is deliberate for a
  completeness tool, and the dedicated `firewall-policy-ordering` / `acl-rule-ordering` commands
  surface the same error. A missing endpoint is surfaced with its reason, never silently omitted.
- **Zone-pair enumeration can miss a pair with no user-defined policies** -> pairs are derived from
  the policy set itself, so a pair with nothing to order yields no object; this matches the CRD's
  "one authoritative ordering object per pair" model.
- **Fidelity false positives from volatile upstream fields** -> the comparison is restricted to
  fields the projection reads, and known-gap annotations are excluded by construction (D4).
- **`verify` reads the console again, so it is subject to change between snapshot and verify** ->
  this is inherent and intended; the report distinguishes "changed upstream" (drift) from
  "never captured" (coverage).
- **`all` re-reads shared upstream data once per kind per site** -> the registry keeps each kind's
  projection independent and pure in exchange for repeated reads (for example the network projection
  is loaded for `networks`, `firewall-zones`, `wifi`, and `acl`). Accepted for a read-only,
  run-occasionally CLI; a per-site fetch cache is deliberately left to a future change rather than
  complicating the registry here.

## Migration Plan

1. Land the ordering reads + projections + golden fixtures (additive, no behavior change).
2. Land `verify` (additive).
3. Flip `all` to the complete kind list and site span; update `docs/` and the `docs/unifi-api.md`
   consumer column; add `snapshot/` to `.gitignore`.
4. Rollback: revert the CLI release; no data migration and no cluster state is involved.

## Open Questions

- Whether the release should ship `verify` under `snapshot verify` or a top-level `verify` verb. Low
  impact: the subcommand fits the existing `snapshot`-centric grammar and can be aliased later
  without a spec change.
