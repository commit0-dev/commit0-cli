package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/app"
	"github.com/commit0-dev/commit0/internal/config"
	"github.com/commit0-dev/commit0/pkg/types"
)

var traceCmd = &cobra.Command{
	Use:   "trace <symbol>",
	Short: "Trace code flow from a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		repoSlug, _ := cmd.Flags().GetString("repo")
		direction, _ := cmd.Flags().GetString("direction")
		depth, _ := cmd.Flags().GetInt("depth")

		svc, cleanup, err := wireTraceService(cmd.Context(), cfg)
		if err != nil {
			return err
		}
		defer cleanup()

		result, err := svc.Trace(cmd.Context(), app.TraceRequest{
			Symbol:    args[0],
			RepoSlug:  repoSlug,
			Direction: direction,
			Depth:     depth,
		})
		if err != nil {
			return fmt.Errorf("trace: %w", err)
		}

		fmt.Printf("Trace %s from %s (%dms)\n\n", result.Direction, result.Root.Qualified, result.Timing.TotalMS)
		printHops(result.Tree, 0)
		if result.Explanation != "" {
			fmt.Printf("\nExplanation:\n%s\n", result.Explanation)
		}
		return nil
	},
}

func printHops(hops []types.TraceHop, indent int) {
	prefix := strings.Repeat("  ", indent)
	for _, hop := range hops {
		fmt.Printf("%s-> %s (%s:%d)\n",
			prefix,
			hop.Node.Qualified,
			hop.Node.FilePath,
			hop.Node.StartLine,
		)
		if len(hop.Children) > 0 {
			printHops(hop.Children, indent+1)
		}
	}
}

func init() {
	rootCmd.AddCommand(traceCmd)
	traceCmd.Flags().String("repo", "", "Repository slug")
	traceCmd.Flags().String("direction", "forward", "Trace direction: forward or reverse")
	traceCmd.Flags().Int("depth", 5, "Maximum trace depth")
}
