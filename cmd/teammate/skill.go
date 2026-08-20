// skill.go 实现技能子命令，管理技能的新增、查询、更新和删除。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skills",
}

var skillListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List skills in a workspace",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/workspaces/%s/skills", wsID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var skillCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a skill",
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("description")
		category, _ := cmd.Flags().GetString("category")
		promptTemplate, _ := cmd.Flags().GetString("prompt-template")

		if wsID == "" || name == "" {
			return fmt.Errorf("--workspace and --name are required")
		}

		body := map[string]interface{}{"name": name}
		if desc != "" {
			body["description"] = desc
		}
		if category != "" {
			body["category"] = category
		}
		if promptTemplate != "" {
			body["prompt_template"] = promptTemplate
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/workspaces/%s/skills", wsID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var skillUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a skill",
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
		if desc, _ := cmd.Flags().GetString("description"); desc != "" {
			body["description"] = desc
		}
		if category, _ := cmd.Flags().GetString("category"); category != "" {
			body["category"] = category
		}
		if promptTemplate, _ := cmd.Flags().GetString("prompt-template"); promptTemplate != "" {
			body["prompt_template"] = promptTemplate
		}

		if len(body) == 0 {
			return fmt.Errorf("at least one field to update is required (--name, --description, --category, --prompt-template)")
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Put(fmt.Sprintf("/api/workspaces/%s/skills/%s", wsID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var skillDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a skill",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/workspaces/%s/skills/%s", wsID, args[0])); err != nil {
			return err
		}
		fmt.Println("Skill deleted.")
		return nil
	},
}

func init() {
	skillListCmd.Flags().String("workspace", "", "workspace ID")
	skillCreateCmd.Flags().String("workspace", "", "workspace ID")
	skillCreateCmd.Flags().String("name", "", "skill name")
	skillCreateCmd.Flags().String("description", "", "skill description")
	skillCreateCmd.Flags().String("category", "", "skill category")
	skillCreateCmd.Flags().String("prompt-template", "", "skill prompt template")
	skillUpdateCmd.Flags().String("workspace", "", "workspace ID")
	skillUpdateCmd.Flags().String("name", "", "skill name")
	skillUpdateCmd.Flags().String("description", "", "skill description")
	skillUpdateCmd.Flags().String("category", "", "skill category")
	skillUpdateCmd.Flags().String("prompt-template", "", "skill prompt template")
	skillDeleteCmd.Flags().String("workspace", "", "workspace ID")

	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillCreateCmd)
	skillCmd.AddCommand(skillUpdateCmd)
	skillCmd.AddCommand(skillDeleteCmd)
	rootCmd.AddCommand(skillCmd)
}
