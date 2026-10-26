package unifi

import (
	"net/url"
	"slices"
	"strings"
)

const (
	// operationAnySegment matches exactly one path segment that carries an
	// upstream identifier — the site UUID or an object UUID. It is matched but
	// never copied into the operation label.
	operationAnySegment = "{}"

	// operationUnknown labels a request whose endpoint is not part of the
	// documented Integration v1 surface (docs/unifi-api.md). An unmapped, future,
	// or hostile path is never exported verbatim: it costs one fixed series
	// instead of unbounded cardinality and an upstream identifier in a label.
	operationUnknown = "unknown"

	// operationSegmentSeparator joins an operation's path segments into its
	// label, so the label is a template of the API surface rather than a URL.
	operationSegmentSeparator = "."

	// operationSitesSegment is the path segment that carries the site list and
	// introduces every site-scoped path.
	operationSitesSegment = "sites"
)

// rootOperations are the operations whose path sits directly below the API base
// path.
var rootOperations = []string{"info", operationSitesSegment}

// siteOperations is the closed set of site-scoped operations — the operation
// label set this client can produce (design D3). Each label is the resource path
// below `/v1/sites/{siteId}`, joined with ".": `acl-rules`, `firewall.policies`,
// `vpn.site-to-site-tunnels`. The collection and the single-object forms of one
// resource share a label, because the exported metric carries no method label.
// Adding a client call for a new surface means adding its row to
// docs/unifi-api.md and its label here.
var siteOperations = []string{
	"networks",
	"firewall.zones",
	"firewall.policies",
	"firewall.policies.ordering",
	"acl-rules",
	"acl-rules.ordering",
	"device-tags",
	"wifi.broadcasts",
	"radius.profiles",
	"traffic-matching-lists",
	"dns.policies",
	"switching.lags",
	"switching.mc-lag-domains",
	"switching.switch-stacks",
	"vpn.servers",
	"vpn.site-to-site-tunnels",
}

// operationTemplate maps a path shape below the API base path to the operation
// label it exports. Templates are built from the label lists above, so a label
// and the path it stands for cannot drift apart, and no part of a request URL
// can reach the label.
type operationTemplate struct {
	segments  []string
	operation string
}

// operationTemplates is the closed set of accepted path shapes: one per root
// operation, and the collection plus object form of every site-scoped one.
var operationTemplates = buildOperationTemplates()

// buildOperationTemplates expands the operation labels into path shapes.
func buildOperationTemplates() []operationTemplate {
	templates := make([]operationTemplate, 0, len(rootOperations)+2*len(siteOperations))
	for _, operation := range rootOperations {
		templates = append(templates, operationTemplate{segments: []string{operation}, operation: operation})
	}
	for _, operation := range siteOperations {
		collection := append([]string{operationSitesSegment, operationAnySegment}, strings.Split(operation, operationSegmentSeparator)...)
		templates = append(templates,
			operationTemplate{segments: collection, operation: operation},
			operationTemplate{segments: append(slices.Clone(collection), operationAnySegment), operation: operation},
		)
	}
	return templates
}

// operationFromEndpoint derives the operation label for a request URL. It
// strips the scheme, host, API base path, site UUID, object UUIDs, and query
// string, then looks the remaining path shape up in operationTemplates. A path
// that matches no template — an endpoint newer than this table, or one built
// outside endpoint() — is reported as operationUnknown.
func operationFromEndpoint(endpoint string) string {
	segments, ok := apiPathSegments(endpoint)
	if !ok {
		return operationUnknown
	}
	operation := operationUnknown
	matched := -1
	for _, template := range operationTemplates {
		literals, ok := template.match(segments)
		if ok && literals > matched {
			operation, matched = template.operation, literals
		}
	}
	return operation
}

// match reports whether the template fits the given segments and how many
// literal segments it pinned. A template with more literals is more specific:
// `sites/{}/acl-rules/ordering` beats `sites/{}/acl-rules/{}`.
func (t operationTemplate) match(segments []string) (int, bool) {
	if len(t.segments) != len(segments) {
		return 0, false
	}
	literals := 0
	for i, want := range t.segments {
		if want == operationAnySegment {
			continue
		}
		if segments[i] != want {
			return 0, false
		}
		literals++
	}
	return literals, true
}

// apiPathSegments returns the path segments below the fixed API base path. The
// base path is located by its own segments rather than by string concatenation,
// so a console URL that carries a path prefix cannot shift the match.
func apiPathSegments(endpoint string) ([]string, bool) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, false
	}
	// EscapedPath keeps an escaped separator inside a segment from splitting it.
	segments := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	for i := 0; i+len(basePathSegments) <= len(segments); i++ {
		if !slices.Equal(segments[i:i+len(basePathSegments)], basePathSegments) {
			continue
		}
		rest := segments[i+len(basePathSegments):]
		if len(rest) == 0 {
			return nil, false
		}
		return rest, true
	}
	return nil, false
}
