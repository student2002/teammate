// project.go 实现项目子命令，管理项目的增删改查和成员配置。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:     "project",
	Short:   "Manage projects",
	Aliases: []string{"proj"},
}

var projectListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List projects in a workspace",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/projects", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var projectGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get project details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/projects/%s", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var projectCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new project",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("description")
		repoURL, _ := cmd.Flags().GetString("repo-url")

		if wsID == "" || name == "" || repoURL == "" {
			return fmt.Errorf("--workspace, --name, and --repo-url are required")
		}

		body := map[string]string{"name": name, "repo_url": repoURL}
		if desc != "" {
			body["description"] = desc
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/projects", wsID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var projectUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a project",
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
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s/projects/%s", wsID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var projectDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/projects/%s", wsID, args[0])); err != nil {
			return err
		}
		fmt.Println("Project deleted.")
		return nil
	},
}

var projectMemberCmd = &cobra.Command{
	Use:   "member",
	Short: "Manage project members",
}

var projectMemberListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List project members",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/projects/%s/members", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var projectMemberAddCmd = &cobra.Command{
	Use:   "add <project-id>",
	Short: "Add a member to a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		memberType, _ := cmd.Flags().GetString("type")
		agentID, _ := cmd.Flags().GetString("agent")
		role, _ := cmd.Flags().GetString("role")

		if memberType == "" {
			return fmt.Errorf("--type is required (human or agent)")
		}

		body := map[string]string{
			"member_type": memberType,
			"role":        role,
		}
		if agentID != "" {
			body["agent_id"] = agentID
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/projects/%s/members", wsID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var projectReviewerCmd = &cobra.Command{
	Use:   "reviewer",
	Short: "Manage project reviewers",
}

var projectReviewerListCmd = &cobra.Command{
	Use:   "list <project-id>",
	Short: "List project reviewers",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/projects/%s/reviewers", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	projectListCmd.Flags().String("workspace", "", "workspace ID")
	projectGetCmd.Flags().String("workspace", "", "workspace ID")
	projectCreateCmd.Flags().String("workspace", "", "workspace ID")
	projectCreateCmd.Flags().String("name", "", "project name")
	projectCreateCmd.Flags().String("repo-url", "", "git repository URL (required)")
	projectCreateCmd.Flags().String("description", "", "project description")
	projectUpdateCmd.Flags().String("workspace", "", "workspace ID")
	projectUpdateCmd.Flags().String("name", "", "project name")
	projectUpdateCmd.Flags().String("description", "", "project description")
	projectMemberListCmd.Flags().String("workspace", "", "workspace ID")
	projectReviewerListCmd.Flags().String("workspace", "", "workspace ID")
	projectDeleteCmd.Flags().String("workspace", "", "workspace ID")

	projectMemberAddCmd.Flags().String("workspace", "", "workspace ID")
	projectMemberAddCmd.Flags().String("type", "", "member type (human or agent)")
	projectMemberAddCmd.Flags().String("agent", "", "agent ID (required when type=agent)")
	projectMemberAddCmd.Flags().String("role", "developer", "project role (lead, developer, reviewer)")

	projectMemberCmd.AddCommand(projectMemberListCmd)
	projectMemberCmd.AddCommand(projectMemberAddCmd)
	projectReviewerCmd.AddCommand(projectReviewerListCmd)
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectGetCmd)
	projectCmd.AddCommand(projectCreateCmd)
	projectCmd.AddCommand(projectUpdateCmd)
	projectCmd.AddCommand(projectDeleteCmd)
	projectCmd.AddCommand(projectMemberCmd)
	projectCmd.AddCommand(projectReviewerCmd)
	rootCmd.AddCommand(projectCmd)
}
