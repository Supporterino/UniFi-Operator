package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/Supporterino/UniFi-Operator/cli/internal/snapshot"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

var update = flag.Bool("update", false, "update golden files")

// Upstream opaque identifiers present in the testdata fixtures. None of these may appear
// in the emitted output, and no spec may carry an id-shaped field.
const (
	fixtureNetworkIDOne   = "66a1b2c3-d4e5-4f6a-8b7c-8d9e0f1a2b3c"
	fixtureNetworkIDTwo   = "77b2c3d4-e5f6-4a7b-9c8d-9e0f1a2b3c4d"
	fixtureNetworkIDThree = "88c3d4e5-f6a7-4b8c-9d0e-0f1a2b3c4d5e"
	fixtureZoneID         = "99d4e5f6-a7b8-4c9d-0e1f-1a2b3c4d5e6f"
	fixtureDeviceID       = "aa1b2c3d-4e5f-4a6b-8c7d-9e0f1a2b3c4d"
	fixtureSiteID         = "5f3a2b1c-9d8e-4f6a-8b4c-3d2e1f0a9b8c"
	fixtureBranchSiteID   = "6a4b3c2d-0e9f-4a7b-9c8d-4e3f2a1b0c9d"
)

const (
	pathSites    = "/proxy/network/integration/v1/sites"
	pathNetworks = "/proxy/network/integration/v1/sites/" + fixtureSiteID + "/networks"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

// networkDetailFixtures maps each upstream network id to its recorded Network details
// response. The list fixture (networks.json) carries only overview fields, so the variant
// configuration is only present here.
func networkDetailFixtures(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		fixtureNetworkIDOne:   readFixture(t, "network-default.json"),
		fixtureNetworkIDTwo:   readFixture(t, "network-guest-wifi.json"),
		fixtureNetworkIDThree: readFixture(t, "network-iot.json"),
	}
}

// newFixtureServer serves the recorded Integration v1 fixtures. The branch site has no
// networks.
func newFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	sites := readFixture(t, "sites.json")
	networks := readFixture(t, "networks.json")
	details := networkDetailFixtures(t)
	emptyPage := []byte(`{"count":0,"data":[],"limit":200,"offset":0,"totalCount":0}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == pathSites:
			_, _ = w.Write(sites)
		case r.URL.Path == pathNetworks:
			_, _ = w.Write(networks)
		case r.URL.Path == "/proxy/network/integration/v1/sites/"+fixtureBranchSiteID+"/networks":
			_, _ = w.Write(emptyPage)
		case strings.HasPrefix(r.URL.Path, pathNetworks+"/"):
			id := strings.TrimPrefix(r.URL.Path, pathNetworks+"/")
			detail, ok := details[id]
			if !ok {
				http.Error(w, "unknown network "+id, http.StatusNotFound)
				return
			}
			_, _ = w.Write(detail)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// runSnapshot executes `snapshot` with args and returns stdout (CRs) and stderr (warnings).
func runSnapshot(t *testing.T, args ...string) (string, string) {
	t.Helper()

	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"snapshot"}, args...))
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot: %v\n%s", err, stderr.String())
	}
	return stdout.String(), stderr.String()
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()

	goldenPath := filepath.Join("..", "..", "testdata", "golden", name)
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
		t.Fatalf("read golden %s (run with -update to create): %v", name, err)
	}
	if got != string(want) {
		t.Errorf("snapshot output mismatch for %s\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestSnapshotNetworksGolden(t *testing.T) {
	srv := newFixtureServer(t)
	out, _ := runSnapshot(t, "networks", "--controller", srv.URL)
	compareGolden(t, "networks.golden.yaml", out)
}

func TestSnapshotSitesGolden(t *testing.T) {
	srv := newFixtureServer(t)
	out, _ := runSnapshot(t, "sites", "--controller", srv.URL)
	compareGolden(t, "sites.golden.yaml", out)
}

func TestSnapshotControllerGolden(t *testing.T) {
	// `snapshot controller` never reads the console, so a fixed URL keeps the golden
	// deterministic.
	out, _ := runSnapshot(t, "controller", "--controller", "https://unifi.example.com")
	compareGolden(t, "controller.golden.yaml", out)
}

func TestSnapshotControllerWarnsOnInsecureURL(t *testing.T) {
	out, errOut := runSnapshot(t, "controller", "--controller", "http://unifi.example.com")
	if !strings.Contains(errOut, "https://") {
		t.Errorf("stderr = %q, want an insecure-controller-url warning", errOut)
	}

	docs := parseEmittedCRs(t, out)
	if len(docs) != 1 {
		t.Fatalf("got %d emitted CRs, want 1", len(docs))
	}
	if got := docs[0].Metadata.Annotations[snapshot.AnnotationInsecureControllerURL]; got != "true" {
		t.Errorf("annotation %s = %q, want %q", snapshot.AnnotationInsecureControllerURL, got, "true")
	}
}

func TestSnapshotSpecHasNoOpaqueID(t *testing.T) {
	srv := newFixtureServer(t)
	out, _ := runSnapshot(t, "all", "--controller", srv.URL)

	for _, id := range []string{
		fixtureNetworkIDOne, fixtureNetworkIDTwo, fixtureNetworkIDThree,
		fixtureZoneID, fixtureDeviceID, fixtureSiteID, fixtureBranchSiteID,
	} {
		if strings.Contains(out, id) {
			t.Errorf("emitted output contains upstream identifier %q:\n%s", id, out)
		}
	}

	docs := parseEmittedCRs(t, out)
	if len(docs) != 6 {
		t.Fatalf("got %d emitted CRs, want 6 (controller + 2 sites + 3 networks)", len(docs))
	}
	for _, doc := range docs {
		if doc.APIVersion != "unifi.supporterino.de/v1alpha1" {
			t.Errorf("apiVersion = %q, want unifi.supporterino.de/v1alpha1", doc.APIVersion)
		}
		if doc.Metadata.Name == "" {
			t.Errorf("%s is missing metadata.name", doc.Kind)
		}
		if err := rejectOpaqueKeys(doc.Spec); err != nil {
			t.Errorf("%s/%s: %v", doc.Kind, doc.Metadata.Name, err)
		}
		if doc.Kind == "UnifiNetwork" {
			if err := validateNetworkSpec(doc.Spec); err != nil {
				t.Errorf("UnifiNetwork/%s does not satisfy the operator schema: %v", doc.Metadata.Name, err)
			}
		}
	}
}

var opaqueKeyNames = map[string]bool{
	"id": true, "_id": true, "siteid": true, "networkid": true, "controllerid": true,
	"upstreamid": true, "zoneid": true, "deviceid": true, "prefixdelegationwaninterfaceid": true,
}

// rejectOpaqueKeys walks a decoded spec and rejects any field that would carry an opaque
// upstream identifier.
func rejectOpaqueKeys(v any) error {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			if opaqueKeyNames[strings.ToLower(key)] {
				return fmt.Errorf("opaque identifier field %q is not allowed in spec", key)
			}
			if err := rejectOpaqueKeys(child); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range value {
			if err := rejectOpaqueKeys(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// validateNetworkSpec enforces the constraints the operator's UnifiNetwork CRD declares.
func validateNetworkSpec(spec map[string]any) error {
	siteRef, ok := spec["siteRef"].(map[string]any)
	if !ok || asString(siteRef["name"]) == "" {
		return errors.New("spec.siteRef.name is required and must be non-empty")
	}
	if asString(spec["name"]) == "" {
		return errors.New("spec.name is required and must be non-empty")
	}
	vlan, ok := spec["vlanId"].(float64)
	if !ok || vlan < 1 || vlan > 4009 {
		return fmt.Errorf("spec.vlanId %v is outside the CRD range 1..4009", spec["vlanId"])
	}

	management := asString(spec["management"])
	_, hasGateway := spec["gateway"]
	_, hasSwitch := spec["switch"]
	switch management {
	case "GATEWAY":
		if !hasGateway {
			return errors.New("spec.gateway is required for management GATEWAY")
		}
		if hasSwitch {
			return errors.New("spec.switch is forbidden for management GATEWAY")
		}
		return validateGatewaySpec(spec["gateway"])
	case "SWITCH":
		if !hasSwitch {
			return errors.New("spec.switch is required for management SWITCH")
		}
		if hasGateway {
			return errors.New("spec.gateway is forbidden for management SWITCH")
		}
		return validateSwitchSpec(spec["switch"])
	case "UNMANAGED":
		if hasGateway || hasSwitch {
			return errors.New("UNMANAGED must not carry a gateway or switch variant")
		}
	default:
		return fmt.Errorf("spec.management %q is not GATEWAY, SWITCH, or UNMANAGED", management)
	}
	return nil
}

func validateGatewaySpec(v any) error {
	gateway, ok := v.(map[string]any)
	if !ok {
		return errors.New("spec.gateway must be an object")
	}
	for _, field := range []string{"cellularBackupEnabled", "internetAccessEnabled", "isolationEnabled"} {
		if _, ok := gateway[field].(bool); !ok {
			return fmt.Errorf("spec.gateway.%s is required and must be a boolean", field)
		}
	}
	return validateIPv4(gateway["ipv4Configuration"])
}

func validateSwitchSpec(v any) error {
	switchSpec, ok := v.(map[string]any)
	if !ok {
		return errors.New("spec.switch must be an object")
	}
	for _, field := range []string{"cellularBackupEnabled", "isolationEnabled"} {
		if _, ok := switchSpec[field].(bool); !ok {
			return fmt.Errorf("spec.switch.%s is required and must be a boolean", field)
		}
	}
	deviceTagRef, ok := switchSpec["deviceTagRef"].(map[string]any)
	if !ok || asString(deviceTagRef["name"]) == "" {
		return errors.New("spec.switch.deviceTagRef.name is required")
	}
	return validateIPv4(switchSpec["ipv4Configuration"])
}

func validateIPv4(v any) error {
	ipv4, ok := v.(map[string]any)
	if !ok {
		return errors.New("ipv4Configuration is required")
	}
	if _, ok := ipv4["autoScaleEnabled"].(bool); !ok {
		return errors.New("ipv4Configuration.autoScaleEnabled is required")
	}
	if asString(ipv4["hostIpAddress"]) == "" {
		return errors.New("ipv4Configuration.hostIpAddress is required")
	}
	prefix, ok := ipv4["prefixLength"].(float64)
	if !ok || prefix < 8 || prefix > 30 {
		return fmt.Errorf("ipv4Configuration.prefixLength %v is outside the CRD range 8..30", ipv4["prefixLength"])
	}
	return nil
}

func TestSnapshotWritesDirectory(t *testing.T) {
	srv := newFixtureServer(t)
	dir := t.TempDir()
	runSnapshot(t, "all", "--controller", srv.URL, "--output", dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 6 {
		t.Fatalf("got %d files, want 6 (controller + 2 sites + 3 networks)", len(entries))
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
	const body = `{"count":2,"data":[` +
		`{"id":"id-1","management":"UNMANAGED","name":"Guest WiFi","vlanId":20,"enabled":true},` +
		`{"id":"id-2","management":"UNMANAGED","name":"guest-wifi","vlanId":30,"enabled":true}],` +
		`"limit":200,"offset":0,"totalCount":2}`
	srv := newServer(t, body)

	dir := t.TempDir()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"snapshot", "networks", "--controller", srv.URL, "--output", dir})
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

// newDetailStatusServer serves one listed network (overview only) and answers its detail
// request with the given HTTP status.
func newDetailStatusServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	const networksBody = `{"count":1,"data":[` +
		`{"id":"` + fixtureNetworkIDOne + `","management":"UNMANAGED","name":"Default",` +
		`"enabled":true,"vlanId":1,"default":true}],` +
		`"limit":200,"offset":0,"totalCount":1}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sites"):
			_, _ = w.Write([]byte(`{"count":1,"data":[{"id":"` + fixtureSiteID + `","internalReference":"default","name":"Default"}],"limit":200,"offset":0,"totalCount":1}`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(networksBody))
		default:
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"statusCode":` + fmt.Sprint(status) + `,"statusName":"ERROR","code":"api.network.error","message":"detail failure"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSnapshotSkipsVanishedNetwork(t *testing.T) {
	srv := newDetailStatusServer(t, http.StatusNotFound)

	out, errOut := runSnapshot(t, "networks", "--controller", srv.URL)
	if docs := parseEmittedCRs(t, out); len(docs) != 0 {
		t.Fatalf("got %d CRs for a vanished network, want 0:\n%s", len(docs), out)
	}
	if !strings.Contains(errOut, "disappeared") {
		t.Errorf("stderr = %q, want a vanished-network warning", errOut)
	}
}

func TestSnapshotSurfacesDetailError(t *testing.T) {
	srv := newDetailStatusServer(t, http.StatusInternalServerError)

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"snapshot", "networks", "--controller", srv.URL})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected the snapshot to fail on a non-404 detail error")
	}
	if !strings.Contains(err.Error(), "details") {
		t.Errorf("error = %v, want it to name the failing detail read", err)
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
	if !strings.Contains(out.String(), "controller") || !strings.Contains(out.String(), "sites") {
		t.Errorf("help output does not list the supported kinds:\n%s", out.String())
	}
}

func TestSnapshotResolvesAPIKeyFromEnv(t *testing.T) {
	t.Setenv("UNIFI_API_KEY", "env-secret")

	if got := captureAPIKey(t, nil); got != "env-secret" {
		t.Errorf("%s = %q, want %q", unifi.APIKeyHeader, got, "env-secret")
	}
}

func TestSnapshotAPIKeyFlagWinsOverEnv(t *testing.T) {
	t.Setenv("UNIFI_API_KEY", "env-secret")

	if got := captureAPIKey(t, []string{"--api-key", "flag-secret"}); got != "flag-secret" {
		t.Errorf("%s = %q, want %q", unifi.APIKeyHeader, got, "flag-secret")
	}
}

// captureAPIKey runs snapshot against an httptest server (empty sites and networks) and
// returns the value the client sent in the API key header.
func captureAPIKey(t *testing.T, extraArgs []string) string {
	t.Helper()

	keyCh := make(chan string, 4)
	srv := httptest.NewServer(apiKeyHandler(keyCh))
	t.Cleanup(srv.Close)

	args := append([]string{"snapshot", "all", "--controller", srv.URL}, extraArgs...)
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

func apiKeyHandler(keyCh chan<- string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case keyCh <- r.Header.Get(unifi.APIKeyHeader):
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sites"):
			_, _ = w.Write([]byte(`{"count":1,"data":[{"id":"site-1","internalReference":"default","name":"Default"}],"limit":200,"offset":0,"totalCount":1}`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(`{"count":0,"data":[],"limit":200,"offset":0,"totalCount":0}`))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	})
}

// newServer serves the given networks page for the default site with empty sites. Each
// listed network's detail endpoint echoes the list element, so the overview-only list is
// sufficient for networks whose variant fields are not exercised.
func newServer(t *testing.T, networksBody string) *httptest.Server {
	t.Helper()
	byID := indexNetworksByID(t, networksBody)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/sites"):
			_, _ = w.Write([]byte(`{"count":1,"data":[{"id":"` + fixtureSiteID + `","internalReference":"default","name":"Default"}],"limit":200,"offset":0,"totalCount":1}`))
		case strings.HasSuffix(r.URL.Path, "/networks"):
			_, _ = w.Write([]byte(networksBody))
		case strings.Contains(r.URL.Path, "/networks/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			detail, ok := byID[id]
			if !ok {
				http.Error(w, "unknown network "+id, http.StatusNotFound)
				return
			}
			_, _ = w.Write(detail)
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// indexNetworksByID decodes a list page and indexes each element by its upstream id so a
// detail request can echo it.
func indexNetworksByID(t *testing.T, body string) map[string][]byte {
	t.Helper()
	var page struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("parse networks fixture: %v", err)
	}
	out := make(map[string][]byte, len(page.Data))
	for _, raw := range page.Data {
		var item struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatalf("parse network id: %v", err)
		}
		out[item.ID] = raw
	}
	return out
}

func TestSnapshotResolvesSiteInternalReference(t *testing.T) {
	srv := newFixtureServer(t)
	out, _ := runSnapshot(t, "networks", "--controller", srv.URL, "--site", "branch")

	docs := parseEmittedCRs(t, out)
	if len(docs) != 0 {
		t.Fatalf("got %d networks for the branch site, want 0", len(docs))
	}

	out, _ = runSnapshot(t, "networks", "--controller", srv.URL, "--site", "default")
	for _, doc := range parseEmittedCRs(t, out) {
		siteRef := doc.Spec["siteRef"].(map[string]any)
		if asString(siteRef["name"]) != "default" {
			t.Errorf("siteRef.name = %q, want default", siteRef["name"])
		}
	}
}

func TestSnapshotSiteNotFound(t *testing.T) {
	srv := newFixtureServer(t)

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"snapshot", "networks", "--controller", srv.URL, "--site", "missing"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected an error for a missing site")
	}
	if !strings.Contains(err.Error(), "missing") || !strings.Contains(err.Error(), "default") {
		t.Errorf("error = %v, want it to name the missing site and the available sites", err)
	}
}

func TestSnapshotResolvesControllerFromEnv(t *testing.T) {
	srv := newServer(t, `{"count":0,"data":[],"limit":200,"offset":0,"totalCount":0}`)
	t.Setenv("UNIFI_URL", srv.URL)

	root := NewRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"snapshot", "networks"})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot via UNIFI_URL: %v\n%s", err, out.String())
	}
}

func TestSnapshotRequiresController(t *testing.T) {
	t.Setenv("UNIFI_URL", "")

	root := NewRootCommand()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"snapshot", "networks"})
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
		Name        string            `json:"name"`
		Annotations map[string]string `json:"annotations"`
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
