package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/sdk"
)

var flowCmd = &cobra.Command{
	Use:   "flow <symbol>",
	Short: "Trace field-level data flow from a symbol",
	Long: `Trace how data flows through functions — follows data_flow, reads, and writes edges.
Unlike 'trace' (which follows call edges), 'flow' tracks where data actually goes.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		direction, _ := cmd.Flags().GetString("direction")
		depth, _ := cmd.Flags().GetInt("depth")
		fieldPath, _ := cmd.Flags().GetString("field")
		showMutations, _ := cmd.Flags().GetBool("show-mutations")

		c := sdk.New(serverURL(cmd))
		result, err := c.Flow(cmd.Context(), sdk.FlowRequest{
			Symbol:        args[0],
			FieldPath:     fieldPath,
			RepoSlug:      repoSlug,
			Direction:     direction,
			Depth:         depth,
			ShowMutations: showMutations,
		})
		if err != nil {
			return fmt.Errorf("flow: %w", err)
		}

		fmt.Printf("%s %s %s %s\n\n",
			bold("Flow"),
			cyan(result.Direction),
			bold("from"),
			bold(result.Root.Qualified))

		if len(result.Chains) == 0 {
			fmt.Println(dim("  No data flow paths found."))
			return nil
		}

		for _, chain := range result.Chains {
			if chain.FieldPath != "" {
				fmt.Printf("  %s %s\n", bold("Field:"), cyan(chain.FieldPath))
			}
			for _, hop := range chain.Hops {
				mutation := ""
				if hop.MutationType != "" && hop.MutationType != "none" {
					mutation = fmt.Sprintf(" %s", cyan(fmt.Sprintf("[%s: %s]", hop.MutationType, hop.MutationExpr)))
				}
				field := ""
				if hop.FieldPath != "" {
					field = fmt.Sprintf(" field=%s", gray(hop.FieldPath))
				}
				fmt.Printf("    %s %s%s%s\n",
					dim(cyan("->")),
					bold(hop.Node.Qualified),
					field,
					mutation,
				)
			}
			if chain.TaintPoint != nil {
				fmt.Printf("    %s %s at %s\n",
					bold("Taint:"),
					cyan(chain.TaintPoint.Node.Qualified),
					gray(fmt.Sprintf("%s:%d", chain.TaintPoint.Node.FilePath, chain.TaintPoint.Node.StartLine)),
				)
			}
			fmt.Println()
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(flowCmd)
	flowCmd.Flags().String("repo", "", "Repository slug")
	flowCmd.Flags().String("direction", "forward", "Flow direction: forward or reverse")
	flowCmd.Flags().Int("depth", 10, "Maximum flow depth")
	flowCmd.Flags().String("field", "", "Filter by field path (e.g., user.Email)")
	flowCmd.Flags().Bool("show-mutations", false, "Highlight data mutations in the flow")
}
