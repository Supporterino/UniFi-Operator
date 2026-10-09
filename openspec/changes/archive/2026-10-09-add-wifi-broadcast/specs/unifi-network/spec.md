## MODIFIED Requirements

### Requirement: Deletion cleans up upstream state

`UnifiNetwork` SHALL use a finalizer to remove its upstream network before the object is
finalized, adding and removing the finalizer symmetrically. Before removing its finalizer, a
`UnifiNetwork` SHALL drain the child resources it owns — the `UnifiWifiBroadcast` objects whose
strongest reference is this network — so their own finalizers can still resolve the network and,
through it, the site's controller to delete their upstream state. The network MUST NOT remove its
finalizer while an owned broadcast remains.

#### Scenario: Finalizer removes upstream network
- **WHEN** a `UnifiNetwork` with the finalizer set is deleted
- **THEN** the operator deletes the upstream network and then removes the finalizer

#### Scenario: Owned broadcasts drain first
- **WHEN** a `UnifiNetwork` that owns `UnifiWifiBroadcast` children is deleted
- **THEN** the operator initiates deletion of those broadcasts and removes its own finalizer only
  after they are gone

#### Scenario: Site deletion drains recursively
- **WHEN** a `UnifiSite` is deleted and its owned network has owned broadcasts
- **THEN** the site's drain deletes the network, the network drains its broadcasts, and each
  finalizer is removed in turn until the site finalizer can be removed
