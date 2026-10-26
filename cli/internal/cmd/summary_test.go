package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// uuidPattern matches the upstream identifier shape the summary must never carry.
var uuidPattern = regexp.MustCompile(
	`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// summaryPath returns a path in a fresh directory for a run summary.
func summaryPath(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "summary.json")
}

// readSummary decodes the run summary a command run wrote to path and fails when the file is
// missing, which is how a test proves the summary is opt-in.
func readSummary(t *testing.T, path string) runSummary {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run summary: %v", err)
	}
	var summary runSummary
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatalf("parse run summary: %v\n%s", err, data)
	}
	return summary
}

// normalizeElapsed zeroes the run-dependent elapsed time so the rest of the document can be
// compared against a golden file.
func normalizeElapsed(t *testing.T, path string) string {
	t.Helper()

	summary := readSummary(t, path)
	summary.ElapsedMillis = 0
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("encode run summary: %v", err)
	}
	return string(data) + "\n"
}

// yamlDocuments splits a `---`-separated stream into its documents the way a `kubectl apply
// -f -` reader does, and fails on a document that is not a YAML mapping, so a stream with the
// summary spliced into it cannot pass.
func yamlDocuments(t *testing.T, stream string) []map[string]any {
	t.Helper()

	var docs []map[string]any
	for _, chunk := range strings.Split(stream, "---\n") {
		if strings.TrimSpace(chunk) == "" {
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("parse YAML document: %v\n%s", err, chunk)
		}
		docs = append(docs, doc)
	}
	return docs
}

// walkStrings calls visit for every string value in a decoded JSON document.
func walkStrings(t *testing.T, value any, visit func(string)) {
	t.Helper()

	switch typed := value.(type) {
	case string:
		visit(typed)
	case []any:
		for _, element := range typed {
			walkStrings(t, element, visit)
		}
	case map[string]any:
		for key, element := range typed {
			visit(key)
			walkStrings(t, element, visit)
		}
	}
}

// TestSnapshotSummaryKindGolden pins the summary of a per-kind run: the kind name, the emitted
// resource count per Custom Resource kind, the single site entry, and the distinct warning
// count.
func TestSnapshotSummaryKindGolden(t *testing.T) {
	srv := newFixtureServer(t)
	path := summaryPath(t)

	_, errOut := runSnapshot(t, "networks", "--controller", srv.URL, "--summary-json", path)
	compareGolden(t, "summary-networks.golden.json", normalizeElapsed(t, path))

	summary := readSummary(t, path)
	if summary.SchemaVersion != summarySchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", summary.SchemaVersion, summarySchemaVersion)
	}
	if summary.Kind != "networks" {
		t.Errorf("kind = %q, want %q", summary.Kind, "networks")
	}
	if summary.Resources != 3 {
		t.Errorf("resources = %d, want the 3 emitted networks", summary.Resources)
	}
	if len(summary.Kinds) != 1 || summary.Kinds[0] != (summaryKind{Kind: "UnifiNetwork", Count: 3}) {
		t.Errorf("kinds = %v, want a single UnifiNetwork count of 3", summary.Kinds)
	}
	if len(summary.Sites) != 1 || summary.Sites[0].Site != "default" || summary.Sites[0].Resources != 3 {
		t.Errorf("sites = %v, want one 3-resource entry for the default site", summary.Sites)
	}
	if want := strings.Count(errOut, "warning: "); summary.Warnings != want {
		t.Errorf("warnings = %d, want the %d warning(s) on stderr", summary.Warnings, want)
	}
	if summary.Notes != 0 {
		t.Errorf("notes = %d, want 0 for networks", summary.Notes)
	}
	if summary.Verify != nil {
		t.Errorf("verify = %+v, want it absent on a snapshot summary", summary.Verify)
	}
}

// TestSnapshotSummaryAllGolden pins the summary of an `all` run: every kind's count across the
// two fixture sites, and one per-site entry each, including the branch site whose only
// site-scoped emission is its ACL rule ordering.
func TestSnapshotSummaryAllGolden(t *testing.T) {
	srv := newFixtureServer(t)
	path := summaryPath(t)

	runSnapshot(t, "all", "--controller", srv.URL, "--summary-json", path)
	compareGolden(t, "summary-all.golden.json", normalizeElapsed(t, path))

	summary := readSummary(t, path)
	if summary.Kind != allKind {
		t.Errorf("kind = %q, want %q", summary.Kind, allKind)
	}
	if summary.Resources != allFixtureResourceCount {
		t.Errorf("resources = %d, want the %d emitted resources", summary.Resources, allFixtureResourceCount)
	}

	counted := 0
	for _, kind := range summary.Kinds {
		counted += kind.Count
	}
	if counted != summary.Resources {
		t.Errorf("per-kind counts total %d, want the %d emitted resources", counted, summary.Resources)
	}

	if len(summary.Sites) != 2 {
		t.Fatalf("sites = %v, want one entry for each of the 2 fixture sites", summary.Sites)
	}
	if summary.Sites[0].Site != "default" || summary.Sites[0].Resources == 0 {
		t.Errorf("first site entry = %+v, want the populated default site", summary.Sites[0])
	}
	if summary.Sites[1].Site != "branch" || summary.Sites[1].Resources != 1 {
		t.Errorf("second site entry = %+v, want the branch site's single ordering resource", summary.Sites[1])
	}
	// The `controller` and `sites` kinds are projected once rather than per site, so they are
	// counted in Kinds and never in a site entry.
	siteTotal := 0
	for _, site := range summary.Sites {
		siteTotal += site.Resources
	}
	if siteTotal+3 != summary.Resources {
		t.Errorf("site entries total %d and site-agnostic kinds account for 3, want %d resources",
			siteTotal, summary.Resources)
	}
}

// TestSnapshotSummaryKeepsResourceStreamParseable covers the stream contract: with resources on
// stdout and the summary requested, stdout is still nothing but a `---`-separated YAML stream
// of resources, and the summary lands on stderr.
func TestSnapshotSummaryKeepsResourceStreamParseable(t *testing.T) {
	srv := newFixtureServer(t)

	out, errOut := runSnapshot(t, "all", "--controller", srv.URL, "--output", "-", "--summary-json", "-")

	docs := yamlDocuments(t, out)
	if len(docs) != allFixtureResourceCount {
		t.Fatalf("stdout holds %d YAML documents, want %d", len(docs), allFixtureResourceCount)
	}
	for _, doc := range docs {
		if doc["kind"] == nil || doc["apiVersion"] == nil {
			t.Fatalf("stdout document is not a Custom Resource: %v", doc)
		}
	}
	if strings.Contains(out, "schemaVersion") {
		t.Error("stdout carries the run summary; it must never be written to the resource stream")
	}

	payload := errOut[strings.Index(errOut, "{\n"):]
	var summary runSummary
	if err := json.Unmarshal([]byte(payload), &summary); err != nil {
		t.Fatalf("stderr summary is not a JSON object: %v\n%s", err, errOut)
	}
	if summary.SchemaVersion != summarySchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", summary.SchemaVersion, summarySchemaVersion)
	}
	if summary.Resources != allFixtureResourceCount {
		t.Errorf("summary resources = %d, want the %d documents on stdout", summary.Resources, allFixtureResourceCount)
	}
}

// TestSnapshotSummaryDefaultBytesUnchanged proves the summary is opt-in: requesting it changes
// neither stdout nor stderr, and the default output still matches the committed golden.
func TestSnapshotSummaryDefaultBytesUnchanged(t *testing.T) {
	srv := newFixtureServer(t)

	plainOut, plainErr := runSnapshot(t, "networks", "--controller", srv.URL)
	compareGolden(t, "networks.golden.yaml", plainOut)
	// A summary run writes to the named file only, so the streams must be byte-identical.
	path := summaryPath(t)
	summaryOut, summaryErr := runSnapshot(t, "networks", "--controller", srv.URL, "--summary-json", path)
	if summaryOut != plainOut {
		t.Errorf("--summary-json changed stdout:\nplain:\n%s\nsummary:\n%s", plainOut, summaryOut)
	}
	if summaryErr != plainErr {
		t.Errorf("--summary-json changed stderr:\nplain: %q\nsummary: %q", plainErr, summaryErr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("--summary-json wrote no summary: %v", err)
	}

	// The `all` path is multi-site and multi-kind, so check the streams there too. Its output
	// carries the httptest URL, so it is compared run-to-run rather than against a golden.
	plainOut, plainErr = runSnapshot(t, "all", "--controller", srv.URL)
	path = summaryPath(t)
	summaryOut, summaryErr = runSnapshot(t, "all", "--controller", srv.URL, "--summary-json", path)
	if summaryOut != plainOut {
		t.Error("--summary-json changed the `all` stdout stream")
	}
	if summaryErr != plainErr {
		t.Errorf("--summary-json changed the `all` stderr:\nplain: %q\nsummary: %q", plainErr, summaryErr)
	}
}

// TestSnapshotSummaryDefaultWritesNothing proves a default invocation writes no summary at all,
// so no file appears beside the emitted resources.
func TestSnapshotSummaryDefaultWritesNothing(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()

	root := NewRootCommand()
	var stdout, stderr strings.Builder
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"snapshot", "networks", "--controller", srv.URL, "--output", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot networks: %v\n%s", err, stderr.String())
	}

	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(before) == 0 {
		t.Fatal("the snapshot wrote no resource file")
	}
	if strings.Contains(stderr.String(), "schemaVersion") {
		t.Error("a default invocation wrote a summary to stderr")
	}
	// The output directory must hold resource YAML only, with no summary file among it.
	summaryDir := t.TempDir()
	root = NewRootCommand()
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"snapshot", "networks", "--controller", srv.URL, "--output", summaryDir,
		"--summary-json", filepath.Join(summaryDir, "run.json")})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot networks with a summary: %v\n%s", err, stderr.String())
	}
	after, err := os.ReadDir(summaryDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(after) != len(before)+1 {
		t.Errorf("output dir holds %d entries, want the %d resources plus the summary", len(after), len(before))
	}
}

// TestSnapshotSummaryCarriesNoUpstreamIdentifiers walks every value of the summary of a run over
// two sites and asserts no upstream UUID or `_id` reached it.
func TestSnapshotSummaryCarriesNoUpstreamIdentifiers(t *testing.T) {
	srv := newFixtureServer(t)
	path := summaryPath(t)

	runSnapshot(t, "all", "--controller", srv.URL, "--summary-json", path)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run summary: %v", err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse run summary: %v", err)
	}

	walkStrings(t, document, func(value string) {
		if uuidPattern.MatchString(value) {
			t.Errorf("run summary carries an upstream UUID: %q", value)
		}
		if strings.Contains(value, "_id") {
			t.Errorf("run summary carries an upstream identifier field: %q", value)
		}
	})

	summary := readSummary(t, path)
	if summary.SchemaVersion != summarySchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", summary.SchemaVersion, summarySchemaVersion)
	}
}

// TestSnapshotSummaryReadOnlyListing covers the read-only listing kind: it emits no Custom
// Resource, so its summary reports zero resources and no site entries rather than failing, and
// the listing still goes to stdout.
func TestSnapshotSummaryReadOnlyListing(t *testing.T) {
	srv := newFixtureServer(t)
	path := summaryPath(t)

	out, _ := runSnapshot(t, deviceTagsKind, "--controller", srv.URL, "--summary-json", path)
	if strings.TrimSpace(out) == "" {
		t.Fatal("the device-tag listing printed nothing to stdout")
	}

	summary := readSummary(t, path)
	if summary.Kind != deviceTagsKind {
		t.Errorf("kind = %q, want %q", summary.Kind, deviceTagsKind)
	}
	if summary.Resources != 0 || len(summary.Kinds) != 0 || len(summary.Sites) != 0 {
		t.Errorf("summary = %+v, want no resources and no sites for a read-only listing", summary)
	}
	if summary.SchemaVersion != summarySchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", summary.SchemaVersion, summarySchemaVersion)
	}
}

// TestSnapshotVerifySummaryGolden pins the verify summary of a faithful snapshot: the run shape
// plus verify's coverage and fidelity tallies.
func TestSnapshotVerifySummaryGolden(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	path := summaryPath(t)
	out, _, err := runVerify(t, "--controller", srv.URL, "--output", dir, "--summary-json", path)
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}
	compareGolden(t, "summary-verify.golden.json", normalizeElapsed(t, path))
}

// TestSnapshotVerifySummaryCounts asserts verify's coverage numbers in the summary match the
// report and that skipped stays warnings-only.
func TestSnapshotVerifySummaryCounts(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	path := summaryPath(t)
	out, errOut, err := runVerify(t, "--controller", srv.URL, "--output", dir, "--summary-json", path)
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}

	summary := readSummary(t, path)
	if summary.Verify == nil {
		t.Fatalf("summary carries no verify object:\n%s", out)
	}
	verify := summary.Verify
	if verify.Projected != allFixtureResourceCount {
		t.Errorf("projected = %d, want the %d freshly projected resources", verify.Projected, allFixtureResourceCount)
	}
	if verify.Missing != 0 || verify.Unexpected != 0 || verify.Drifted != 0 {
		t.Errorf("coverage/fidelity = %+v, want no gap in a faithful snapshot", verify)
	}
	warnings := strings.Count(errOut, "warning: ")
	if verify.Skipped != warnings {
		t.Errorf("skipped = %d, want the %d projection warning(s)", verify.Skipped, warnings)
	}
	if summary.Kind != allKind {
		t.Errorf("kind = %q, want %q for a verify run", summary.Kind, allKind)
	}
	if summary.Resources != allFixtureResourceCount || len(summary.Sites) != 2 {
		t.Errorf("run shape = %d resource(s) over %d site(s), want %d over 2",
			summary.Resources, len(summary.Sites), allFixtureResourceCount)
	}
	if summary.SchemaVersion != summarySchemaVersion {
		t.Errorf("schemaVersion = %d, want %d", summary.SchemaVersion, summarySchemaVersion)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read run summary: %v", err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse run summary: %v", err)
	}
	walkStrings(t, document, func(value string) {
		if uuidPattern.MatchString(value) {
			t.Errorf("verify summary carries an upstream UUID: %q", value)
		}
	})
}

// TestSnapshotVerifySummaryNoteDoesNotIncrementSkipped drives the unconfigured Zone-Based
// Firewall path: the note is printed and counted as a note, but skipped stays the number of
// projection warnings, so a subsystem gap never reads as a dropped object.
func TestSnapshotVerifySummaryNoteDoesNotIncrementSkipped(t *testing.T) {
	srv := newZBFOffServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	path := summaryPath(t)
	out, errOut, err := runVerify(t, "--controller", srv.URL, "--output", dir, "--summary-json", path)
	if err != nil {
		t.Fatalf("verify of a ZBF-off snapshot: %v\n%s", err, out)
	}

	summary := readSummary(t, path)
	if summary.Verify == nil {
		t.Fatalf("summary carries no verify object:\n%s", out)
	}
	lines := strings.Split(strings.TrimSuffix(errOut, "\n"), "\n")
	notes := 0
	for _, line := range lines {
		if line == "warning: "+zbfNote("default") {
			notes++
		}
	}
	if notes != 1 {
		t.Fatalf("the ZBF note printed %d time(s), want exactly 1:\n%s", notes, errOut)
	}
	if summary.Notes != notes {
		t.Errorf("notes = %d, want the %d printed subsystem note(s)", summary.Notes, notes)
	}
	if summary.Warnings+summary.Notes != len(lines) {
		t.Errorf("warnings+notes = %d, want the %d stderr line(s)",
			summary.Warnings+summary.Notes, len(lines))
	}
	if summary.Verify.Skipped != summary.Warnings {
		t.Errorf("skipped = %d, want the %d projection warning(s); the note was counted",
			summary.Verify.Skipped, summary.Warnings)
	}
}
