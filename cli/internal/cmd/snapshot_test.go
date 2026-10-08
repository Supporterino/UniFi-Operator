package cmd

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

var update = flag.Bool("update", false, "update golden files")

// Upstream opaque identifiers present in testdata/networks.json. None of these may
// appear in the emitted output, and no spec may carry an id-shaped field.
const (
	fixtureNetworkIDOne = "66a1b2c3d4e5f6a7b8c9d0e1"
	fixtureNetworkIDTwo = "66a1b2c3d4e5f6a7b8c9d0e2"
	fixtureSiteID       = "5f3a2b1c9d8e7f6a5b4c3d2e"
)

// subnetPattern mirrors the operator UnifiNetworkSpec regex.
var subnetPattern = regexp.MustCompile(`^([0-9]{1,3}\.){3}[0-9]{1,3}(/[0-9]{1,2})?$`)

func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "networks.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/proxy/network/api/s/default/rest/networkconf"; got != want {
			http.Error(w, "unexpected path "+got, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write(fixture); err != nil {
			t.Errorf("write fixture: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runSnapshot(t *testing.T, args ...string) string {
	t.Helper()

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"snapshot"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot: %v\n%s", err, out.String())
	}
	return out.String()
}

func TestSnapshotNetworksGolden(t *testing.T) {
	srv := newFixtureServer(t)
	got := runSnapshot(t, "--controller", srv.URL)

	goldenPath := filepath.Join("..", "..", "testdata", "golden", "networks.golden.yaml")
	if *update {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if got != string(want) {
		t.Errorf("snapshot output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestSnapshotSpecHasNoOpaqueID(t *testing.T) {
	srv := newFixtureServer(t)
	out := runSnapshot(t, "--controller", srv.URL)

	for _, id := range []string{fixtureNetworkIDOne, fixtureNetworkIDTwo, fixtureSiteID} {
		if strings.Contains(out, id) {
			t.Errorf("emitted output contains upstream identifier %q:\n%s", id, out)
		}
	}

	docs := parseEmittedCRs(t, out)
	if len(docs) != 2 {
		t.Fatalf("got %d emitted CRs, want 2", len(docs))
	}
	for _, doc := range docs {
		if doc.APIVersion != "unifi.supporterino.de/v1alpha1" {
			t.Errorf("apiVersion = %q, want unifi.supporterino.de/v1alpha1", doc.APIVersion)
		}
		if doc.Kind != "UnifiNetwork" {
			t.Errorf("kind = %q, want UnifiNetwork", doc.Kind)
		}
		if doc.Metadata.Name == "" {
			t.Error("emitted CR is missing metadata.name")
		}
		if err := validateAgainstOperatorSpec(doc.Spec); err != nil {
			t.Errorf("spec does not satisfy the operator UnifiNetworkSpec schema: %v", err)
		}
	}
}

// validateAgainstOperatorSpec enforces the constraints the operator's UnifiNetwork CRD
// declares in operator/api/v1alpha1/unifinetwork_types.go.
func validateAgainstOperatorSpec(spec map[string]any) error {
	for key := range spec {
		switch strings.ToLower(key) {
		case "id", "_id", "siteid", "networkid", "controllerid", "upstreamid":
			return fmt.Errorf("opaque identifier field %q is not allowed in spec", key)
		}
	}

	site, _ := spec["site"].(string)
	if site == "" {
		return errors.New("spec.site is required and must be non-empty")
	}
	name, _ := spec["name"].(string)
	if name == "" {
		return errors.New("spec.name is required and must be non-empty")
	}
	if subnet, ok := spec["subnet"].(string); ok && subnet != "" && !subnetPattern.MatchString(subnet) {
		return fmt.Errorf("spec.subnet %q does not match the CRD pattern", subnet)
	}
	if vlan, ok := spec["vlan"].(float64); ok {
		if vlan < 1 || vlan > 4094 {
			return fmt.Errorf("spec.vlan %v is outside the CRD range 1..4094", vlan)
		}
	}
	return nil
}

func TestSnapshotWritesDirectory(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	runSnapshot(t, "--controller", srv.URL, "--output", dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d files, want 2", len(entries))
	}
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", entry.Name(), err)
		}
		if strings.Contains(string(data), "---") {
			t.Errorf("%s contains a document separator", entry.Name())
		}
	}
}

func TestSnapshotCollisionWritesDistinctFiles(t *testing.T) {
	const body = `{"meta":{"rc":"ok"},"data":[` +
		`{"_id":"id-1","name":"Guest WiFi","ip_subnet":"192.168.2.0/24","vlan":20,"enabled":true},` +
		`{"_id":"id-2","name":"guest-wifi","ip_subnet":"192.168.3.0/24","vlan":30,"enabled":true}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"snapshot", "--controller", srv.URL, "--output", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot: %v\n%s", err, stderr.String())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d files, want 2 (collision must not overwrite): %v", len(entries), entries)
	}
	if !strings.Contains(stderr.String(), "warning:") {
		t.Errorf("expected a collision warning on stderr, got %q", stderr.String())
	}
}

func TestSnapshotHelpDoesNotLeakAPIKey(t *testing.T) {
	t.Setenv("UNIFI_API_KEY", "SUPERSECRET")

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"snapshot", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if strings.Contains(out.String(), "SUPERSECRET") {
		t.Errorf("help output leaked the API key:\n%s", out.String())
	}
}

func TestSnapshotResolvesAPIKeyFromEnv(t *testing.T) {
	t.Setenv("UNIFI_API_KEY", "env-secret")

	gotKey := captureAPIKey(t, nil)
	if gotKey != "env-secret" {
		t.Errorf("%s = %q, want %q", unifi.APIKeyHeader, gotKey, "env-secret")
	}
}

func TestSnapshotAPIKeyFlagWinsOverEnv(t *testing.T) {
	t.Setenv("UNIFI_API_KEY", "env-secret")

	gotKey := captureAPIKey(t, []string{"--api-key", "flag-secret"})
	if gotKey != "flag-secret" {
		t.Errorf("%s = %q, want %q", unifi.APIKeyHeader, gotKey, "flag-secret")
	}
}

// captureAPIKey runs snapshot against an httptest server and returns the value the
// client sent in the API key header. extraArgs are appended to the snapshot args.
func captureAPIKey(t *testing.T, extraArgs []string) string {
	t.Helper()

	keyCh := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keyCh <- r.Header.Get(unifi.APIKeyHeader)
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	args := append([]string{"snapshot", "--controller", srv.URL}, extraArgs...)
	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot: %v\n%s", err, out.String())
	}

	select {
	case key := <-keyCh:
		return key
	default:
		t.Fatal("controller was never called")
		return ""
	}
}

func TestSnapshotOmitsAbsentEnabled(t *testing.T) {
	const body = `{"meta":{"rc":"ok"},"data":[` +
		`{"_id":"id-1","name":"Default","ip_subnet":"192.168.1.0/24","vlan":1}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	out := runSnapshot(t, "--controller", srv.URL)
	docs := parseEmittedCRs(t, out)
	if len(docs) != 1 {
		t.Fatalf("got %d emitted CRs, want 1", len(docs))
	}
	if _, ok := docs[0].Spec["enabled"]; ok {
		t.Errorf("spec.enabled = %v, want omitted when upstream omits it", docs[0].Spec["enabled"])
	}
	if err := validateAgainstOperatorSpec(docs[0].Spec); err != nil {
		t.Errorf("spec does not satisfy the operator schema: %v", err)
	}
}

func TestSnapshotResolvesControllerFromEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"meta":{"rc":"ok"},"data":[]}`)); err != nil {
			t.Errorf("write body: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	t.Setenv("UNIFI_URL", srv.URL)

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"snapshot"})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot via UNIFI_URL: %v\n%s", err, out.String())
	}
}

func TestSnapshotRequiresController(t *testing.T) {
	t.Setenv("UNIFI_URL", "")

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"snapshot"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error when --controller is missing")
	}
}

func TestSnapshotRejectsUnknownKind(t *testing.T) {
	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"snapshot", "--controller", "http://controller.invalid", "switches"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected an error for an unknown snapshot kind")
	}
}

// emittedCR is the part of an emitted Custom Resource the tests inspect.
type emittedCR struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec map[string]any `json:"spec"`
}

func parseEmittedCRs(t *testing.T, stream string) []emittedCR {
	t.Helper()

	var docs []emittedCR
	for _, chunk := range strings.Split(stream, "---") {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		var doc emittedCR
		if err := yaml.Unmarshal([]byte(chunk), &doc); err != nil {
			t.Fatalf("parse emitted CR: %v\n%s", err, chunk)
		}
		docs = append(docs, doc)
	}
	return docs
}
