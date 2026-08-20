// node.go 实现节点子命令，管理任务节点的认领、审批等操作。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var nodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Manage task nodes",
}

var nodeListCmd = &cobra.Command{
	Use:     "list <task-id>",
	Short:   "List nodes for a task",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/tasks/%s/nodes", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var nodeClaimCmd = &cobra.Command{
	Use:   "claim <node-id>",
	Short: "Claim a node for an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		if taskID == "" {
			return fmt.Errorf("--task is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/nodes/%s/claim", taskID, args[0]),
			nil, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var nodeApproveCmd = &cobra.Command{
	Use:   "approve <node-id>",
	Short: "Approve a node",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		comment, _ := cmd.Flags().GetString("comment")
		if taskID == "" {
			return fmt.Errorf("--task is required")
		}

		body := map[string]string{}
		if comment != "" {
			body["comment"] = comment
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/nodes/%s/approve", taskID, args[0]),
			body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var nodeRejectCmd = &cobra.Command{
	Use:   "reject <node-id>",
	Short: "Reject a node",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		targetNodeID, _ := cmd.Flags().GetString("target")
		comment, _ := cmd.Flags().GetString("comment")
		if taskID == "" {
			return fmt.Errorf("--task is required")
		}

		body := map[string]string{}
		if targetNodeID != "" {
			body["target_node_id"] = targetNodeID
		}
		if comment != "" {
			body["comment"] = comment
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/nodes/%s/reject", taskID, args[0]),
			body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var nodeManualCmd = &cobra.Command{
	Use:   "manual <node-id>",
	Short: "Mark a node for manual intervention",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		comment, _ := cmd.Flags().GetString("comment")
		if taskID == "" {
			return fmt.Errorf("--task is required")
		}

		body := map[string]string{}
		if comment != "" {
			body["comment"] = comment
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/nodes/%s/manual", taskID, args[0]),
			body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var nodeResolveCmd = &cobra.Command{
	Use:   "resolve <node-id>",
	Short: "Resolve a manual intervention node back to pending",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		comment, _ := cmd.Flags().GetString("comment")
		if taskID == "" {
			return fmt.Errorf("--task is required")
		}

		body := map[string]string{}
		if comment != "" {
			body["comment"] = comment
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/nodes/%s/resolve", taskID, args[0]),
			body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var nodeLogCmd = &cobra.Command{
	Use:   "log <node-id>",
	Short: "Show transitions for a node",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		taskID, _ := cmd.Flags().GetString("task")
		projectID, _ := cmd.Flags().GetString("project")
		if taskID == "" || projectID == "" {
			return fmt.Errorf("--task and --project are required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/tasks/%s/nodes/%s/transitions",
			projectID, taskID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	nodeClaimCmd.Flags().String("task", "", "task ID")
	nodeApproveCmd.Flags().String("task", "", "task ID")
	nodeApproveCmd.Flags().String("comment", "", "comment")
	nodeRejectCmd.Flags().String("task", "", "task ID")
	nodeRejectCmd.Flags().String("target", "", "target node ID to reject to")
	nodeRejectCmd.Flags().String("comment", "", "comment")
	nodeManualCmd.Flags().String("task", "", "task ID")
	nodeManualCmd.Flags().String("comment", "", "comment")
	nodeResolveCmd.Flags().String("task", "", "task ID")
	nodeResolveCmd.Flags().String("comment", "", "comment")
	nodeLogCmd.Flags().String("task", "", "task ID")
	nodeLogCmd.Flags().String("project", "", "project ID")

	nodeCmd.AddCommand(nodeListCmd)
	nodeCmd.AddCommand(nodeClaimCmd)
	nodeCmd.AddCommand(nodeApproveCmd)
	nodeCmd.AddCommand(nodeRejectCmd)
	nodeCmd.AddCommand(nodeManualCmd)
	nodeCmd.AddCommand(nodeResolveCmd)
	nodeCmd.AddCommand(nodeLogCmd)
	rootCmd.AddCommand(nodeCmd)
}
