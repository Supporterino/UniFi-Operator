# Spec Delta

## ADDED Requirements

### Requirement: `all` emits every projectable kind
`snapshot all` SHALL emit one resource for every kind the snapshot command can project,
including firewall-policy ordering and ACL-rule ordering, across every site on the console,
so a single invocation is complete. It MUST NOT omit a projectable kind, and it MUST NOT remain
limited to the four kinds and single site it previously covered.

#### Scenario: Every kind is present in `all`
- **WHEN** `snapshot all` runs against a console that holds objects of every projectable kind
- **THEN** the output contains resources of every projectable kind, including
  `UnifiFirewallPolicyOrdering` and `UnifiAclRuleOrdering`, and no projectable kind is absent

#### Scenario: `all` spans every site
- **WHEN** the console has more than one site carrying site-scoped objects
- **THEN** `all` emits the site-scoped resources of every site, not only the default site

### Requirement: Firewall-policy ordering is emitted
`snapshot` SHALL project the upstream firewall-policy ordering for each source/destination zone
pair into a `UnifiFirewallPolicyOrdering`, referencing emitted `UnifiFirewallPolicy` resources by
Kubernetes identity and expressing the system-defined partition as the anchored boundary. No
upstream zone or policy UUID SHALL appear in `spec`.

#### Scenario: Ordering references policies by name
- **WHEN** a zone pair has an upstream policy ordering over user-defined policies
- **THEN** the emitted `UnifiFirewallPolicyOrdering.spec.beforeSystemDefined` and `afterSystemDefined`
  name emitted `UnifiFirewallPolicy` resources, and no upstream policy or zone UUID appears in `spec`

#### Scenario: System-defined policies are anchored, not listed
- **WHEN** the upstream ordering includes system-defined policies in its partition
- **THEN** those policies are not emitted into `spec.beforeSystemDefined` or `spec.afterSystemDefined`;
  the system boundary is represented by the anchored partition instead

### Requirement: ACL-rule ordering is emitted
`snapshot` SHALL project the upstream site-global ACL-rule order into a `UnifiAclRuleOrdering`,
referencing emitted `UnifiAclRule` resources by Kubernetes identity in upstream order. No upstream
rule UUID SHALL appear in `spec`.

#### Scenario: ACL order references rules by name
- **WHEN** a site has an upstream ACL-rule order over user-defined rules
- **THEN** the emitted `UnifiAclRuleOrdering.spec.orderedAclRules` names emitted `UnifiAclRule`
  resources in upstream order, and no upstream rule UUID appears in `spec`

### Requirement: Snapshot completeness can be verified
`snapshot verify` SHALL re-read the live console and report, writing no resource, (a) coverage —
every projectable kind present upstream has a projection and no representable object was skipped —
and (b) fidelity — any field drift between an emitted resource and a fresh upstream read. It SHALL
exit non-zero when an explained-by-nothing gap is found and MUST NOT create, update, or delete any
upstream object.

#### Scenario: A skipped object is reported
- **WHEN** an upstream object that should be representable is absent from the emitted set
- **THEN** `verify` reports the object and exits with a non-zero status

#### Scenario: Known placeholders are not drift
- **WHEN** an emitted resource carries an annotated placeholder such as an unreadable passphrase Secret
- **THEN** `verify` classifies it as a known, expected gap and does not report it as fidelity drift

#### Scenario: Verify writes nothing
- **WHEN** `snapshot verify` runs against a console
- **THEN** it writes no file and performs no upstream mutation
