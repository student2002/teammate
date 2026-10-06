// agent.go implements the agent subcommand, managing agent registration, status viewing, deletion, and other operations.
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
	Short: "Create an agent instance under a daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		daemonID, _ := cmd.Flags().GetString("daemon")
		name, _ := cmd.Flags().GetString("name")
		provider, _ := cmd.Flags().GetString("provider")
		instructions, _ := cmd.Flags().GetString("instructions")
		personaKey, _ := cmd.Flags().GetString("persona-key")
		gitName, _ := cmd.Flags().GetString("git-name")
		gitEmail, _ := cmd.Flags().GetString("git-email")

		if daemonID == "" || name == "" {
			return fmt.Errorf("--daemon and --name are required")
		}
		if gitName == "" || gitEmail == "" {
			return fmt.Errorf("--git-name and --git-email are required")
		}

		body := map[string]string{
			"name":      name,
			"git_name":  gitName,
			"git_email": gitEmail,
		}
		if provider != "" {
			body["provider"] = provider
		}
		if instructions != "" {
			body["instructions"] = instructions
		}
		if personaKey != "" {
			body["persona_key"] = personaKey
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/daemons/%s/agents", wsID, daemonID), body, &result); err != nil {
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		agentID, _ := cmd.Flags().GetString("agent")
		roleName, _ := cmd.Flags().GetString("role")

		if agentID == "" || roleName == "" {
			return fmt.Errorf("--agent and --role are required")
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/agents/%s/stats", wsID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	agentListCmd.Flags().String("workspace", "", "workspace ID")
	agentGetCmd.Flags().String("workspace", "", "workspace ID")
	agentCreateCmd.Flags().String("workspace", "", "workspace ID")
	agentCreateCmd.Flags().String("daemon", "", "daemon ID (from the web agents page; the instance runs on this daemon)")
	agentCreateCmd.Flags().String("name", "", "agent name")
	agentCreateCmd.Flags().String("provider", "", "tool provider; must be present in the daemon's detected-tools snapshot")
	agentCreateCmd.Flags().String("instructions", "", "agent instructions")
	agentCreateCmd.Flags().String("persona-key", "", "persona key (generated when omitted; immutable after creation)")
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
	agentStatsCmd.Flags().String("workspace", "", "workspace ID")

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
