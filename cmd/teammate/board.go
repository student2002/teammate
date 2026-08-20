// board.go 实现看板子命令，以看板视图展示项目任务。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var boardCmd = &cobra.Command{
	Use:   "board",
	Short: "Show board view",
}

var boardShowCmd = &cobra.Command{
	Use:   "show <project-id>",
	Short: "Show board for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/board", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	boardCmd.AddCommand(boardShowCmd)
	rootCmd.AddCommand(boardCmd)
}
