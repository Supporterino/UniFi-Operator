package unifi

import (
	"context"
	"errors"
	"testing"
)

func TestFixtureClient(t *testing.T) {
	t.Parallel()

	client := NewFixtureClient(Network{ID: testNetworkID, Name: testNetworkName, Enabled: true})

	networks, err := client.ListNetworks(context.Background(), testSite)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if len(networks) != 1 || networks[0].Name != testNetworkName {
		t.Fatalf("unexpected networks: %+v", networks)
	}

	// Mutating the returned slice must not affect the fixture.
	networks[0].Name = "mutated"
	again, err := client.ListNetworks(context.Background(), testSite)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}
	if again[0].Name != testNetworkName {
		t.Errorf("fixture was mutated: %q", again[0].Name)
	}

	got, err := client.GetNetwork(context.Background(), testSite, testNetworkID)
	if err != nil {
		t.Fatalf("GetNetwork: %v", err)
	}
	if got.Name != testNetworkName {
		t.Errorf("GetNetwork name = %q, want %q", got.Name, testNetworkName)
	}

	if _, err := client.GetNetwork(context.Background(), testSite, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetNetwork missing error = %v, want ErrNotFound", err)
	}
}
