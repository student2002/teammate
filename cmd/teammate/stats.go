// stats.go 实现统计子命令，查看项目和代理的统计数据。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var statsCmd = &cobra.Command{
	Use:   "stats",
	Short: "View statistics",
}

var statsProjectCmd = &cobra.Command{
	Use:   "project <id>",
	Short: "Get project statistics",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/stats", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var statsAgentCmd = &cobra.Command{
	Use:   "agent <id>",
	Short: "Get agent statistics",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/agents/%s/stats", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	statsCmd.AddCommand(statsProjectCmd)
	statsCmd.AddCommand(statsAgentCmd)
	rootCmd.AddCommand(statsCmd)
}
