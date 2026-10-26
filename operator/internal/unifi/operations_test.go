package unifi

import (
	"bufio"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

const (
	// testConsoleURL is a console base URL with no path prefix of its own.
	testConsoleURL = "https://console.example"

	// testIntegrationBasePath is the Integration v1 base path, spelled out so the
	// normalizer is pinned against docs/unifi-api.md rather than against the
	// constant it strips.
	testIntegrationBasePath = "/proxy/network/integration"

	// Alternative upstream identifiers: one endpoint called for two sites or two
	// objects must still produce one operation label.
	testSiteIDAlt   = "3c9e1f4b-7a2d-4e8f-9b61-0d5c8a7e2f10"
	testObjectIDAlt = "d41f8b02-6c35-4a19-8e77-5b2a9c6d3e84"

	// operationNetworks is the label both cases of one resource family expect.
	operationNetworks = "networks"
)

var (
	// operationPlaceholder matches a documented path placeholder such as {siteId}.
	operationPlaceholder = regexp.MustCompile(`\{[^{}]*\}`)
	// documentedEndpointRow matches the endpoint cell of a docs/unifi-api.md row.
	documentedEndpointRow = regexp.MustCompile("^`(/v1/[^`]*)`$")
	// operationUUIDPattern matches the 8-4-4-4-12 form UniFi uses for object IDs.
	operationUUIDPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
)

// wantOperations is the whole operation label set, sorted. It is the closed set
// the upstream metrics may ever carry: adding a client call for a new surface
// means adding a row to docs/unifi-api.md, a template to operationTemplates, and
// this entry.
var wantOperations = []string{
	"acl-rules",
	"acl-rules.ordering",
	"device-tags",
	"dns.policies",
	testOperationFirewallPolicies,
	"firewall.policies.ordering",
	"firewall.zones",
	testOperationInfo,
	operationNetworks,
	"radius.profiles",
	"sites",
	"switching.lags",
	"switching.mc-lag-domains",
	"switching.switch-stacks",
	"traffic-matching-lists",
	"vpn.servers",
	"vpn.site-to-site-tunnels",
	"wifi.broadcasts",
}

// TestOperationLabelForEveryDocumentedEndpoint is the drift guard: every
// endpoint listed in docs/unifi-api.md — the authoritative inventory — must
// normalize to a stable, non-empty label drawn from wantOperations.
func TestOperationLabelForEveryDocumentedEndpoint(t *testing.T) {
	allowed := make(map[string]struct{}, len(wantOperations))
	for _, operation := range wantOperations {
		allowed[operation] = struct{}{}
	}

	produced := make(map[string]struct{}, len(wantOperations))
	for _, path := range documentedEndpoints(t) {
		t.Run(strings.ReplaceAll(strings.TrimPrefix(path, "/v1/"), "/", "."), func(t *testing.T) {
			got := operationFromEndpoint(consoleEndpoint(path, testSiteID, testNetworkID))
			if got == "" || got == operationUnknown {
				t.Fatalf("%s: operation label = %q, want a label from the bounded set", path, got)
			}
			if _, ok := allowed[got]; !ok {
				t.Fatalf("%s: operation label %q is not in the bounded set", path, got)
			}
			assertBoundedOperationLabel(t, path, got)
			produced[got] = struct{}{}
		})
	}

	if got := slices.Sorted(maps.Keys(produced)); !slices.Equal(got, wantOperations) {
		t.Errorf("documented endpoint operations = %v, want %v", got, wantOperations)
	}
}

// TestOperationLabelCollapsesSiteAndObjectIDs pins the cardinality guarantee:
// the site UUID, the object UUIDs, and the query string never reach the label,
// so repeated calls for different sites and objects share one series.
func TestOperationLabelCollapsesSiteAndObjectIDs(t *testing.T) {
	const (
		networksPath    = "/v1/sites/{siteId}/networks"
		networkPath     = "/v1/sites/{siteId}/networks/{networkId}"
		policyPath      = "/v1/sites/{siteId}/firewall/policies/{firewallPolicyId}"
		orderingPath    = "/v1/sites/{siteId}/firewall/policies/ordering"
		aclRulePath     = "/v1/sites/{siteId}/acl-rules/{aclRuleId}"
		aclOrderingPath = "/v1/sites/{siteId}/acl-rules/ordering"
	)

	cases := []struct {
		name   string
		first  string
		second string
		want   string
	}{
		{
			name:   "collection with pagination",
			first:  consoleEndpoint(networksPath, testSiteID, testNetworkID) + "?offset=0&limit=200",
			second: consoleEndpoint(networksPath, testSiteIDAlt, testObjectIDAlt) + "?offset=200&limit=200",
			want:   operationNetworks,
		},
		{
			name:   "object by UUID",
			first:  consoleEndpoint(networkPath, testSiteID, testNetworkID),
			second: consoleEndpoint(networkPath, testSiteIDAlt, testObjectIDAlt),
			want:   operationNetworks,
		},
		{
			name:   "firewall policy object",
			first:  consoleEndpoint(policyPath, testSiteID, testNetworkID),
			second: consoleEndpoint(policyPath, testSiteIDAlt, testObjectIDAlt),
			want:   testOperationFirewallPolicies,
		},
		{
			name:   "ordering with zone query parameters",
			first:  consoleEndpoint(orderingPath, testSiteID, testNetworkID) + "?sourceFirewallZoneId=" + testNetworkID + "&destinationFirewallZoneId=" + testNetworkID,
			second: consoleEndpoint(orderingPath, testSiteIDAlt, testObjectIDAlt) + "?sourceFirewallZoneId=" + testObjectIDAlt + "&destinationFirewallZoneId=" + testObjectIDAlt,
			want:   "firewall.policies.ordering",
		},
		{
			name:   "ACL rule object",
			first:  consoleEndpoint(aclRulePath, testSiteID, testNetworkID),
			second: consoleEndpoint(aclRulePath, testSiteIDAlt, testObjectIDAlt),
			want:   "acl-rules",
		},
		{
			name:   "ACL rule ordering is not read as an ACL rule",
			first:  consoleEndpoint(aclOrderingPath, testSiteID, testNetworkID),
			second: consoleEndpoint(aclOrderingPath, testSiteIDAlt, testObjectIDAlt),
			want:   "acl-rules.ordering",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			first, second := operationFromEndpoint(tc.first), operationFromEndpoint(tc.second)
			if first != tc.want || second != tc.want {
				t.Fatalf("operations = %q, %q, want both %q", first, second, tc.want)
			}
			assertBoundedOperationLabel(t, tc.first, first)
			assertBoundedOperationLabel(t, tc.second, second)
		})
	}
}

// TestOperationLabelForUnmappedPathIsUnknown pins the fallback: a path outside
// the documented surface collapses to one fixed value instead of exporting a
// URL, an upstream identifier, or an endpoint this client does not own.
func TestOperationLabelForUnmappedPathIsUnknown(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
	}{
		{name: "no API base path", endpoint: testConsoleURL + "/firewall/policies"},
		{name: "base path only", endpoint: testConsoleURL + testIntegrationBasePath + "/v1"},
		{name: "unimplemented upstream endpoint", endpoint: consoleEndpoint("/v1/sites/{siteId}/devices", testSiteID, testNetworkID)},
		{name: "unexpected trailing segment", endpoint: consoleEndpoint("/v1/sites/{siteId}/networks/{networkId}", testSiteID, testNetworkID) + "/extra"},
		{name: "unparseable URL", endpoint: "://console.example"},
		{name: "escaped separator in a segment", endpoint: consoleEndpoint("/v1/sites/{siteId}", testSiteID, testNetworkID) + "/%2e%2e"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := operationFromEndpoint(tc.endpoint); got != operationUnknown {
				t.Errorf("operation = %q, want %q", got, operationUnknown)
			}
		})
	}
}

// documentedEndpoints returns every endpoint path listed in the table rows of
// docs/unifi-api.md's endpoint map.
func documentedEndpoints(t *testing.T) []string {
	t.Helper()

	const docPath = "../../../docs/unifi-api.md"
	file, err := os.Open(docPath)
	if err != nil {
		t.Fatalf("open %s: %v", docPath, err)
	}
	defer func() { _ = file.Close() }()

	seen := map[string]struct{}{}
	var endpoints []string
	rows := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		// A map row is | Method | `endpoint` | ... — the path is the third cell.
		cells := strings.Split(scanner.Text(), "|")
		if len(cells) < 3 {
			continue
		}
		match := documentedEndpointRow.FindStringSubmatch(strings.TrimSpace(cells[2]))
		if match == nil || strings.Contains(match[1], "...") {
			continue
		}
		rows++
		if _, ok := seen[match[1]]; ok {
			continue
		}
		seen[match[1]] = struct{}{}
		endpoints = append(endpoints, match[1])
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read %s: %v", docPath, err)
	}
	if rows < 45 || len(endpoints) < 25 {
		t.Fatalf("found %d endpoint rows (%d distinct paths) in %s, want the full endpoint map", rows, len(endpoints), docPath)
	}
	return endpoints
}

// consoleEndpoint renders a documented path, substituting a UUID for each
// placeholder in order (site first, then the object), as the absolute URL
// HTTPClient.endpoint builds.
func consoleEndpoint(path, siteID, objectID string) string {
	first := true
	concrete := operationPlaceholder.ReplaceAllStringFunc(path, func(string) string {
		if first {
			first = false
			return siteID
		}
		return objectID
	})
	return testConsoleURL + testIntegrationBasePath + concrete
}

// assertBoundedOperationLabel rejects the shapes a leaked identifier would take
// in an operation label.
func assertBoundedOperationLabel(t *testing.T, path, label string) {
	t.Helper()

	switch {
	case strings.Contains(label, "/"):
		t.Errorf("%s: label %q contains a path separator", path, label)
	case operationUUIDPattern.MatchString(label):
		t.Errorf("%s: label %q contains an upstream UUID", path, label)
	case strings.ContainsAny(label, " \t\n"):
		t.Errorf("%s: label %q contains whitespace", path, label)
	}
}
