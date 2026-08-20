// workspace.go 实现工作区子命令，管理工作区的增删改查和成员。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var workspaceCmd = &cobra.Command{
	Use:     "workspace",
	Short:   "Manage workspaces",
	Aliases: []string{"ws"},
}

var workspaceListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all workspaces",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get("/api/workspaces", &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workspaceGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get workspace details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workspaceCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new workspace",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("description")
		prefix, _ := cmd.Flags().GetString("issue-prefix")

		if name == "" {
			return fmt.Errorf("--name is required")
		}

		body := map[string]string{"name": name}
		if desc != "" {
			body["description"] = desc
		}
		if prefix != "" {
			body["issue_prefix"] = prefix
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post("/api/workspaces", body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workspaceUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s", args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var workspaceDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a workspace",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s", args[0])); err != nil {
			return err
		}
		fmt.Println("Workspace deleted.")
		return nil
	},
}

var workspaceMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage workspace members",
}

var workspaceMemberListCmd = &cobra.Command{
	Use:   "list <workspace-id>",
	Short: "List workspace members",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/members", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	workspaceCreateCmd.Flags().String("name", "", "workspace name")
	workspaceCreateCmd.Flags().String("description", "", "workspace description")
	workspaceCreateCmd.Flags().String("issue-prefix", "TM", "issue prefix (e.g. TM-1)")
	workspaceUpdateCmd.Flags().String("name", "", "workspace name")
	workspaceUpdateCmd.Flags().String("description", "", "workspace description")

	workspaceMemberCmd.AddCommand(workspaceMemberListCmd)
	workspaceCmd.AddCommand(workspaceListCmd)
	workspaceCmd.AddCommand(workspaceGetCmd)
	workspaceCmd.AddCommand(workspaceCreateCmd)
	workspaceCmd.AddCommand(workspaceUpdateCmd)
	workspaceCmd.AddCommand(workspaceDeleteCmd)
	workspaceCmd.AddCommand(workspaceMemberCmd)
	rootCmd.AddCommand(workspaceCmd)
}
