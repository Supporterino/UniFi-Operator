// Package cmd implements the unifi-operator-cli command tree.
package cmd

import "github.com/spf13/cobra"

// NewRootCommand builds the root cobra command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "unifi-operator-cli",
		Short: "Snapshot a UniFi controller into Kubernetes Custom Resources",
		Long: "unifi-operator-cli reads a live UniFi Network controller and emits\n" +
			"adoptable Custom Resources that the UniFi-Operator reconciles.",
		SilenceUsage: true,
	}
	root.AddCommand(newSnapshotCommand())
	return root
}

// Execute runs the root command tree.
func Execute() error {
	return NewRootCommand().Execute()
}
