// workspace.go implements the workspace subcommand, managing workspace CRUD operations and members.
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

var workspaceUseCmd = &cobra.Command{
	Use:   "use <id>",
	Short: "Switch the remembered workspace for workspace-scoped commands",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		creds, err := loadCredentials()
		if err != nil {
			return fmt.Errorf("not logged in; run 'teammate auth login' first: %w", err)
		}

		// The server verifies membership and returns the effective role; a
		// non-member workspace is rejected here rather than at command use.
		client := newAPIClient()
		var result struct {
			WorkspaceID string `json:"workspace_id"`
			Role        string `json:"role"`
		}
		if err := client.Post("/api/auth/switch-workspace", map[string]string{
			"workspace_id": args[0],
		}, &result); err != nil {
			return err
		}

		creds.WorkspaceID = result.WorkspaceID
		if creds.WorkspaceID == "" {
			creds.WorkspaceID = args[0]
		}
		if err := saveCredentials(creds); err != nil {
			return fmt.Errorf("save credentials: %w", err)
		}

		fmt.Printf("Workspace switched to %s (role: %s)\n", creds.WorkspaceID, result.Role)
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
	workspaceCmd.AddCommand(workspaceUseCmd)
	workspaceCmd.AddCommand(workspaceMemberCmd)
	rootCmd.AddCommand(workspaceCmd)
}
