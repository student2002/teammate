// search.go 实现搜索子命令，搜索任务和代理。
package main

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search tasks and agents",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		keyword := args[0]
		projectID, _ := cmd.Flags().GetString("project")
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}

		client := newAPIClient()

		// 搜索任务
		path := fmt.Sprintf("/api/workspaces/%s/search/tasks?q=%s", url.PathEscape(wsID), url.QueryEscape(keyword))
		if projectID != "" {
			path += "&projectId=" + url.QueryEscape(projectID)
		}
		var tasks interface{}
		if err := client.Get(path, &tasks); err != nil {
			return err
		}

		// 搜索 Agent
		agentPath := fmt.Sprintf("/api/workspaces/%s/search/agents?q=%s", url.PathEscape(wsID), url.QueryEscape(keyword))
		var agents interface{}
		if err := client.Get(agentPath, &agents); err != nil {
			return err
		}

		return printOutput(map[string]interface{}{
			"tasks":  tasks,
			"agents": agents,
		}, getOutputFormat())
	},
}

func init() {
	searchCmd.Flags().String("project", "", "project ID")
	searchCmd.Flags().String("workspace", "", "workspace ID")
	rootCmd.AddCommand(searchCmd)
}
