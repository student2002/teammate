// cmd/teammate-server — Teammate Server standalone deployment binary (entry point for the deployment toolchain).
//
// Decoupled from the `teammate` management CLI: the CLI is positioned as a pure HTTP API
// (zero direct DB access), while starting the service and running schema migrations are
// operations prior to deployment that are handled by this binary. Usage:
//
//	teammate-server                 # start the HTTP service (under test: ./internal/server)
//	teammate-server migrate --path <dir>   # run database migrations
//
// See the CLI tool design doc (§5.10). Configuration is injected via TEAMS_* environment variables (server.LoadConfig).
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/teammate/server/internal/db"
	"github.com/teammate/server/internal/server"
)

var migratePath string

var rootCmd = &cobra.Command{
	Use:   "teammate-server",
	Short: "Teammate Server (deployment toolchain): start service / run database migrations",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := server.LoadConfig()

		srv, err := server.New(cfg)
		if err != nil {
			return err
		}

		return srv.Start()
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations (requires direct DB access)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := server.LoadConfig()

		mp := migratePath
		if mp == "" {
			mp = "./internal/db/migrations"
		}

		if err := db.Migrate(cfg.DatabaseURL, mp); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		fmt.Println("migrations applied successfully")
		return nil
	},
}

func init() {
	migrateCmd.Flags().StringVar(&migratePath, "path", "", "migrations directory path")
	rootCmd.AddCommand(migrateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}