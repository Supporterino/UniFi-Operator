package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Supporterino/UniFi-Operator/cli/internal/emit"
	"github.com/Supporterino/UniFi-Operator/cli/internal/snapshot"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

// defaultVerifyOutput is the conventional snapshot directory `snapshot verify` reads when
// --output is not set (design D6). Unlike `snapshot`, which defaults to stdout so nothing
// is written by accident, verify reads the documented full-snapshot destination.
const defaultVerifyOutput = "snapshot/"

// knownGapAnnotations are the lossy-projection annotations that mark a resource as an
// expected gap rather than fidelity drift (design D4). A resource carrying any of them does
// not hold a fully faithful copy of upstream state, so it is not compared field-for-field.
var knownGapAnnotations = []string{
	snapshot.AnnotationUnresolvedWifiSecret,
	snapshot.AnnotationUnresolvedRadiusProfile,
	snapshot.AnnotationUnrepresentedWifiSettings,
	snapshot.AnnotationUnresolvedDeviceTag,
	snapshot.AnnotationUnresolvedNetworkRef,
	snapshot.AnnotationUnrepresentedIPv6,
	snapshot.AnnotationUnresolvedPolicyRef,
	snapshot.AnnotationUnresolvedAclRuleRef,
}

// snapshotVerifyOptions holds the `snapshot verify` flags. It reads the same console
// coordinates as `snapshot` because it re-reads the live console, but resolves no site: it
// mirrors `all` and spans every site.
type snapshotVerifyOptions struct {
	controller    string
	controllerRef string
	apiKey        string
	output        string
}

func newSnapshotVerifyCommand() *cobra.Command {
	opts := &snapshotVerifyOptions{}
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Re-read a UniFi controller and report coverage and fidelity gaps in a snapshot",
		Long: "verify re-reads a live UniFi Network controller and checks a snapshot directory\n" +
			"written by `snapshot all` against it. That directory spans every site and kind, the\n" +
			"same scope verify re-projects; it is not a per-kind `snapshot <kind>` directory, which\n" +
			"covers one site and leaves site-scoped names unqualified. It writes nothing and\n" +
			"performs no upstream mutation. It reports coverage — objects the console has but the\n" +
			"snapshot is missing, and objects the projection skipped — and fidelity — field-level\n" +
			"differences between an emitted spec and a fresh projection of the same object.\n" +
			"Resources carrying a known-gap annotation (an unresolved passphrase, RADIUS profile,\n" +
			"device tag, network/policy/rule reference, or an unrepresented setting) are expected\n" +
			"gaps, not drift. It exits 0 when only expected gaps remain and non-zero when an\n" +
			"unexplained gap exists. Warnings go to stderr.",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Resolve environment fallbacks only when the flag was not set, matching
			// `snapshot`; secrets must never be flag defaults.
			if !cmd.Flags().Changed("controller") {
				opts.controller = os.Getenv("UNIFI_URL")
			}
			if !cmd.Flags().Changed("api-key") {
				opts.apiKey = os.Getenv("UNIFI_API_KEY")
			}
			return opts.run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.controller, "controller", "", "UniFi controller base URL (or UNIFI_URL)")
	flags.StringVar(&opts.controllerRef, "controller-ref", snapshot.DefaultControllerRef,
		"metadata.name of the UnifiController the snapshot references")
	flags.StringVar(&opts.apiKey, "api-key", "", "UniFi API key (or UNIFI_API_KEY); never logged")
	flags.StringVarP(&opts.output, "output", "o", defaultVerifyOutput, "snapshot directory to verify")
	return cmd
}

// run re-reads the console, projects every projectable kind across every site (the same
// scope as `snapshot all`), and compares that fresh projection to the resources on disk.
// It writes only to out (the report) and warn (projection warnings); it creates, updates,
// or deletes no file and mutates nothing upstream.
func (o *snapshotVerifyOptions) run(ctx context.Context, out, warn io.Writer) error {
	if o.controller == "" {
		return fmt.Errorf("--controller is required (or set UNIFI_URL)")
	}
	if o.controllerRef == "" {
		return fmt.Errorf("--controller-ref must not be empty")
	}
	if o.output == "" || o.output == "-" {
		return fmt.Errorf("--output must name a snapshot directory, not stdout")
	}

	disk, err := readSnapshotDirectory(o.output)
	if err != nil {
		return err
	}

	client, err := unifi.NewClient(o.controller, o.apiKey, false)
	if err != nil {
		return err
	}

	// Reuse the snapshot command's single kind registry (design D1/D4): projecting live
	// state through the same steps the snapshot used is what makes the comparison
	// meaningful. projectAll ignores --site and spans every site, mirroring `all`.
	projector := &snapshotOptions{controller: o.controller, controllerRef: o.controllerRef}
	fresh, warnings, err := projector.projectAll(ctx, client)
	if err != nil {
		return err
	}

	report, err := compareSnapshot(o.output, disk, fresh, warnings)
	if err != nil {
		return err
	}

	for _, w := range warnings {
		if _, err := fmt.Fprintf(warn, "warning: %s\n", w); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	if _, err := io.WriteString(out, report.String()); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	if gaps := report.unexplained(); gaps > 0 {
		return fmt.Errorf("snapshot verify: %d unexplained gap(s) found in %s", gaps, o.output)
	}
	return nil
}

// readSnapshotDirectory parses every YAML Custom Resource a prior snapshot wrote into dir.
// Non-YAML entries and subdirectories are ignored; a malformed YAML file is a hard error so
// a corrupt snapshot is not silently treated as empty.
func readSnapshotDirectory(dir string) ([]emit.Resource, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read snapshot directory %q: %w", dir, err)
	}
	resources := make([]emit.Resource, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		resource, err := emit.Unmarshal(data)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		resources = append(resources, resource)
	}
	return resources, nil
}

// resourceKey identifies an emitted resource by kind and metadata.name.
type resourceKey struct {
	kind string
	name string
}

func (k resourceKey) String() string { return k.kind + "/" + k.name }

// verifyReport is the human-readable outcome of a `snapshot verify` run.
type verifyReport struct {
	output    string
	projected int
	// missing lists projected resources that the directory does not hold (kind/name).
	missing []string
	// stale lists on-disk resources the fresh projection no longer contains (kind/name).
	stale []string
	// expected lists known-gap resources and the annotations that explain them.
	expected []string
	// drifts lists field-level spec differences between disk and the fresh projection.
	drifts []string
	// skipped counts the projection warnings (objects the projection could not represent).
	skipped int
}

// unexplained is the number of gaps nothing accounts for: a projected object missing from
// the directory, an on-disk object absent upstream, and unexplained spec drift. Projection
// skips and known-gap annotations are explained, so they do not count.
func (r verifyReport) unexplained() int {
	return len(r.missing) + len(r.stale) + len(r.drifts)
}

// String renders the report. It never contains an upstream UUID: identities are Kubernetes
// kind/name pairs only.
func (r verifyReport) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "snapshot verify: %s\n", r.output)
	fmt.Fprintf(&b, "projected %d resource(s); %d projection warning(s)\n", r.projected, r.skipped)
	fmt.Fprintf(&b, "coverage: %d missing, %d unexpected\n", len(r.missing), len(r.stale))
	fmt.Fprintf(&b, "fidelity: %d drift(s), %d expected gap(s)\n", len(r.drifts), len(r.expected))
	writeLines := func(label string, lines []string) {
		for _, line := range lines {
			fmt.Fprintf(&b, "  %s: %s\n", label, line)
		}
	}
	writeLines("missing", r.missing)
	writeLines("unexpected", r.stale)
	writeLines("expected gap", r.expected)
	writeLines("drift", r.drifts)
	if r.unexplained() == 0 {
		b.WriteString("result: OK (no unexplained gaps)\n")
	} else {
		fmt.Fprintf(&b, "result: FAIL (%d unexplained gap(s))\n", r.unexplained())
	}
	return b.String()
}

// compareSnapshot runs the coverage and fidelity checks (design D4). Coverage is one-way:
// every freshly projected resource must be present on disk; a missing one is an unexplained
// gap. Fidelity compares each on-disk resource's spec to the fresh projection of the same
// kind/name, treating a known-gap annotation as expected and an absent counterpart as
// unexpected.
func compareSnapshot(output string, disk, fresh []emit.Resource, warnings []string) (verifyReport, error) {
	report := verifyReport{output: output, projected: len(fresh), skipped: len(warnings)}

	diskByKey := make(map[resourceKey]emit.Resource, len(disk))
	for _, resource := range disk {
		diskByKey[resourceKey{resource.Kind, resource.Metadata.Name}] = resource
	}
	freshByKey := make(map[resourceKey]emit.Resource, len(fresh))
	for _, resource := range fresh {
		freshByKey[resourceKey{resource.Kind, resource.Metadata.Name}] = resource
	}

	// Coverage: a projected object with no file on disk was not captured.
	for _, key := range sortedResourceKeys(freshByKey) {
		if _, ok := diskByKey[key]; !ok {
			report.missing = append(report.missing, key.String())
		}
	}

	// Fidelity: an on-disk resource must match what the console would project now.
	for _, key := range sortedResourceKeys(diskByKey) {
		diskResource := diskByKey[key]
		freshResource, ok := freshByKey[key]
		if !ok {
			report.stale = append(report.stale, key.String())
			continue
		}
		if gaps := knownGaps(diskResource.Metadata.Annotations); len(gaps) > 0 {
			report.expected = append(report.expected, fmt.Sprintf("%s (%s)", key, strings.Join(gaps, ", ")))
			continue
		}
		drifts, err := diffResource(key, diskResource, freshResource)
		if err != nil {
			return verifyReport{}, err
		}
		report.drifts = append(report.drifts, drifts...)
	}
	return report, nil
}

// knownGaps returns the known-gap annotations present on a resource, in the stable order of
// knownGapAnnotations, so the report is deterministic.
func knownGaps(annotations map[string]string) []string {
	if len(annotations) == 0 {
		return nil
	}
	var gaps []string
	for _, name := range knownGapAnnotations {
		if _, ok := annotations[name]; ok {
			gaps = append(gaps, name)
		}
	}
	return gaps
}

// diffResource canonicalises both specs and reports the field-level differences between the
// snapshot's spec and the freshly projected one. Only spec is compared: status is the
// operator's and upstream ids legitimately live there.
func diffResource(key resourceKey, disk, fresh emit.Resource) ([]string, error) {
	diskSpec, err := canonical(disk.Spec)
	if err != nil {
		return nil, fmt.Errorf("canonicalise %s snapshot spec: %w", key, err)
	}
	freshSpec, err := canonical(fresh.Spec)
	if err != nil {
		return nil, fmt.Errorf("canonicalise %s projected spec: %w", key, err)
	}
	var drifts []string
	for _, line := range diffValue("spec", diskSpec, freshSpec) {
		drifts = append(drifts, key.String()+" "+line)
	}
	return drifts, nil
}

// canonical round-trips a value through JSON so a typed projected spec and a generic
// decoded one compare on equal terms (numbers as float64, maps as map[string]any).
func canonical(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// diffValue walks two canonicalised values and returns one human-readable line per
// difference, addressing each by its dotted path with bracketed slice indexes.
func diffValue(path string, snapshotValue, upstreamValue any) []string {
	switch snapshotMap := snapshotValue.(type) {
	case map[string]any:
		upstreamMap, ok := upstreamValue.(map[string]any)
		if !ok {
			return []string{fmt.Sprintf("%s: type differs (snapshot=%s upstream=%s)", path, render(snapshotValue), render(upstreamValue))}
		}
		keys := make([]string, 0, len(snapshotMap)+len(upstreamMap))
		seen := make(map[string]bool, len(snapshotMap)+len(upstreamMap))
		for key := range snapshotMap {
			seen[key] = true
			keys = append(keys, key)
		}
		for key := range upstreamMap {
			if !seen[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)

		var out []string
		for _, key := range keys {
			child := path + "." + key
			snapshotChild, hasSnapshot := snapshotMap[key]
			upstreamChild, hasUpstream := upstreamMap[key]
			switch {
			case !hasSnapshot:
				out = append(out, fmt.Sprintf("%s: missing from the snapshot (upstream=%s)", child, render(upstreamChild)))
			case !hasUpstream:
				out = append(out, fmt.Sprintf("%s: absent upstream (snapshot=%s)", child, render(snapshotChild)))
			default:
				out = append(out, diffValue(child, snapshotChild, upstreamChild)...)
			}
		}
		return out

	case []any:
		upstreamSlice, ok := upstreamValue.([]any)
		if !ok {
			return []string{fmt.Sprintf("%s: type differs (snapshot=%s upstream=%s)", path, render(snapshotValue), render(upstreamValue))}
		}
		var out []string
		if len(snapshotMap) != len(upstreamSlice) {
			out = append(out, fmt.Sprintf("%s: length differs (snapshot=%d upstream=%d)", path, len(snapshotMap), len(upstreamSlice)))
		}
		limit := min(len(snapshotMap), len(upstreamSlice))
		for i := 0; i < limit; i++ {
			out = append(out, diffValue(fmt.Sprintf("%s[%d]", path, i), snapshotMap[i], upstreamSlice[i])...)
		}
		return out

	default:
		if !reflect.DeepEqual(snapshotValue, upstreamValue) {
			return []string{fmt.Sprintf("%s: differs (snapshot=%s upstream=%s)", path, render(snapshotValue), render(upstreamValue))}
		}
		return nil
	}
}

// render is the JSON form of a leaf value for a report line; it carries no upstream secret.
func render(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// sortedResourceKeys returns map keys in a deterministic order for a stable report.
func sortedResourceKeys[V any](m map[resourceKey]V) []resourceKey {
	keys := make([]resourceKey, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].name < keys[j].name
	})
	return keys
}
