// Command unifi-operator-cli snapshots a UniFi controller into adoptable Custom Resources.
package main

import (
	"os"

	"github.com/Supporterino/UniFi-Operator/cli/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
