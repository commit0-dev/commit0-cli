package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
)

var apiCmd = &cobra.Command{
	Use:   "api",
	Short: "Discover and analyze API surfaces from source code",
}

var apiDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover all HTTP API endpoints from the code graph",
	RunE: func(cmd *cobra.Command, _ []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")

		c := sdk.New(serverURL(cmd))
		surface, err := c.APIDiscover(cmd.Context(), sdk.APIDiscoverRequest{
			RepoSlug: repoSlug,
		})
		if err != nil {
			return fmt.Errorf("discover: %w", err)
		}

		if len(surface.Endpoints) == 0 {
			fmt.Println("No API endpoints discovered.")
			return nil
		}

		fmt.Printf("Discovered %d API endpoints\n\n", len(surface.Endpoints))

		tbl := tablewriter.NewTable(os.Stdout,
			tablewriter.WithHeaderAlignment(tw.AlignLeft),
			tablewriter.WithConfig(tablewriter.Config{
				Row: tw.CellConfig{Formatting: tw.CellFormatting{AutoWrap: tw.WrapNone}},
			}),
		)
		tbl.Header([]string{"Method", "Path", "Handler", "Auth", "Middleware"})

		for _, ep := range surface.Endpoints {
			auth := "NONE"
			if len(ep.AuthChain) > 0 {
				auth = strings.Join(ep.AuthChain, ", ")
			}
			mw := ""
			if len(ep.Endpoint.Middleware) > 0 {
				mw = strings.Join(ep.Endpoint.Middleware, ", ")
			}
			tbl.Append([]string{ //nolint:errcheck
				ep.Endpoint.Method,
				ep.Endpoint.Path,
				ep.Endpoint.Handler,
				auth,
				mw,
			})
		}
		tbl.Render() //nolint:errcheck

		noAuth := 0
		for _, ep := range surface.Endpoints {
			if len(ep.AuthChain) == 0 {
				noAuth++
			}
		}
		if noAuth > 0 {
			fmt.Printf("\n%d endpoint(s) without authentication middleware\n", noAuth)
		}

		fmt.Printf("Completed in %dms\n", surface.Timing.TotalMS)
		return nil
	},
}

var apiSpecCmd = &cobra.Command{
	Use:   "spec",
	Short: "Generate OpenAPI 3.0 specification from the code graph",
	RunE: func(cmd *cobra.Command, _ []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")

		c := sdk.New(serverURL(cmd))
		spec, err := c.APISpec(cmd.Context(), repoSlug)
		if err != nil {
			return fmt.Errorf("generate openapi: %w", err)
		}

		fmt.Println(string(spec))
		return nil
	},
}

func init() {
	apiCmd.PersistentFlags().String("repo", "", "Repository slug")
	apiCmd.AddCommand(apiDiscoverCmd)
	apiCmd.AddCommand(apiSpecCmd)
	rootCmd.AddCommand(apiCmd)
}
