// runtime.go 实现运行时子命令，管理 Agent 守护进程运行时。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var runtimeCmd = &cobra.Command{
	Use:   "runtime",
	Short: "Manage runtimes",
}

var runtimeListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List all runtimes",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/runtimes", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var runtimeRegisterCmd = &cobra.Command{
	Use:   "register",
	Short: "Register a runtime",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		agentID, _ := cmd.Flags().GetString("agent")
		provider, _ := cmd.Flags().GetString("provider")
		version, _ := cmd.Flags().GetString("version")

		if wsID == "" || agentID == "" {
			return fmt.Errorf("--workspace and --agent are required")
		}

		body := map[string]string{
			"agent_id": agentID,
			"provider": provider,
			"version":  version,
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/runtimes", wsID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var runtimeHeartbeatCmd = &cobra.Command{
	Use:   "heartbeat <id>",
	Short: "Send runtime heartbeat",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/runtimes/%s/heartbeat", wsID, args[0]),
			nil, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	runtimeListCmd.Flags().String("workspace", "", "workspace ID")
	runtimeRegisterCmd.Flags().String("workspace", "", "workspace ID")
	runtimeRegisterCmd.Flags().String("agent", "", "agent ID")
	runtimeRegisterCmd.Flags().String("provider", "", "provider name")
	runtimeRegisterCmd.Flags().String("version", "", "version")
	runtimeHeartbeatCmd.Flags().String("workspace", "", "workspace ID")

	runtimeCmd.AddCommand(runtimeListCmd)
	runtimeCmd.AddCommand(runtimeRegisterCmd)
	runtimeCmd.AddCommand(runtimeHeartbeatCmd)
	rootCmd.AddCommand(runtimeCmd)
}
