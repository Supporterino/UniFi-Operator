// Package snapshot projects UniFi controller objects into Custom Resources that a user
// can `kubectl apply` and have the operator adopt.
package snapshot

import (
	"fmt"
	"strings"

	"github.com/Supporterino/UniFi-Operator/cli/internal/emit"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

const (
	// Group is the API group of the emitted Custom Resources.
	Group = "unifi.supporterino.de"
	// Version is the API version of the emitted Custom Resources.
	Version = "v1alpha1"
	// KindUnifiNetwork is the kind for a projected UniFi network.
	KindUnifiNetwork = "UnifiNetwork"
	// AnnotationOriginalName records the upstream network name on a resource whose
	// metadata.name was disambiguated to avoid a collision.
	AnnotationOriginalName = "unifi.supporterino.de/original-name"
)

// UnifiNetworkSpec is the desired state emitted for a UniFi network. The field names and
// types mirror the operator's UnifiNetworkSpec (api/v1alpha1) so emitted resources
// validate against the CRD. It intentionally carries no upstream _id or site_id: per
// docs/crd-conventions.md, opaque identifiers belong in status, which the operator owns.
type UnifiNetworkSpec struct {
	Site    string `json:"site"`
	Name    string `json:"name"`
	VLAN    *int32 `json:"vlan,omitempty"`
	Subnet  string `json:"subnet,omitempty"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// Networks projects each UniFi network into a UnifiNetwork Custom Resource. The site
// argument is the human-configured site name (the snapshot --site value), never an
// upstream site_id.
//
// Two upstream networks can sanitize to the same metadata.name. Rather than silently
// overwriting one (or dropping it), the later resource gets a stable numeric suffix and
// an AnnotationOriginalName annotation, and a human-readable warning is returned. The
// suffix is assigned in input order, so the result is deterministic. Returned warnings
// are empty when there are no collisions.
func Networks(site string, networks []unifi.Network) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(networks))
	var warnings []string
	used := make(map[string]bool, len(networks))
	nextSuffix := make(map[string]int, len(networks))

	for _, n := range networks {
		base := DNSSafeName(n.Name)
		name := base
		if used[name] {
			if nextSuffix[base] == 0 {
				nextSuffix[base] = 1
			}
			for {
				nextSuffix[base]++
				name = fmt.Sprintf("%s-%d", base, nextSuffix[base])
				if !used[name] {
					break
				}
			}
			warnings = append(warnings, fmt.Sprintf(
				"network %q: projected name %q is already in use; emitting as %q",
				n.Name, base, name))
		} else {
			nextSuffix[base] = 1
		}
		used[name] = true

		// Emit enabled only when the controller supplied it, so an omitted field falls
		// through to the CRD default (true) instead of being forced to false.
		spec := UnifiNetworkSpec{
			Site:    site,
			Name:    n.Name,
			Subnet:  n.Subnet,
			Enabled: n.Enabled,
		}
		if n.VLAN >= 1 && n.VLAN <= 4094 {
			vlan := int32(n.VLAN)
			spec.VLAN = &vlan
		}

		metadata := emit.Metadata{Name: name}
		if name != base {
			metadata.Annotations = map[string]string{AnnotationOriginalName: n.Name}
		}
		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiNetwork,
			Metadata:   metadata,
			Spec:       spec,
		})
	}
	return resources, warnings
}

// DNSSafeName converts a UniFi object name into a DNS-1123 subdomain-safe resource name.
// Non-alphanumeric runs collapse to a single dash; an empty result falls back to
// "network".
func DNSSafeName(name string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case b.Len() > 0 && !lastDash:
			b.WriteByte('-')
			lastDash = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "network"
	}
	return out
}
