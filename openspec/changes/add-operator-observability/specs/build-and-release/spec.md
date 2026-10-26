# Spec Delta

## ADDED Requirements

### Requirement: RBAC sources stay in sync

The operator's generated manager RBAC rules and the Helm chart's manager role SHALL grant the same
permissions, and a gate SHALL fail and name the diverging rules when the generated role gains or
loses a rule the chart does not reflect. The chart's role is a hand-maintained copy, so this
requirement exists to make a rule change that misses one of the two paths visible before release.

#### Scenario: Divergent rules fail the gate

- **WHEN** a marker on the operator adds a permission to the generated role without the chart role
  being updated
- **THEN** the synchronization gate fails and reports the missing rule

#### Scenario: The gate runs with the other repository checks

- **WHEN** a pull request is opened
- **THEN** the RBAC synchronization check runs alongside the manifest, lint, test, CRD-sync, and
  documentation gates

#### Scenario: A synchronized pair passes

- **WHEN** the generated role and the chart role grant the same rules
- **THEN** the gate passes without reporting a divergence
