## MODIFIED Requirements

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

## REMOVED Requirements

### Requirement: Device tag is declarative

**Reason**: The Integration v1 v10.4.57 surface exposes only `GET /v1/sites/{siteId}/device-tags`;
there is no create, update, or delete endpoint, so a tag cannot be authored upstream.

**Migration**: Use a name-based device-tag selector on the consuming resource; the operator reads
existing tags through the list endpoint and never creates one.

### Requirement: Device tags are site-scoped

**Reason**: With no `UnifiDeviceTag` Custom Resource there is nothing to scope to a site; selectors
are resolved against the site of the consuming resource.

**Migration**: Set the device-tag selector on the consumer that already references its `UnifiSite`.

### Requirement: Device tag status and cleanup are observable

**Reason**: With no Custom Resource and no upstream writes there is no status, no finalizer, and no
upstream state to clean up.

**Migration**: None required; readiness is reported on the consuming resource.
