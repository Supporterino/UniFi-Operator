package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// runVerify executes `snapshot verify` and returns stdout (report), stderr (warnings and
// the cobra error echo) and the error, so a command test can assert both exit codes.
func runVerify(t *testing.T, args ...string) (string, string, error) {
	t.Helper()

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"snapshot", "verify"}, args...))
	err := root.Execute()
	return stdout.String(), stderr.String(), err
}

// writeAllSnapshot runs `snapshot all` into dir against controller, the way a user builds
// the directory `snapshot verify` reads.
func writeAllSnapshot(t *testing.T, dir, controller string) {
	t.Helper()

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"snapshot", "all", "--controller", controller, "--output", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot all: %v\n%s", err, stderr.String())
	}
}

// dirContents fingerprints the files in dir so a test can prove verify wrote nothing.
func dirContents(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	contents := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		contents[entry.Name()] = string(data)
	}
	return contents
}

// rewriteSpec mutates the top-level spec object of an emitted CR file in place.
func rewriteSpec(t *testing.T, path string, mutate func(spec map[string]any)) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	spec, ok := doc["spec"].(map[string]any)
	if !ok {
		t.Fatalf("%s has no spec object", path)
	}
	mutate(spec)
	out, err := yaml.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal %s: %v", path, err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestSnapshotVerifyFaithfulSnapshotExitsZero runs `all` into a directory and verifies it
// unchanged: with only expected gaps (known-gap annotations and projection skips) the report
// is OK and verify exits 0.
func TestSnapshotVerifyFaithfulSnapshotExitsZero(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	out, _, err := runVerify(t, "--controller", srv.URL, "--output", dir)
	if err != nil {
		t.Fatalf("verify of a faithful snapshot: %v\n%s", err, out)
	}
	if !strings.Contains(out, "result: OK") {
		t.Errorf("report does not report OK:\n%s", out)
	}
	if !strings.Contains(out, "fidelity: 0 drift(s)") {
		t.Errorf("report does not report zero drift:\n%s", out)
	}
}

// TestSnapshotVerifyProjectionWarningCountIsDistinct proves the report's projection-warning
// count is the number of distinct warnings actually emitted (the orchestrator deduplicates
// them), not the raw per-kind-per-site occurrence count, so the header does not overstate it.
func TestSnapshotVerifyProjectionWarningCountIsDistinct(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	out, errOut, err := runVerify(t, "--controller", srv.URL, "--output", dir)
	if err != nil {
		t.Fatalf("verify: %v\n%s", err, out)
	}

	distinct := strings.Count(errOut, "warning: ")
	want := fmt.Sprintf("projected %d resource(s); %d projection warning(s)", allFixtureResourceCount, distinct)
	if !strings.Contains(out, want) {
		t.Errorf("report header does not count the %d distinct warning(s) emitted:\n%s\nstderr:\n%s", distinct, out, errOut)
	}
}

// TestSnapshotVerifyReportsOmittedObject removes one emitted file and asserts verify reports
// the missing object by kind/name and exits non-zero (spec scenario "a skipped object is
// reported").
func TestSnapshotVerifyReportsOmittedObject(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	removed := filepath.Join(dir, "unifinetwork-default-guest-wifi.yaml")
	if err := os.Remove(removed); err != nil {
		t.Fatalf("remove %s: %v", removed, err)
	}

	out, _, err := runVerify(t, "--controller", srv.URL, "--output", dir)
	if err == nil {
		t.Fatalf("verify accepted a snapshot missing an object:\n%s", out)
	}
	if !strings.Contains(out, "missing: UnifiNetwork/default-guest-wifi") {
		t.Errorf("report does not name the omitted object:\n%s", out)
	}
	if !strings.Contains(out, "result: FAIL") {
		t.Errorf("report does not report FAIL:\n%s", out)
	}
}

// TestSnapshotVerifyKnownGapIsNotDrift tampers with an annotated known-gap resource (the
// corp broadcast carries the unresolved-wifi-secret placeholder annotation) and asserts the
// change is classified as an expected gap, not fidelity drift, so verify still exits 0.
func TestSnapshotVerifyKnownGapIsNotDrift(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	rewriteSpec(t, filepath.Join(dir, "unifiwifibroadcast-default-corp.yaml"), func(spec map[string]any) {
		security, ok := spec["securityConfiguration"].(map[string]any)
		if !ok {
			t.Fatal("corp broadcast has no securityConfiguration")
		}
		personal, ok := security["wpa2Personal"].(map[string]any)
		if !ok {
			t.Fatal("corp broadcast has no wpa2Personal security")
		}
		passphrase, ok := personal["passphrase"].(map[string]any)
		if !ok {
			t.Fatal("corp broadcast has no passphrase placeholder")
		}
		passphrase["name"] = "user-supplied-secret"
	})

	out, _, err := runVerify(t, "--controller", srv.URL, "--output", dir)
	if err != nil {
		t.Fatalf("verify reported an unexplained gap for an annotated known gap: %v\n%s", err, out)
	}
	if !strings.Contains(out, "expected gap: UnifiWifiBroadcast/default-corp") {
		t.Errorf("report does not classify the placeholder Secret as an expected gap:\n%s", out)
	}
	if strings.Contains(out, "drift: UnifiWifiBroadcast/default-corp") {
		t.Errorf("report classifies the placeholder Secret as drift:\n%s", out)
	}
}

// TestSnapshotVerifyReportsDrift tampers with an unannotated resource and asserts verify
// reports the field-level difference and exits non-zero, so a real upstream change since the
// snapshot reads as drift.
func TestSnapshotVerifyReportsDrift(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	rewriteSpec(t, filepath.Join(dir, "unifinetwork-default-guest-wifi.yaml"), func(spec map[string]any) {
		spec["vlanId"] = 21
	})

	out, _, err := runVerify(t, "--controller", srv.URL, "--output", dir)
	if err == nil {
		t.Fatalf("verify accepted a drifted snapshot:\n%s", out)
	}
	if !strings.Contains(out, "drift: UnifiNetwork/default-guest-wifi") {
		t.Errorf("report does not name the drifted resource:\n%s", out)
	}
	if !strings.Contains(out, "spec.vlanId") {
		t.Errorf("report does not name the drifted field:\n%s", out)
	}
}

// TestSnapshotVerifyWritesNothing asserts verify creates, updates, and deletes no file in
// the snapshot directory (spec scenario "verify writes nothing").
func TestSnapshotVerifyWritesNothing(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	writeAllSnapshot(t, dir, srv.URL)

	before := dirContents(t, dir)
	if _, _, err := runVerify(t, "--controller", srv.URL, "--output", dir); err != nil {
		t.Fatalf("verify: %v", err)
	}
	after := dirContents(t, dir)

	if !reflect.DeepEqual(before, after) {
		t.Errorf("verify changed the snapshot directory:\nbefore=%v\nafter=%v", before, after)
	}
}

// TestSnapshotVerifyDefaultOutputReadsSnapshotDir proves the -o default is snapshot/ rather
// than stdout: without -o and without a snapshot/ directory present, verify fails reading the
// directory instead of emitting a report to stdout.
func TestSnapshotVerifyDefaultOutputReadsSnapshotDir(t *testing.T) {
	srv := newFixtureServer(t)

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cwd, defaultVerifyOutput)); err == nil {
		t.Skip("snapshot/ already exists in the working directory")
	}

	out, _, err := runVerify(t, "--controller", srv.URL)
	if err == nil {
		t.Fatalf("verify with a missing default output directory did not fail:\n%s", out)
	}
	if !strings.Contains(err.Error(), defaultVerifyOutput) {
		t.Errorf("error = %v, want it to name the default output directory %q", err, defaultVerifyOutput)
	}
}
