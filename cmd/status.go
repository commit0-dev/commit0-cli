package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0-cli/sdk"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show server state (idle, indexing, etc.)",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := sdk.New(serverURL(cmd))

		// Call health endpoint which now includes state info.
		resp, err := c.HealthCheck(cmd.Context())
		if err != nil {
			return fmt.Errorf("server unreachable: %w", err)
		}

		state, _ := resp["state"].(string)
		activeJobs, _ := resp["active_jobs"].(float64)

		if state == "indexing" {
			fmt.Printf("Server: %s (%d active index jobs)\n", state, int(activeJobs))
		} else {
			fmt.Printf("Server: %s\n", state)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(statusCmd)
}
