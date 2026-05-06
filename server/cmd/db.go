package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/commit0-dev/commit0/server/internal/adapters/surreal"
)

var dbCmd = &cobra.Command{
	Use:   "db",
	Short: "Manage the local SurrealDB instance",
}

var dbStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start a local SurrealDB instance",
	RunE: func(cmd *cobra.Command, args []string) error {
		bind, _ := cmd.Flags().GetString("bind")
		data, _ := cmd.Flags().GetString("data")
		user, _ := cmd.Flags().GetString("user")
		pass, _ := cmd.Flags().GetString("pass")

		ctx := cmd.Context()
		proc, err := surreal.StartSurrealDB(ctx, bind, data, user, pass)
		if err != nil {
			return fmt.Errorf("start SurrealDB: %w", err)
		}

		fmt.Printf("SurrealDB started on %s (data: %s)\n", bind, data)
		fmt.Println("Press Ctrl+C to stop.")

		// Block until SIGINT / SIGTERM.
		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		stopCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := surreal.StopSurrealDB(stopCtx, proc); err != nil {
			return fmt.Errorf("stop SurrealDB: %w", err)
		}
		fmt.Println("SurrealDB stopped.")
		return nil
	},
}

var dbStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Signal a running SurrealDB process to stop (sends SIGTERM)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Fprintln(os.Stderr, "hint: stop the 'commit0 db start' process with Ctrl+C or SIGTERM")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(dbCmd)
	dbCmd.AddCommand(dbStartCmd)
	dbCmd.AddCommand(dbStopCmd)

	dbStartCmd.Flags().String("bind", "0.0.0.0:8000", "Address to bind SurrealDB")
	dbStartCmd.Flags().String("data", "memory", "Storage path (use 'memory' for ephemeral)")
	dbStartCmd.Flags().String("user", "root", "Root username")
	dbStartCmd.Flags().String("pass", "root", "Root password")
}
