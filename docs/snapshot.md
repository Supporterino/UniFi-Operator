# CLI snapshot

`unifi-operator-cli snapshot` reads a live UniFi Network controller over the Integration v1 API
and writes the equivalent Custom Resources as YAML, so they can be reviewed and `kubectl apply`-ed
for the operator to adopt. It is read-only against the console: `snapshot` and `verify` never
create, update, or delete an upstream object.

Every endpoint a kind consumes is mapped in the [UniFi API contract](unifi-api.md); the shape of
every emitted CR follows the [CRD conventions](crd-conventions.md).

## Usage

```bash
unifi-operator-cli snapshot <kind|all> --controller <url> [--site <ref>] [-o <dir|->]
unifi-operator-cli snapshot verify [-o <dir>] --controller <url>
```

`--controller` (or `UNIFI_URL`) is required; `--api-key` (or `UNIFI_API_KEY`) supplies the
credential and is never logged. `--controller-ref` names the `UnifiController` the emitted `sites`
reference (default `unifi-controller`).

## Kinds

`snapshot <kind>` projects a single kind for the site named by `--site` (default `default`).
`all` is different — see [Breaking change: `all`](#breaking-change-all) below.

| Kind | Emits |
|------|-------|
| `all` | Every projectable kind below, across every site |
| `controller` | A `UnifiController` template (placeholder `secretRef`, no credential) |
| `sites` | One `UnifiSite` per upstream site |
| `networks` | `UnifiNetwork` |
| `firewall-zones` | `UnifiFirewallZone` |
| `firewall-policies` | `UnifiFirewallPolicy` |
| `firewall-policy-ordering` | `UnifiFirewallPolicyOrdering` (one per user-defined zone pair) |
| `wifi` | `UnifiWifiBroadcast` |
| `radius` | `UnifiRadiusProfile` (adopted by name) |
| `traffic-matching` | `UnifiTrafficMatchingList` |
| `acl` | `UnifiAclRule` |
| `acl-rule-ordering` | `UnifiAclRuleOrdering` |
| `dns` | `UnifiDnsPolicy` |
| `switching` | `UnifiSwitchingLag`, `UnifiSwitchingMcLagDomain`, `UnifiSwitchingSwitchStack` (adopted) |
| `vpn` | `UnifiVpnServer`, `UnifiSiteToSiteTunnel` (adopted by name) |
| `device-tags` | No CR — a read-only `name<TAB>device-count` listing for the `--site` site |

`device-tags` emits no Custom Resource, so it is excluded from `all`.

### Breaking change: `all`

**BREAKING.** `snapshot all` now emits **every projectable kind** across **every site** on the
console, iterating the console's full site list. It previously covered only `controller`, `sites`,
`networks`, and `wifi` for a single site.

`snapshot all` now **ignores `--site`**; the flag still restricts the per-kind commands. A workflow
that relied on the old single-site `all` scope must switch to the per-kind commands — for example
`snapshot networks --site <ref>` — to get the same coverage. This is deliberate: one command is
genuinely complete, and two overlapping "everything" verbs were rejected by design.

Because it spans every site, `all` **site-qualifies** each site-scoped resource name as
`<site>-<name>` — `UnifiNetwork/<site>-default` rather than `UnifiNetwork/default`. Sites
routinely share site-scoped object names (`Default`/`Guest` networks, `Internal`/`External`
zones, identical zone pairs), and qualification keeps their Kubernetes identities distinct so one
site's object cannot overwrite another's in the output directory. `snapshot verify` reuses the
same projection and so qualifies names the same way, matching the `kind/name` it keys the
directory by.

## Output

`-o`/`--output` selects where emitted CRs go:

- `-` (default) — stdout, one YAML document per resource, each preceded by a `---` separator.
- a directory — the directory is created and each resource is written to
  `<lowercase-kind>-<metadata.name>.yaml`.

A per-kind command projects a single site, so it leaves site-scoped names **unqualified** (the
upstream-derived name); `snapshot networks --site default` writes `UnifiNetwork/default` while
`snapshot all` writes `UnifiNetwork/<site>-default`. A per-kind `-o <dir>` is therefore not
interchangeable with an `all` directory for the site-scoped kinds.

Nothing is written to disk unless a directory is given, so a mistyped command cannot litter the
working tree. `snapshot/` is the conventional full-snapshot directory and is git-ignored.

## `snapshot verify`

`snapshot verify` re-reads the live console and checks a previously written snapshot directory
against it. It defaults to reading `snapshot/` (`-o` overrides), writes no file, and mutates
nothing upstream. It spans every site, mirroring `all`.

```bash
unifi-operator-cli snapshot all -o snapshot/
unifi-operator-cli snapshot verify           # verifies snapshot/
```

It reports two things:

- **Coverage** — objects the console has that the snapshot should contain, plus objects the
  projection skipped. A projected object with no file on disk, or an on-disk resource with no
  upstream object, is a gap.
- **Fidelity** — field-level `spec` drift between an emitted resource and a fresh in-memory
  projection of the same object. `status` is not compared; it belongs to the operator. Upstream
  state that changed between the snapshot and the verify is reported as drift, which is intended.

A resource carrying any [known-gap annotation](#known-gaps) is classified as an expected gap, not
drift, and is not compared field-for-field.

### Exit codes

| Exit | Meaning |
|------|---------|
| `0` | Only expected/explained gaps remain (or none at all) |
| non-zero | At least one unexplained gap: a missing file, an unexpected/on-disk-only resource, or unexplained drift |

Warnings (objects the projection could not represent) go to **stderr**; the report goes to stdout.
A sample report:

```text
snapshot verify: snapshot/
projected 42 resource(s); 1 projection warning(s)
coverage: 1 missing, 0 unexpected
fidelity: 0 drift(s), 1 expected gap(s)
  missing: UnifiNetwork/default-guest-wifi
  expected gap: UnifiWifiBroadcast/default-lobby (unifi.supporterino.de/unresolved-wifi-secret)
result: FAIL (1 unexplained gap(s))
```

### Known gaps

Some upstream state cannot be represented losslessly in `spec`. The CLI emits such a resource with
an annotation recording the gap plus a warning, rather than inventing a `spec` field. `verify`
treats a resource carrying **any** of the following annotations as an expected gap:

| Annotation | Gap |
|------------|-----|
| `unifi.supporterino.de/unresolved-wifi-secret` | A WiFi personal passphrase is not recoverable from the console; `spec.securityConfiguration` carries a placeholder Secret reference |
| `unifi.supporterino.de/unresolved-radius-profile` | An enterprise broadcast references a RADIUS profile UUID that cannot be represented; a placeholder `UnifiRadiusProfile` reference is emitted |
| `unifi.supporterino.de/unrepresented-wifi-settings` | An upstream broadcast carries settings the reduced CLI projection does not model |
| `unifi.supporterino.de/unresolved-device-tag` | A switch-managed network's upstream device binding is not uniquely resolvable to a device tag |
| `unifi.supporterino.de/unresolved-network-ref` | A firewall zone referenced an upstream network that was not emitted |
| `unifi.supporterino.de/unrepresented-ipv6` | Part of a gateway network's IPv6 configuration is not representable |
| `unifi.supporterino.de/unresolved-policy-ref` | A firewall-policy ordering referenced an upstream policy that was not emitted |
| `unifi.supporterino.de/unresolved-acl-rule-ref` | An ACL-rule ordering referenced an upstream rule that was not emitted |

## Emitted CRs follow the golden rule

Like every other producer in the workspace, the CLI puts **no upstream `_id` in `spec`**:
cross-resource references use Kubernetes identity (the emitted resource's `metadata.name`), and
upstream UUIDs, where recorded at all, live in `status`. See [CRD conventions](crd-conventions.md)
Rule 6.

## Related

- [UniFi API contract](unifi-api.md) — the endpoints each kind consumes.
- [CRD conventions](crd-conventions.md) — the rules emitted CRs obey.
- [Debugging](debugging.md) — diagnosing reconcile and drift problems.
