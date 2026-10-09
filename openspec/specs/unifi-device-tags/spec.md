# unifi-device-tags Specification

## Purpose

Defines the shared device-tag selector idiom: resources select devices by a name-based device-tag
selector that the operator resolves through the read-only device-tags list, without a device-tag
Custom Resource and without writing upstream.

## Requirements

### Requirement: Device tags are usable as selectors

Other Custom Resources SHALL select devices through a typed, name-based device-tag selector and
MUST NOT reference a device-tag Custom Resource or expose an opaque device-tag identifier. The
controller MUST resolve the selector through the read-only device-tags list
(`GET /v1/sites/{siteId}/device-tags`) and report the outcome in the consumer's `status`.

#### Scenario: Tag referenced by a selector
- **WHEN** a network, WiFi, ACL, or switching resource selects devices by a device-tag name
- **THEN** the operator resolves the tag to its member devices through the read-only device-tags list

#### Scenario: Unknown tag fails closed
- **WHEN** a selector names a device tag that does not exist on the site
- **THEN** the consumer reports `Ready=False` and does not mutate upstream state
