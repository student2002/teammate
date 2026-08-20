// agent.go 实现 agent 子命令，管理 Agent 的注册、状态查看、删除等操作。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
}

var agentListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List agents in a workspace",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/agents", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get agent details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/agents/%s", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		name, _ := cmd.Flags().GetString("name")
		provider, _ := cmd.Flags().GetString("provider")
		instructions, _ := cmd.Flags().GetString("instructions")
		model, _ := cmd.Flags().GetString("model")
		gitName, _ := cmd.Flags().GetString("git-name")
		gitEmail, _ := cmd.Flags().GetString("git-email")

		if wsID == "" || name == "" {
			return fmt.Errorf("--workspace and --name are required")
		}
		if gitName == "" || gitEmail == "" {
			return fmt.Errorf("--git-name and --git-email are required")
		}

		body := map[string]string{
			"name":      name,
			"provider":  provider,
			"git_name":  gitName,
			"git_email": gitEmail,
		}
		if instructions != "" {
			body["instructions"] = instructions
		}
		if model != "" {
			body["model"] = model
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/agents", wsID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		instructions, _ := cmd.Flags().GetString("instructions")
		model, _ := cmd.Flags().GetString("model")
		gitName, _ := cmd.Flags().GetString("git-name")
		gitEmail, _ := cmd.Flags().GetString("git-email")

		body := map[string]string{}
		if instructions != "" {
			body["instructions"] = instructions
		}
		if model != "" {
			body["model"] = model
		}
		if gitName != "" {
			body["git_name"] = gitName
		}
		if gitEmail != "" {
			body["git_email"] = gitEmail
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s/agents/%s", wsID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/agents/%s", wsID, args[0])); err != nil {
			return err
		}
		fmt.Println("Agent deleted.")
		return nil
	},
}

var agentStatusCmd = &cobra.Command{
	Use:   "status <id> <status>",
	Short: "Update agent status (online, offline, busy, paused)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Patch(fmt.Sprintf("/api/workspaces/%s/agents/%s/status", wsID, args[0]),
			map[string]string{"status": args[1]}, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentGrantRoleCmd = &cobra.Command{
	Use:   "grant-role",
	Short: "Grant a predefined role's permissions to an agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		agentID, _ := cmd.Flags().GetString("agent")
		roleName, _ := cmd.Flags().GetString("role")

		if wsID == "" || agentID == "" || roleName == "" {
			return fmt.Errorf("--workspace, --agent, and --role are required")
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/agents/%s/grant-role", wsID, agentID),
			map[string]string{"role": roleName}, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentListRolesCmd = &cobra.Command{
	Use:     "list-roles",
	Short:   "List available agent roles",
	Aliases: []string{"ls-roles"},
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get("/api/agent-roles", &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentSkillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage agent skills",
}

var agentSkillAddCmd = &cobra.Command{
	Use:   "add <agent-id> <skill-id>",
	Short: "Add a skill to an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/agents/%s/skills", wsID, args[0]),
			map[string]string{"skill_id": args[1]}, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentSkillRemoveCmd = &cobra.Command{
	Use:   "remove <agent-id> <skill-id>",
	Short: "Remove a skill from an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/agents/%s/skills/%s", wsID, args[0], args[1])); err != nil {
			return err
		}
		fmt.Println("Skill removed from agent.")
		return nil
	},
}

var agentSkillListCmd = &cobra.Command{
	Use:     "list <agent-id>",
	Short:   "List skills assigned to an agent",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/agents/%s/skills", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentMcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage agent MCP servers",
}

var agentMcpAddCmd = &cobra.Command{
	Use:   "add <agent-id> <mcp-server-id>",
	Short: "Add an MCP server to an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/agents/%s/mcp-servers", wsID, args[0]),
			map[string]string{"mcp_server_id": args[1]}, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentMcpRemoveCmd = &cobra.Command{
	Use:   "remove <agent-id> <mcp-server-id>",
	Short: "Remove an MCP server from an agent",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/agents/%s/mcp-servers/%s", wsID, args[0], args[1])); err != nil {
			return err
		}
		fmt.Println("MCP server removed from agent.")
		return nil
	},
}

var agentMcpListCmd = &cobra.Command{
	Use:     "list <agent-id>",
	Short:   "List MCP servers assigned to an agent",
	Aliases: []string{"ls"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/agents/%s/mcp-servers", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var agentStatsCmd = &cobra.Command{
	Use:   "stats <id>",
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
	agentListCmd.Flags().String("workspace", "", "workspace ID")
	agentGetCmd.Flags().String("workspace", "", "workspace ID")
	agentCreateCmd.Flags().String("workspace", "", "workspace ID")
	agentCreateCmd.Flags().String("name", "", "agent name")
	agentCreateCmd.Flags().String("provider", "claude", "agent provider (claude, openclaw, opencode, etc.)")
	agentCreateCmd.Flags().String("instructions", "", "agent instructions")
	agentCreateCmd.Flags().String("model", "", "model to use")
	agentCreateCmd.Flags().String("git-name", "", "git commit author name (required)")
	agentCreateCmd.Flags().String("git-email", "", "git commit author email (required)")
	agentUpdateCmd.Flags().String("workspace", "", "workspace ID")
	agentUpdateCmd.Flags().String("instructions", "", "agent instructions")
	agentUpdateCmd.Flags().String("model", "", "model to use")
	agentUpdateCmd.Flags().String("git-name", "", "git commit author name")
	agentUpdateCmd.Flags().String("git-email", "", "git commit author email")
	agentDeleteCmd.Flags().String("workspace", "", "workspace ID")
	agentStatusCmd.Flags().String("workspace", "", "workspace ID")
	agentGrantRoleCmd.Flags().String("workspace", "", "workspace ID")
	agentGrantRoleCmd.Flags().String("agent", "", "agent UUID")
	agentGrantRoleCmd.Flags().String("role", "", "role name (developer, reviewer, ops)")
	agentSkillAddCmd.Flags().String("workspace", "", "workspace ID")
	agentSkillRemoveCmd.Flags().String("workspace", "", "workspace ID")
	agentSkillListCmd.Flags().String("workspace", "", "workspace ID")
	agentMcpAddCmd.Flags().String("workspace", "", "workspace ID")
	agentMcpRemoveCmd.Flags().String("workspace", "", "workspace ID")
	agentMcpListCmd.Flags().String("workspace", "", "workspace ID")

	agentSkillCmd.AddCommand(agentSkillAddCmd)
	agentSkillCmd.AddCommand(agentSkillRemoveCmd)
	agentSkillCmd.AddCommand(agentSkillListCmd)
	agentMcpCmd.AddCommand(agentMcpAddCmd)
	agentMcpCmd.AddCommand(agentMcpRemoveCmd)
	agentMcpCmd.AddCommand(agentMcpListCmd)
	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentGetCmd)
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentUpdateCmd)
	agentCmd.AddCommand(agentDeleteCmd)
	agentCmd.AddCommand(agentStatusCmd)
	agentCmd.AddCommand(agentSkillCmd)
	agentCmd.AddCommand(agentMcpCmd)
	agentCmd.AddCommand(agentStatsCmd)
	agentCmd.AddCommand(agentGrantRoleCmd)
	agentCmd.AddCommand(agentListRolesCmd)
	rootCmd.AddCommand(agentCmd)
}
