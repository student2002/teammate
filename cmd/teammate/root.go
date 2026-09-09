// root.go defines the CLI root command, providing subcommand registration, service connection, and common initialization.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	outputFmt string
)

// Exit codes
const (
	ExitSuccess   = 0
	ExitGeneral   = 1
	ExitParam     = 2
	ExitAuth      = 10
	ExitForbidden = 11
	ExitNotFound  = 20
	ExitConflict  = 30
	ExitDB        = 40
	ExitInternal  = 50
)

// skipAuthCommands lists commands that do not require authentication (full CommandPath).
var skipAuthCommands = map[string]bool{
	"teammate auth login":            true,
	"teammate auth register":         true,
	"teammate completion bash":       true,
	"teammate completion zsh":        true,
	"teammate completion fish":       true,
	"teammate completion powershell": true,
	"teammate":                       true, // root itself
}

var rootCmd = &cobra.Command{
	Use:   "teammate",
	Short: "Teammate - AI-powered team collaboration platform",
	Long:  "Teammate Server - AI-powered team collaboration platform.\nManage workspaces, projects, agents, tasks, and more.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "output format: table, json, yaml")
	rootCmd.PersistentFlags().String("token", "", "JWT token (overrides stored credentials)")

	// Global authentication interception
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if skipAuthCommands[cmd.CommandPath()] {
			return nil
		}
		// Validate the token (exit if not logged in or expired)
		requireAuth()
		return nil
	}
}

// getOutputFormat returns the current output format.
func getOutputFormat() OutputFormat {
	return parseOutputFormat(outputFmt)
}

// exitError prints an error message and exits with the specified exit code.
func exitError(code int, msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(code)
}
