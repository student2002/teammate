// mcp.go 实现 MCP 服务器子命令，管理 MCP 服务器的增删改查。
package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP servers",
}

var mcpListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List MCP servers in a workspace",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/mcp-servers", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var mcpCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create an MCP server",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		name, _ := cmd.Flags().GetString("name")
		url, _ := cmd.Flags().GetString("url")
		srvType, _ := cmd.Flags().GetString("type")
		authType, _ := cmd.Flags().GetString("auth-type")
		envVarsStr, _ := cmd.Flags().GetString("env-vars")

		if wsID == "" || name == "" || url == "" {
			return fmt.Errorf("--workspace, --name, and --url are required")
		}

		body := map[string]interface{}{"name": name, "url": url}
		if srvType != "" {
			body["type"] = srvType
		}
		if authType != "" {
			body["auth_type"] = authType
		}
		if envVarsStr != "" {
			var envVars map[string]interface{}
			if err := json.Unmarshal([]byte(envVarsStr), &envVars); err != nil {
				return fmt.Errorf("invalid --env-vars JSON: %w", err)
			}
			body["env_vars"] = envVars
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/mcp-servers", wsID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var mcpUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}

		body := map[string]interface{}{}
		if name, _ := cmd.Flags().GetString("name"); name != "" {
			body["name"] = name
		}
		if url, _ := cmd.Flags().GetString("url"); url != "" {
			body["url"] = url
		}
		if srvType, _ := cmd.Flags().GetString("type"); srvType != "" {
			body["type"] = srvType
		}
		if authType, _ := cmd.Flags().GetString("auth-type"); authType != "" {
			body["auth_type"] = authType
		}
		if envVarsStr, _ := cmd.Flags().GetString("env-vars"); envVarsStr != "" {
			var envVars map[string]interface{}
			if err := json.Unmarshal([]byte(envVarsStr), &envVars); err != nil {
				return fmt.Errorf("invalid --env-vars JSON: %w", err)
			}
			body["env_vars"] = envVars
		}
		if status, _ := cmd.Flags().GetString("status"); status != "" {
			body["status"] = status
		}

		if len(body) == 0 {
			return fmt.Errorf("at least one field to update is required (--name, --url, --type, --auth-type, --env-vars, --status)")
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s/mcp-servers/%s", wsID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var mcpDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete an MCP server",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/mcp-servers/%s", wsID, args[0])); err != nil {
			return err
		}
		fmt.Println("MCP server deleted.")
		return nil
	},
}

var mcpHealthCheckCmd = &cobra.Command{
	Use:   "health-check <id>",
	Short: "Check MCP server health",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/mcp-servers/%s/health-check", wsID, args[0]),
			nil, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	mcpListCmd.Flags().String("workspace", "", "workspace ID")
	mcpCreateCmd.Flags().String("workspace", "", "workspace ID")
	mcpCreateCmd.Flags().String("name", "", "MCP server name")
	mcpCreateCmd.Flags().String("url", "", "MCP server URL")
	mcpCreateCmd.Flags().String("type", "", "MCP server type (sse, streamable_http, stdio)")
	mcpCreateCmd.Flags().String("auth-type", "", "authentication type (none, bearer, api_key)")
	mcpCreateCmd.Flags().String("env-vars", "", "environment variables as JSON object")
	mcpUpdateCmd.Flags().String("workspace", "", "workspace ID")
	mcpUpdateCmd.Flags().String("name", "", "MCP server name")
	mcpUpdateCmd.Flags().String("url", "", "MCP server URL")
	mcpUpdateCmd.Flags().String("type", "", "MCP server type")
	mcpUpdateCmd.Flags().String("auth-type", "", "authentication type")
	mcpUpdateCmd.Flags().String("env-vars", "", "environment variables as JSON object")
	mcpUpdateCmd.Flags().String("status", "", "MCP server status (active, inactive)")
	mcpDeleteCmd.Flags().String("workspace", "", "workspace ID")
	mcpHealthCheckCmd.Flags().String("workspace", "", "workspace ID")

	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpCreateCmd)
	mcpCmd.AddCommand(mcpUpdateCmd)
	mcpCmd.AddCommand(mcpDeleteCmd)
	mcpCmd.AddCommand(mcpHealthCheckCmd)
	rootCmd.AddCommand(mcpCmd)
}
