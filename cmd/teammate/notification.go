// notification.go implements the notification subcommand, querying and managing notifications.
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
		wsID, err := resolveWorkspaceID(cmd)
		if err != nil {
			return err
		}
		memberID, _ := cmd.Flags().GetString("member")

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
