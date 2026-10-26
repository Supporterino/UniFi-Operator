/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package utils

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"
)

// Paths are relative to this package's directory, which is the working directory `go test` uses.
// The chart's manager role is a hand-maintained copy of the generated one, so the two are compared
// here rather than trusted to stay in step by review.
const (
	generatedRolePath = "../../config/rbac/role.yaml"
	chartRolePath     = "../../../charts/unifi-operator/templates/role.yaml"
)

// helmAction matches an inline Helm action such as {{ include "unifi-operator.fullname" . }}.
var helmAction = regexp.MustCompile(`\{\{[^}]*\}\}`)

// ruleCount records how often one normalized rule appears in a role.
type ruleCount struct {
	rule  rbacv1.PolicyRule
	count int
}

func TestRBACGeneratedRoleMatchesChartRole(t *testing.T) {
	t.Parallel()

	generated := countRules(loadGeneratedRole(t).Rules)
	chart := countRules(loadChartRole(t).Rules)

	missing := rulesMissingFrom(generated, chart)
	extra := rulesMissingFrom(chart, generated)
	if len(missing) == 0 && len(extra) == 0 {
		return
	}

	t.Error(divergenceReport(missing, extra))
}

// TestRBACRolesGrantEventWrites pins the events grant on both roles: the manager needs
// create/patch on core events to emit condition-transition Events.
func TestRBACRolesGrantEventWrites(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		path string
		role *rbacv1.ClusterRole
	}{
		{path: generatedRolePath, role: loadGeneratedRole(t)},
		{path: chartRolePath, role: loadChartRole(t)},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			if !grantsEventWrites(tc.role.Rules) {
				t.Fatalf("%s grants no rule with create and patch on core events", tc.path)
			}
		})
	}
}

func grantsEventWrites(rules []rbacv1.PolicyRule) bool {
	for _, rule := range rules {
		if !slices.Contains(rule.APIGroups, "") && !slices.Contains(rule.APIGroups, "*") {
			continue
		}
		if !slices.Contains(rule.Resources, "events") && !slices.Contains(rule.Resources, "*") {
			continue
		}
		if slices.Contains(rule.Verbs, "create") && slices.Contains(rule.Verbs, "patch") {
			return true
		}
	}

	return false
}

func countRules(rules []rbacv1.PolicyRule) map[string]*ruleCount {
	counts := make(map[string]*ruleCount, len(rules))
	for _, rule := range rules {
		normalized := normalizeRule(rule)
		key := formatRule(normalized)
		if rc, ok := counts[key]; ok {
			rc.count++
			continue
		}
		counts[key] = &ruleCount{rule: normalized, count: 1}
	}

	return counts
}

// rulesMissingFrom returns the rules that want grants more often than have does. RBAC ignores rule
// order and the order inside a rule, so the comparison is a multiset of normalized rules.
func rulesMissingFrom(want, have map[string]*ruleCount) []*ruleCount {
	missing := make([]*ruleCount, 0, len(want))
	for key, rc := range want {
		if rc.count > countOf(have, key) {
			missing = append(missing, rc)
		}
	}

	slices.SortFunc(missing, func(a, b *ruleCount) int {
		return strings.Compare(formatRule(a.rule), formatRule(b.rule))
	})

	return missing
}

func countOf(counts map[string]*ruleCount, key string) int {
	if rc, ok := counts[key]; ok {
		return rc.count
	}

	return 0
}

func divergenceReport(missing, extra []*ruleCount) string {
	var b strings.Builder
	b.WriteString("the generated manager role and the Helm chart manager role grant different rules\n")
	fmt.Fprintf(&b, "  generated: %s\n", generatedRolePath)
	fmt.Fprintf(&b, "  chart:     %s\n", chartRolePath)

	if len(missing) > 0 {
		fmt.Fprintf(&b, "\ngranted by the generated role but not by the chart; add to %s:\n", chartRolePath)
		writeRules(&b, missing)
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "\ngranted by the chart role but not by the generated role; remove from %s:\n", chartRolePath)
		writeRules(&b, extra)
	}

	b.WriteString("\nrun 'task manifests' after changing a +kubebuilder:rbac marker, then mirror the change\n")

	return b.String()
}

func writeRules(b *strings.Builder, rules []*ruleCount) {
	for _, rc := range rules {
		fmt.Fprintf(b, "  - %s", formatRule(rc.rule))
		if rc.count > 1 {
			fmt.Fprintf(b, " (granted %d times)", rc.count)
		}
		b.WriteString("\n")
	}
}

// normalizeRule returns a copy of rule with every list sorted, so two rules that grant the same
// permissions compare equal regardless of how they are spelled.
func normalizeRule(rule rbacv1.PolicyRule) rbacv1.PolicyRule {
	return rbacv1.PolicyRule{
		APIGroups:       sortedCopy(rule.APIGroups),
		Resources:       sortedCopy(rule.Resources),
		ResourceNames:   sortedCopy(rule.ResourceNames),
		Verbs:           sortedCopy(rule.Verbs),
		NonResourceURLs: sortedCopy(rule.NonResourceURLs),
	}
}

func sortedCopy(values []string) []string {
	if values == nil {
		return nil
	}

	sorted := slices.Clone(values)
	slices.Sort(sorted)

	return sorted
}

// formatRule renders a normalized rule as a single stable, human-readable line that also serves as
// its comparison key.
func formatRule(rule rbacv1.PolicyRule) string {
	parts := []string{
		"apiGroups=" + formatStrings(rule.APIGroups),
		"resources=" + formatStrings(rule.Resources),
		"verbs=" + formatStrings(rule.Verbs),
	}
	if len(rule.ResourceNames) > 0 {
		parts = append(parts, "resourceNames="+formatStrings(rule.ResourceNames))
	}
	if len(rule.NonResourceURLs) > 0 {
		parts = append(parts, "nonResourceURLs="+formatStrings(rule.NonResourceURLs))
	}

	return strings.Join(parts, " ")
}

func formatStrings(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}

	return "[" + strings.Join(quoted, ", ") + "]"
}

func loadGeneratedRole(t *testing.T) *rbacv1.ClusterRole {
	t.Helper()

	return loadClusterRole(t, generatedRolePath, nil)
}

func loadChartRole(t *testing.T) *rbacv1.ClusterRole {
	t.Helper()

	return loadClusterRole(t, chartRolePath, stripHelmTemplate)
}

// loadClusterRole reads a ClusterRole manifest, rewriting the bytes first when the source is
// templated, and fails the test if the file is unreadable, unparseable, or grants no rules (an
// empty parse would make the comparison pass vacuously).
func loadClusterRole(t *testing.T, path string, rewrite func([]byte) []byte) *rbacv1.ClusterRole {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if rewrite != nil {
		content = rewrite(content)
	}

	var role rbacv1.ClusterRole
	if err := yaml.Unmarshal(content, &role); err != nil {
		t.Fatalf("parse %s as a ClusterRole: %v", path, err)
	}
	if role.Kind != "ClusterRole" {
		t.Fatalf("%s does not describe a ClusterRole (kind %q)", path, role.Kind)
	}
	if len(role.Rules) == 0 {
		t.Fatalf("%s grants no rules; the parity comparison would be vacuous", path)
	}

	return &role
}

// stripHelmTemplate drops Helm control lines and replaces inline actions with a placeholder, so the
// literal YAML the chart renders around them parses as a Kubernetes object.
func stripHelmTemplate(content []byte) []byte {
	var b strings.Builder
	b.Grow(len(content))

	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "{{") {
			continue
		}
		b.WriteString(helmAction.ReplaceAllString(line, "helm-template"))
		b.WriteString("\n")
	}

	return []byte(b.String())
}
