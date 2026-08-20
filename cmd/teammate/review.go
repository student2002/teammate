// review.go 实现审查子命令，管理代码审查队列。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var reviewCmd = &cobra.Command{
	Use:   "review",
	Short: "Review operations",
}

var reviewQueueCmd = &cobra.Command{
	Use:   "queue <project-id>",
	Short: "Get review queue for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/review/review-queue", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var reviewCheckSelfCmd = &cobra.Command{
	Use:   "check-self <task-id> <node-id>",
	Short: "Check if a review node is a self-review",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/tasks/%s/review/nodes/%s/self-review-check", args[0], args[1]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	reviewCmd.AddCommand(reviewQueueCmd)
	reviewCmd.AddCommand(reviewCheckSelfCmd)
	rootCmd.AddCommand(reviewCmd)
}
