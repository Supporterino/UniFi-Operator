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

	resources, _, warnings := Networks("default", []unifi.Network{{
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

	resources, _, warnings := Networks("default", networks, nil)
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
	again, _, _ := Networks("default", networks, nil)
	for i := range again {
		if again[i].Metadata.Name != resources[i].Metadata.Name {
			t.Errorf("run 2 resource %d name = %q, want %q", i, again[i].Metadata.Name, resources[i].Metadata.Name)
		}
	}
}

func TestNetworksGatewayMapping(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", []unifi.Network{{
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

func TestNetworksGatewayPrefixDelegationAnnotated(t *testing.T) {
	t.Parallel()

	resources, _, warnings := Networks("default", []unifi.Network{{
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

	resources, _, warnings := Networks("default", []unifi.Network{{
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

	resources, _, warnings := Networks("default", []unifi.Network{{
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
	resources, _, warnings := Networks("default", []unifi.Network{{
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
	resources, _, warnings := Networks("default", []unifi.Network{{
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

	resources, warnings := FirewallZones("default", networkNames, zones)
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

	resources, warnings := FirewallZones("default", map[string]string{"known": "known-net"}, []unifi.FirewallZone{
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

	resources, _, warnings := Networks("default", []unifi.Network{{
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

	resources, _, warnings := Networks("default", []unifi.Network{
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
	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, tags, []unifi.WifiBroadcast{wifiStandardBroadcast()})
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

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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

func TestWifiBroadcastsSkipsUnresolvedNetwork(t *testing.T) {
	t.Parallel()

	resources, warnings := WifiBroadcasts(nil, nil, []unifi.WifiBroadcast{wifiStandardBroadcast()})
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

	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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
	resources, warnings := WifiBroadcasts(map[string]string{"net-1": "default"}, tags, []unifi.WifiBroadcast{wifiStandardBroadcast()})
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

	resources, _ := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{broadcast})
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

	resources, _ := WifiBroadcasts(map[string]string{"net-1": "default"}, nil, []unifi.WifiBroadcast{first, second})
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
