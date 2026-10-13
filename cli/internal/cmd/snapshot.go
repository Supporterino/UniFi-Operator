package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Supporterino/UniFi-Operator/cli/internal/emit"
	"github.com/Supporterino/UniFi-Operator/cli/internal/snapshot"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

type snapshotOptions struct {
	controller    string
	controllerRef string
	site          string
	apiKey        string
	output        string
}

func newSnapshotCommand() *cobra.Command {
	opts := &snapshotOptions{}
	cmd := &cobra.Command{
		Use:   "snapshot [kind]",
		Short: "Read a UniFi controller and emit adoptable Custom Resources",
		Long: "snapshot reads objects from a UniFi Network controller over the Integration v1 API\n" +
			"and writes the equivalent Custom Resources as YAML. Supported kinds: all, controller,\n" +
			"sites, networks, firewall-zones, firewall-policies, wifi, radius, traffic-matching,\n" +
			"acl, dns, switching, vpn, device-tags. `all` emits a UnifiController template, the\n" +
			"UnifiSites, the UnifiNetworks, and the UnifiWifiBroadcasts of the site selected with\n" +
			"--site. `firewall-zones` emits the site's UnifiFirewallZones; `firewall-policies` emits\n" +
			"the site's UnifiFirewallPolicies (zone and matching-list references are resolved by\n" +
			"name); `wifi` emits the site's UnifiWifiBroadcasts (personal passphrases are emitted\n" +
			"as placeholder Secret references); `radius` emits the site's UnifiRadiusProfiles\n" +
			"(adopted by name); `traffic-matching` emits the site's UnifiTrafficMatchingLists;\n" +
			"`acl` emits the site's UnifiAclRules (network and device-tag references are resolved\n" +
			"by name); `dns` emits the site's UnifiDnsPolicies; `switching` emits the site's\n" +
			"UnifiSwitchingLags, UnifiSwitchingMcLagDomains, and UnifiSwitchingSwitchStacks\n" +
			"(adopted); `vpn` emits the site's UnifiVpnServers and UnifiSiteToSiteTunnels (adopted\n" +
			"by name); `device-tags` prints the read-only device-tag names and device counts\n" +
			"without emitting a resource.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := "all"
			if len(args) == 1 {
				kind = args[0]
			}
			// Resolve environment fallbacks only when the flag was not set. A non-empty
			// flag default would be echoed in --help, so secrets must never be defaults.
			if !cmd.Flags().Changed("controller") {
				opts.controller = os.Getenv("UNIFI_URL")
			}
			if !cmd.Flags().Changed("api-key") {
				opts.apiKey = os.Getenv("UNIFI_API_KEY")
			}
			return opts.run(cmd.Context(), kind, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.controller, "controller", "", "UniFi controller base URL (or UNIFI_URL)")
	flags.StringVar(&opts.controllerRef, "controller-ref", snapshot.DefaultControllerRef,
		"metadata.name of the UnifiController the emitted sites reference")
	flags.StringVar(&opts.site, "site", "default",
		"upstream site internalReference to snapshot networks from")
	flags.StringVar(&opts.apiKey, "api-key", "", "UniFi API key (or UNIFI_API_KEY); never logged")
	flags.StringVarP(&opts.output, "output", "o", "-", "output directory for CR files, or - for stdout")
	return cmd
}

func (o *snapshotOptions) run(ctx context.Context, kind string, out, warn io.Writer) error {
	switch kind {
	case "all", "controller", "sites", "networks", "firewall-zones", "firewall-policies", "wifi", "radius", "traffic-matching", "acl", "dns", "switching", "vpn", "device-tags":
	default:
		return fmt.Errorf(
			"unknown snapshot kind %q: supported kinds are all, controller, sites, networks, firewall-zones, firewall-policies, wifi, radius, traffic-matching, acl, dns, switching, vpn, device-tags",
			kind)
	}
	if o.controller == "" {
		return fmt.Errorf("--controller is required (or set UNIFI_URL)")
	}
	if o.controllerRef == "" {
		return fmt.Errorf("--controller-ref must not be empty")
	}

	client, err := unifi.NewClient(o.controller, o.apiKey, false)
	if err != nil {
		return err
	}

	needSites := kind != "controller"
	var sites []unifi.Site
	var siteResources []emit.Resource
	var siteNames map[string]string
	var siteWarnings []string
	if needSites {
		sites, err = client.ListSites(ctx)
		if err != nil {
			return fmt.Errorf("snapshot sites: %w", err)
		}
		siteResources, siteNames, siteWarnings = snapshot.Sites(o.controllerRef, sites)
	}

	var resources []emit.Resource
	var warnings []string

	if kind == "controller" || kind == "all" {
		controller, controllerWarnings := snapshot.Controller(o.controller, o.controllerRef)
		resources = append(resources, controller)
		warnings = append(warnings, controllerWarnings...)
	}
	if kind == "sites" || kind == "all" {
		resources = append(resources, siteResources...)
		warnings = append(warnings, siteWarnings...)
	}
	if kind == "device-tags" {
		_, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		tags, err := client.ListDeviceTags(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot device-tags: %w", err)
		}
		return writeDeviceTags(out, tags)
	}
	if kind == "radius" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		profiles, err := client.ListRadiusProfiles(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot radius: %w", err)
		}
		radiusResources, radiusWarnings := snapshot.RadiusProfiles(siteName, profiles)
		resources = append(resources, radiusResources...)
		warnings = append(warnings, radiusWarnings...)
	}
	if kind == "traffic-matching" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		lists, err := client.ListTrafficMatchingLists(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot traffic-matching: %w", err)
		}
		listResources, listWarnings := snapshot.TrafficMatchingLists(siteName, lists)
		resources = append(resources, listResources...)
		warnings = append(warnings, listWarnings...)
	}
	if kind == "firewall-policies" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		zones, err := client.ListZones(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot firewall-policies: %w", err)
		}
		policies, err := client.ListFirewallPolicies(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot firewall-policies: %w", err)
		}
		// The matching-list endpoints are only needed when a policy carries a traffic
		// filter, so fetch them lazily: a console without the lists endpoint can still
		// snapshot filter-free policies.
		var lists []unifi.TrafficMatchingList
		if anyFirewallPolicyFilter(policies) {
			lists, err = client.ListTrafficMatchingLists(ctx, siteID)
			if err != nil {
				return fmt.Errorf("snapshot firewall-policies: %w", err)
			}
		}
		policyResources, policyWarnings := snapshot.FirewallPolicies(siteName, zones, lists, policies)
		resources = append(resources, policyResources...)
		warnings = append(warnings, policyWarnings...)
	}
	if kind == "acl" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		// Network membership is referenced by Kubernetes identity, so load the network
		// projection the `networks` command emits and reuse its UUID -> name map.
		detailed, tags, loadWarnings, err := loadNetworkProjection(ctx, client, siteID)
		if err != nil {
			return fmt.Errorf("snapshot acl: %w", err)
		}
		warnings = append(warnings, loadWarnings...)
		_, networkNames, networkWarnings := snapshot.Networks(siteName, detailed, tags)
		warnings = append(warnings, networkWarnings...)

		rules, err := client.ListAclRules(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot acl: %w", err)
		}
		// The device-tag list resolves an enforcing-device UUID to a tag name; it is only
		// needed when a rule scopes enforcement.
		deviceTags := tags
		if anyAclEnforcingFilter(rules) && deviceTags == nil {
			deviceTags, err = client.ListDeviceTags(ctx, siteID)
			if err != nil {
				return fmt.Errorf("snapshot acl: %w", err)
			}
		}
		ruleResources, ruleWarnings := snapshot.AclRules(siteName, networkNames, deviceTags, rules)
		resources = append(resources, ruleResources...)
		warnings = append(warnings, ruleWarnings...)
	}
	if kind == "dns" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		policies, err := client.ListDnsPolicies(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot dns: %w", err)
		}
		dnsResources, dnsWarnings := snapshot.DnsPolicies(siteName, policies)
		resources = append(resources, dnsResources...)
		warnings = append(warnings, dnsWarnings...)
	}
	if kind == "switching" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		lags, err := client.ListLags(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot switching: %w", err)
		}
		domains, err := client.ListMcLagDomains(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot switching: %w", err)
		}
		stacks, err := client.ListSwitchStacks(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot switching: %w", err)
		}
		// The device-tag list resolves a LAG member device UUID to a tag name; it is only
		// needed when at least one LAG is present.
		var deviceTags []unifi.DeviceTag
		if len(lags) > 0 {
			deviceTags, err = client.ListDeviceTags(ctx, siteID)
			if err != nil {
				return fmt.Errorf("snapshot switching: %w", err)
			}
		}
		lagResources, lagWarnings := snapshot.SwitchingLags(siteName, deviceTags, lags)
		resources = append(resources, lagResources...)
		warnings = append(warnings, lagWarnings...)
		domainResources, domainWarnings := snapshot.SwitchingMcLagDomains(siteName, domains)
		resources = append(resources, domainResources...)
		warnings = append(warnings, domainWarnings...)
		stackResources, stackWarnings := snapshot.SwitchingSwitchStacks(siteName, stacks)
		resources = append(resources, stackResources...)
		warnings = append(warnings, stackWarnings...)
	}
	if kind == "vpn" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		servers, err := client.ListVpnServers(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot vpn: %w", err)
		}
		tunnels, err := client.ListSiteToSiteTunnels(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot vpn: %w", err)
		}
		serverResources, serverWarnings := snapshot.VpnServers(siteName, servers)
		resources = append(resources, serverResources...)
		warnings = append(warnings, serverWarnings...)
		tunnelResources, tunnelWarnings := snapshot.SiteToSiteTunnels(siteName, tunnels)
		resources = append(resources, tunnelResources...)
		warnings = append(warnings, tunnelWarnings...)
	}
	if kind == "networks" || kind == "all" || kind == "firewall-zones" || kind == "wifi" {
		siteName, siteID, err := resolveSite(sites, siteNames, o.site)
		if err != nil {
			return err
		}
		detailed, tags, loadWarnings, err := loadNetworkProjection(ctx, client, siteID)
		if err != nil {
			return err
		}
		warnings = append(warnings, loadWarnings...)

		networkResources, networkNames, networkWarnings := snapshot.Networks(siteName, detailed, tags)
		warnings = append(warnings, networkWarnings...)
		if kind == "networks" || kind == "all" {
			resources = append(resources, networkResources...)
		}
		if kind == "firewall-zones" {
			zones, err := client.ListZones(ctx, siteID)
			if err != nil {
				return fmt.Errorf("snapshot firewall-zones: %w", err)
			}
			zoneResources, zoneWarnings := snapshot.FirewallZones(siteName, networkNames, zones)
			resources = append(resources, zoneResources...)
			warnings = append(warnings, zoneWarnings...)
		}
		if kind == "wifi" || kind == "all" {
			broadcasts, broadcastTags, broadcastWarnings, err := loadWifiProjection(ctx, client, siteID)
			if err != nil {
				return err
			}
			warnings = append(warnings, broadcastWarnings...)
			broadcastResources, projectWarnings := snapshot.WifiBroadcasts(networkNames, siteName, broadcastTags, broadcasts)
			resources = append(resources, broadcastResources...)
			warnings = append(warnings, projectWarnings...)
		}
	}

	for _, w := range warnings {
		if _, err := fmt.Fprintf(warn, "warning: %s\n", w); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return emitResources(out, o.output, resources)
}

// resolveSite returns the emitted UnifiSite name and the upstream UUID for the site
// selected by internalReference. It fails closed with an actionable error when the site is
// absent or has no upstream id.
func resolveSite(sites []unifi.Site, siteNames map[string]string, internalReference string) (string, string, error) {
	siteName, ok := siteNames[internalReference]
	if !ok {
		return "", "", fmt.Errorf("site %q not found on the controller (available: %s)",
			internalReference, strings.Join(siteReferences(sites), ", "))
	}
	siteID, ok := findSiteID(sites, internalReference)
	if !ok {
		return "", "", fmt.Errorf("site %q has no upstream id", internalReference)
	}
	return siteName, siteID, nil
}

// findSiteID returns the upstream UUID of the site whose internalReference matches.
func findSiteID(sites []unifi.Site, internalReference string) (string, bool) {
	for _, site := range sites {
		if site.InternalReference == internalReference {
			return site.ID, true
		}
	}
	return "", false
}

// loadNetworkProjection reads every network of the site with its details and, only when a
// switch-managed network is present, the read-only device tags needed to reverse-map the
// switch device binding. Fetching the tags lazily keeps a console that supports networks
// but not device tags able to snapshot networks that carry no switch binding; when a
// switch binding does need the tags and the list fails, the snapshot fails closed rather
// than emitting an unresolvable selector.
func loadNetworkProjection(ctx context.Context, client *unifi.Client, siteID string) ([]unifi.Network, []unifi.DeviceTag, []string, error) {
	networks, err := client.ListNetworks(ctx, siteID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("snapshot networks: %w", err)
	}
	detailed, warnings, err := loadNetworkDetails(ctx, client, siteID, networks)
	if err != nil {
		return nil, nil, nil, err
	}
	if !anySwitchManaged(detailed) {
		return detailed, nil, warnings, nil
	}
	tags, err := client.ListDeviceTags(ctx, siteID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("snapshot device-tags: %w", err)
	}
	return detailed, tags, warnings, nil
}

// anySwitchManaged reports whether any network uses the SWITCH management variant.
func anySwitchManaged(networks []unifi.Network) bool {
	for _, network := range networks {
		if network.Management == snapshot.ManagementSwitch {
			return true
		}
	}
	return false
}

// writeDeviceTags prints one "name<TAB>device-count" line per read-only device tag. There
// is no device-tag Custom Resource to emit, so the listing is a discovery aid only.
func writeDeviceTags(out io.Writer, tags []unifi.DeviceTag) error {
	for _, tag := range tags {
		if _, err := fmt.Fprintf(out, "%s\t%d\n", tag.Name, len(tag.DeviceIDs)); err != nil {
			return fmt.Errorf("write device tag %q: %w", tag.Name, err)
		}
	}
	return nil
}

// siteReferences lists the upstream internalReferences for an error message. It never
// contains a credential.
func siteReferences(sites []unifi.Site) []string {
	refs := make([]string, 0, len(sites))
	for _, site := range sites {
		refs = append(refs, site.InternalReference)
	}
	return refs
}

// loadNetworkDetails resolves the full detail of every listed network. The list endpoint
// returns only overview fields, so the management-variant configuration is read per
// network. A network that disappeared between the list and the detail read (404) is
// skipped with a warning; any other error is returned so the snapshot fails closed rather
// than emitting a partial projection.
func loadNetworkDetails(ctx context.Context, client *unifi.Client, siteID string, networks []unifi.Network) ([]unifi.Network, []string, error) {
	details := make([]unifi.Network, 0, len(networks))
	var warnings []string
	for _, network := range networks {
		detail, err := client.GetNetwork(ctx, siteID, network.ID)
		if err != nil {
			var apiErr *unifi.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				warnings = append(warnings, fmt.Sprintf(
					"network %q: disappeared before its details could be read; skipping", network.Name))
				continue
			}
			return nil, nil, fmt.Errorf("read network %q details: %w", network.Name, err)
		}
		details = append(details, detail)
	}
	return details, warnings, nil
}

// loadWifiProjection reads every WiFi broadcast of the site with its details and, only
// when a DEVICE_TAGS scope is present, the read-only device tags needed to reverse-map the
// scope to tag names. Fetching the tags lazily keeps a console whose broadcasts set no
// device scope able to snapshot without the extra call.
func loadWifiProjection(ctx context.Context, client *unifi.Client, siteID string) ([]unifi.WifiBroadcast, []unifi.DeviceTag, []string, error) {
	overviews, err := client.ListWifiBroadcasts(ctx, siteID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("snapshot wifi: %w", err)
	}
	detailed, warnings, err := loadWifiBroadcastDetails(ctx, client, siteID, overviews)
	if err != nil {
		return nil, nil, nil, err
	}
	if !anyWifiDeviceTagFilter(detailed) {
		return detailed, nil, warnings, nil
	}
	tags, err := client.ListDeviceTags(ctx, siteID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("snapshot device-tags: %w", err)
	}
	return detailed, tags, warnings, nil
}

// anyWifiDeviceTagFilter reports whether any broadcast scopes to a DEVICE_TAGS filter, the
// only scope that needs the device-tag list to reverse-map to names.
func anyWifiDeviceTagFilter(broadcasts []unifi.WifiBroadcast) bool {
	for _, broadcast := range broadcasts {
		if broadcast.BroadcastingDeviceFilter != nil && broadcast.BroadcastingDeviceFilter.Type == "DEVICE_TAGS" {
			return true
		}
	}
	return false
}

// anyFirewallPolicyFilter reports whether any policy carries a source or destination
// traffic filter, the only case that needs the traffic-matching-list endpoint to resolve a
// filter's list reference to a name.
func anyFirewallPolicyFilter(policies []unifi.FirewallPolicy) bool {
	for _, policy := range policies {
		if policy.Source.TrafficFilter != nil || policy.Destination.TrafficFilter != nil {
			return true
		}
	}
	return false
}

// anyAclEnforcingFilter reports whether any ACL rule scopes enforcement to specific
// devices, the only case that needs the device-tag list to resolve a device UUID to a tag
// name.
func anyAclEnforcingFilter(rules []unifi.AclRule) bool {
	for _, rule := range rules {
		if rule.EnforcingDeviceFilter != nil {
			return true
		}
	}
	return false
}

// loadWifiBroadcastDetails resolves the full detail of every listed broadcast. The list
// endpoint returns only overview fields, so the variant configuration is read per
// broadcast. A broadcast that disappeared between the list and the detail read (404) is
// skipped with a warning; any other error is returned so the snapshot fails closed rather
// than emitting a partial projection.
func loadWifiBroadcastDetails(ctx context.Context, client *unifi.Client, siteID string, overviews []unifi.WifiBroadcastOverview) ([]unifi.WifiBroadcast, []string, error) {
	details := make([]unifi.WifiBroadcast, 0, len(overviews))
	var warnings []string
	for _, overview := range overviews {
		detail, err := client.GetWifiBroadcast(ctx, siteID, overview.ID)
		if err != nil {
			var apiErr *unifi.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				warnings = append(warnings, fmt.Sprintf(
					"wifi broadcast %q: disappeared before its details could be read; skipping", overview.Name))
				continue
			}
			return nil, nil, fmt.Errorf("read wifi broadcast %q details: %w", overview.Name, err)
		}
		details = append(details, detail)
	}
	return details, warnings, nil
}

func emitResources(out io.Writer, output string, resources []emit.Resource) error {
	toStdout := output == "-"
	if !toStdout {
		if err := os.MkdirAll(output, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	for _, r := range resources {
		data, err := emit.Marshal(r)
		if err != nil {
			return err
		}
		if toStdout {
			if _, err := fmt.Fprintf(out, "---\n%s", data); err != nil {
				return fmt.Errorf("write %s: %w", r.Metadata.Name, err)
			}
			continue
		}
		file := fmt.Sprintf("%s-%s.yaml", strings.ToLower(r.Kind), r.Metadata.Name)
		if err := os.WriteFile(filepath.Join(output, file), data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file, err)
		}
	}
	return nil
}
