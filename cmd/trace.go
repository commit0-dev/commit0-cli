package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/internal/adapters/client"
	"github.com/commit0-dev/commit0/pkg/types"
)

var traceCmd = &cobra.Command{
	Use:   "trace <symbol>",
	Short: "Trace code flow from a symbol",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		direction, _ := cmd.Flags().GetString("direction")
		depth, _ := cmd.Flags().GetInt("depth")

		c := client.New(serverURL(cmd))
		result, err := c.Trace(cmd.Context(), client.TraceRequest{
			Symbol:    args[0],
			RepoSlug:  repoSlug,
			Direction: direction,
			Depth:     depth,
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
}
