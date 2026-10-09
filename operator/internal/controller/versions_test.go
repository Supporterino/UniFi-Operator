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

import "testing"

func TestParseVersion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    Version
		wantErr bool
	}{
		{name: "three components", input: testAppVersion, want: Version{Major: 10, Minor: 4, Patch: 57}},
		{name: "leading v", input: "v10.1.78", want: Version{Major: 10, Minor: 1, Patch: 78}},
		{name: "prerelease suffix", input: "10.0.162-rc.1", want: Version{Major: 10, Minor: 0, Patch: 162}},
		{name: "surrounding spaces", input: " 10.4.57 ", want: Version{Major: 10, Minor: 4, Patch: 57}},
		{name: "major only", input: "10", want: Version{Major: 10}},
		{name: "major and minor", input: "10.4", want: Version{Major: 10, Minor: 4}},
		{name: "empty", input: "", wantErr: true},
		{name: "bare v", input: "v", wantErr: true},
		{name: "non numeric", input: testUnparseableVersion, wantErr: true},
		{name: "empty component", input: "10..57", wantErr: true},
		{name: "too many components", input: "10.4.57.1", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseVersion(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseVersion(%q) error = nil, want error", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVersion(%q) error = %v, want nil", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseVersion(%q) = %+v, want %+v", tt.input, got, tt.want)
			}
		})
	}
}

func TestCheckCapability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		detected   string
		capability Capability
		wantErr    bool
	}{
		{name: "official api at minimum", detected: "10.1.78", capability: CapabilityOfficialAPI},
		{name: "official api above minimum", detected: testAppVersion, capability: CapabilityOfficialAPI},
		{name: "official api below minimum", detected: "10.1.77", capability: CapabilityOfficialAPI, wantErr: true},
		{name: "official api older minor", detected: "10.0.200", capability: CapabilityOfficialAPI, wantErr: true},
		{name: "networks at minimum", detected: "10.0.162", capability: CapabilityNetworks},
		{name: "networks below minimum", detected: "10.0.161", capability: CapabilityNetworks, wantErr: true},
		{name: "firewall at minimum", detected: "10.1.84", capability: CapabilityFirewallDNS},
		{name: "firewall below minimum", detected: "10.1.83", capability: CapabilityFirewallDNS, wantErr: true},
		{name: "unparseable version", detected: testUnparseableVersion, capability: CapabilityOfficialAPI, wantErr: true},
		{name: "unknown capability", detected: testAppVersion, capability: Capability("Bogus"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := CheckCapability(tt.detected, tt.capability)
			if tt.wantErr && err == nil {
				t.Fatalf("CheckCapability(%q, %q) error = nil, want error", tt.detected, tt.capability)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("CheckCapability(%q, %q) error = %v, want nil", tt.detected, tt.capability, err)
			}
		})
	}
}
