// workflow.go implements the workflow template subcommand, managing workflow templates.
package main

import (
	"encoding/json"
	"fmt"
	"os"

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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/workflows/%s", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

// readGraphPayload loads the full-graph JSON payload (name/description/enabled/
// nodes/edges) shared by create and update; the server validates the graph.
func readGraphPayload(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read graph file: %w", err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse graph JSON: %w", err)
	}
	return payload, nil
}

var workflowCreateCmd = &cobra.Command{
	Use:   "create --file <graph.json>",
	Short: "Create a workflow template from a full-graph JSON file",
	Long: `Create a workflow template from a full-graph JSON payload.

The file holds the V2 write contract: {name, description, enabled, nodes, edges}.
Every node needs a stable "id", a registry-known "type_key" and a valid "config";
edges reference node ids with explicit source/target ports. The server validates
the whole graph and rejects invalid ones.

Example:
  teammate workflow create --workspace WS_ID --file graph.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		file, _ := cmd.Flags().GetString("file")
		if file == "" {
			return fmt.Errorf("--file is required")
		}
		payload, err := readGraphPayload(file)
		if err != nil {
			return err
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/workflows", wsID), payload, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workflowUpdateCmd = &cobra.Command{
	Use:   "update <id> --file <graph.json>",
	Short: "Replace a workflow template's whole graph from a JSON file",
	Long: `Replace a workflow template's whole graph from a full-graph JSON payload
(same shape as create). The template version bumps on every save; node/edge ids
in the file stay stable across replaces.

Example:
  teammate workflow update WS_WORKFLOW_ID --workspace WS_ID --file graph.json`,
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		file, _ := cmd.Flags().GetString("file")
		if file == "" {
			return fmt.Errorf("--file is required")
		}
		payload, err := readGraphPayload(file)
		if err != nil {
			return err
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s/workflows/%s", wsID, args[0]), payload, &result); err != nil {
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/workflows/%s/stats", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	workflowListCmd.Flags().String("workspace", "", "workspace ID")
	workflowGetCmd.Flags().String("workspace", "", "workspace ID")
	workflowCreateCmd.Flags().String("workspace", "", "workspace ID")
	workflowCreateCmd.Flags().String("file", "", "full-graph JSON payload file")
	workflowUpdateCmd.Flags().String("workspace", "", "workspace ID")
	workflowUpdateCmd.Flags().String("file", "", "full-graph JSON payload file")
	workflowDeleteCmd.Flags().String("workspace", "", "workspace ID")
	workflowStatsCmd.Flags().String("workspace", "", "workspace ID")

	workflowCmd.AddCommand(workflowListCmd)
	workflowCmd.AddCommand(workflowGetCmd)
	workflowCmd.AddCommand(workflowCreateCmd)
	workflowCmd.AddCommand(workflowUpdateCmd)
	workflowCmd.AddCommand(workflowDeleteCmd)
	workflowCmd.AddCommand(workflowStatsCmd)
	rootCmd.AddCommand(workflowCmd)
}
