// Package emit serialises projected Custom Resources to YAML for `kubectl apply`.
package emit

import (
	"fmt"

	"sigs.k8s.io/yaml"
)

// Resource is a generic Kubernetes object. Spec holds the projected desired state.
type Resource struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Metadata   Metadata `json:"metadata"`
	Spec       any      `json:"spec,omitempty"`
}

// Metadata is the subset of ObjectMeta the CLI sets on emitted resources.
type Metadata struct {
	Name        string            `json:"name"`
	Namespace   string            `json:"namespace,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Marshal renders a Resource as YAML.
func Marshal(r Resource) ([]byte, error) {
	b, err := yaml.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("marshal %s/%s: %w", r.Kind, r.Metadata.Name, err)
	}
	return b, nil
}
