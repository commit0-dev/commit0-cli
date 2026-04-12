package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var healthzCmd = &cobra.Command{
	Use:   "healthz",
	Short: "Check if the server is ready (for Docker healthcheck)",
	RunE: func(cmd *cobra.Command, args []string) error {
		port := 8080
		if p, _ := cmd.Flags().GetInt("port"); p != 0 {
			port = p
		}
		url := fmt.Sprintf("http://localhost:%d/healthz", port)

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			fmt.Fprintf(os.Stderr, "healthz: %v\n", err)
			os.Exit(1)
		}
		defer resp.Body.Close()

		var result map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			fmt.Fprintf(os.Stderr, "healthz: invalid response: %v\n", err)
			os.Exit(1)
		}

		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "healthz: not ready (status %d)\n", resp.StatusCode)
			os.Exit(1)
		}
		return nil
	},
}

func init() {
	healthzCmd.Flags().Int("port", 8080, "Server port to check")
	rootCmd.AddCommand(healthzCmd)
}
