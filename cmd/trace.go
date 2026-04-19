package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
	"github.com/commit0-dev/commit0-cli/pkg/types"
)

var traceCmd = &cobra.Command{
	Use:   "trace <symbol>",
	Short: "Trace code flow from a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		direction, _ := cmd.Flags().GetString("direction")
		depth, _ := cmd.Flags().GetInt("depth")
		noExplain, _ := cmd.Flags().GetBool("no-explain")
		edgesRaw, _ := cmd.Flags().GetString("edges")

		var edgeLabels []string
		if edgesRaw != "" {
			for _, e := range strings.Split(edgesRaw, ",") {
				e = strings.TrimSpace(e)
				if e != "" {
					edgeLabels = append(edgeLabels, e)
				}
			}
		}

		c := sdk.New(serverURL(cmd))
		result, err := c.Trace(cmd.Context(), sdk.TraceRequest{
			Symbol:     args[0],
			RepoSlug:   repoSlug,
			Direction:  direction,
			Depth:      depth,
			NoExplain:  noExplain,
			EdgeLabels: edgeLabels,
		})
		if err != nil {
			return fmt.Errorf("trace: %w", err)
		}

		fmt.Printf("%s %s %s %s\n\n",
			bold("Trace"),
			cyan(result.Direction),
			bold("from"),
			bold(result.Root.Qualified)+gray(fmt.Sprintf(" (%dms)", result.Timing.TotalMS)))
		printHops(result.Tree, 0)
		if result.Explanation != "" {
			fmt.Printf("\n%s\n%s\n", bold("Explanation:"), result.Explanation)
		}
		return nil
	},
}

func printHops(hops []types.TraceHop, indent int) {
	prefix := strings.Repeat("  ", indent)
	for _, hop := range hops {
		fmt.Printf("%s%s %s %s\n",
			prefix,
			dim(cyan("->")),
			bold(hop.Node.Qualified),
			gray(fmt.Sprintf("(%s:%d)", hop.Node.FilePath, hop.Node.StartLine)),
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
	traceCmd.Flags().Bool("no-explain", false, "Skip LLM explanation (faster)")
	traceCmd.Flags().String("edges", "", "Edge types to follow (comma-separated: calls,data_flow,reads,writes)")
}
