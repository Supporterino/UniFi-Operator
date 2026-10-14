// Package version reports the CLI build version.
//
// Version is a package-level string variable so the release build can stamp the bundle
// version into the binary at link time with
//
//	-ldflags "-X github.com/Supporterino/UniFi-Operator/cli/internal/version.Version=<value>"
//
// Keep the import path and the symbol name stable: the Taskfile and goreleaser both target
// them verbatim.
package version

// Version is the build version surfaced by `unifi-operator-cli --version`. It defaults to a
// development marker so a local `go build` still reports a non-empty value.
var Version = "dev"
