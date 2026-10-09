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

	// KindUnifiController is the kind for a projected UniFi controller connection.
	KindUnifiController = "UnifiController"
	// KindUnifiSite is the kind for a projected UniFi site.
	KindUnifiSite = "UnifiSite"
	// KindUnifiNetwork is the kind for a projected UniFi network.
	KindUnifiNetwork = "UnifiNetwork"

	// Management discriminator values accepted by UnifiNetwork.spec.management.
	ManagementGateway   = "GATEWAY"
	ManagementSwitch    = "SWITCH"
	ManagementUnmanaged = "UNMANAGED"

	// AnnotationOriginalName records the upstream object name on a resource whose
	// metadata.name was disambiguated to avoid a collision.
	AnnotationOriginalName = "unifi.supporterino.de/original-name"
	// AnnotationUnresolvedDeviceTag records that a switch-managed network's upstream
	// device binding could not be expressed as a UnifiDeviceTag reference.
	AnnotationUnresolvedDeviceTag = "unifi.supporterino.de/unresolved-device-tag"
	// AnnotationUnrepresentedIPv6 records that part of a gateway network's IPv6
	// configuration could not be represented in spec.
	AnnotationUnrepresentedIPv6 = "unifi.supporterino.de/unrepresented-ipv6"
	// AnnotationControllerTemplate marks an emitted UnifiController as a template the
	// user must complete with an API-key Secret.
	AnnotationControllerTemplate = "unifi.supporterino.de/controller-template"
	// AnnotationInsecureControllerURL marks an emitted UnifiController whose spec.url is
	// not https. The CRD requires an https URL, so the user must update it before applying.
	AnnotationInsecureControllerURL = "unifi.supporterino.de/insecure-controller-url"

	// DefaultControllerRef is the default UnifiController name emitted sites reference
	// and the default name of the emitted UnifiController template.
	DefaultControllerRef = "unifi-controller"
	// DefaultSecretName and DefaultSecretKey are the placeholder Secret coordinates in
	// the emitted UnifiController template. They carry no credential value.
	DefaultSecretName = "unifi-api-key"
	DefaultSecretKey  = "api-key"

	// unresolvedDeviceTagName is the deterministic placeholder deviceTagRef.name
	// emitted for a switch-managed network. The upstream deviceId is opaque and cannot
	// be turned into a UnifiDeviceTag reference.
	unresolvedDeviceTagName = "unresolved-device-tag"

	minVLAN = 1
	maxVLAN = 4009

	minIPv4Prefix = 8
	maxIPv4Prefix = 30

	minIPv6Prefix = 64
	maxIPv6Prefix = 127
)

// CoreRef is a reference to another Custom Resource by Kubernetes name, same namespace.
type CoreRef struct {
	Name string `json:"name"`
}

// SecretKeySelector identifies a same-namespace Secret key. It carries no value.
type SecretKeySelector struct {
	Name string `json:"name"`
	Key  string `json:"key"`
}

// UnifiControllerSpec is the desired state emitted for a UniFi controller connection.
// InsecureSkipVerify is intentionally left unset so TLS verification stays on by default.
type UnifiControllerSpec struct {
	URL       string             `json:"url"`
	SecretRef *SecretKeySelector `json:"secretRef"`
}

// UnifiSiteSpec is the desired state emitted for a UniFi site. The upstream site UUID is
// never emitted; the controller adopts the site by InternalReference and records the UUID
// in status.
type UnifiSiteSpec struct {
	ControllerRef     CoreRef `json:"controllerRef"`
	InternalReference string  `json:"internalReference"`
}

// DHCPGuarding holds the trusted DHCP server addresses for a network.
type DHCPGuarding struct {
	TrustedDHCPServerIPAddresses []string `json:"trustedDhcpServerIpAddresses"`
}

// IPv4Configuration is the IPv4 configuration shared by the gateway and switch variants.
type IPv4Configuration struct {
	AutoScaleEnabled        bool     `json:"autoScaleEnabled"`
	HostIPAddress           string   `json:"hostIpAddress"`
	PrefixLength            int32    `json:"prefixLength"`
	AdditionalHostIPSubnets []string `json:"additionalHostIpSubnets,omitempty"`
}

// IPv6ClientAddressAssignment configures IPv6 client addressing.
type IPv6ClientAddressAssignment struct {
	SLAACEnabled bool `json:"slaacEnabled"`
}

// RouterAdvertisement configures IPv6 router advertisement.
type RouterAdvertisement struct {
	Priority string `json:"priority"`
}

// IPv6Configuration is the emitted IPv6 configuration. Prefix-delegation IPv6 references
// the WAN by opaque id upstream and is therefore never emitted (see AnnotationUnrepresentedIPv6).
type IPv6Configuration struct {
	InterfaceType                string                      `json:"interfaceType"`
	ClientAddressAssignment      IPv6ClientAddressAssignment `json:"clientAddressAssignment"`
	AdditionalHostIPSubnets      []string                    `json:"additionalHostIpSubnets,omitempty"`
	DNSServerIPAddressesOverride []string                    `json:"dnsServerIpAddressesOverride,omitempty"`
	RouterAdvertisement          *RouterAdvertisement        `json:"routerAdvertisement,omitempty"`
	HostIPAddress                string                      `json:"hostIpAddress,omitempty"`
	PrefixLength                 int32                       `json:"prefixLength,omitempty"`
}

// GatewayNetworkOptions is the emitted GATEWAY variant.
type GatewayNetworkOptions struct {
	CellularBackupEnabled bool               `json:"cellularBackupEnabled"`
	InternetAccessEnabled bool               `json:"internetAccessEnabled"`
	IsolationEnabled      bool               `json:"isolationEnabled"`
	IPv4Configuration     IPv4Configuration  `json:"ipv4Configuration"`
	IPv6Configuration     *IPv6Configuration `json:"ipv6Configuration,omitempty"`
	MDNSForwardingEnabled *bool              `json:"mdnsForwardingEnabled,omitempty"`
}

// SwitchNetworkOptions is the emitted SWITCH variant. DeviceTagRef is required by the
// CRD; because the upstream deviceId is opaque it is emitted as a deterministic
// placeholder and the resource is annotated.
type SwitchNetworkOptions struct {
	CellularBackupEnabled bool              `json:"cellularBackupEnabled"`
	IsolationEnabled      bool              `json:"isolationEnabled"`
	IPv4Configuration     IPv4Configuration `json:"ipv4Configuration"`
	DeviceTagRef          CoreRef           `json:"deviceTagRef"`
}

// UnifiNetworkSpec is the desired state emitted for a UniFi network. The field names and
// types mirror the operator's UnifiNetworkSpec (api/v1alpha1) so emitted resources
// validate against the CRD. It carries no upstream id, zoneId, or deviceId: per
// docs/crd-conventions.md opaque identifiers belong in status, which the operator owns.
type UnifiNetworkSpec struct {
	SiteRef      CoreRef                `json:"siteRef"`
	Management   string                 `json:"management"`
	Name         string                 `json:"name"`
	Enabled      *bool                  `json:"enabled,omitempty"`
	VLANID       int32                  `json:"vlanId"`
	DHCPGuarding *DHCPGuarding          `json:"dhcpGuarding,omitempty"`
	Gateway      *GatewayNetworkOptions `json:"gateway,omitempty"`
	Switch       *SwitchNetworkOptions  `json:"switch,omitempty"`
}

// Controller emits a UnifiController template: the console URL plus a placeholder
// same-namespace secretRef the user must complete with a Secret holding their API key.
// It never reads the console and carries no credential. It returns a human-readable
// warning describing the required completion.
//
// The CRD requires spec.url to be https so the API key never travels in cleartext. A
// non-https controller URL is still emitted (the low-level client accepts http for local
// testing) but is annotated with AnnotationInsecureControllerURL and warned about, so the
// user switches to https before applying rather than having the API server reject it.
func Controller(url, name string) (emit.Resource, []string) {
	annotations := map[string]string{
		AnnotationControllerTemplate: "replace spec.secretRef with a same-namespace Secret holding your UniFi API key",
	}
	warnings := []string{fmt.Sprintf(
		"UnifiController %q is a template: set spec.secretRef to a same-namespace Secret holding your UniFi API key before applying",
		name)}
	if !strings.HasPrefix(url, "https://") {
		annotations[AnnotationInsecureControllerURL] = "true"
		warnings = append(warnings, fmt.Sprintf(
			"UnifiController %q: spec.url is not https://, but the operator requires an HTTPS controller URL; update spec.url to https:// before applying (annotation %s=true)",
			name, AnnotationInsecureControllerURL))
	}
	resource := emit.Resource{
		APIVersion: Group + "/" + Version,
		Kind:       KindUnifiController,
		Metadata: emit.Metadata{
			Name:        name,
			Annotations: annotations,
		},
		Spec: UnifiControllerSpec{
			URL:       url,
			SecretRef: &SecretKeySelector{Name: DefaultSecretName, Key: DefaultSecretKey},
		},
	}
	return resource, warnings
}

// Sites emits a UnifiSite for each upstream site and returns the deterministic
// metadata.name chosen for every upstream internalReference. Site names are derived with
// DNSSafeName; collisions get a stable numeric suffix. The returned name map is what
// Networks must use as siteRef.name so site and network references agree.
func Sites(controllerRef string, sites []unifi.Site) ([]emit.Resource, map[string]string, []string) {
	resources := make([]emit.Resource, 0, len(sites))
	names := make(map[string]string, len(sites))
	var warnings []string
	alloc := newNameAllocator()

	for _, site := range sites {
		base := DNSSafeName(site.InternalReference)
		name, collided := alloc.allocate(base)
		names[site.InternalReference] = name

		metadata := emit.Metadata{Name: name}
		if collided {
			metadata.Annotations = map[string]string{AnnotationOriginalName: site.InternalReference}
			warnings = append(warnings, fmt.Sprintf(
				"site %q: projected name %q is already in use; emitting as %q",
				site.InternalReference, base, name))
		}

		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiSite,
			Metadata:   metadata,
			Spec: UnifiSiteSpec{
				ControllerRef:     CoreRef{Name: controllerRef},
				InternalReference: site.InternalReference,
			},
		})
	}
	return resources, names, warnings
}

// Networks projects each UniFi network into a UnifiNetwork Custom Resource. siteName is
// the metadata.name of the target UnifiSite as returned by Sites, so siteRef.name always
// matches the emitted site. Colliding projected names get a stable numeric suffix and an
// AnnotationOriginalName annotation rather than overwriting one another.
func Networks(siteName string, networks []unifi.Network) ([]emit.Resource, []string) {
	resources := make([]emit.Resource, 0, len(networks))
	var warnings []string
	alloc := newNameAllocator()

	for _, network := range networks {
		spec, annotations, networkWarnings, ok := buildNetworkSpec(siteName, network)
		if !ok {
			warnings = append(warnings, networkWarnings...)
			continue
		}

		base := DNSSafeName(network.Name)
		name, collided := alloc.allocate(base)
		metadata := emit.Metadata{Name: name}
		if collided {
			if annotations == nil {
				annotations = map[string]string{}
			}
			annotations[AnnotationOriginalName] = network.Name
			networkWarnings = append(networkWarnings, fmt.Sprintf(
				"network %q: projected name %q is already in use; emitting as %q",
				network.Name, base, name))
		}
		if len(annotations) > 0 {
			metadata.Annotations = annotations
		}

		resources = append(resources, emit.Resource{
			APIVersion: Group + "/" + Version,
			Kind:       KindUnifiNetwork,
			Metadata:   metadata,
			Spec:       spec,
		})
		warnings = append(warnings, networkWarnings...)
	}
	return resources, warnings
}

// buildNetworkSpec maps one upstream network into a spec valid against the CRD. It
// returns ok=false (with a warning and no resource) when a required variant field is
// missing or cannot be represented faithfully, so the CLI never emits an invalid CR.
func buildNetworkSpec(siteName string, network unifi.Network) (UnifiNetworkSpec, map[string]string, []string, bool) {
	if network.VLANID < minVLAN || network.VLANID > maxVLAN {
		return UnifiNetworkSpec{}, nil, []string{fmt.Sprintf(
			"network %q: upstream vlanId %d is outside the CRD range %d..%d; skipping",
			network.Name, network.VLANID, minVLAN, maxVLAN)}, false
	}

	spec := UnifiNetworkSpec{
		SiteRef:    CoreRef{Name: siteName},
		Management: network.Management,
		Name:       network.Name,
		Enabled:    network.Enabled,
		VLANID:     network.VLANID,
	}
	if network.DHCPGuarding != nil && len(network.DHCPGuarding.TrustedDHCPServerIPAddresses) > 0 {
		spec.DHCPGuarding = &DHCPGuarding{
			TrustedDHCPServerIPAddresses: network.DHCPGuarding.TrustedDHCPServerIPAddresses,
		}
	}

	var annotations map[string]string
	var warnings []string

	switch network.Management {
	case ManagementGateway:
		gateway, gatewayWarnings, ok := gatewayOptions(network, &annotations)
		if !ok {
			return UnifiNetworkSpec{}, nil, gatewayWarnings, false
		}
		spec.Gateway = gateway
		warnings = append(warnings, gatewayWarnings...)
	case ManagementSwitch:
		switchOptions, switchWarnings, ok := switchNetworkOptions(network, &annotations)
		if !ok {
			return UnifiNetworkSpec{}, nil, switchWarnings, false
		}
		spec.Switch = switchOptions
		warnings = append(warnings, switchWarnings...)
	case ManagementUnmanaged:
		// Only the common fields are representable.
	default:
		return UnifiNetworkSpec{}, nil, []string{fmt.Sprintf(
			"network %q: unknown management %q; skipping", network.Name, network.Management)}, false
	}

	return spec, annotations, warnings, true
}

// gatewayOptions maps the observed gateway union into the emitted GATEWAY variant.
func gatewayOptions(network unifi.Network, annotations *map[string]string) (*GatewayNetworkOptions, []string, bool) {
	if network.CellularBackupEnabled == nil || network.InternetAccessEnabled == nil || network.IsolationEnabled == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: gateway configuration is missing from the controller response; skipping", network.Name)}, false
	}
	if network.IPv4Configuration == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: gateway ipv4Configuration is missing from the controller response; skipping", network.Name)}, false
	}
	ipv4, err := mapIPv4(network.IPv4Configuration)
	if err != nil {
		return nil, []string{fmt.Sprintf("network %q: %v; skipping", network.Name, err)}, false
	}

	gateway := &GatewayNetworkOptions{
		CellularBackupEnabled: *network.CellularBackupEnabled,
		InternetAccessEnabled: *network.InternetAccessEnabled,
		IsolationEnabled:      *network.IsolationEnabled,
		IPv4Configuration:     *ipv4,
		MDNSForwardingEnabled: network.MDNSForwardingEnabled,
	}

	var warnings []string
	if network.IPv6Configuration != nil {
		ipv6, reason, ok := mapIPv6(network.IPv6Configuration)
		if ok {
			gateway.IPv6Configuration = ipv6
		} else {
			setAnnotation(annotations, AnnotationUnrepresentedIPv6, reason)
			warnings = append(warnings, fmt.Sprintf("network %q: %s", network.Name, reason))
		}
	}
	return gateway, warnings, true
}

// switchNetworkOptions maps the observed switch union into the emitted SWITCH variant.
// The upstream device binding is an opaque deviceId; DeviceTagRef is emitted as a
// deterministic placeholder and the gap is recorded in an annotation and a warning. It
// returns ok=false when a required switch field is missing or invalid so no invalid CR
// is emitted.
func switchNetworkOptions(network unifi.Network, annotations *map[string]string) (*SwitchNetworkOptions, []string, bool) {
	if network.CellularBackupEnabled == nil || network.IsolationEnabled == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: switch configuration is missing from the controller response; skipping", network.Name)}, false
	}
	if network.IPv4Configuration == nil {
		return nil, []string{fmt.Sprintf(
			"network %q: switch ipv4Configuration is missing from the controller response; skipping", network.Name)}, false
	}
	ipv4, err := mapIPv4(network.IPv4Configuration)
	if err != nil {
		return nil, []string{fmt.Sprintf("network %q: %v; skipping", network.Name, err)}, false
	}

	reason := "upstream device binding is an opaque device id that cannot be expressed as a UnifiDeviceTag reference; spec.switch.deviceTagRef.name is a placeholder and must be set to the managing device tag"
	setAnnotation(annotations, AnnotationUnresolvedDeviceTag, reason)

	return &SwitchNetworkOptions{
		CellularBackupEnabled: *network.CellularBackupEnabled,
		IsolationEnabled:      *network.IsolationEnabled,
		IPv4Configuration:     *ipv4,
		DeviceTagRef:          CoreRef{Name: unresolvedDeviceTagName},
	}, []string{fmt.Sprintf("network %q: %s", network.Name, reason)}, true
}

// mapIPv4 validates and maps the observed IPv4 configuration.
func mapIPv4(in *unifi.IPv4Configuration) (*IPv4Configuration, error) {
	if in.HostIPAddress == "" || in.PrefixLength < minIPv4Prefix || in.PrefixLength > maxIPv4Prefix {
		return nil, fmt.Errorf(
			"ipv4Configuration is incomplete or out of range (hostIpAddress=%q prefixLength=%d)",
			in.HostIPAddress, in.PrefixLength)
	}
	return &IPv4Configuration{
		AutoScaleEnabled:        in.AutoScaleEnabled,
		HostIPAddress:           in.HostIPAddress,
		PrefixLength:            in.PrefixLength,
		AdditionalHostIPSubnets: in.AdditionalHostIPSubnets,
	}, nil
}

// mapIPv6 maps the observed IPv6 configuration. Prefix-delegation IPv6 references the WAN
// by opaque id, which the CR models by name; it cannot be represented without a WAN
// lookup, so it returns ok=false with a human-readable reason.
func mapIPv6(in *unifi.IPv6Configuration) (*IPv6Configuration, string, bool) {
	switch in.InterfaceType {
	case "STATIC":
		if in.HostIPAddress == "" || in.PrefixLength < minIPv6Prefix || in.PrefixLength > maxIPv6Prefix {
			return nil, "IPv6 static configuration is incomplete or out of range; ipv6Configuration was omitted", false
		}
		out := &IPv6Configuration{
			InterfaceType:                in.InterfaceType,
			ClientAddressAssignment:      IPv6ClientAddressAssignment{SLAACEnabled: in.ClientAddressAssignment.SLAACEnabled},
			AdditionalHostIPSubnets:      in.AdditionalHostIPSubnets,
			DNSServerIPAddressesOverride: in.DNSServerIPAddressesOverride,
			HostIPAddress:                in.HostIPAddress,
			PrefixLength:                 in.PrefixLength,
		}
		if in.RouterAdvertisement != nil {
			out.RouterAdvertisement = &RouterAdvertisement{Priority: in.RouterAdvertisement.Priority}
		}
		return out, "", true
	case "PREFIX_DELEGATION":
		return nil, "IPv6 prefix delegation selects the WAN by opaque id, which cannot be represented as a WAN name; ipv6Configuration was omitted", false
	default:
		return nil, fmt.Sprintf("unknown IPv6 interface type %q; ipv6Configuration was omitted", in.InterfaceType), false
	}
}

// setAnnotation records a non-empty annotation on the map, allocating it on first use.
func setAnnotation(annotations *map[string]string, key, value string) {
	if *annotations == nil {
		*annotations = map[string]string{}
	}
	(*annotations)[key] = value
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

// nameAllocator assigns deterministic, unique DNS-1123 names. Collisions get a stable
// numeric suffix; the suffix is assigned in input order, so the result is deterministic.
type nameAllocator struct {
	used map[string]bool
	next map[string]int
}

func newNameAllocator() *nameAllocator {
	return &nameAllocator{used: map[string]bool{}, next: map[string]int{}}
}

// allocate returns the name to use for base and whether base was already taken.
func (a *nameAllocator) allocate(base string) (string, bool) {
	if !a.used[base] {
		a.used[base] = true
		a.next[base] = 1
		return base, false
	}
	next := a.next[base]
	if next == 0 {
		next = 1
	}
	for {
		next++
		name := fmt.Sprintf("%s-%d", base, next)
		if !a.used[name] {
			a.used[name] = true
			a.next[base] = next
			return name, true
		}
	}
}
