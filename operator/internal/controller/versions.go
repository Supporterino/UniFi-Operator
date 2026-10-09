/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Capability identifies a version-gated UniFi Integration v1 capability. The
// pinned minimum application version for each capability comes from
// docs/unifi-api.md; a console below a capability's minimum must not attempt it.
type Capability string

// Version-gated capabilities. Their minimums are pinned in capabilityMinimums.
const (
	// CapabilityOfficialAPI is the overall Integration v1 surface (for example
	// GET /v1/info and GET /v1/sites).
	CapabilityOfficialAPI Capability = "OfficialAPI"
	// CapabilityNetworks is the site network CRUD surface.
	CapabilityNetworks Capability = "Networks"
	// CapabilityFirewallDNS is the firewall and DNS policy surface.
	CapabilityFirewallDNS Capability = "FirewallDNS"
	// CapabilityWifi is the WiFi broadcast CRUD surface. It is pinned to the
	// Official API minimum because broadcast CRUD is part of the overall
	// Integration v1 surface.
	CapabilityWifi Capability = "WiFi"
)

// capabilityMinimums pins the minimum UniFi Network application version per
// capability (docs/unifi-api.md): Official API >= 10.1.78, networks CRUD
// >= 10.0.162, firewall/DNS >= 10.1.84, WiFi broadcasts >= 10.1.78.
var capabilityMinimums = map[Capability]Version{
	CapabilityOfficialAPI: {Major: 10, Minor: 1, Patch: 78},
	CapabilityNetworks:    {Major: 10, Minor: 0, Patch: 162},
	CapabilityFirewallDNS: {Major: 10, Minor: 1, Patch: 84},
	CapabilityWifi:        {Major: 10, Minor: 1, Patch: 78},
}

// Version is a parsed dotted UniFi Network application version.
type Version struct {
	Major int
	Minor int
	Patch int
}

// ParseVersion parses a dotted numeric application version such as "10.4.57".
// A leading "v" and a trailing build or pre-release suffix (for example
// "10.4.57-rc.1") are tolerated; only the leading numeric components are
// compared. It returns an error for a value with no numeric version prefix.
func ParseVersion(s string) (Version, error) {
	trimmed := strings.TrimPrefix(strings.TrimSpace(s), "v")

	end := strings.IndexFunc(trimmed, func(r rune) bool {
		return r != '.' && !unicode.IsDigit(r)
	})
	numeric := trimmed
	if end >= 0 {
		numeric = trimmed[:end]
	}
	if numeric == "" || numeric == "." {
		return Version{}, fmt.Errorf("version %q has no numeric prefix", s)
	}

	parts := strings.Split(numeric, ".")
	if len(parts) > 3 {
		return Version{}, fmt.Errorf("version %q has too many components", s)
	}
	numbers := make([]int, 3)
	for i, part := range parts {
		if part == "" {
			return Version{}, fmt.Errorf("version %q has an empty component", s)
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return Version{}, fmt.Errorf("version %q is not numeric", s)
		}
		numbers[i] = n
	}
	return Version{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, nil
}

// Compare returns -1, 0, or 1 as v is less than, equal to, or greater than
// other.
func (v Version) Compare(other Version) int {
	switch {
	case v.Major != other.Major:
		return sign(v.Major - other.Major)
	case v.Minor != other.Minor:
		return sign(v.Minor - other.Minor)
	default:
		return sign(v.Patch - other.Patch)
	}
}

// String formats the version as "major.minor.patch".
func (v Version) String() string {
	return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch)
}

// CheckCapability returns nil when the detected application version meets the
// capability's pinned minimum. It returns an error when the version cannot be
// parsed or is below the minimum, so callers fail closed with the
// VersionUnsupported reason and do not attempt the capability. The error text
// contains no credentials.
func CheckCapability(detected string, capability Capability) error {
	minimum, ok := capabilityMinimums[capability]
	if !ok {
		return fmt.Errorf("unknown capability %q", capability)
	}
	version, err := ParseVersion(detected)
	if err != nil {
		return fmt.Errorf("cannot determine %s support: %w", capability, err)
	}
	if version.Compare(minimum) < 0 {
		return fmt.Errorf("%s requires UniFi Network %s or newer, detected %s", capability, minimum, version)
	}
	return nil
}

// sign normalizes an integer to -1, 0, or 1.
func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
