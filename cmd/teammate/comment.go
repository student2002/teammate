// comment.go 实现评论子命令，管理任务评论的创建和查询。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var commentCmd = &cobra.Command{
	Use:   "comment",
	Short: "Manage task comments",
}

var commentListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List comments for a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/tasks/%s/comments", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var commentAddCmd = &cobra.Command{
	Use:   "add <task-id>",
	Short: "Add a comment to a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		content, _ := cmd.Flags().GetString("content")
		if content == "" {
			return fmt.Errorf("--content is required")
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/comments", args[0]),
			map[string]string{"content": content}, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	commentAddCmd.Flags().String("content", "", "comment content")

	commentCmd.AddCommand(commentListCmd)
	commentCmd.AddCommand(commentAddCmd)
	rootCmd.AddCommand(commentCmd)
}
