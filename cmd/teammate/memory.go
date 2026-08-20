// memory.go 实现记忆子命令，管理工作区共享记忆。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var memoryCmd = &cobra.Command{
	Use:   "memory",
	Short: "Manage workspace memories",
}

var memoryListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List memories",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/memories?workspace_id=%s", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var memorySearchCmd = &cobra.Command{
	Use:   "search <keyword>",
	Short: "Search memories",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/memories/search?q=%s&workspace_id=%s", args[0], wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var memoryAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a memory",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		title, _ := cmd.Flags().GetString("title")
		content, _ := cmd.Flags().GetString("content")

		if wsID == "" || title == "" || content == "" {
			return fmt.Errorf("--workspace, --title, and --content are required")
		}

		body := map[string]string{
			"workspace_id": wsID,
			"title":        title,
			"content":      content,
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post("/api/memories", body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var memoryDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a memory",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/memories/%s", args[0])); err != nil {
			return err
		}
		fmt.Println("Memory deleted.")
		return nil
	},
}

func init() {
	memoryListCmd.Flags().String("workspace", "", "workspace ID")
	memorySearchCmd.Flags().String("workspace", "", "workspace ID")
	memoryAddCmd.Flags().String("workspace", "", "workspace ID")
	memoryAddCmd.Flags().String("title", "", "memory title")
	memoryAddCmd.Flags().String("content", "", "memory content")

	memoryCmd.AddCommand(memoryListCmd)
	memoryCmd.AddCommand(memorySearchCmd)
	memoryCmd.AddCommand(memoryAddCmd)
	memoryCmd.AddCommand(memoryDeleteCmd)
	rootCmd.AddCommand(memoryCmd)
}
