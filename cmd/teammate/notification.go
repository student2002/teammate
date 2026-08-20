// notification.go 实现通知子命令，查询和管理通知。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var notificationCmd = &cobra.Command{
	Use:     "notification",
	Short:   "Manage notifications",
	Aliases: []string{"notify"},
}

var notificationListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List notifications",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		memberID, _ := cmd.Flags().GetString("member")

		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}

		path := fmt.Sprintf("/api/workspaces/%s/notifications", wsID)
		if memberID != "" {
			path += "?member_id=" + memberID
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Get(path, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	notificationListCmd.Flags().String("workspace", "", "workspace ID")
	notificationListCmd.Flags().String("member", "", "member ID")

	notificationCmd.AddCommand(notificationListCmd)
	rootCmd.AddCommand(notificationCmd)
}
