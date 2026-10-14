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

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "opaque-network-id",
		Management:            ManagementGateway,
		Name:                  "Default",
		Enabled:               boolPtr(true),
		VLANID:                10,
		ZoneID:                "opaque-zone-id",
		CellularBackupEnabled: boolPtr(false),
		InternetAccessEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(false),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: true, HostIPAddress: "10.0.0.1", PrefixLength: 24},
	}}, nil)
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
	if spec.SiteRef.Name != "default" || spec.Name != "Default" || spec.Management != ManagementGateway {
		t.Errorf("spec = %+v, want siteRef.name=default name=Default management=GATEWAY", spec)
	}
	if spec.Gateway == nil {
		t.Fatal("spec.gateway = nil, want gateway variant")
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{
		"opaque-network-id",
		"opaque-zone-id",
		`"_id"`,
		`"id"`,
		`"zoneId"`,
		`"deviceId"`,
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
		{ID: "id-1", Management: ManagementUnmanaged, Name: "Guest WiFi", VLANID: 20, Enabled: boolPtr(true)},
		{ID: "id-2", Management: ManagementUnmanaged, Name: "guest-wifi", VLANID: 30, Enabled: boolPtr(true)},
		{ID: "id-3", Management: ManagementUnmanaged, Name: "Guest WiFi", VLANID: 40, Enabled: boolPtr(true)},
	}

	resources, _, warnings := Networks("default", "", networks, nil)
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

	for i, r := range resources {
		spec, ok := r.Spec.(UnifiNetworkSpec)
		if !ok {
			t.Fatalf("resource %d spec type = %T, want UnifiNetworkSpec", i, r.Spec)
		}
		if spec.Name != networks[i].Name {
			t.Errorf("resource %d spec.name = %q, want %q", i, spec.Name, networks[i].Name)
		}
		if spec.SiteRef.Name != "default" {
			t.Errorf("resource %d siteRef.name = %q, want default", i, spec.SiteRef.Name)
		}
	}

	// Disambiguation is deterministic across runs.
	again, _, _ := Networks("default", "", networks, nil)
	for i := range again {
		if again[i].Metadata.Name != resources[i].Metadata.Name {
			t.Errorf("run 2 resource %d name = %q, want %q", i, again[i].Metadata.Name, resources[i].Metadata.Name)
		}
	}
}

func TestNetworksGatewayMapping(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "id-1",
		Management:            ManagementGateway,
		Name:                  "Guest WiFi",
		Enabled:               boolPtr(false),
		VLANID:                20,
		ZoneID:                "zone-opaque",
		CellularBackupEnabled: boolPtr(false),
		InternetAccessEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(true),
		MDNSForwardingEnabled: boolPtr(true),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: true, HostIPAddress: "192.168.20.1", PrefixLength: 24},
		IPv6Configuration: &unifi.IPv6Configuration{
			InterfaceType:           "STATIC",
			ClientAddressAssignment: unifi.IPv6ClientAddressAssignment{SLAACEnabled: true},
			HostIPAddress:           "fd00:20::1",
			PrefixLength:            64,
		},
	}}, nil)
	if len(resources) != 1 || len(warnings) != 0 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 0", len(resources), len(warnings))
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	gw := spec.Gateway
	if gw == nil {
		t.Fatal("gateway = nil")
	}
	if gw.CellularBackupEnabled || !gw.InternetAccessEnabled || !gw.IsolationEnabled {
		t.Errorf("gateway booleans = %+v", gw)
	}
	if gw.MDNSForwardingEnabled == nil || !*gw.MDNSForwardingEnabled {
		t.Errorf("mdnsForwardingEnabled = %v, want true", gw.MDNSForwardingEnabled)
	}
	if gw.IPv4Configuration.HostIPAddress != "192.168.20.1" || gw.IPv4Configuration.PrefixLength != 24 {
		t.Errorf("ipv4Configuration = %+v", gw.IPv4Configuration)
	}
	if gw.IPv6Configuration == nil || gw.IPv6Configuration.InterfaceType != "STATIC" || gw.IPv6Configuration.PrefixLength != 64 {
		t.Errorf("ipv6Configuration = %+v", gw.IPv6Configuration)
	}
}

func TestNetworksGatewayCarriesDHCPConfiguration(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "id-1",
		Management:            ManagementGateway,
		Name:                  "LAN",
		VLANID:                10,
		CellularBackupEnabled: boolPtr(false),
		InternetAccessEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(false),
		IPv4Configuration: &unifi.IPv4Configuration{
			AutoScaleEnabled: true,
			HostIPAddress:    "192.168.10.1",
			PrefixLength:     24,
			DHCPConfiguration: &unifi.DHCPConfiguration{
				Mode:                         "SERVER",
				IPAddressRange:               &unifi.DHCPIPAddressRange{Start: "192.168.10.10", Stop: "192.168.10.200"},
				DNSServerIPAddressesOverride: []string{"192.168.10.1"},
				LeaseTimeSeconds:             86400,
				DomainName:                   "lan.example",
				PingConflictDetectionEnabled: true,
			},
		},
	}}, nil)
	if len(resources) != 1 || len(warnings) != 0 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 0: %v", len(resources), len(warnings), warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Gateway == nil {
		t.Fatal("gateway = nil")
	}
	dhcp := spec.Gateway.IPv4Configuration.DHCPConfiguration
	if dhcp == nil {
		t.Fatal("ipv4Configuration.dhcpConfiguration = nil, want the DHCP settings")
	}
	if dhcp.Mode != "SERVER" || dhcp.DomainName != "lan.example" ||
		dhcp.LeaseTimeSeconds != 86400 || !dhcp.PingConflictDetectionEnabled {
		t.Errorf("dhcp = %+v", dhcp)
	}
	if dhcp.IPAddressRange == nil || dhcp.IPAddressRange.Start != "192.168.10.10" || dhcp.IPAddressRange.Stop != "192.168.10.200" {
		t.Errorf("ipAddressRange = %+v", dhcp.IPAddressRange)
	}
	if len(dhcp.DNSServerIPAddressesOverride) != 1 || dhcp.DNSServerIPAddressesOverride[0] != "192.168.10.1" {
		t.Errorf("dnsServerIpAddressesOverride = %v", dhcp.DNSServerIPAddressesOverride)
	}
}

func TestNetworksSwitchCarriesDHCPConfiguration(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "id-1",
		Management:            ManagementSwitch,
		Name:                  "IoT",
		VLANID:                30,
		CellularBackupEnabled: boolPtr(false),
		IsolationEnabled:      boolPtr(false),
		IPv4Configuration: &unifi.IPv4Configuration{
			AutoScaleEnabled: false,
			HostIPAddress:    "192.168.30.1",
			PrefixLength:     24,
			DHCPConfiguration: &unifi.DHCPConfiguration{
				Mode: "SERVER",
			},
		},
	}}, nil)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %v", len(resources), warnings)
	}
	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Switch == nil || spec.Switch.IPv4Configuration.DHCPConfiguration == nil {
		t.Fatalf("switch ipv4Configuration.dhcpConfiguration = %+v, want the DHCP settings", spec.Switch)
	}
	if got := spec.Switch.IPv4Configuration.DHCPConfiguration.Mode; got != "SERVER" {
		t.Errorf("dhcp mode = %q, want SERVER", got)
	}
}

func TestNetworksFiltersInvalidTrustedDHCPServers(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		Management: ManagementUnmanaged,
		Name:       "Default",
		Enabled:    boolPtr(true),
		VLANID:     1,
		DHCPGuarding: &unifi.DHCPGuarding{TrustedDHCPServerIPAddresses: []string{
			"192.168.1.254",
			"",
			"",
			"not-an-ip",
			// IPv4-mapped IPv6 is rejected by the CEL rule ip(self).family() == 4.
			"::ffff:1.2.3.4",
			// Over the item schema's MaxLength=15.
			"192.168.100.1000",
		}},
	}}, nil)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %v", len(resources), warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.DHCPGuarding == nil || len(spec.DHCPGuarding.TrustedDHCPServerIPAddresses) != 1 {
		t.Fatalf("dhcpGuarding = %+v, want exactly one trusted address", spec.DHCPGuarding)
	}
	if got := spec.DHCPGuarding.TrustedDHCPServerIPAddresses[0]; got != "192.168.1.254" {
		t.Errorf("trusted[0] = %q, want 192.168.1.254", got)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), `""`) {
		t.Errorf("spec contains an empty entry: %s", raw)
	}
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "Default") && strings.Contains(warning, "trusted DHCP") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one naming the network and the dropped entries", warnings)
	}
}

func TestNetworksOmitsGuardingWhenAllEntriesInvalid(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		Management:   ManagementUnmanaged,
		Name:         "Default",
		Enabled:      boolPtr(true),
		VLANID:       1,
		DHCPGuarding: &unifi.DHCPGuarding{TrustedDHCPServerIPAddresses: []string{"", ""}},
	}}, nil)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %v", len(resources), warnings)
	}
	if spec := resources[0].Spec.(UnifiNetworkSpec); spec.DHCPGuarding != nil {
		t.Errorf("dhcpGuarding = %+v, want omitted when no entry is valid", spec.DHCPGuarding)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want one naming the dropped entries", warnings)
	}
}

func TestNetworksSkipsUnrepresentableDHCPConfiguration(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		Management:            ManagementGateway,
		Name:                  "LAN",
		VLANID:                10,
		CellularBackupEnabled: boolPtr(false),
		InternetAccessEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(false),
		IPv4Configuration: &unifi.IPv4Configuration{
			AutoScaleEnabled:  false,
			HostIPAddress:     "10.0.0.1",
			PrefixLength:      24,
			DHCPConfiguration: &unifi.DHCPConfiguration{Mode: ""},
		},
	}}, nil)
	if len(resources) != 0 {
		t.Fatalf("got %d resources, want 0 (an empty DHCP mode cannot be represented)", len(resources))
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want one skipped-network warning", warnings)
	}
}

func TestNetworksGatewayPrefixDelegationAnnotated(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		Management:            ManagementGateway,
		Name:                  "LAN",
		VLANID:                10,
		CellularBackupEnabled: boolPtr(false),
		InternetAccessEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(false),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: false, HostIPAddress: "10.0.0.1", PrefixLength: 24},
		IPv6Configuration: &unifi.IPv6Configuration{
			InterfaceType:                  "PREFIX_DELEGATION",
			ClientAddressAssignment:        unifi.IPv6ClientAddressAssignment{SLAACEnabled: true},
			PrefixDelegationWANInterfaceID: "opaque-wan-id",
		},
	}}, nil)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Gateway == nil || spec.Gateway.IPv6Configuration != nil {
		t.Errorf("gateway ipv6Configuration = %+v, want omitted", spec.Gateway)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnrepresentedIPv6]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnrepresentedIPv6)
	}
	if strings.Contains(warnings[0], "opaque-wan-id") {
		t.Errorf("warning leaks opaque WAN id: %s", warnings[0])
	}
}

func TestNetworksSwitchPlaceholder(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		Management:            ManagementSwitch,
		Name:                  "IoT",
		VLANID:                30,
		DeviceID:              "opaque-device-id",
		CellularBackupEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(true),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: false, HostIPAddress: "192.168.30.1", PrefixLength: 24},
	}}, nil)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Switch == nil {
		t.Fatal("switch variant = nil")
	}
	if spec.Switch.DeviceTag.Name != unresolvedDeviceTagName {
		t.Errorf("deviceTag.name = %q, want %q", spec.Switch.DeviceTag.Name, unresolvedDeviceTagName)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedDeviceTag)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "opaque-device-id") {
		t.Errorf("spec leaks device UUID: %s", raw)
	}
	if strings.Contains(string(resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]), "opaque-device-id") {
		t.Error("annotation leaks device UUID")
	}
}

func TestNetworksSwitchResolvesUniqueDeviceTag(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "net-iot",
		Management:            ManagementSwitch,
		Name:                  "IoT",
		VLANID:                30,
		DeviceID:              "device-1",
		CellularBackupEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(true),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: false, HostIPAddress: "192.168.30.1", PrefixLength: 24},
	}}, []unifi.DeviceTag{
		{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"device-1"}},
		{ID: "tag-2", Name: "aps", DeviceIDs: []string{"device-2"}},
	})
	if len(resources) != 1 || len(warnings) != 0 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 0: %v", len(resources), len(warnings), warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Switch.DeviceTag.Name != "iot-switch" {
		t.Errorf("deviceTag.name = %q, want iot-switch", spec.Switch.DeviceTag.Name)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]; ok {
		t.Errorf("annotations = %v, want no %s annotation", resources[0].Metadata.Annotations, AnnotationUnresolvedDeviceTag)
	}
}

func TestNetworksSwitchMultiDeviceTagFallsBackToPlaceholder(t *testing.T) {
	t.Parallel()

	// The device belongs to exactly one tag, but that tag has more than one member. The
	// operator requires a single-device tag, so the emitter must keep the annotated
	// placeholder rather than emit a selector the operator rejects with DeviceTagAmbiguous.
	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "net-iot",
		Management:            ManagementSwitch,
		Name:                  "IoT",
		VLANID:                30,
		DeviceID:              "device-1",
		CellularBackupEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(true),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: false, HostIPAddress: "192.168.30.1", PrefixLength: 24},
	}}, []unifi.DeviceTag{
		{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"device-1", "device-3"}},
		{ID: "tag-2", Name: "aps", DeviceIDs: []string{"device-2"}},
	})
	if len(resources) != 1 || len(warnings) != 1 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 1: %v", len(resources), len(warnings), warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Switch.DeviceTag.Name != unresolvedDeviceTagName {
		t.Errorf("deviceTag.name = %q, want %q", spec.Switch.DeviceTag.Name, unresolvedDeviceTagName)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedDeviceTag)
	}
}

func TestNetworksSwitchAmbiguousDeviceTagFallsBackToPlaceholder(t *testing.T) {
	t.Parallel()

	// The device is a member of two tags, so no single selector resolves it; the emitter
	// must keep the annotated placeholder rather than pick one arbitrarily.
	resources, _, warnings := Networks("default", "", []unifi.Network{{
		ID:                    "net-iot",
		Management:            ManagementSwitch,
		Name:                  "IoT",
		VLANID:                30,
		DeviceID:              "device-1",
		CellularBackupEnabled: boolPtr(true),
		IsolationEnabled:      boolPtr(true),
		IPv4Configuration:     &unifi.IPv4Configuration{AutoScaleEnabled: false, HostIPAddress: "192.168.30.1", PrefixLength: 24},
	}}, []unifi.DeviceTag{
		{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"device-1"}},
		{ID: "tag-2", Name: "lab", DeviceIDs: []string{"device-1"}},
	})
	if len(resources) != 1 || len(warnings) != 1 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 1: %v", len(resources), len(warnings), warnings)
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Switch.DeviceTag.Name != unresolvedDeviceTagName {
		t.Errorf("deviceTag.name = %q, want %q", spec.Switch.DeviceTag.Name, unresolvedDeviceTagName)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedDeviceTag)
	}
}

func TestFirewallZonesUsesEmittedNetworkNames(t *testing.T) {
	t.Parallel()

	// The emitted network names carry a collision suffix; the zone must reference those
	// names rather than re-deriving them from the upstream network names.
	networkNames := map[string]string{
		"upstream-1": "guest-wifi",
		"upstream-2": "guest-wifi-2",
	}
	zones := []unifi.FirewallZone{
		{
			ID:         "zone-1",
			Name:       "Custom Zone",
			NetworkIDs: []string{"upstream-1", "upstream-2"},
			Metadata:   unifi.NetworkMetadata{Origin: "USER_DEFINED"},
		},
		{
			ID:         "zone-2",
			Name:       "Internal",
			NetworkIDs: []string{"upstream-2"},
			Metadata:   unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"},
		},
	}

	resources, warnings := FirewallZones("default", "", networkNames, zones)
	if len(resources) != 2 || len(warnings) != 0 {
		t.Fatalf("got %d resources, %d warnings; want 2 and 0: %v", len(resources), len(warnings), warnings)
	}
	if resources[0].Kind != KindUnifiFirewallZone || resources[0].Metadata.Name != "custom-zone" {
		t.Errorf("resource 0 = %+v", resources[0])
	}

	spec := resources[0].Spec.(UnifiFirewallZoneSpec)
	if spec.SiteRef.Name != "default" || spec.Name != "Custom Zone" {
		t.Errorf("spec = %+v", spec)
	}
	wantRefs := []string{"guest-wifi", "guest-wifi-2"}
	if len(spec.NetworkRefs) != len(wantRefs) {
		t.Fatalf("networkRefs = %+v, want %v", spec.NetworkRefs, wantRefs)
	}
	for i, want := range wantRefs {
		if spec.NetworkRefs[i].Name != want {
			t.Errorf("networkRefs[%d] = %q, want %q", i, spec.NetworkRefs[i].Name, want)
		}
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"zone-1", "upstream-1", "upstream-2"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec leaks upstream identifier %q: %s", forbidden, raw)
		}
	}
}

func TestFirewallZonesAnnotatesUnresolvedNetworkRef(t *testing.T) {
	t.Parallel()

	resources, warnings := FirewallZones("default", "", map[string]string{"known": "known-net"}, []unifi.FirewallZone{
		{Name: "Mixed", NetworkIDs: []string{"known", "missing-id"}},
	})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", warnings)
	}

	spec := resources[0].Spec.(UnifiFirewallZoneSpec)
	if len(spec.NetworkRefs) != 1 || spec.NetworkRefs[0].Name != "known-net" {
		t.Errorf("networkRefs = %+v, want [known-net]", spec.NetworkRefs)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedNetworkRef]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedNetworkRef)
	}
	if strings.Contains(warnings[0], "missing-id") {
		t.Errorf("warning leaks upstream id: %s", warnings[0])
	}
}

func TestNetworksUnmanagedCommonOnly(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{{
		Management:   ManagementUnmanaged,
		Name:         "Default",
		Enabled:      nil,
		VLANID:       1,
		DHCPGuarding: &unifi.DHCPGuarding{TrustedDHCPServerIPAddresses: []string{"192.168.1.254"}},
	}}, nil)
	if len(resources) != 1 || len(warnings) != 0 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 0", len(resources), len(warnings))
	}

	spec := resources[0].Spec.(UnifiNetworkSpec)
	if spec.Gateway != nil || spec.Switch != nil {
		t.Errorf("unmanaged spec must carry no variant: %+v", spec)
	}
	if spec.Enabled != nil {
		t.Errorf("Enabled = %v, want nil (upstream omitted it)", *spec.Enabled)
	}
	if spec.DHCPGuarding == nil || len(spec.DHCPGuarding.TrustedDHCPServerIPAddresses) != 1 {
		t.Errorf("dhcpGuarding = %+v", spec.DHCPGuarding)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "enabled") {
		t.Errorf("spec should omit enabled when upstream omits it: %s", raw)
	}
}

func TestNetworksSkipsInvalidVLAN(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", "", []unifi.Network{
		{Management: ManagementUnmanaged, Name: "zero", VLANID: 0},
		{Management: ManagementUnmanaged, Name: "high", VLANID: 5000},
	}, nil)
	if len(resources) != 0 {
		t.Fatalf("got %d resources, want 0", len(resources))
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want 2", warnings)
	}
}

func TestSitesEmitsControllerRefAndNames(t *testing.T) {
	t.Parallel()

	sites := []unifi.Site{
		{ID: "opaque-site-id", InternalReference: "default", Name: "Default"},
		{ID: "opaque-site-id-2", InternalReference: "Branch Office", Name: "Branch Office"},
	}
	resources, names, warnings := Sites("unifi", sites)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(resources))
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if names["default"] != "default" || names["Branch Office"] != "branch-office" {
		t.Errorf("names = %v", names)
	}

	for i, r := range resources {
		if r.Kind != KindUnifiSite {
			t.Errorf("resource %d kind = %q, want %q", i, r.Kind, KindUnifiSite)
		}
		spec := r.Spec.(UnifiSiteSpec)
		if spec.ControllerRef.Name != "unifi" {
			t.Errorf("resource %d controllerRef.name = %q, want unifi", i, spec.ControllerRef.Name)
		}
		if spec.InternalReference != sites[i].InternalReference {
			t.Errorf("resource %d internalReference = %q, want %q", i, spec.InternalReference, sites[i].InternalReference)
		}
		if r.Metadata.Name != names[sites[i].InternalReference] {
			t.Errorf("resource %d name = %q, want %q", i, r.Metadata.Name, names[sites[i].InternalReference])
		}
		if !dns1123Subdomain.MatchString(r.Metadata.Name) {
			t.Errorf("resource %d name %q is not DNS-1123 safe", i, r.Metadata.Name)
		}

		raw, err := json.Marshal(spec)
		if err != nil {
			t.Fatalf("marshal spec: %v", err)
		}
		if strings.Contains(string(raw), sites[i].ID) {
			t.Errorf("site spec leaks upstream id: %s", raw)
		}
	}
}

func wifiStandardBroadcast() unifi.WifiBroadcast {
	return unifi.WifiBroadcast{
		ID:                                  "upstream-broadcast-id",
		Name:                                "Corp",
		Enabled:                             true,
		HideName:                            false,
		ClientIsolationEnabled:              false,
		MulticastToUnicastConversionEnabled: true,
		UapsdEnabled:                        false,
		Channel2gLockedTo6:                  false,
		DtimPeriod2gLockedTo3:               false,
		Type:                                WifiBroadcastTypeStandard,
		Metadata:                            unifi.NetworkMetadata{Origin: "USER_DEFINED"},
		Network:                             &unifi.WifiNetworkReference{Type: "SPECIFIC", NetworkID: "net-1"},
		SecurityConfiguration:               &unifi.WifiSecurityConfiguration{Type: WifiSecurityWPA2Personal, Passphrase: "hunter2", PmfMode: "OPTIONAL"},
		BroadcastingDeviceFilter:            &unifi.WifiBroadcastingDeviceFilter{Type: "DEVICE_TAGS", DeviceTagIDs: []string{"tag-1"}},
		AdvertiseDeviceName:                 boolPtr(false),
		ArpProxyEnabled:                     boolPtr(false),
		BssTransitionEnabled:                boolPtr(true),
		BroadcastingFrequenciesGHz:          []float64{2.4, 5},
	}
}

func TestWifiBroadcastsProjectsStandardAndDropsUpstreamIDs(t *testing.T) {
	t.Parallel()

	tags := []unifi.DeviceTag{{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"dev-1"}}}
	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", tags, []unifi.WifiBroadcast{wifiStandardBroadcast()})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %v", len(resources), warnings)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want the passphrase warning", warnings)
	}

	resource := resources[0]
	if resource.Kind != KindUnifiWifiBroadcast || resource.Metadata.Name != "corp" {
		t.Errorf("resource = %+v", resource)
	}
	if _, ok := resource.Metadata.Annotations[AnnotationUnresolvedWifiSecret]; !ok {
		t.Errorf("annotations = %v, want %s", resource.Metadata.Annotations, AnnotationUnresolvedWifiSecret)
	}
	if got := resource.Status.(WifiBroadcastStatus).WifiBroadcastID; got != "upstream-broadcast-id" {
		t.Errorf("status.wifiBroadcastID = %q, want the upstream id", got)
	}

	spec := resource.Spec.(UnifiWifiBroadcastSpec)
	if spec.NetworkRef.Name != "default" || spec.Type != WifiBroadcastTypeStandard || spec.Name != "Corp" {
		t.Errorf("spec = %+v", spec)
	}
	if spec.Standard == nil || spec.Standard.BssTransitionEnabled != true {
		t.Fatalf("standard = %+v", spec.Standard)
	}
	wantFrequencies := []string{"2.4", "5"}
	if len(spec.Standard.BroadcastingFrequenciesGHz) != len(wantFrequencies) {
		t.Fatalf("frequencies = %v, want %v", spec.Standard.BroadcastingFrequenciesGHz, wantFrequencies)
	}
	for i, want := range wantFrequencies {
		if spec.Standard.BroadcastingFrequenciesGHz[i] != want {
			t.Errorf("frequency %d = %q, want %q", i, spec.Standard.BroadcastingFrequenciesGHz[i], want)
		}
	}
	if len(spec.DeviceTags) != 1 || spec.DeviceTags[0].Name != "iot-switch" {
		t.Errorf("deviceTags = %+v, want [iot-switch]", spec.DeviceTags)
	}
	personal := spec.SecurityConfiguration.WPA2Personal
	if personal == nil || personal.Passphrase == nil {
		t.Fatalf("securityConfiguration = %+v, want a personal passphrase placeholder", spec.SecurityConfiguration)
	}
	if personal.Passphrase.Name != unresolvedWifiSecretName || personal.Passphrase.Key != unresolvedWifiSecretKey {
		t.Errorf("passphrase placeholder = %+v", personal.Passphrase)
	}
	if personal.PmfMode == nil || *personal.PmfMode != "OPTIONAL" {
		t.Errorf("pmfMode = %v, want OPTIONAL", personal.PmfMode)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"upstream-broadcast-id", "net-1", "tag-1", "hunter2", `"networkId"`, `"id"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec leaks %q: %s", forbidden, raw)
		}
	}
}

func TestWifiBroadcastsPresharedKeysPlaceholder(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	broadcast.SecurityConfiguration = &unifi.WifiSecurityConfiguration{
		Type:       WifiSecurityWPA2Personal,
		Passphrase: "hunter2",
		PresharedKeys: []unifi.WifiPresharedKey{{
			Network:    unifi.WifiNetworkReference{Type: "SPECIFIC", NetworkID: "net-1"},
			Passphrase: "preshared-secret",
		}},
	}

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 || len(warnings) != 1 {
		t.Fatalf("got %d resources, %d warnings; want 1 and 1", len(resources), len(warnings))
	}
	spec := resources[0].Spec.(UnifiWifiBroadcastSpec)
	personal := spec.SecurityConfiguration.WPA2Personal
	if personal == nil || len(personal.PresharedKeys) != 1 {
		t.Fatalf("presharedKeys = %+v, want one entry", personal)
	}
	key := personal.PresharedKeys[0]
	if key.Network.Name != "default" {
		t.Errorf("preshared key network = %q, want the emitted name default", key.Network.Name)
	}
	if key.Passphrase.Name != unresolvedWifiSecretName || key.Passphrase.Key != unresolvedWifiSecretKey {
		t.Errorf("preshared passphrase = %+v, want the placeholder", key.Passphrase)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "preshared-secret") {
		t.Errorf("spec leaks the preshared passphrase: %s", raw)
	}
}

func TestWifiBroadcastsAnnotatesUnrepresentedSettings(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	broadcast.MloEnabled = json.RawMessage(`true`)

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	annotation := resources[0].Metadata.Annotations[AnnotationUnrepresentedWifiSettings]
	if !strings.Contains(annotation, "mloEnabled") {
		t.Errorf("annotation %s = %q, want it to name mloEnabled", AnnotationUnrepresentedWifiSettings, annotation)
	}
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "mloEnabled") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one naming mloEnabled", warnings)
	}

	// The unmodeled setting must not leak into spec (it is not modeled at all).
	raw, err := json.Marshal(resources[0].Spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "mloEnabled") {
		t.Errorf("spec unexpectedly models mloEnabled: %s", raw)
	}
}

func TestWifiBroadcastsAnnotatesUnrepresentedRadiusConfiguration(t *testing.T) {
	t.Parallel()

	// A personal broadcast carrying the non-enterprise radiusConfiguration is
	// reduced without it; the emitter must annotate and warn rather than let the
	// user silently clear RADIUS MAC-auth upstream.
	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	broadcast.SecurityConfiguration = &unifi.WifiSecurityConfiguration{
		Type:       WifiSecurityWPA2Personal,
		Passphrase: "hunter2",
		RadiusConfiguration: &unifi.WifiRadiusConfiguration{
			NasID: unifi.WifiRadiusNasID{Type: "USER_DEFINED", Value: "nas"},
		},
	}

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	annotation := resources[0].Metadata.Annotations[AnnotationUnrepresentedWifiSettings]
	if !strings.Contains(annotation, "securityConfiguration.radiusConfiguration") {
		t.Errorf("annotation %s = %q, want it to name the radius configuration", AnnotationUnrepresentedWifiSettings, annotation)
	}
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "securityConfiguration.radiusConfiguration") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want one naming the radius configuration", warnings)
	}
}

func TestWifiBroadcastsWPA2PersonalAlwaysPlaceholder(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	// The console omitted the passphrase; the emitter must still emit the placeholder.
	broadcast.SecurityConfiguration = &unifi.WifiSecurityConfiguration{Type: WifiSecurityWPA2Personal}

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	personal := resources[0].Spec.(UnifiWifiBroadcastSpec).SecurityConfiguration.WPA2Personal
	if personal == nil || personal.Passphrase == nil {
		t.Fatalf("passphrase = %+v, want the placeholder even when the detail omits it", personal)
	}
	if personal.Passphrase.Name != unresolvedWifiSecretName || personal.Passphrase.Key != unresolvedWifiSecretKey {
		t.Errorf("passphrase placeholder = %+v", personal.Passphrase)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedWifiSecret]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedWifiSecret)
	}
	if len(warnings) == 0 {
		t.Errorf("warnings = %v, want a passphrase warning", warnings)
	}
}

func TestWifiBroadcastsIotOptimized(t *testing.T) {
	t.Parallel()

	broadcast := unifi.WifiBroadcast{
		ID:                    "iot-id",
		Name:                  "IoT",
		Enabled:               true,
		Type:                  WifiBroadcastTypeIotOptimized,
		Network:               &unifi.WifiNetworkReference{Type: "SPECIFIC", NetworkID: "net-1"},
		SecurityConfiguration: &unifi.WifiSecurityConfiguration{Type: WifiSecurityWPA2Personal, Passphrase: "secret"},
	}

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %v", len(resources), warnings)
	}
	spec := resources[0].Spec.(UnifiWifiBroadcastSpec)
	if spec.IotOptimized == nil {
		t.Errorf("iotOptimized = nil, want the IOT marker")
	}
	if spec.Standard != nil {
		t.Errorf("standard = %+v, want nil for IOT_OPTIMIZED", spec.Standard)
	}
}

func TestWifiBroadcastsEmitsNativeBinding(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	// OPEN security carries no passphrase, so a faithful NATIVE projection emits no warning.
	broadcast.SecurityConfiguration = &unifi.WifiSecurityConfiguration{Type: WifiSecurityOpen}
	broadcast.Network = &unifi.WifiNetworkReference{Type: "NATIVE"}

	resources, warnings := WifiBroadcasts(nil, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, %d warnings; want 1 emitted NATIVE broadcast", len(resources), len(warnings))
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none for a NATIVE-bound broadcast", warnings)
	}

	resource := resources[0]
	spec := resource.Spec.(UnifiWifiBroadcastSpec)
	if spec.NativeNetwork == nil || !*spec.NativeNetwork {
		t.Errorf("nativeNetwork = %v, want pointer to true", spec.NativeNetwork)
	}
	if spec.SiteRef == nil || spec.SiteRef.Name != "default" {
		t.Errorf("siteRef = %+v, want the emitted site name default", spec.SiteRef)
	}
	if spec.NetworkRef != nil {
		t.Errorf("networkRef = %+v, want nil for a NATIVE binding", spec.NetworkRef)
	}
	if _, ok := resource.Metadata.Annotations[AnnotationUnrepresentedWifiSettings]; ok {
		t.Errorf("annotations = %v, want no unrepresented-settings annotation", resource.Metadata.Annotations)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"net-1", "upstream-broadcast-id", `"networkId"`, `"networkRef"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec leaks %q: %s", forbidden, raw)
		}
	}
}

func TestWifiBroadcastsProjectsBandSteering(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	broadcast.SecurityConfiguration = &unifi.WifiSecurityConfiguration{Type: WifiSecurityOpen}
	bandSteering := true
	broadcast.BandSteeringEnabled = &bandSteering

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}

	spec := resources[0].Spec.(UnifiWifiBroadcastSpec)
	if spec.BandSteeringEnabled == nil || !*spec.BandSteeringEnabled {
		t.Errorf("bandSteeringEnabled = %v, want pointer to true in the common field", spec.BandSteeringEnabled)
	}
	if annotation, ok := resources[0].Metadata.Annotations[AnnotationUnrepresentedWifiSettings]; ok {
		t.Errorf("annotation %s = %q, want none naming band steering", AnnotationUnrepresentedWifiSettings, annotation)
	}
	for _, warning := range warnings {
		if strings.Contains(warning, "bandSteeringEnabled") {
			t.Errorf("warnings = %v, want none naming bandSteeringEnabled", warnings)
		}
	}
}

func TestWifiBroadcastsSkipsUnresolvedNetwork(t *testing.T) {
	t.Parallel()

	resources, warnings := WifiBroadcasts(nil, "default", "", nil, []unifi.WifiBroadcast{wifiStandardBroadcast()})
	if len(resources) != 0 {
		t.Fatalf("got %d resources, want 0", len(resources))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "network") {
		t.Errorf("warnings = %v, want an unresolved-network warning", warnings)
	}
}

func TestWifiBroadcastsEmitsEnterpriseWithPlaceholder(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = nil
	coaEnabled := true
	broadcast.SecurityConfiguration = &unifi.WifiSecurityConfiguration{
		Type:       WifiSecurityWPA2Enterprise,
		CoaEnabled: &coaEnabled,
		PmfMode:    "OPTIONAL",
		RadiusConfiguration: &unifi.WifiRadiusConfiguration{
			NasID:     unifi.WifiRadiusNasID{Type: "USER_DEFINED", Value: "unifi-nas"},
			ProfileID: "opaque-profile-uuid",
		},
	}

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (enterprise must be emitted, not dropped)", len(resources))
	}
	enterprise := resources[0].Spec.(UnifiWifiBroadcastSpec).SecurityConfiguration.WPA2Enterprise
	if enterprise == nil {
		t.Fatal("wpa2Enterprise = nil, want the enterprise variant")
	}
	if !enterprise.CoaEnabled || enterprise.RadiusConfiguration.RadiusProfileRef.Name != unresolvedRadiusProfileName {
		t.Errorf("enterprise = %+v, want the placeholder radiusProfileRef", enterprise)
	}
	if enterprise.RadiusConfiguration.NasID.Value != "unifi-nas" {
		t.Errorf("nasId = %+v, want the upstream value preserved", enterprise.RadiusConfiguration.NasID)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedRadiusProfile]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedRadiusProfile)
	}
	found := false
	for _, warning := range warnings {
		if strings.Contains(warning, "RADIUS") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want a RADIUS warning", warnings)
	}

	raw, err := json.Marshal(resources[0].Spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "opaque-profile-uuid") {
		t.Errorf("spec leaks the RADIUS profile UUID: %s", raw)
	}
}

func TestWifiBroadcastsAmbiguousDeviceTagAnnotated(t *testing.T) {
	t.Parallel()

	tags := []unifi.DeviceTag{{ID: "tag-1", Name: "aps", DeviceIDs: []string{"dev-1", "dev-2"}}}
	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", tags, []unifi.WifiBroadcast{wifiStandardBroadcast()})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	spec := resources[0].Spec.(UnifiWifiBroadcastSpec)
	if len(spec.DeviceTags) != 0 {
		t.Errorf("deviceTags = %+v, want none (a multi-device tag is unresolvable)", spec.DeviceTags)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedDeviceTag)
	}
	if len(warnings) == 0 {
		t.Errorf("warnings = %v, want a device-scope warning", warnings)
	}
}

func TestWifiBroadcastsDevicesFilterAnnotated(t *testing.T) {
	t.Parallel()

	broadcast := wifiStandardBroadcast()
	broadcast.BroadcastingDeviceFilter = &unifi.WifiBroadcastingDeviceFilter{Type: "DEVICES", DeviceIDs: []string{"dev-uuid"}}

	resources, _ := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{broadcast})
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1", len(resources))
	}
	spec := resources[0].Spec.(UnifiWifiBroadcastSpec)
	if len(spec.DeviceTags) != 0 {
		t.Errorf("deviceTags = %+v, want none for a DEVICES filter", spec.DeviceTags)
	}
	if _, ok := resources[0].Metadata.Annotations[AnnotationUnresolvedDeviceTag]; !ok {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedDeviceTag)
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "dev-uuid") {
		t.Errorf("spec leaks a device UUID: %s", raw)
	}
}

func TestWifiBroadcastsDisambiguatesNames(t *testing.T) {
	t.Parallel()

	first := wifiStandardBroadcast()
	first.ID = "id-1"
	second := wifiStandardBroadcast()
	second.ID = "id-2"

	resources, _ := WifiBroadcasts(map[string]string{"net-1": "default"}, "default", "", nil, []unifi.WifiBroadcast{first, second})
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(resources))
	}
	if resources[0].Metadata.Name != "corp" || resources[1].Metadata.Name != "corp-2" {
		t.Errorf("names = %q, %q; want corp, corp-2", resources[0].Metadata.Name, resources[1].Metadata.Name)
	}
	if got := resources[1].Metadata.Annotations[AnnotationOriginalName]; got != second.Name {
		t.Errorf("second annotations[%s] = %q, want %q", AnnotationOriginalName, got, second.Name)
	}
}

func TestControllerTemplate(t *testing.T) {
	t.Parallel()

	resource, warnings := Controller("https://unifi.example.com", "unifi")
	if resource.Kind != KindUnifiController {
		t.Errorf("kind = %q, want %q", resource.Kind, KindUnifiController)
	}
	if resource.Metadata.Name != "unifi" {
		t.Errorf("name = %q, want unifi", resource.Metadata.Name)
	}
	if _, ok := resource.Metadata.Annotations[AnnotationControllerTemplate]; !ok {
		t.Errorf("annotations = %v, want %s", resource.Metadata.Annotations, AnnotationControllerTemplate)
	}
	spec := resource.Spec.(UnifiControllerSpec)
	if spec.URL != "https://unifi.example.com" {
		t.Errorf("url = %q", spec.URL)
	}
	if spec.SecretRef == nil || spec.SecretRef.Name == "" || spec.SecretRef.Key == "" {
		t.Errorf("secretRef = %+v, want name and key placeholders", spec.SecretRef)
	}
	if _, ok := resource.Metadata.Annotations[AnnotationInsecureControllerURL]; ok {
		t.Errorf("annotations = %v, want no %s for an https URL",
			resource.Metadata.Annotations, AnnotationInsecureControllerURL)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1", warnings)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	if strings.Contains(string(raw), "insecureSkipVerify") {
		t.Errorf("spec must leave insecureSkipVerify unset: %s", raw)
	}
}

func TestControllerInsecureURLAnnotated(t *testing.T) {
	t.Parallel()

	resource, warnings := Controller("http://unifi.internal:8080", "unifi")
	if got := resource.Metadata.Annotations[AnnotationInsecureControllerURL]; got != "true" {
		t.Errorf("annotation %s = %q, want %q", AnnotationInsecureControllerURL, got, "true")
	}
	if _, ok := resource.Metadata.Annotations[AnnotationControllerTemplate]; !ok {
		t.Errorf("annotations = %v, want the controller-template annotation too", resource.Metadata.Annotations)
	}
	spec := resource.Spec.(UnifiControllerSpec)
	if spec.URL != "http://unifi.internal:8080" {
		t.Errorf("url = %q, want the URL emitted unchanged", spec.URL)
	}
	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want 2 (template + insecure)", warnings)
	}
	if !strings.Contains(warnings[1], "https://") {
		t.Errorf("insecure warning = %q, want it to name https", warnings[1])
	}
}

func TestTrafficMatchingListsProjectsTypesAndDropsUpstreamIDs(t *testing.T) {
	t.Parallel()

	lists := []unifi.TrafficMatchingList{
		{
			ID:   "opaque-list-id",
			Name: "Corp Addresses",
			Type: TrafficMatchingTypeIPv4Addresses,
			Items: []unifi.TrafficMatchingItem{
				{Type: TrafficMatchingItemIPAddress, Value: "10.0.0.1"},
				{Type: TrafficMatchingItemIPAddressRange, Start: "10.0.0.10", Stop: "10.0.0.20"},
				{Type: TrafficMatchingItemSubnet, Value: "10.0.0.0/24"},
			},
		},
		{
			ID:   "opaque-ports-id",
			Name: "Web Ports",
			Type: TrafficMatchingTypePorts,
			Items: []unifi.TrafficMatchingItem{
				{Type: TrafficMatchingItemPortNumber, Value: "443"},
				{Type: TrafficMatchingItemPortNumberRange, Start: "8000", Stop: "8080"},
			},
		},
	}

	resources, warnings := TrafficMatchingLists("default", "", lists)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2", len(resources))
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resources[0].Kind != KindUnifiTrafficMatchingList {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiTrafficMatchingList)
	}

	spec, ok := resources[0].Spec.(UnifiTrafficMatchingListSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiTrafficMatchingListSpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" || spec.Name != "Corp Addresses" || spec.Type != TrafficMatchingTypeIPv4Addresses {
		t.Errorf("spec = %+v, want siteRef.name=default name=Corp Addresses type=IPV4_ADDRESSES", spec)
	}
	if len(spec.Items) != 3 || spec.Items[2].Type != TrafficMatchingItemSubnet ||
		spec.Items[2].Address == nil || spec.Items[2].Address.Value != "10.0.0.0/24" {
		t.Errorf("items = %+v, want the three typed entries", spec.Items)
	}
	if spec.Items[1].AddressRange == nil || spec.Items[1].AddressRange.Start != "10.0.0.10" || spec.Items[1].AddressRange.Stop != "10.0.0.20" {
		t.Errorf("items[1] = %+v, want an address range", spec.Items[1])
	}
	if spec.Items[0].Address == nil || spec.Items[0].Address.Value != "10.0.0.1" {
		t.Errorf("items[0] = %+v, want an address value", spec.Items[0])
	}
	if status, ok := resources[0].Status.(TrafficMatchingListStatus); !ok || status.ListID != "opaque-list-id" {
		t.Errorf("status = %+v, want the upstream id in status only", resources[0].Status)
	}

	ports, ok := resources[1].Spec.(UnifiTrafficMatchingListSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiTrafficMatchingListSpec", resources[1].Spec)
	}
	if ports.Items[0].Port == nil || ports.Items[0].Port.Value != 443 {
		t.Errorf("ports items[0] = %+v, want port value 443", ports.Items[0])
	}
	if ports.Items[1].PortRange == nil || ports.Items[1].PortRange.Start != 8000 || ports.Items[1].PortRange.Stop != 8080 {
		t.Errorf("ports items[1] = %+v, want a port range", ports.Items[1])
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"opaque-list-id", "opaque-ports-id", `"_id"`, `"id"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestTrafficMatchingListsSkipsUnrepresentable(t *testing.T) {
	t.Parallel()

	lists := []unifi.TrafficMatchingList{
		{
			ID:    "bad-type",
			Name:  "Unknown Type",
			Type:  "NOT_A_TYPE",
			Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemIPAddress, Value: "10.0.0.1"}},
		},
		{
			ID:    "mismatch",
			Name:  "Mismatched Entries",
			Type:  TrafficMatchingTypePorts,
			Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemSubnet, Value: "10.0.0.0/24"}},
		},
		{
			ID:    "missing-value",
			Name:  "Missing Value",
			Type:  TrafficMatchingTypeIPv4Addresses,
			Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemIPAddress}},
		},
		{
			ID:    "bad-port",
			Name:  "Bad Port",
			Type:  TrafficMatchingTypePorts,
			Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemPortNumber, Value: "not-a-port"}},
		},
		{
			ID:    "port-out-of-range",
			Name:  "Port Out Of Range",
			Type:  TrafficMatchingTypePorts,
			Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemPortNumber, Value: "70000"}},
		},
		{
			ID:    "good",
			Name:  "Good List",
			Type:  TrafficMatchingTypeIPv6Addresses,
			Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemSubnet, Value: "2001:db8::/64"}},
		},
	}

	resources, warnings := TrafficMatchingLists("default", "", lists)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the representable list): %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "good-list" {
		t.Errorf("name = %q, want good-list", resources[0].Metadata.Name)
	}
	if len(warnings) != 5 {
		t.Errorf("warnings = %v, want 5 (one per skipped list)", warnings)
	}
}

func TestFirewallPoliciesResolvesZonesFiltersAndRecordsStatus(t *testing.T) {
	t.Parallel()

	const (
		userZoneID = "user-zone-uuid"
		sysZoneID  = "sys-zone-uuid"
		listID     = "list-uuid"
	)
	zones := []unifi.FirewallZone{
		{ID: userZoneID, Name: "Custom Zone", Metadata: unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined}},
		{ID: sysZoneID, Name: "Internal", Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"}},
	}
	lists := []unifi.TrafficMatchingList{
		{ID: listID, Name: "Corp Addresses", Type: TrafficMatchingTypeIPv4Addresses, Items: []unifi.TrafficMatchingItem{{Type: TrafficMatchingItemIPAddress, Value: "10.0.0.1"}}},
	}
	policies := []unifi.FirewallPolicy{
		{
			ID:              "policy-uuid",
			Metadata:        unifi.NetworkMetadata{Origin: "USER_DEFINED"},
			Name:            "Allow LAN to WAN",
			Enabled:         true,
			LoggingEnabled:  false,
			Description:     "reach the internet",
			Action:          unifi.FirewallPolicyAction{Type: "ALLOW", AllowReturnTraffic: boolPtr(false)},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: "IPV4_AND_IPV6"},
			Source: unifi.FirewallPolicyZone{
				ZoneID: userZoneID,
				TrafficFilter: &unifi.FirewallPolicyTrafficFilter{
					Type: "IP_ADDRESS",
					IPAddressFilter: &unifi.FirewallPolicyIPAddressFilter{
						Type: "TRAFFIC_MATCHING_LIST", TrafficMatchingListID: listID, MatchOpposite: false,
					},
				},
			},
			Destination: unifi.FirewallPolicyZone{ZoneID: sysZoneID},
		},
		{
			ID:              "policy-uuid-2",
			Metadata:        unifi.NetworkMetadata{Origin: "USER_DEFINED"},
			Name:            "Block IoT",
			Enabled:         true,
			Action:          unifi.FirewallPolicyAction{Type: "BLOCK"},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: "IPV4"},
			Source:          unifi.FirewallPolicyZone{ZoneID: sysZoneID},
			Destination:     unifi.FirewallPolicyZone{ZoneID: userZoneID},
		},
	}

	resources, _, warnings := FirewallPolicies("default", "", zones, lists, policies)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2: %+v", len(resources), resources)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resources[0].Kind != KindUnifiFirewallPolicy {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiFirewallPolicy)
	}

	spec, ok := resources[0].Spec.(UnifiFirewallPolicySpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiFirewallPolicySpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" || spec.Name != "Allow LAN to WAN" || spec.Action.Type != "ALLOW" {
		t.Errorf("spec = %+v, want siteRef=default name=Allow LAN to WAN action=ALLOW", spec)
	}
	if spec.Action.AllowReturnTraffic == nil || *spec.Action.AllowReturnTraffic {
		t.Errorf("action.allowReturnTraffic = %v, want explicit false", spec.Action.AllowReturnTraffic)
	}
	if spec.Source.ZoneRef == nil || spec.Source.ZoneRef.Name != "custom-zone" {
		t.Errorf("source.zoneRef = %+v, want custom-zone", spec.Source.ZoneRef)
	}
	if spec.Source.Filter == nil || spec.Source.Filter.IPAddress == nil || spec.Source.Filter.IPAddress.ListRef.Name != "corp-addresses" {
		t.Errorf("source.filter = %+v, want ipAddress listRef corp-addresses", spec.Source.Filter)
	}
	if spec.Destination.BuiltInZone == nil || spec.Destination.BuiltInZone.Name != "Internal" {
		t.Errorf("destination.builtInZone = %+v, want Internal", spec.Destination.BuiltInZone)
	}
	if status, ok := resources[0].Status.(FirewallPolicyStatus); !ok || status.PolicyID != "policy-uuid" ||
		status.SourceZoneID != userZoneID || status.DestinationZoneID != sysZoneID || status.Origin != "USER_DEFINED" {
		t.Errorf("status = %+v, want policy and zone ids in status only", resources[0].Status)
	}

	block := resources[1].Spec.(UnifiFirewallPolicySpec)
	if block.Action.AllowReturnTraffic != nil {
		t.Errorf("BLOCK action.allowReturnTraffic = %v, want nil", block.Action.AllowReturnTraffic)
	}
	if block.Source.BuiltInZone == nil || block.Source.BuiltInZone.Name != "Internal" {
		t.Errorf("block source.builtInZone = %+v, want Internal", block.Source.BuiltInZone)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"policy-uuid", userZoneID, sysZoneID, listID, `"zoneId"`, `"trafficMatchingListId"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestFirewallPoliciesSkipsUnrepresentable(t *testing.T) {
	t.Parallel()

	zones := []unifi.FirewallZone{
		{ID: "zone-1", Name: "Custom Zone", Metadata: unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined}},
	}
	policies := []unifi.FirewallPolicy{
		{
			ID: "inline", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Name: "Inline Filter",
			Action:          unifi.FirewallPolicyAction{Type: "ALLOW", AllowReturnTraffic: boolPtr(false)},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: "IPV4"},
			Source: unifi.FirewallPolicyZone{
				ZoneID: "zone-1",
				TrafficFilter: &unifi.FirewallPolicyTrafficFilter{
					Type:            "IP_ADDRESS",
					IPAddressFilter: &unifi.FirewallPolicyIPAddressFilter{Type: "IP_ADDRESSES", MatchOpposite: false},
				},
			},
			Destination: unifi.FirewallPolicyZone{ZoneID: "zone-1"},
		},
		{
			ID: "unknown-zone", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Name: "Unknown Zone",
			Action:          unifi.FirewallPolicyAction{Type: "BLOCK"},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: "IPV4"},
			Source:          unifi.FirewallPolicyZone{ZoneID: "not-in-list"},
			Destination:     unifi.FirewallPolicyZone{ZoneID: "zone-1"},
		},
		{
			ID: "unresolved-list", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Name: "Unresolved List",
			Action:          unifi.FirewallPolicyAction{Type: "BLOCK"},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: "IPV4"},
			Source: unifi.FirewallPolicyZone{
				ZoneID: "zone-1",
				TrafficFilter: &unifi.FirewallPolicyTrafficFilter{
					Type: "PORT",
					PortFilter: &unifi.FirewallPolicyPortFilter{
						Type: "TRAFFIC_MATCHING_LIST", TrafficMatchingListID: "missing-list", MatchOpposite: false,
					},
				},
			},
			Destination: unifi.FirewallPolicyZone{ZoneID: "zone-1"},
		},
		{
			ID: "good", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Name: "Good Policy",
			Action:          unifi.FirewallPolicyAction{Type: "REJECT"},
			IPProtocolScope: unifi.FirewallPolicyIPProtocolScope{IPVersion: "IPV4"},
			Source:          unifi.FirewallPolicyZone{ZoneID: "zone-1"},
			Destination:     unifi.FirewallPolicyZone{ZoneID: "zone-1"},
		},
	}

	resources, _, warnings := FirewallPolicies("default", "", zones, nil, policies)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the representable policy): %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "good-policy" {
		t.Errorf("name = %q, want good-policy", resources[0].Metadata.Name)
	}
	if len(warnings) != 3 {
		t.Errorf("warnings = %v, want 3 (one per skipped policy)", warnings)
	}
}

func TestAclRulesProjectsTypesAndResolvesRefs(t *testing.T) {
	t.Parallel()

	networkNames := map[string]string{"net-1": "iot"}
	deviceTags := []unifi.DeviceTag{{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"dev-1"}}}
	prefix := int32(48)
	rules := []unifi.AclRule{
		{
			ID: "rule-uuid", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
			Type: "IPV4", Name: "Allow Guest to DNS", Enabled: true, Description: "dns",
			Action: "ALLOW",
			Source: unifi.AclRuleEndpoint{
				Type:                 "IP_ADDRESSES_OR_SUBNETS",
				IPAddressesOrSubnets: []string{"192.168.20.0/24"},
				PortFilter:           []int32{53},
			},
			Destination:           unifi.AclRuleEndpoint{Type: "NETWORKS", NetworkIDs: []string{"net-1"}},
			ProtocolFilter:        []string{"TCP", "UDP"},
			EnforcingDeviceFilter: &unifi.AclRuleDeviceFilter{Type: "DEVICES", DeviceIDs: []string{"dev-1"}},
		},
		{
			ID: "rule-uuid-2", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
			Type: "MAC", Name: "Allow Switch MAC", Enabled: true, Action: "ALLOW",
			Source:          unifi.AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"aa:bb:cc:dd:ee:ff"}, PrefixLength: &prefix},
			Destination:     unifi.AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"00:11:22:33:44:55"}},
			NetworkIDFilter: "net-1",
		},
	}

	resources, _, warnings := AclRules("default", "", networkNames, deviceTags, rules)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2: %+v", len(resources), resources)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resources[0].Kind != KindUnifiAclRule {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiAclRule)
	}

	ipv4, ok := resources[0].Spec.(UnifiAclRuleSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiAclRuleSpec", resources[0].Spec)
	}
	if ipv4.SiteRef.Name != "default" || ipv4.Name != "Allow Guest to DNS" || ipv4.Type != "IPV4" || ipv4.Action != "ALLOW" {
		t.Errorf("ipv4 spec = %+v, want siteRef=default type=IPV4 action=ALLOW", ipv4)
	}
	if ipv4.Source.IPAddressesOrSubnets == nil || len(ipv4.Source.IPAddressesOrSubnets.Addresses) != 1 ||
		ipv4.Source.IPAddressesOrSubnets.Addresses[0] != "192.168.20.0/24" ||
		len(ipv4.Source.IPAddressesOrSubnets.Ports) != 1 || ipv4.Source.IPAddressesOrSubnets.Ports[0] != AclPort(53) {
		t.Errorf("ipv4 source = %+v, want addresses + port 53", ipv4.Source)
	}
	if ipv4.Destination.Networks == nil || len(ipv4.Destination.Networks.NetworkRefs) != 1 ||
		ipv4.Destination.Networks.NetworkRefs[0].Name != "iot" {
		t.Errorf("ipv4 destination = %+v, want networkRef iot", ipv4.Destination)
	}
	if len(ipv4.ProtocolFilter) != 2 || ipv4.EnforcingDeviceTags[0].Name != "iot-switch" {
		t.Errorf("ipv4 protocolFilter/enforcing tags = %+v %+v", ipv4.ProtocolFilter, ipv4.EnforcingDeviceTags)
	}
	if status, ok := resources[0].Status.(AclRuleStatus); !ok || status.RuleID != "rule-uuid" || status.Origin != "USER_DEFINED" {
		t.Errorf("status = %+v, want rule id and origin in status only", resources[0].Status)
	}

	mac := resources[1].Spec.(UnifiAclRuleSpec)
	if mac.Type != "MAC" || mac.NetworkRef == nil || mac.NetworkRef.Name != "iot" {
		t.Errorf("mac spec = %+v, want networkRef iot", mac)
	}
	if mac.Source.MacAddresses == nil || mac.Source.MacAddresses.PrefixLength == nil || *mac.Source.MacAddresses.PrefixLength != 48 {
		t.Errorf("mac source = %+v, want prefixLength 48", mac.Source)
	}

	raw, err := json.Marshal(ipv4)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"rule-uuid", "net-1", "dev-1", `"networkIds"`, `"networkIdFilter"`, `"deviceIds"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestAclRulesSkipsUnrepresentable(t *testing.T) {
	t.Parallel()

	networkNames := map[string]string{"net-1": "iot"}
	deviceTags := []unifi.DeviceTag{{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"dev-1"}}}
	ip := func() unifi.AclRuleEndpoint {
		return unifi.AclRuleEndpoint{Type: "IP_ADDRESSES_OR_SUBNETS", IPAddressesOrSubnets: []string{"0.0.0.0/0"}}
	}
	rules := []unifi.AclRule{
		{ID: "unknown-net", Type: "IPV4", Name: "Unknown Net", Action: "BLOCK",
			Source:      unifi.AclRuleEndpoint{Type: "NETWORKS", NetworkIDs: []string{"missing"}},
			Destination: ip()},
		{ID: "untagged", Type: "IPV4", Name: "Untagged", Action: "ALLOW",
			Source: ip(), Destination: ip(),
			EnforcingDeviceFilter: &unifi.AclRuleDeviceFilter{Type: "DEVICES", DeviceIDs: []string{"ghost"}}},
		{ID: "unknown-action", Type: "IPV4", Name: "Unknown Action", Action: "DROP",
			Source: ip(), Destination: ip()},
		{ID: "mac-protocol", Type: "MAC", Name: "Mac Protocol", Action: "ALLOW",
			Source:          unifi.AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"aa:bb:cc:dd:ee:ff"}},
			Destination:     unifi.AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"00:11:22:33:44:55"}},
			NetworkIDFilter: "net-1", ProtocolFilter: []string{"TCP"}},
		{ID: "ipv4-mac-endpoint", Type: "IPV4", Name: "IPv4 Mac Endpoint", Action: "BLOCK",
			Source:      unifi.AclRuleEndpoint{Type: "MAC_ADDRESSES", MacAddresses: []string{"aa:bb:cc:dd:ee:ff"}},
			Destination: ip()},
		{ID: "good", Type: "IPV4", Name: "Good Rule", Action: "BLOCK", Source: ip(), Destination: ip()},
	}

	resources, _, warnings := AclRules("default", "", networkNames, deviceTags, rules)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the representable rule): %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "good-rule" {
		t.Errorf("name = %q, want good-rule", resources[0].Metadata.Name)
	}
	if len(warnings) != 5 {
		t.Errorf("warnings = %v, want 5 (one per skipped rule)", warnings)
	}
}

func TestFirewallPolicyZonePairs(t *testing.T) {
	t.Parallel()

	policies := []unifi.FirewallPolicy{
		{ID: "p1", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: "a"}, Destination: unifi.FirewallPolicyZone{ZoneID: "b"}},
		{ID: "p2", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: "a"}, Destination: unifi.FirewallPolicyZone{ZoneID: "b"}},
		{ID: "sys", Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: "x"}, Destination: unifi.FirewallPolicyZone{ZoneID: "y"}},
		{ID: "p3", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: "b"}, Destination: unifi.FirewallPolicyZone{ZoneID: "a"}},
	}

	pairs := FirewallPolicyZonePairs(policies)
	want := []FirewallPolicyZonePair{
		{SourceZoneID: "a", DestinationZoneID: "b"},
		{SourceZoneID: "b", DestinationZoneID: "a"},
	}
	if len(pairs) != len(want) {
		t.Fatalf("pairs = %v, want %v (distinct user-defined pairs only)", pairs, want)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Errorf("pairs[%d] = %v, want %v", i, pairs[i], want[i])
		}
	}
}

func TestFirewallPolicyOrderingResolvesAndAnchorsSystem(t *testing.T) {
	t.Parallel()

	const (
		userZoneID = "user-zone-uuid"
		sysZoneID  = "sys-zone-uuid"
	)
	zones := []unifi.FirewallZone{
		{ID: userZoneID, Name: "Custom Zone", Metadata: unifi.NetworkMetadata{Origin: firewallZoneOriginUserDefined}},
		{ID: sysZoneID, Name: "Internal", Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"}},
	}
	policies := []unifi.FirewallPolicy{
		{ID: "order-user", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: userZoneID}, Destination: unifi.FirewallPolicyZone{ZoneID: sysZoneID}},
		{ID: "order-unemitted", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: userZoneID}, Destination: unifi.FirewallPolicyZone{ZoneID: sysZoneID}},
		{ID: "order-system", Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"}, Source: unifi.FirewallPolicyZone{ZoneID: userZoneID}, Destination: unifi.FirewallPolicyZone{ZoneID: sysZoneID}},
	}
	// order-system is emitted (it has a name) but system-defined, so it must be anchored
	// rather than listed; order-unemitted has no emitted name and must be skipped.
	policyNames := map[string]string{"order-user": "user-policy", "order-system": "system-policy"}
	orderings := map[FirewallPolicyZonePair]unifi.FirewallPolicyOrdering{
		{SourceZoneID: userZoneID, DestinationZoneID: sysZoneID}: {
			OrderedFirewallPolicyIDs: unifi.FirewallPolicyOrderingIDs{
				BeforeSystemDefined: []string{"order-user", "order-system"},
				AfterSystemDefined:  []string{"order-unemitted"},
			},
		},
	}

	resources, warnings := FirewallPolicyOrdering("default", "", zones, policies, policyNames, orderings)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %+v", len(resources), resources)
	}
	if resources[0].Kind != KindUnifiFirewallPolicyOrdering {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiFirewallPolicyOrdering)
	}
	spec, ok := resources[0].Spec.(UnifiFirewallPolicyOrderingSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiFirewallPolicyOrderingSpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" {
		t.Errorf("siteRef = %+v, want default", spec.SiteRef)
	}
	if spec.Source.ZoneRef == nil || spec.Source.ZoneRef.Name != "custom-zone" {
		t.Errorf("source = %+v, want zoneRef custom-zone", spec.Source)
	}
	if spec.Destination.BuiltInZone == nil || spec.Destination.BuiltInZone.Name != "Internal" {
		t.Errorf("destination = %+v, want builtInZone Internal", spec.Destination)
	}
	if len(spec.BeforeSystemDefined) != 1 || spec.BeforeSystemDefined[0].Name != "user-policy" {
		t.Errorf("beforeSystemDefined = %+v, want [user-policy] (system policy anchored)", spec.BeforeSystemDefined)
	}
	if len(spec.AfterSystemDefined) != 0 {
		t.Errorf("afterSystemDefined = %+v, want empty (unemitted policy skipped)", spec.AfterSystemDefined)
	}
	if got := resources[0].Metadata.Annotations[AnnotationUnresolvedPolicyRef]; got == "" {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedPolicyRef)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want one", warnings)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{userZoneID, sysZoneID, "order-user", "order-system", "order-unemitted", `"zoneId"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestAclRuleOrderingResolvesInUpstreamOrder(t *testing.T) {
	t.Parallel()

	ruleNames := map[string]string{"r1": "first-rule", "r2": "second-rule"}
	ordered := []string{"r2", "missing", "r1"}

	resources, warnings := AclRuleOrdering("default", "", ruleNames, ordered)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %+v", len(resources), resources)
	}
	if resources[0].Kind != KindUnifiAclRuleOrdering {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiAclRuleOrdering)
	}
	spec, ok := resources[0].Spec.(UnifiAclRuleOrderingSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiAclRuleOrderingSpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" {
		t.Errorf("siteRef = %+v, want default", spec.SiteRef)
	}
	if len(spec.OrderedAclRules) != 2 || spec.OrderedAclRules[0].Name != "second-rule" || spec.OrderedAclRules[1].Name != "first-rule" {
		t.Errorf("orderedAclRules = %+v, want [second-rule first-rule] in upstream order", spec.OrderedAclRules)
	}
	if got := resources[0].Metadata.Annotations[AnnotationUnresolvedAclRuleRef]; got == "" {
		t.Errorf("annotations = %v, want %s", resources[0].Metadata.Annotations, AnnotationUnresolvedAclRuleRef)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want one", warnings)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"r1", "r2", "missing", `"id"`, `"ruleID"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func i32Ptr(v int32) *int32 { return &v }

func TestDnsPoliciesProjectsVariantAndRecordsStatus(t *testing.T) {
	t.Parallel()

	policies := []unifi.DnsPolicy{
		{
			ID: "dns-uuid", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
			Type: "A_RECORD", Domain: "www.example.com", Enabled: true,
			IPv4Address: "192.168.1.10", TTLSeconds: i32Ptr(14400),
		},
		{
			ID: "dns-uuid-2", Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"},
			Type: "SRV_RECORD", Domain: "_ldap._tcp.example.com", Enabled: true,
			Port: i32Ptr(389), Priority: i32Ptr(0), Protocol: "_tcp",
			ServerDomain: "dc.example.com", Service: "_ldap", Weight: i32Ptr(100),
		},
	}

	resources, warnings := DnsPolicies("default", "", policies)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2: %+v", len(resources), resources)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resources[0].Kind != KindUnifiDnsPolicy {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiDnsPolicy)
	}
	if resources[0].Metadata.Name != "www-example-com-a-record" {
		t.Errorf("name = %q, want www-example-com-a-record", resources[0].Metadata.Name)
	}

	a, ok := resources[0].Spec.(UnifiDnsPolicySpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiDnsPolicySpec", resources[0].Spec)
	}
	if a.SiteRef.Name != "default" || a.Type != "A_RECORD" || a.Domain != "www.example.com" {
		t.Errorf("a spec = %+v, want siteRef=default type=A_RECORD", a)
	}
	if a.ARecord == nil || a.ARecord.IPv4Address != "192.168.1.10" || a.ARecord.TTLSeconds != 14400 {
		t.Errorf("a record = %+v", a.ARecord)
	}
	if a.AaaaRecord != nil || a.CnameRecord != nil || a.MxRecord != nil || a.SrvRecord != nil || a.TxtRecord != nil || a.ForwardDomain != nil {
		t.Errorf("a spec carries a foreign variant: %+v", a)
	}
	if status, ok := resources[0].Status.(DnsPolicyStatus); !ok || status.PolicyID != "dns-uuid" || status.Origin != "USER_DEFINED" {
		t.Errorf("status = %+v, want policy id and origin in status only", resources[0].Status)
	}

	srv := resources[1].Spec.(UnifiDnsPolicySpec)
	if srv.SrvRecord == nil || srv.SrvRecord.Port != 389 || srv.SrvRecord.Weight != 100 || srv.SrvRecord.Priority != 0 {
		t.Errorf("srv record = %+v", srv.SrvRecord)
	}

	raw, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"dns-uuid", `"policyID"`, `"_id"`, `"id"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestDnsPoliciesSkipsUnrepresentable(t *testing.T) {
	t.Parallel()

	policies := []unifi.DnsPolicy{
		{ID: "unknown", Type: "NAPTR_RECORD", Domain: "unknown.example.com"},
		{ID: "foreign", Type: "A_RECORD", Domain: "mismatch.example.com", TargetDomain: "www.example.com"},
		{ID: "missing", Type: "A_RECORD", Domain: "missing.example.com", IPv4Address: "192.168.1.20"},
		{ID: "range", Type: "AAAA_RECORD", Domain: "range.example.com", IPv6Address: "fd00::1", TTLSeconds: i32Ptr(90000)},
		{ID: "good", Type: "TXT_RECORD", Domain: "text.example.com", Text: "hello"},
	}

	resources, warnings := DnsPolicies("default", "", policies)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the representable policy): %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "text-example-com-txt-record" {
		t.Errorf("name = %q, want text-example-com-txt-record", resources[0].Metadata.Name)
	}
	if len(warnings) != 4 {
		t.Errorf("warnings = %v, want 4 (one per skipped policy)", warnings)
	}
}

func TestDnsPoliciesDisambiguatesSameDomainType(t *testing.T) {
	t.Parallel()

	policies := []unifi.DnsPolicy{
		{ID: "a", Type: "A_RECORD", Domain: "www.example.com", IPv4Address: "192.168.1.10", TTLSeconds: i32Ptr(300)},
		{ID: "b", Type: "A_RECORD", Domain: "www.example.com", IPv4Address: "192.168.1.11", TTLSeconds: i32Ptr(300)},
	}

	resources, warnings := DnsPolicies("default", "", policies)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2: %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "www-example-com-a-record" || resources[1].Metadata.Name != "www-example-com-a-record-2" {
		t.Errorf("names = %q, %q, want distinct suffixed names", resources[0].Metadata.Name, resources[1].Metadata.Name)
	}
	if got := resources[1].Metadata.Annotations[AnnotationOriginalName]; got != "www.example.com A_RECORD" {
		t.Errorf("annotation %s = %q, want the original domain and type", AnnotationOriginalName, got)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1 collision warning", warnings)
	}
}

func TestSwitchingLagsResolvesDeviceTagsAndRecordsStatus(t *testing.T) {
	t.Parallel()

	deviceTags := []unifi.DeviceTag{
		{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"dev-1"}},
		{ID: "tag-2", Name: "aps", DeviceIDs: []string{"dev-2", "dev-3"}},
	}
	lags := []unifi.Lag{{
		ID: "lag-uuid", Type: "LOCAL", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
		Members: []unifi.LagMember{{DeviceID: "dev-1", PortIdxs: []int32{1, 2}}},
	}}

	resources, warnings := SwitchingLags("default", "", deviceTags, lags)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1: %+v", len(resources), resources)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resources[0].Kind != KindUnifiSwitchingLag {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiSwitchingLag)
	}
	if resources[0].Metadata.Name != "local-iot-switch-1-2" {
		t.Errorf("name = %q, want local-iot-switch-1-2", resources[0].Metadata.Name)
	}

	spec, ok := resources[0].Spec.(UnifiSwitchingLagSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiSwitchingLagSpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" || spec.Type != "LOCAL" || len(spec.Members) != 1 {
		t.Fatalf("spec = %+v, want siteRef=default type=LOCAL one member", spec)
	}
	if spec.Members[0].DeviceTag.Name != "iot-switch" || len(spec.Members[0].PortIdxs) != 2 {
		t.Errorf("member = %+v, want iot-switch ports [1 2]", spec.Members[0])
	}

	status, ok := resources[0].Status.(UnifiSwitchingLagStatus)
	if !ok || status.LagID != "lag-uuid" || status.Type != "LOCAL" ||
		len(status.Members) != 1 || status.Members[0].DeviceID != "dev-1" {
		t.Errorf("status = %+v, want lag id, type, and observed members", resources[0].Status)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"lag-uuid", "dev-1", `"deviceId"`, `"deviceID"`, `"id"`} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains opaque identifier %s: %s", forbidden, raw)
		}
	}
}

func TestSwitchingLagsSkipsUnrepresentable(t *testing.T) {
	t.Parallel()

	deviceTags := []unifi.DeviceTag{
		{ID: "tag-1", Name: "iot-switch", DeviceIDs: []string{"dev-1", "dev-4"}},
		{ID: "tag-2", Name: "aps", DeviceIDs: []string{"dev-4"}},
	}
	lags := []unifi.Lag{
		{ID: "unknown-type", Type: "BOGUS", Members: []unifi.LagMember{{DeviceID: "dev-1", PortIdxs: []int32{1}}}},
		{ID: "no-members", Type: "LOCAL"},
		{ID: "untagged", Type: "LOCAL", Members: []unifi.LagMember{{DeviceID: "ghost", PortIdxs: []int32{1}}}},
		{ID: "ambiguous", Type: "LOCAL", Members: []unifi.LagMember{{DeviceID: "dev-4", PortIdxs: []int32{1}}}},
		{ID: "no-ports", Type: "LOCAL", Members: []unifi.LagMember{{DeviceID: "dev-1"}}},
		{ID: "good", Type: "LOCAL", Members: []unifi.LagMember{{DeviceID: "dev-1", PortIdxs: []int32{1}}}},
	}

	resources, warnings := SwitchingLags("default", "", deviceTags, lags)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the representable LAG): %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "local-iot-switch-1" {
		t.Errorf("name = %q, want local-iot-switch-1", resources[0].Metadata.Name)
	}
	if len(warnings) != 5 {
		t.Errorf("warnings = %v, want 5 (one per skipped LAG)", warnings)
	}
}

func TestSwitchingMcLagDomainsAdoptsAndFlattens(t *testing.T) {
	t.Parallel()

	domains := []unifi.McLagDomain{
		{
			ID: "dom-uuid", Name: "Core MC-LAG", Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
			Peers: []unifi.McLagPeer{{DeviceID: "dev-2", LinkPortIdxs: []int32{7, 8}, Role: "TOP"}},
			Lags:  []unifi.McLagLocal{{ID: "lag-1", Members: []unifi.LagMember{{DeviceID: "dev-1", PortIdxs: []int32{9}}}}},
		},
		{ID: "blank", Name: ""},
	}

	resources, warnings := SwitchingMcLagDomains("default", "", domains)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the named domain): %+v", len(resources), resources)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1 (the unnamed domain)", warnings)
	}
	if resources[0].Kind != KindUnifiSwitchingMcLagDomain {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiSwitchingMcLagDomain)
	}
	spec := resources[0].Spec.(UnifiSwitchingMcLagDomainSpec)
	if spec.SiteRef.Name != "default" || spec.Name != "Core MC-LAG" {
		t.Errorf("spec = %+v, want siteRef=default name=Core MC-LAG", spec)
	}
	status := resources[0].Status.(UnifiSwitchingMcLagDomainStatus)
	if status.DomainID != "dom-uuid" || len(status.Members) != 1 || status.Members[0].DeviceID != "dev-1" ||
		len(status.Peers) != 1 || status.Peers[0].Role != "TOP" {
		t.Errorf("status = %+v, want domain id, flattened members, and peers", status)
	}
}

func TestSwitchingSwitchStacksAdoptsAndRecordsMembers(t *testing.T) {
	t.Parallel()

	stacks := []unifi.SwitchStack{
		{
			ID: "stack-uuid", Name: "Core Stack", Metadata: unifi.NetworkMetadata{Origin: "SYSTEM_DEFINED"},
			Members: []unifi.SwitchStackMember{{DeviceID: "dev-1"}, {DeviceID: "dev-2"}},
		},
		{ID: "blank", Name: ""},
	}

	resources, warnings := SwitchingSwitchStacks("default", "", stacks)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the named stack): %+v", len(resources), resources)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1 (the unnamed stack)", warnings)
	}
	if resources[0].Kind != KindUnifiSwitchingSwitchStack {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiSwitchingSwitchStack)
	}
	spec := resources[0].Spec.(UnifiSwitchingSwitchStackSpec)
	if spec.SiteRef.Name != "default" || spec.Name != "Core Stack" {
		t.Errorf("spec = %+v, want siteRef=default name=Core Stack", spec)
	}
	status := resources[0].Status.(UnifiSwitchingSwitchStackStatus)
	if status.StackID != "stack-uuid" || len(status.Members) != 2 {
		t.Errorf("status = %+v, want stack id and two observed members", status)
	}
}

func TestVpnServersAdoptsAndRecordsStatus(t *testing.T) {
	t.Parallel()

	servers := []unifi.VpnServer{
		{
			ID: "server-uuid", Name: "WireGuard Server", Type: "WIREGUARD", Enabled: true,
			Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
		},
		{ID: "blank", Name: "", Type: "OPENVPN", Enabled: true},
	}

	resources, warnings := VpnServers("default", "", servers)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the named server): %+v", len(resources), resources)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1 (the unnamed server)", warnings)
	}
	if resources[0].Kind != KindUnifiVpnServer {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiVpnServer)
	}
	spec, ok := resources[0].Spec.(UnifiVpnServerSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiVpnServerSpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" || spec.Name != "WireGuard Server" {
		t.Errorf("spec = %+v, want siteRef=default name=WireGuard Server", spec)
	}
	status, ok := resources[0].Status.(VpnServerStatus)
	if !ok || status.ServerID != "server-uuid" || status.Type != "WIREGUARD" || status.Enabled == nil || !*status.Enabled {
		t.Errorf("status = %+v, want serverID, type, and enabled in status only", resources[0].Status)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"server-uuid", `"serverID"`, `"type"`, `"enabled"`, `"id"`, "secret", "psk"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains %s: %s", forbidden, raw)
		}
	}
}

func TestVpnServersDisambiguatesNames(t *testing.T) {
	t.Parallel()

	servers := []unifi.VpnServer{
		{ID: "a", Name: "Edge VPN", Type: "WIREGUARD", Enabled: true},
		{ID: "b", Name: "edge-vpn", Type: "OPENVPN", Enabled: false},
	}

	resources, warnings := VpnServers("default", "", servers)
	if len(resources) != 2 {
		t.Fatalf("got %d resources, want 2: %+v", len(resources), resources)
	}
	if resources[0].Metadata.Name != "edge-vpn" || resources[1].Metadata.Name != "edge-vpn-2" {
		t.Errorf("names = %q, %q, want distinct suffixed names", resources[0].Metadata.Name, resources[1].Metadata.Name)
	}
	if got := resources[1].Metadata.Annotations[AnnotationOriginalName]; got != "edge-vpn" {
		t.Errorf("annotation %s = %q, want the original name", AnnotationOriginalName, got)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1 collision warning", warnings)
	}
}

func TestSiteToSiteTunnelsAdoptsAndRecordsStatus(t *testing.T) {
	t.Parallel()

	tunnels := []unifi.SiteToSiteTunnel{
		{
			ID: "tunnel-uuid", Name: "HQ to Branch", Type: "IPSEC",
			Metadata: unifi.NetworkMetadata{Origin: "USER_DEFINED"},
		},
		{ID: "blank", Name: "", Type: "WIREGUARD"},
	}

	resources, warnings := SiteToSiteTunnels("default", "", tunnels)
	if len(resources) != 1 {
		t.Fatalf("got %d resources, want 1 (only the named tunnel): %+v", len(resources), resources)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want 1 (the unnamed tunnel)", warnings)
	}
	if resources[0].Kind != KindUnifiSiteToSiteTunnel {
		t.Errorf("kind = %q, want %q", resources[0].Kind, KindUnifiSiteToSiteTunnel)
	}
	spec, ok := resources[0].Spec.(UnifiSiteToSiteTunnelSpec)
	if !ok {
		t.Fatalf("spec type = %T, want UnifiSiteToSiteTunnelSpec", resources[0].Spec)
	}
	if spec.SiteRef.Name != "default" || spec.Name != "HQ to Branch" {
		t.Errorf("spec = %+v, want siteRef=default name=HQ to Branch", spec)
	}
	status, ok := resources[0].Status.(SiteToSiteTunnelStatus)
	if !ok || status.TunnelID != "tunnel-uuid" || status.Type != "IPSEC" {
		t.Errorf("status = %+v, want tunnelID and type in status only", resources[0].Status)
	}

	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	for _, forbidden := range []string{"tunnel-uuid", `"tunnelID"`, `"type"`, `"id"`, "secret", "psk"} {
		if strings.Contains(string(raw), forbidden) {
			t.Errorf("spec contains %s: %s", forbidden, raw)
		}
	}
}
