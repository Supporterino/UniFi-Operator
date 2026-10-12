# Spec Delta

## ADDED Requirements

### Requirement: Unconfigured firewall subsystem yields an empty zone set
When the console reports that Zone-Based Firewall is not configured, the UniFi client SHALL return
an empty zone set instead of an error, so firewall reconciliation reports no zones rather than
failing.

#### Scenario: Unconfigured subsystem maps to empty
- **WHEN** the firewall zones endpoint returns the `zone-based-firewall-not-configured` error
- **THEN** the client returns an empty zone list and no error

#### Scenario: Reconciliation does not fail on the unconfigured subsystem
- **WHEN** a `UnifiFirewallZone` reconciles against a site whose console has Zone-Based Firewall off
- **THEN** the controller resolves the zone set as empty and does not report a failing condition
  solely for the unconfigured subsystem
