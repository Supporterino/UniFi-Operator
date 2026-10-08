package snapshot

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

var dns1123Subdomain = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

func boolPtr(b bool) *bool { return &b }

func TestDNSSafeName(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"Default":    "default",
		"Guest WiFi": "guest-wifi",
		"LAN 2":      "lan-2",
		"corp_net":   "corp-net",
		"":           "network",
	}
	for in, want := range tests {
		if got := DNSSafeName(in); got != want {
			t.Errorf("DNSSafeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNetworksProjectsSiteAndDropsUpstreamIDs(t *testing.T) {
	t.Parallel()

	resources, warnings := Networks("default", []unifi.Network{{
		ID:      "66a1b2c3d4e5f6a7b8c9d0e1",
		SiteID:  "5f3a2b1c9d8e7f6a5b4c3d2e",
		Name:    "Default",
		Subnet:  "192.168.1.0/24",
		VLAN:    20,
		Enabled: boolPtr(true),
	}})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	spec, ok := resources[0].Spec.(UnifiNetworkSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiNetworkSpec", resources[0].Spec)
	}
	if spec.Site != "default" || spec.Name != "Default" {
		t.Errorf("spec = %+v, want site=default name=Default", spec)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{
		"66a1b2c3d4e5f6a7b8c9d0e1", // upstream _id
		"5f3a2b1c9d8e7f6a5b4c3d2e", // upstream site_id
		`"_id"`,
		`"site_id"`,
		`"siteId"`,
	} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestNetworksDisambiguatesCollidingNames(t *testing.T) {
	t.Parallel()

	// "Guest WiFi" and "guest-wifi" sanitize to the same base name; the second must not
	// silently overwrite the first.
	networks := []unifi.Network{
		{ID: "id-1", Name: "Guest WiFi", Subnet: "192.168.2.0/24", VLAN: 20, Enabled: boolPtr(true)},
		{ID: "id-2", Name: "guest-wifi", Subnet: "192.168.3.0/24", VLAN: 30, Enabled: boolPtr(true)},
		{ID: "id-3", Name: "Guest WiFi", Subnet: "192.168.4.0/24", VLAN: 40, Enabled: boolPtr(true)},
	}

	resources, warnings := Networks("default", networks)
	if len(resources) != 3 {
		t.Fatalf("got %d resources, want 3", len(resources))
	}
	if len(warnings) != 2 {
		t.Fatalf("got %d warnings, want 2: %v", len(warnings), warnings)
	}

	wantNames := []string{"guest-wifi", "guest-wifi-2", "guest-wifi-3"}
	for i, want := range wantNames {
		if got := resources[i].Metadata.Name; got != want {
			t.Errorf("resource %d name = %q, want %q", i, got, want)
		}
		if !dns1123Subdomain.MatchString(resources[i].Metadata.Name) {
			t.Errorf("resource %d name %q is not a valid DNS-1123 subdomain", i, resources[i].Metadata.Name)
		}
	}

	// The first keeps its natural name and is not annotated; collisions are annotated
	// with the original upstream name so nothing is lost.
	if ann := resources[0].Metadata.Annotations; ann != nil {
		t.Errorf("resource 0 annotations = %v, want none", ann)
	}
	for i := 1; i < len(resources); i++ {
		if got := resources[i].Metadata.Annotations[AnnotationOriginalName]; got != networks[i].Name {
			t.Errorf("resource %d annotation %s = %q, want %q", i, AnnotationOriginalName, got, networks[i].Name)
		}
	}

	// Each spec preserves its own upstream name and carries no opaque identifier.
	for i, r := range resources {
		spec, ok := r.Spec.(UnifiNetworkSpec)
		if !ok {
			t.Fatalf("resource %d spec type = %T, want UnifiNetworkSpec", i, r.Spec)
		}
		if spec.Name != networks[i].Name {
			t.Errorf("resource %d spec.name = %q, want %q", i, spec.Name, networks[i].Name)
		}
		raw, err := json.Marshal(spec)
		if err != nil {
			t.Fatalf("marshal spec: %v", err)
		}
		if strings.Contains(string(raw), networks[i].ID) {
			t.Errorf("resource %d spec contains upstream _id %q", i, networks[i].ID)
		}
	}

	// Disambiguation is deterministic across runs.
	again, _ := Networks("default", networks)
	for i := range again {
		if again[i].Metadata.Name != resources[i].Metadata.Name {
			t.Errorf("run 2 resource %d name = %q, want %q", i, again[i].Metadata.Name, resources[i].Metadata.Name)
		}
	}
}

func TestNetworksOmitsAbsentEnabled(t *testing.T) {
	t.Parallel()

	// The controller omitted "enabled"; the projection must not force it to false, so the
	// CRD default (true) can apply.
	resources, warnings := Networks("default", []unifi.Network{{
		ID:   "id-1",
		Name: "Default",
	}})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}

	spec, ok := resources[0].Spec.(UnifiNetworkSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiNetworkSpec", resources[0].Spec)
	}
	if spec.Enabled != nil {
		t.Errorf("spec.Enabled = %v, want nil (upstream omitted it)", *spec.Enabled)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "enabled") {
		t.Errorf("spec should omit enabled when upstream omits it: %s", raw)
	}
	if !strings.Contains(string(raw), `"site":"default"`) || !strings.Contains(string(raw), `"name":"Default"`) {
		t.Errorf("spec must still carry site and name: %s", raw)
	}
}
