package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
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
			"sites, networks, firewall-zones, firewall-policies, firewall-policy-ordering, wifi,\n" +
			"radius, traffic-matching, acl, acl-rule-ordering, dns, switching, vpn, device-tags.\n" +
			"`all` emits every projectable kind across every site on the console; it ignores\n" +
			"--site. `firewall-zones` emits the\n" +
			"site's UnifiFirewallZones; `firewall-policies` emits the site's UnifiFirewallPolicies\n" +
			"(zone and matching-list references are resolved by name); `firewall-policy-ordering`\n" +
			"emits the site's UnifiFirewallPolicyOrderings (one per user-defined zone pair; policy\n" +
			"references are resolved by name and the system-defined partition is anchored); `wifi`\n" +
			"emits the site's UnifiWifiBroadcasts (personal passphrases are emitted as placeholder\n" +
			"Secret references); `radius` emits the site's UnifiRadiusProfiles (adopted by name);\n" +
			"`traffic-matching` emits the site's UnifiTrafficMatchingLists; `acl` emits the site's\n" +
			"UnifiAclRules (network and device-tag references are resolved by name);\n" +
			"`acl-rule-ordering` emits the site's UnifiAclRuleOrdering (rule references are resolved\n" +
			"by name in upstream order); `dns` emits the site's UnifiDnsPolicies; `switching` emits\n" +
			"the site's UnifiSwitchingLags, UnifiSwitchingMcLagDomains, and UnifiSwitchingSwitchStacks\n" +
			"(adopted); `vpn` emits the site's UnifiVpnServers and UnifiSiteToSiteTunnels (adopted\n" +
			"by name); `device-tags` prints the read-only device-tag names and device counts\n" +
			"without emitting a resource. The `verify` subcommand re-reads the live console and\n" +
			"reports coverage and fidelity gaps in a previously written snapshot directory.",
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
		"upstream site internalReference the per-kind commands snapshot (ignored by `all`)")
	flags.StringVar(&opts.apiKey, "api-key", "", "UniFi API key (or UNIFI_API_KEY); never logged")
	flags.StringVarP(&opts.output, "output", "o", "-", "output directory for CR files, or - for stdout")
	cmd.AddCommand(newSnapshotVerifyCommand())
	return cmd
}

func (o *snapshotOptions) run(ctx context.Context, kind string, out, warn io.Writer) error {
	if !acceptedKind(kind) {
		return fmt.Errorf(
			"unknown snapshot kind %q: supported kinds are %s",
			kind, strings.Join(acceptedKinds, ", "))
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

	var resources []emit.Resource
	var warnings []string

	switch {
	case kind == allKind:
		resources, warnings, err = o.projectAll(ctx, client)
	case slices.Contains(nonProjectableKinds, kind):
		return o.runNonProjectable(ctx, client, kind, out)
	default:
		resources, warnings, err = o.projectKind(ctx, client, kind)
	}
	if err != nil {
		return err
	}

	for _, w := range warnings {
		if _, err := fmt.Fprintf(warn, "warning: %s\n", w); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return emitResources(out, o.output, resources)
}

// allKind is the aggregate kind that projects every projectable kind across every site;
// deviceTagsKind is the read-only device-tag listing that emits no Custom Resource.
const (
	allKind        = "all"
	deviceTagsKind = "device-tags"
)

// acceptedKinds lists every `snapshot <kind>` argument the command accepts, in the order
// the --help text presents them. Validation is driven by this list; it is kept in lockstep
// with the projection lists (projectableKinds and nonProjectableKinds) by
// TestSnapshotAllCoversEveryAcceptedKind, so a kind cannot be added to one without the
// other.
var acceptedKinds = []string{
	allKind, "controller", "sites", "networks", "firewall-zones", "firewall-policies",
	"firewall-policy-ordering", "wifi", "radius", "traffic-matching", "acl",
	"acl-rule-ordering", "dns", "switching", "vpn", deviceTagsKind,
}

// nonProjectableKinds lists accepted kinds that emit no Custom Resource because they are
// read-only listings. They are excluded from `all`.
var nonProjectableKinds = []string{deviceTagsKind}

// runNonProjectable dispatches a read-only listing kind to its handler. The dispatch keys
// off the same list acceptedKind validates against, so a kind added to nonProjectableKinds
// is routed here rather than falling through to projectKind; one with no handler is
// reported with its own error instead of projectKind's misleading "unknown snapshot kind".
func (o *snapshotOptions) runNonProjectable(ctx context.Context, client *unifi.Client, kind string, out io.Writer) error {
	switch kind {
	case deviceTagsKind:
		return o.listDeviceTags(ctx, client, out)
	default:
		return fmt.Errorf("snapshot kind %q is a read-only listing with no handler", kind)
	}
}

// kindScope describes how `snapshot all` projects a kind.
type kindScope int

const (
	// scopeSnapshot kinds are projected once per snapshot and need no site context.
	scopeSnapshot kindScope = iota
	// scopeSites kinds are projected once per snapshot from the full upstream site list.
	scopeSites
	// scopeSite kinds are projected once per site on the console.
	scopeSite
)

// siteRef is the emitted metadata.name, the upstream id, and the site qualifier of the site
// a site-scoped projection runs against. namePrefix is the `<site>-` qualifier `all` and
// `verify` prepend to every site-scoped resource name so two sites cannot emit the same
// (kind, metadata.name); it is empty for a per-kind invocation, which projects one site
// (design D7).
type siteRef struct {
	name       string
	id         string
	namePrefix string
}

// projectionInput is what every kindProjection reads. A site-scoped projection reads site;
// the `sites` projection reads sites; the `controller` projection reads neither.
type projectionInput struct {
	client        *unifi.Client
	controllerURL string
	controllerRef string
	sites         []unifi.Site
	site          siteRef
}

// kindProjection is a pure read-and-project step for one snapshot kind. It reads whatever
// upstream objects the kind needs and returns the emitted resources plus human-readable
// warnings. It performs no I/O other than reading the controller and never mutates
// upstream, so the same step is reused by `snapshot all`, a per-kind `snapshot <kind>`
// invocation, and (later) `snapshot verify`, which projects live state in memory to
// compare against a written snapshot directory. Orchestration - the site loop, warning
// output, and file or stdout emission - lives outside the projection.
type kindProjection func(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error)

// projectableKind is one entry in projectableKinds.
type projectableKind struct {
	kind  string
	scope kindScope
	// project is the kind's read-and-project step.
	project kindProjection
}

// projectableKinds is the single ordered source of truth for every kind `snapshot` can
// project. `snapshot all` iterates it, projecting the scopeSnapshot and scopeSites kinds
// once and every scopeSite kind once per site; a per-kind invocation looks its kind up
// here. A kind therefore cannot be added to the dispatch without `all` picking it up, and
// TestSnapshotAllCoversEveryAcceptedKind enforces the coupling against acceptedKinds,
// nonProjectableKinds, and the projections themselves.
var projectableKinds = []projectableKind{
	{kind: "controller", scope: scopeSnapshot, project: projectController},
	{kind: "sites", scope: scopeSites, project: projectSites},
	{kind: "networks", scope: scopeSite, project: projectNetworks},
	{kind: "firewall-zones", scope: scopeSite, project: projectFirewallZones},
	{kind: "firewall-policies", scope: scopeSite, project: projectFirewallPolicies},
	{kind: "firewall-policy-ordering", scope: scopeSite, project: projectFirewallPolicyOrdering},
	{kind: "wifi", scope: scopeSite, project: projectWifi},
	{kind: "radius", scope: scopeSite, project: projectRadius},
	{kind: "traffic-matching", scope: scopeSite, project: projectTrafficMatching},
	{kind: "acl", scope: scopeSite, project: projectAcl},
	{kind: "acl-rule-ordering", scope: scopeSite, project: projectAclRuleOrdering},
	{kind: "dns", scope: scopeSite, project: projectDns},
	{kind: "switching", scope: scopeSite, project: projectSwitching},
	{kind: "vpn", scope: scopeSite, project: projectVpn},
}

// acceptedKind reports whether kind is a kind the snapshot command accepts: the `all`
// aggregate, a projectable kind, or a non-projectable listing kind.
func acceptedKind(kind string) bool {
	if kind == allKind {
		return true
	}
	if _, ok := projectableKindFor(kind); ok {
		return true
	}
	return slices.Contains(nonProjectableKinds, kind)
}

// projectableKindFor returns the registry entry for kind, if it is a projectable kind.
func projectableKindFor(kind string) (projectableKind, bool) {
	for _, entry := range projectableKinds {
		if entry.kind == kind {
			return entry, true
		}
	}
	return projectableKind{}, false
}

// projectAll projects every projectable kind across every site on the console. It reads
// the site list once, then iterates projectableKinds: the site-agnostic kinds are projected
// once and each site-scoped kind once per site, against that site's emitted name and
// upstream id. It deliberately ignores --site, which only restricts the per-kind commands.
func (o *snapshotOptions) projectAll(ctx context.Context, client *unifi.Client) ([]emit.Resource, []string, error) {
	sites, err := client.ListSites(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot sites: %w", err)
	}
	// Site-scoped resources must name the site as the `sites` kind emits it, including any
	// collision suffix, so read the emitted names from the same pure projection. Sites is
	// deterministic and side-effect free, so recomputing its name map here (the `sites`
	// projection does the same for the resources it emits) is safe.
	_, siteNames, _ := snapshot.Sites(o.controllerRef, sites)

	input := projectionInput{
		client:        client,
		controllerURL: o.controller,
		controllerRef: o.controllerRef,
		sites:         sites,
	}
	var resources []emit.Resource
	// Several kinds share one projection - the network projection backs networks,
	// firewall-zones, wifi, acl, and acl-rule-ordering - so the same projection warning is
	// produced once per kind
	// per site it feeds. Collapse exact duplicates here, at the orchestrator boundary, so the
	// kinds stay pure and a shared warning is reported once rather than once per consumer.
	// First-seen order is preserved to keep the output deterministic; a distinct warning is
	// never merged into another.
	seenWarnings := make(map[string]bool)
	var warnings []string
	appendWarnings := func(entryWarnings []string) {
		for _, warning := range entryWarnings {
			if seenWarnings[warning] {
				continue
			}
			seenWarnings[warning] = true
			warnings = append(warnings, warning)
		}
	}
	for _, entry := range projectableKinds {
		if entry.scope != scopeSite {
			res, entryWarnings, err := entry.project(ctx, input)
			if err != nil {
				return nil, nil, err
			}
			resources = append(resources, res...)
			appendWarnings(entryWarnings)
			continue
		}
		for _, site := range sites {
			siteInput := input
			siteName := siteNames[site.InternalReference]
			siteInput.site = siteRef{name: siteName, id: site.ID, namePrefix: siteName + "-"}
			res, entryWarnings, err := entry.project(ctx, siteInput)
			if err != nil {
				return nil, nil, err
			}
			resources = append(resources, res...)
			appendWarnings(entryWarnings)
		}
	}
	return resources, warnings, nil
}

// projectKind projects a single kind for a per-kind `snapshot <kind>` invocation. A
// site-scoped kind is resolved to the site named by --site (the default site when the flag
// is unset); the site-agnostic `sites` kind runs against the full site list.
func (o *snapshotOptions) projectKind(ctx context.Context, client *unifi.Client, kind string) ([]emit.Resource, []string, error) {
	entry, ok := projectableKindFor(kind)
	if !ok {
		return nil, nil, fmt.Errorf("unknown snapshot kind %q", kind)
	}
	input := projectionInput{
		client:        client,
		controllerURL: o.controller,
		controllerRef: o.controllerRef,
	}
	if entry.scope == scopeSnapshot {
		return entry.project(ctx, input)
	}

	sites, err := client.ListSites(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot sites: %w", err)
	}
	input.sites = sites
	if entry.scope == scopeSites {
		return entry.project(ctx, input)
	}

	// Site-scoped: only the emitted name map is needed here; the collision warnings Sites
	// produces are output by the `sites` kind, so they are not repeated per kind. The site
	// qualifier is deliberately left empty: a per-kind command projects a single site, so it
	// keeps the upstream-derived names its goldens capture (design D7).
	_, siteNames, _ := snapshot.Sites(o.controllerRef, sites)
	siteName, siteID, err := resolveSite(sites, siteNames, o.site)
	if err != nil {
		return nil, nil, err
	}
	input.site = siteRef{name: siteName, id: siteID}
	return entry.project(ctx, input)
}

// listDeviceTags prints the read-only device-tag listing for the site named by --site.
// device-tags emits no Custom Resource, so it is not a projectable kind and is excluded
// from `all`.
func (o *snapshotOptions) listDeviceTags(ctx context.Context, client *unifi.Client, out io.Writer) error {
	sites, err := client.ListSites(ctx)
	if err != nil {
		return fmt.Errorf("snapshot sites: %w", err)
	}
	_, siteNames, _ := snapshot.Sites(o.controllerRef, sites)
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

// projectController emits the UnifiController template. It never reads the console.
func projectController(_ context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	resource, warnings := snapshot.Controller(in.controllerURL, in.controllerRef)
	return []emit.Resource{resource}, warnings, nil
}

// projectSites emits a UnifiSite per upstream site. The emitted-name map it also builds is
// recomputed by the orchestrator for the site-scoped kinds.
func projectSites(_ context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	resources, _, warnings := snapshot.Sites(in.controllerRef, in.sites)
	return resources, warnings, nil
}

func projectNetworks(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	detailed, tags, warnings, err := loadNetworkProjection(ctx, in.client, in.site.id)
	if err != nil {
		return nil, nil, err
	}
	resources, _, networkWarnings := snapshot.Networks(in.site.name, in.site.namePrefix, detailed, tags)
	warnings = append(warnings, networkWarnings...)
	return resources, warnings, nil
}

func projectFirewallZones(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	detailed, tags, warnings, err := loadNetworkProjection(ctx, in.client, in.site.id)
	if err != nil {
		return nil, nil, err
	}
	// A zone's member networks are referenced by Kubernetes identity, so load the network
	// projection (for its UUID -> name map) the `networks` kind emits.
	_, networkNames, networkWarnings := snapshot.Networks(in.site.name, in.site.namePrefix, detailed, tags)
	warnings = append(warnings, networkWarnings...)

	zones, err := in.client.ListZones(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot firewall-zones: %w", err)
	}
	resources, zoneWarnings := snapshot.FirewallZones(in.site.name, in.site.namePrefix, networkNames, zones)
	warnings = append(warnings, zoneWarnings...)
	return resources, warnings, nil
}

func projectFirewallPolicies(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	zones, err := in.client.ListZones(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot firewall-policies: %w", err)
	}
	policies, err := in.client.ListFirewallPolicies(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot firewall-policies: %w", err)
	}
	// The matching-list endpoints are only needed when a policy carries a traffic filter,
	// so fetch them lazily: a console without the lists endpoint can still snapshot
	// filter-free policies.
	var lists []unifi.TrafficMatchingList
	if anyFirewallPolicyFilter(policies) {
		lists, err = in.client.ListTrafficMatchingLists(ctx, in.site.id)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot firewall-policies: %w", err)
		}
	}
	resources, _, warnings := snapshot.FirewallPolicies(in.site.name, in.site.namePrefix, zones, lists, policies)
	return resources, warnings, nil
}

func projectFirewallPolicyOrdering(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	zones, err := in.client.ListZones(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot firewall-policy-ordering: %w", err)
	}
	policies, err := in.client.ListFirewallPolicies(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot firewall-policy-ordering: %w", err)
	}
	// The matching-list endpoints are only needed when a policy carries a traffic filter,
	// so fetch them lazily so the policy-name map agrees with the `firewall-policies`
	// projection (an unresolvable filter would otherwise drop a policy the ordering
	// references).
	var lists []unifi.TrafficMatchingList
	if anyFirewallPolicyFilter(policies) {
		lists, err = in.client.ListTrafficMatchingLists(ctx, in.site.id)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot firewall-policy-ordering: %w", err)
		}
	}
	// Reuse the policy projection's upstream-id -> emitted-name map rather than re-deriving
	// names (design D2); its resources are not emitted by this kind.
	_, policyNames, warnings := snapshot.FirewallPolicies(in.site.name, in.site.namePrefix, zones, lists, policies)

	orderings := make(map[snapshot.FirewallPolicyZonePair]unifi.FirewallPolicyOrdering)
	for _, pair := range snapshot.FirewallPolicyZonePairs(policies) {
		ordering, err := in.client.GetFirewallPolicyOrdering(ctx, in.site.id, pair.SourceZoneID, pair.DestinationZoneID)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot firewall-policy-ordering: %w", err)
		}
		orderings[pair] = ordering
	}
	resources, orderingWarnings := snapshot.FirewallPolicyOrdering(in.site.name, in.site.namePrefix, zones, policies, policyNames, orderings)
	warnings = append(warnings, orderingWarnings...)
	return resources, warnings, nil
}

func projectWifi(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	detailed, tags, warnings, err := loadNetworkProjection(ctx, in.client, in.site.id)
	if err != nil {
		return nil, nil, err
	}
	// A SPECIFIC broadcast references an emitted UnifiNetwork by name.
	_, networkNames, networkWarnings := snapshot.Networks(in.site.name, in.site.namePrefix, detailed, tags)
	warnings = append(warnings, networkWarnings...)

	broadcasts, broadcastTags, broadcastWarnings, err := loadWifiProjection(ctx, in.client, in.site.id)
	if err != nil {
		return nil, nil, err
	}
	warnings = append(warnings, broadcastWarnings...)
	resources, projectWarnings := snapshot.WifiBroadcasts(networkNames, in.site.name, in.site.namePrefix, broadcastTags, broadcasts)
	warnings = append(warnings, projectWarnings...)
	return resources, warnings, nil
}

func projectRadius(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	profiles, err := in.client.ListRadiusProfiles(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot radius: %w", err)
	}
	resources, warnings := snapshot.RadiusProfiles(in.site.name, in.site.namePrefix, profiles)
	return resources, warnings, nil
}

func projectTrafficMatching(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	lists, err := in.client.ListTrafficMatchingLists(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot traffic-matching: %w", err)
	}
	resources, warnings := snapshot.TrafficMatchingLists(in.site.name, in.site.namePrefix, lists)
	return resources, warnings, nil
}

func projectAcl(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	// Network membership is referenced by Kubernetes identity, so load the network
	// projection the `networks` command emits and reuse its UUID -> name map.
	detailed, tags, warnings, err := loadNetworkProjection(ctx, in.client, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot acl: %w", err)
	}
	_, networkNames, networkWarnings := snapshot.Networks(in.site.name, in.site.namePrefix, detailed, tags)
	warnings = append(warnings, networkWarnings...)

	rules, err := in.client.ListAclRules(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot acl: %w", err)
	}
	// The device-tag list resolves an enforcing-device UUID to a tag name; it is only
	// needed when a rule scopes enforcement.
	deviceTags := tags
	if anyAclEnforcingFilter(rules) && deviceTags == nil {
		deviceTags, err = in.client.ListDeviceTags(ctx, in.site.id)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot acl: %w", err)
		}
	}
	resources, _, ruleWarnings := snapshot.AclRules(in.site.name, in.site.namePrefix, networkNames, deviceTags, rules)
	warnings = append(warnings, ruleWarnings...)
	return resources, warnings, nil
}

func projectAclRuleOrdering(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	detailed, tags, warnings, err := loadNetworkProjection(ctx, in.client, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot acl-rule-ordering: %w", err)
	}
	_, networkNames, networkWarnings := snapshot.Networks(in.site.name, in.site.namePrefix, detailed, tags)
	warnings = append(warnings, networkWarnings...)

	rules, err := in.client.ListAclRules(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot acl-rule-ordering: %w", err)
	}
	deviceTags := tags
	if anyAclEnforcingFilter(rules) && deviceTags == nil {
		deviceTags, err = in.client.ListDeviceTags(ctx, in.site.id)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot acl-rule-ordering: %w", err)
		}
	}
	// Reuse the rule projection's upstream-id -> emitted-name map rather than re-deriving
	// names (design D2); its resources are not emitted by this kind.
	_, ruleNames, ruleWarnings := snapshot.AclRules(in.site.name, in.site.namePrefix, networkNames, deviceTags, rules)
	warnings = append(warnings, ruleWarnings...)

	orderedRuleIDs, err := in.client.ListAclRuleOrdering(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot acl-rule-ordering: %w", err)
	}
	resources, orderingWarnings := snapshot.AclRuleOrdering(in.site.name, in.site.namePrefix, ruleNames, orderedRuleIDs)
	warnings = append(warnings, orderingWarnings...)
	return resources, warnings, nil
}

func projectDns(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	policies, err := in.client.ListDnsPolicies(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot dns: %w", err)
	}
	resources, warnings := snapshot.DnsPolicies(in.site.name, in.site.namePrefix, policies)
	return resources, warnings, nil
}

func projectSwitching(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	lags, err := in.client.ListLags(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot switching: %w", err)
	}
	domains, err := in.client.ListMcLagDomains(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot switching: %w", err)
	}
	stacks, err := in.client.ListSwitchStacks(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot switching: %w", err)
	}
	// The device-tag list resolves a LAG member device UUID to a tag name; it is only
	// needed when at least one LAG is present.
	var deviceTags []unifi.DeviceTag
	if len(lags) > 0 {
		deviceTags, err = in.client.ListDeviceTags(ctx, in.site.id)
		if err != nil {
			return nil, nil, fmt.Errorf("snapshot switching: %w", err)
		}
	}
	resources, warnings := snapshot.SwitchingLags(in.site.name, in.site.namePrefix, deviceTags, lags)
	domainResources, domainWarnings := snapshot.SwitchingMcLagDomains(in.site.name, in.site.namePrefix, domains)
	resources = append(resources, domainResources...)
	warnings = append(warnings, domainWarnings...)
	stackResources, stackWarnings := snapshot.SwitchingSwitchStacks(in.site.name, in.site.namePrefix, stacks)
	resources = append(resources, stackResources...)
	warnings = append(warnings, stackWarnings...)
	return resources, warnings, nil
}

func projectVpn(ctx context.Context, in projectionInput) ([]emit.Resource, []string, error) {
	servers, err := in.client.ListVpnServers(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot vpn: %w", err)
	}
	tunnels, err := in.client.ListSiteToSiteTunnels(ctx, in.site.id)
	if err != nil {
		return nil, nil, fmt.Errorf("snapshot vpn: %w", err)
	}
	resources, warnings := snapshot.VpnServers(in.site.name, in.site.namePrefix, servers)
	tunnelResources, tunnelWarnings := snapshot.SiteToSiteTunnels(in.site.name, in.site.namePrefix, tunnels)
	resources = append(resources, tunnelResources...)
	warnings = append(warnings, tunnelWarnings...)
	return resources, warnings, nil
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
