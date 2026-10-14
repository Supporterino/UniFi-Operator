package cmd

import (
	"testing"

	"github.com/Supporterino/UniFi-Operator/cli/internal/version"
)

func TestRootCommandReportsVersion(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	if root.Version == "" {
		t.Fatal("root command Version is empty; a built CLI must report a non-empty version")
	}
	if root.Version != version.Version {
		t.Fatalf("root command Version = %q, want the version package value %q", root.Version, version.Version)
	}
}
