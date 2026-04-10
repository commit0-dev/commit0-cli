package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/sdk"
)

// analysisPrompts maps focus areas to agent prompts that use commit0's own tools.
var analysisPrompts = map[string]string{
	"architecture": `Analyze THIS codebase for hexagonal architecture violations.
Use search_code to find imports between layers. Use trace_calls to follow
dependency chains. Check for: (1) adapter-to-adapter imports, (2) domain
importing adapters, (3) app services bypassing port interfaces, (4) CLI
commands that import server internals directly. For each violation report:
the qualified function name, file:line, what rule it violates, and the fix.`,

	"dead-code": `Find functions in this codebase that are NEVER called.
Use trace_calls with direction=reverse on functions to check if they have
callers. Skip: test helpers, init() functions, exported HTTP handlers,
main(), and interface implementations. For each confirmed dead function:
report its qualified name, file:line, why it appears dead, and whether
to delete it or document why it's kept.`,

	"consistency": `Check consistency between the three layers in this codebase:
(1) Find HTTP handlers in the http adapter that have no corresponding SDK
client method. Use search_code for "func.*handle" and cross-reference with sdk/.
(2) Find SDK methods that have no corresponding CLI command.
(3) Find port interface methods that have no adapter implementation.
For each gap: report what's missing, where it should be added, and a code sketch.`,

	"hotspots": `Find high-risk code areas in this codebase.
Use blast_radius on key functions to measure their impact. Use
get_neighborhood to find nodes with high fan-in (many callers) or
fan-out (calls many things). Report: functions with blast radius > 10,
functions with > 5 direct callers, and why they're risky. Suggest
mitigation: extract interface, add tests, or reduce coupling.`,

	"all": `Perform a comprehensive self-analysis of this codebase covering
4 areas. For each area, use the appropriate tools and report findings
as a separate section:

1. ARCHITECTURE: Check hexagonal layer violations (use search_code + trace_calls)
2. DEAD CODE: Find functions with zero callers (use trace_calls reverse)
3. CONSISTENCY: Check handler-SDK-CLI alignment (use search_code)
4. HOTSPOTS: Find high blast-radius functions (use blast_radius + get_neighborhood)

Be thorough. Use at least 3 tool calls per area. Report concrete findings
with file:line locations, not vague observations.`,
}

var analyzeCmd = &cobra.Command{
	Use:   "analyze",
	Short: "Self-analyze codebase for issues and propose fixes",
	Long: `Run commit0's agent on the indexed codebase to find architecture
violations, dead code, consistency gaps, and high-risk hotspots.
The agent uses its own tools (search, trace, blast) and produces
a structured report with findings and suggested fixes.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		repoSlug, _ := cmd.Flags().GetString("repo")
		focus, _ := cmd.Flags().GetString("focus")

		if repoSlug == "" {
			return fmt.Errorf("--repo is required")
		}

		prompt, ok := analysisPrompts[focus]
		if !ok {
			valid := make([]string, 0, len(analysisPrompts))
			for k := range analysisPrompts {
				valid = append(valid, k)
			}
			return fmt.Errorf("unknown focus %q, valid: %s", focus, strings.Join(valid, ", "))
		}

		c := sdk.New(serverURL(cmd))
		return runAgentQuery(cmd, c, prompt, repoSlug)
	},
}

func init() {
	rootCmd.AddCommand(analyzeCmd)
	analyzeCmd.Flags().String("repo", "", "Repository slug (required)")
	analyzeCmd.Flags().String("focus", "all", "Analysis focus: architecture, dead-code, consistency, hotspots, all")
}
