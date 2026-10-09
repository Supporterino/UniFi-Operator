package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Supporterino/UniFi-Operator/cli/internal/emit"
	"github.com/Supporterino/UniFi-Operator/cli/internal/snapshot"
	"github.com/Supporterino/UniFi-Operator/cli/internal/unifi"
)

type snapshotOptions struct {
	controller    string
	controllerRef string
	site          string
	apiKey        string
	output        string
}

func newSnapshotCommand() *cobra.Command {
	opts := &snapshotOptions{}
	cmd := &cobra.Command{
		Use:   "snapshot [kind]",
		Short: "Read a UniFi controller and emit adoptable Custom Resources",
		Long: "snapshot reads objects from a UniFi Network controller over the Integration v1 API\n" +
			"and writes the equivalent Custom Resources as YAML. Supported kinds: all, controller,\n" +
			"sites, networks. `all` emits a UnifiController template, the UnifiSites, and the\n" +
			"UnifiNetworks of the site selected with --site.",
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
	flags.StringVar(&opts.controllerRef, "controller-ref", snapshot.DefaultControllerRef,
		"metadata.name of the UnifiController the emitted sites reference")
	flags.StringVar(&opts.site, "site", "default",
		"upstream site internalReference to snapshot networks from")
	flags.StringVar(&opts.apiKey, "api-key", "", "UniFi API key (or UNIFI_API_KEY); never logged")
	flags.StringVarP(&opts.output, "output", "o", "-", "output directory for CR files, or - for stdout")
	return cmd
}

func (o *snapshotOptions) run(ctx context.Context, kind string, out, warn io.Writer) error {
	switch kind {
	case "all", "controller", "sites", "networks":
	default:
		return fmt.Errorf("unknown snapshot kind %q: supported kinds are all, controller, sites, networks", kind)
	}
	if o.controller == "" {
		return fmt.Errorf("--controller is required (or set UNIFI_URL)")
	}
	if o.controllerRef == "" {
		return fmt.Errorf("--controller-ref must not be empty")
	}

	client, err := unifi.NewClient(o.controller, o.apiKey, false)
	if err != nil {
		return err
	}

	needSites := kind == "all" || kind == "sites" || kind == "networks"
	var sites []unifi.Site
	var siteResources []emit.Resource
	var siteNames map[string]string
	var siteWarnings []string
	if needSites {
		sites, err = client.ListSites(ctx)
		if err != nil {
			return fmt.Errorf("snapshot sites: %w", err)
		}
		siteResources, siteNames, siteWarnings = snapshot.Sites(o.controllerRef, sites)
	}

	var resources []emit.Resource
	var warnings []string

	if kind == "controller" || kind == "all" {
		controller, controllerWarnings := snapshot.Controller(o.controller, o.controllerRef)
		resources = append(resources, controller)
		warnings = append(warnings, controllerWarnings...)
	}
	if kind == "sites" || kind == "all" {
		resources = append(resources, siteResources...)
		warnings = append(warnings, siteWarnings...)
	}
	if kind == "networks" || kind == "all" {
		siteName, ok := siteNames[o.site]
		if !ok {
			return fmt.Errorf("site %q not found on the controller (available: %s)",
				o.site, strings.Join(siteReferences(sites), ", "))
		}
		siteID, ok := findSiteID(sites, o.site)
		if !ok {
			return fmt.Errorf("site %q has no upstream id", o.site)
		}
		networks, err := client.ListNetworks(ctx, siteID)
		if err != nil {
			return fmt.Errorf("snapshot networks: %w", err)
		}
		detailed, detailWarnings, err := loadNetworkDetails(ctx, client, siteID, networks)
		if err != nil {
			return err
		}
		warnings = append(warnings, detailWarnings...)
		networkResources, networkWarnings := snapshot.Networks(siteName, detailed)
		resources = append(resources, networkResources...)
		warnings = append(warnings, networkWarnings...)
	}

	for _, w := range warnings {
		if _, err := fmt.Fprintf(warn, "warning: %s\n", w); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return emitResources(out, o.output, resources)
}

// findSiteID returns the upstream UUID of the site whose internalReference matches.
func findSiteID(sites []unifi.Site, internalReference string) (string, bool) {
	for _, site := range sites {
		if site.InternalReference == internalReference {
			return site.ID, true
		}
	}
	return "", false
}

// siteReferences lists the upstream internalReferences for an error message. It never
// contains a credential.
func siteReferences(sites []unifi.Site) []string {
	refs := make([]string, 0, len(sites))
	for _, site := range sites {
		refs = append(refs, site.InternalReference)
	}
	return refs
}

// loadNetworkDetails resolves the full detail of every listed network. The list endpoint
// returns only overview fields, so the management-variant configuration is read per
// network. A network that disappeared between the list and the detail read (404) is
// skipped with a warning; any other error is returned so the snapshot fails closed rather
// than emitting a partial projection.
func loadNetworkDetails(ctx context.Context, client *unifi.Client, siteID string, networks []unifi.Network) ([]unifi.Network, []string, error) {
	details := make([]unifi.Network, 0, len(networks))
	var warnings []string
	for _, network := range networks {
		detail, err := client.GetNetwork(ctx, siteID, network.ID)
		if err != nil {
			var apiErr *unifi.APIError
			if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound {
				warnings = append(warnings, fmt.Sprintf(
					"network %q: disappeared before its details could be read; skipping", network.Name))
				continue
			}
			return nil, nil, fmt.Errorf("read network %q details: %w", network.Name, err)
		}
		details = append(details, detail)
	}
	return details, warnings, nil
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
