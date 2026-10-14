// Package version reports the operator build version.
//
// Version is a package-level string variable so the release build can stamp the bundle
// version into the binary at link time with
//
//	-ldflags "-X github.com/Supporterino/UniFi-Operator/operator/internal/version.Version=<value>"
//
// Keep the import path and the symbol name stable: the Taskfile and the Docker image build
// both target them verbatim.
package version

// Version is the build version the operator logs once at startup. It defaults to a
// development marker so a local `go build` still reports a non-empty value.
var Version = "dev"
