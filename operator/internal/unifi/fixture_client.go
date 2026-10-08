package unifi

import (
	"context"
	"sync"
)

// FixtureClient is an in-memory Client that returns canned data. It is the
// default client for the scaffold's manager wiring and the client used by
// reconciler tests, so no test contacts a live controller. It performs no
// network I/O.
type FixtureClient struct {
	mu       sync.RWMutex
	networks []Network
}

// NewFixtureClient returns a FixtureClient seeded with the given networks. The
// slice is copied so callers cannot mutate the fixture's state.
func NewFixtureClient(networks ...Network) *FixtureClient {
	c := &FixtureClient{}
	c.SetNetworks(networks)
	return c
}

// SetNetworks replaces the fixture's networks.
func (c *FixtureClient) SetNetworks(networks []Network) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.networks = append([]Network(nil), networks...)
}

// ListNetworks implements Client. The in-memory fixture serves a single flat
// set of networks and ignores the site argument; it exists for tests and the
// scaffold's manager wiring, not to model multi-site semantics.
func (c *FixtureClient) ListNetworks(_ context.Context, _ string) ([]Network, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]Network(nil), c.networks...), nil
}

// GetNetwork implements Client. Like ListNetworks, the fixture ignores the
// site argument.
func (c *FixtureClient) GetNetwork(_ context.Context, _, id string) (Network, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, network := range c.networks {
		if network.ID == id {
			return network, nil
		}
	}
	return Network{}, ErrNotFound
}
