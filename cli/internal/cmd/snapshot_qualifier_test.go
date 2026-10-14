package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// newSharedNameFixtureServer serves two sites that deliberately share a site-scoped object
// name ("Default") and a firewall zone pair (the built-in Internal -> External pair), so a
// test can prove `snapshot all` disambiguates them by site rather than emitting duplicate
// (kind, metadata.name) values. Every other endpoint answers with an empty page.
func newSharedNameFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()

	const (
		alphaID        = "11111111-1111-4111-8111-111111111111"
		betaID         = "22222222-2222-4222-8222-222222222222"
		networkID      = "33333333-3333-4333-8333-333333333333"
		customZoneID   = "44444444-4444-4444-8444-444444444444"
		internalZoneID = "55555555-5555-4555-8555-555555555555"
		externalZoneID = "66666666-6666-4666-8666-666666666666"
		policyID       = "77777777-7777-4777-8777-777777777777"
	)

	sitesBody := `{"count":2,"data":[` +
		`{"id":"` + alphaID + `","internalReference":"alpha","name":"Alpha"},` +
		`{"id":"` + betaID + `","internalReference":"beta","name":"Beta"}],` +
		`"limit":200,"offset":0,"totalCount":2}`
	networksBody := `{"count":1,"data":[{"id":"` + networkID + `","management":"UNMANAGED",` +
		`"name":"Default","enabled":true,"vlanId":1,"default":true,` +
		`"metadata":{"origin":"SYSTEM_DEFINED","configurable":true}}],"limit":200,"offset":0,"totalCount":1}`
	networkDetail := `{"id":"` + networkID + `","management":"UNMANAGED","name":"Default",` +
		`"enabled":true,"vlanId":1,"default":true,"metadata":{"origin":"SYSTEM_DEFINED","configurable":true}}`
	zonesBody := `{"count":3,"data":[` +
		`{"id":"` + customZoneID + `","name":"Custom Zone","networkIds":["` + networkID + `"],` +
		`"metadata":{"origin":"USER_DEFINED","configurable":true}},` +
		`{"id":"` + internalZoneID + `","name":"Internal","networkIds":[],` +
		`"metadata":{"origin":"SYSTEM_DEFINED","configurable":true}},` +
		`{"id":"` + externalZoneID + `","name":"External","networkIds":[],` +
		`"metadata":{"origin":"SYSTEM_DEFINED","configurable":true}}],` +
		`"limit":200,"offset":0,"totalCount":3}`
	policiesBody := `{"count":1,"data":[{"id":"` + policyID + `","index":0,"name":"Allow DNS",` +
		`"enabled":true,"loggingEnabled":false,"action":{"type":"BLOCK"},` +
		`"ipProtocolScope":{"ipVersion":"IPV4"},` +
		`"source":{"zoneId":"` + internalZoneID + `"},` +
		`"destination":{"zoneId":"` + externalZoneID + `"},` +
		`"metadata":{"origin":"USER_DEFINED"}}],"limit":200,"offset":0,"totalCount":1}`
	orderingBody := `{"orderedFirewallPolicyIds":{"beforeSystemDefined":["` + policyID + `"],"afterSystemDefined":[]}}`
	aclOrderingBody := `{"orderedAclRuleIds":[]}`
	emptyPage := []byte(`{"count":0,"data":[],"limit":200,"offset":0,"totalCount":0}`)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path
		switch {
		case path == pathSites:
			_, _ = w.Write([]byte(sitesBody))
		case strings.HasSuffix(path, "/firewall/policies/ordering"):
			_, _ = w.Write([]byte(orderingBody))
		case strings.HasSuffix(path, "/acl-rules/ordering"):
			_, _ = w.Write([]byte(aclOrderingBody))
		case strings.HasSuffix(path, "/firewall/policies"):
			_, _ = w.Write([]byte(policiesBody))
		case strings.HasSuffix(path, "/firewall/zones"):
			_, _ = w.Write([]byte(zonesBody))
		case strings.HasSuffix(path, "/networks/"+networkID):
			_, _ = w.Write([]byte(networkDetail))
		case strings.HasSuffix(path, "/networks"):
			_, _ = w.Write([]byte(networksBody))
		case strings.Contains(path, "/sites/"):
			_, _ = w.Write(emptyPage)
		default:
			http.Error(w, "unexpected path "+path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSnapshotAllSiteQualifiesSharedNames proves design D7: when two sites share a
// site-scoped object name and a firewall zone pair, `snapshot all` emits distinct
// (kind, metadata.name) values for them, writes one file per resource (no overwrite), and
// `snapshot verify` reads the directory back with no unexplained gap. A per-kind command
// still emits the unqualified upstream-derived name.
func TestSnapshotAllSiteQualifiesSharedNames(t *testing.T) {
	srv := newSharedNameFixtureServer(t)

	// Run `all` to stdout to inspect the emitted set: this is where a cross-site name
	// collision shows up as a duplicate (kind, metadata.name).
	stream, _ := runSnapshot(t, "all", "--controller", srv.URL)
	emitted := parseEmittedCRs(t, stream)

	seen := make(map[string]bool, len(emitted))
	for _, doc := range emitted {
		key := doc.Kind + "/" + doc.Metadata.Name
		if seen[key] {
			t.Errorf("`all` emitted duplicate (kind, name) %s; two sites collided", key)
		}
		seen[key] = true
	}
	for _, want := range []string{
		"UnifiNetwork/alpha-default",
		"UnifiNetwork/beta-default",
		"UnifiFirewallPolicyOrdering/alpha-internal-to-external",
		"UnifiFirewallPolicyOrdering/beta-internal-to-external",
		"UnifiAclRuleOrdering/alpha-acl-rule-ordering",
		"UnifiAclRuleOrdering/beta-acl-rule-ordering",
	} {
		if !seen[want] {
			t.Errorf("`all` did not emit the site-qualified resource %s", want)
		}
	}

	// Writing to a directory must produce one file per emitted resource; a duplicate name
	// would overwrite the first site's file and leave the directory short.
	dir := t.TempDir()
	root := NewRootCommand()
	var stdout, stderr bytes.Buffer
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"snapshot", "all", "--controller", srv.URL, "--output", dir})
	if err := root.Execute(); err != nil {
		t.Fatalf("snapshot all: %v\n%s", err, stderr.String())
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != len(emitted) {
		t.Errorf("directory holds %d file(s) for %d emitted resource(s); a name collision overwrote one", len(entries), len(emitted))
	}

	out, _, err := runVerify(t, "--controller", srv.URL, "--output", dir)
	if err != nil {
		t.Fatalf("verify of a site-qualified snapshot: %v\n%s", err, out)
	}
	if !strings.Contains(out, "result: OK") {
		t.Errorf("verify report is not OK:\n%s", out)
	}

	// A per-kind command projects a single site and keeps the upstream-derived name.
	perKind, _ := runSnapshot(t, "networks", "--controller", srv.URL, "--site", "alpha")
	docs := parseEmittedCRs(t, perKind)
	if len(docs) != 1 {
		t.Fatalf("per-kind networks emitted %d CRs, want 1:\n%s", len(docs), perKind)
	}
	if docs[0].Metadata.Name != "default" {
		t.Errorf("per-kind network name = %q, want unqualified %q", docs[0].Metadata.Name, "default")
	}
	perKindOrdering, _ := runSnapshot(t, "firewall-policy-ordering", "--controller", srv.URL, "--site", "alpha")
	orderings := parseEmittedCRs(t, perKindOrdering)
	if len(orderings) != 1 {
		t.Fatalf("per-kind firewall-policy-ordering emitted %d CRs, want 1:\n%s", len(orderings), perKindOrdering)
	}
	if orderings[0].Metadata.Name != "internal-to-external" {
		t.Errorf("per-kind ordering name = %q, want unqualified %q", orderings[0].Metadata.Name, "internal-to-external")
	}
}
