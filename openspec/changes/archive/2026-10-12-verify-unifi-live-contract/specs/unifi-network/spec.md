# Spec Delta

## ADDED Requirements

### Requirement: IPv4 DHCP configuration is declarative
For `management: GATEWAY` and `management: SWITCH`, `UnifiNetwork.spec.ipv4Configuration` SHALL
expose the upstream DHCP configuration: the addressing mode, the address range, the DNS-server
override list, the lease time, the domain name, and the ping-conflict-detection flag. The
controller SHALL send this configuration on create and update, so adoption does not reset or
diverge from the console's DHCP settings.

#### Scenario: DHCP settings round-trip
- **WHEN** a network is snapshotted from a console whose `ipv4Configuration` sets a DHCP address
  range, DNS-server overrides, and a domain name
- **THEN** the emitted `UnifiNetwork.spec.ipv4Configuration` carries those DHCP settings and a
  re-applied object keeps them upstream

#### Scenario: DHCP configuration is optional
- **WHEN** a `UnifiNetwork` omits the DHCP configuration
- **THEN** the API server accepts the object and the controller sets or changes no DHCP settings
  upstream

### Requirement: DHCP guarding entries are validated
`UnifiNetwork.spec.dhcpGuarding.trustedDhcpServerIpAddresses` SHALL contain only non-empty,
well-formed IP addresses. The API server MUST reject an entry that is empty or not a valid
address, so a console value such as `["192.168.2.1","",""]` cannot be round-tripped onto a Custom
Resource.

#### Scenario: Empty entry rejected
- **WHEN** a `UnifiNetwork` sets `trustedDhcpServerIpAddresses` with an empty-string entry
- **THEN** the API server rejects the object

#### Scenario: Valid entry accepted
- **WHEN** every `trustedDhcpServerIpAddresses` entry is a non-empty IP address
- **THEN** the API server accepts the object and the controller applies the guard upstream
