// workflow.go 实现工作流模板子命令，管理工作流模板。
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var workflowCmd = &cobra.Command{
	Use:     "workflow",
	Short:   "Manage workflow templates",
	Aliases: []string{"wf"},
}

var workflowListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List workflow templates in a workspace",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/workflows", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workflowGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get workflow template details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/workflows/%s", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workflowCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a workflow template",
	Long: `Create a workflow template with optional node definitions.

Nodes are specified as a JSON array via --nodes. Each node object supports:
  name            - node display name (required)
  sort_order      - position in workflow (1-based, required)
  node_type       - standard | review | manual (default: standard)
  assignee_type   - any_agent | specific_agent | human (default: any_agent)
  timeout_minutes - execution timeout in minutes (default: 0 = no timeout)
  max_reject_cycles - max reject count before escalation (default: 5)
  description     - node description

Example:
  teammate workflow create --workspace WS_ID --name "My Flow" --nodes '[
    {"name":"需求分析","sort_order":1,"node_type":"standard","timeout_minutes":30},
    {"name":"代码审查","sort_order":2,"node_type":"review","timeout_minutes":30}
  ]'`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("description")
		nodesJSON, _ := cmd.Flags().GetString("nodes")

		if wsID == "" || name == "" {
			return fmt.Errorf("--workspace and --name are required")
		}

		body := map[string]interface{}{"name": name}
		if desc != "" {
			body["description"] = desc
		}
		if nodesJSON != "" {
			var nodes []map[string]interface{}
			if err := json.Unmarshal([]byte(nodesJSON), &nodes); err != nil {
				return fmt.Errorf("invalid --nodes JSON: %w", err)
			}
			body["nodes"] = nodes
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/workflows", wsID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workflowUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a workflow template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("description")

		body := map[string]string{}
		if name != "" {
			body["name"] = name
		}
		if desc != "" {
			body["description"] = desc
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s/workflows/%s", wsID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workflowDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a workflow template",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/workflows/%s", wsID, args[0])); err != nil {
			return err
		}
		fmt.Println("Workflow template deleted.")
		return nil
	},
}

var workflowStatsCmd = &cobra.Command{
	Use:   "stats <id>",
	Short: "Get workflow template statistics",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/templates/%s/stats", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	workflowListCmd.Flags().String("workspace", "", "workspace ID")
	workflowGetCmd.Flags().String("workspace", "", "workspace ID")
	workflowCreateCmd.Flags().String("workspace", "", "workspace ID")
	workflowCreateCmd.Flags().String("name", "", "workflow name")
	workflowCreateCmd.Flags().String("description", "", "workflow description")
	workflowCreateCmd.Flags().String("nodes", "", "nodes as JSON array")
	workflowUpdateCmd.Flags().String("workspace", "", "workspace ID")
	workflowUpdateCmd.Flags().String("name", "", "workflow name")
	workflowUpdateCmd.Flags().String("description", "", "workflow description")
	workflowDeleteCmd.Flags().String("workspace", "", "workspace ID")

	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowGetCmd)
	workflowCmd.AddCommand(workflowCreateCmd)
	workflowCmd.AddCommand(workflowUpdateCmd)
	workflowCmd.AddCommand(workflowDeleteCmd)
	workflowCmd.AddCommand(workflowStatsCmd)
	rootCmd.AddCommand(workflowCmd)
}
