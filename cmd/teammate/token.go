// token.go 实现 Token 用量子命令，查询 Token 用量。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var tokenCmd = &cobra.Command{
	Use:   "token",
	Short: "Manage token usage",
}

var tokenListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List token usage for a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/tasks/%s/token-usage", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	tokenCmd.AddCommand(tokenListCmd)
	rootCmd.AddCommand(tokenCmd)
}
