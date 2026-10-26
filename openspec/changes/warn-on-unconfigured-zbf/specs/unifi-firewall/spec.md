# Spec Delta

## MODIFIED Requirements

### Requirement: Unconfigured firewall subsystem yields an empty zone set
When the console reports that Zone-Based Firewall is not configured, the UniFi client SHALL return
an empty zone set instead of an error for the zone read path, so firewall reconciliation reports no
zones rather than failing. The empty mapping SHALL NOT be treated as authorisation to create or
update upstream state.

#### Scenario: Unconfigured subsystem maps to empty
- **WHEN** the firewall zones endpoint returns the `zone-based-firewall-not-configured` error
- **THEN** the client returns an empty zone list and no error

#### Scenario: Reconciliation does not fail on the unconfigured subsystem
- **WHEN** a `UnifiFirewallZone` reconciles against a site whose console has Zone-Based Firewall off
- **THEN** the controller resolves the zone set as empty and does not report a failing condition
  solely for the unconfigured subsystem

#### Scenario: Write rejection is not mistaken for an empty read
- **WHEN** the console rejects a firewall-zone create or update with the unconfigured-subsystem
  error
- **THEN** the controller fails closed, mutates nothing upstream, and reports the distinct
  `ZoneBasedFirewallNotConfigured` reason

## ADDED Requirements

### Requirement: Unconfigured firewall subsystem is a distinct terminal condition
When a firewall reconcile cannot proceed because the console reports Zone-Based Firewall as
unconfigured, the controller SHALL set `Ready=False` with reason `ZoneBasedFirewallNotConfigured`
and a message identifying the missing subsystem, instead of `InvalidSpec`. It MUST NOT mutate
upstream and MUST NOT retry the condition as transient.

#### Scenario: UnifiFirewallPolicy on a ZBF-off console
- **WHEN** a `UnifiFirewallPolicy` reconciles against a site whose console has Zone-Based Firewall
  off
- **THEN** it reports `Ready=False` with reason `ZoneBasedFirewallNotConfigured` and no policy is
  created, updated, or deleted

#### Scenario: UnifiFirewallPolicyOrdering on a ZBF-off console
- **WHEN** a `UnifiFirewallPolicyOrdering` reconciles against such a site
- **THEN** it reports `Ready=False` with reason `ZoneBasedFirewallNotConfigured` and no order is
  applied

#### Scenario: Policy failure is not reported as a bad spec
- **WHEN** the console rejects a firewall policy read or write with the unconfigured-subsystem
  error
- **THEN** the condition reason is `ZoneBasedFirewallNotConfigured`, not `InvalidSpec`
