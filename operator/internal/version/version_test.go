package version

import "testing"

func TestVersionDefaultsToNonEmptyMarker(t *testing.T) {
	t.Parallel()

	if Version == "" {
		t.Fatal("Version is empty; a build without an injected version must still report a non-empty development marker")
	}
}
