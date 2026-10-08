package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Supporterino/UniFi-Operator/cli/internal/emit"
	"github.com/Supporterino/UniFi-Operator/cli/internal/snapshot"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

type snapshotOptions struct {
	controller string
	site       string
	apiKey     string
	output     string
}

func newSnapshotCommand() *cobra.Command {
	opts := &snapshotOptions{}
	cmd := &cobra.Command{
		Use:   "snapshot [kind]",
		Short: "Read a UniFi controller and emit adoptable Custom Resources",
		Long: "snapshot reads the selected objects from a UniFi Network controller and writes\n" +
			"the equivalent Custom Resources as YAML. Supported kinds: all, networks.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kind := "all"
			if len(args) == 1 {
				kind = args[0]
			}
			// Resolve environment fallbacks only when the flag was not set. A non-empty
			// flag default would be echoed in --help, so secrets must never be defaults.
			if !cmd.Flags().Changed("controller") {
				opts.controller = os.Getenv("UNIFI_URL")
			}
			if !cmd.Flags().Changed("api-key") {
				opts.apiKey = os.Getenv("UNIFI_API_KEY")
			}
			return opts.run(cmd.Context(), kind, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	flags := cmd.Flags()
	flags.StringVar(&opts.controller, "controller", "", "UniFi controller base URL (or UNIFI_URL)")
	flags.StringVar(&opts.site, "site", "default", "UniFi site name")
	flags.StringVar(&opts.apiKey, "api-key", "", "UniFi API key (or UNIFI_API_KEY); never logged")
	flags.StringVarP(&opts.output, "output", "o", "-", "output directory for CR files, or - for stdout")
	return cmd
}

func (o *snapshotOptions) run(ctx context.Context, kind string, out, warn io.Writer) error {
	switch kind {
	case "all", "networks":
	default:
		return fmt.Errorf("unknown snapshot kind %q: supported kinds are all, networks", kind)
	}
	if o.controller == "" {
		return fmt.Errorf("--controller is required (or set UNIFI_URL)")
	}

	client, err := unifi.NewClient(o.controller, o.site, unifi.WithAPIKey(o.apiKey))
	if err != nil {
		return err
	}
	networks, err := client.ListNetworks(ctx)
	if err != nil {
		return fmt.Errorf("snapshot networks: %w", err)
	}

	resources, warnings := snapshot.Networks(o.site, networks)
	for _, w := range warnings {
		if _, err := fmt.Fprintf(warn, "warning: %s\n", w); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return emitResources(out, o.output, resources)
}

func emitResources(out io.Writer, output string, resources []emit.Resource) error {
	toStdout := output == "-"
	if !toStdout {
		if err := os.MkdirAll(output, 0o755); err != nil {
			return fmt.Errorf("create output directory: %w", err)
		}
	}

	for _, r := range resources {
		data, err := emit.Marshal(r)
		if err != nil {
			return err
		}
		if toStdout {
			if _, err := fmt.Fprintf(out, "---\n%s", data); err != nil {
				return fmt.Errorf("write %s: %w", r.Metadata.Name, err)
			}
			continue
		}
		file := fmt.Sprintf("%s-%s.yaml", strings.ToLower(r.Kind), r.Metadata.Name)
		if err := os.WriteFile(filepath.Join(output, file), data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", file, err)
		}
	}
	return nil
}
