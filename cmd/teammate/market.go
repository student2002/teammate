// market.go 实现社区工作流市场子命令，浏览和导入社区工作流。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var marketCmd = &cobra.Command{
	Use:   "market",
	Short: "Community workflow marketplace",
}

var marketListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List community workflows",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get("/api/community/workflows", &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var marketGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get community workflow details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/community/workflows/%s", args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var marketPublishCmd = &cobra.Command{
	Use:   "publish",
	Short: "Publish a workflow to community",
	RunE: func(cmd *cobra.Command, args []string) error {
		name, _ := cmd.Flags().GetString("name")
		desc, _ := cmd.Flags().GetString("description")
		version, _ := cmd.Flags().GetString("version")
		author, _ := cmd.Flags().GetString("author")
		definitionRaw, _ := cmd.Flags().GetString("definition")

		if name == "" {
			return fmt.Errorf("--name is required")
		}
		if definitionRaw == "" {
			return fmt.Errorf("--definition is required (workflow definition JSON, or @path/to/file.json)")
		}

		// 支持 @ 前缀从文件读取定义
		if strings.HasPrefix(definitionRaw, "@") {
			data, err := os.ReadFile(strings.TrimPrefix(definitionRaw, "@"))
			if err != nil {
				return fmt.Errorf("read definition file: %w", err)
			}
			definitionRaw = string(data)
		}
		// 校验定义是合法 JSON
		if !json.Valid([]byte(definitionRaw)) {
			return fmt.Errorf("--definition is not valid JSON")
		}

		body := map[string]interface{}{
			"name":                name,
			"workflow_definition": json.RawMessage(definitionRaw),
		}
		if desc != "" {
			body["description"] = desc
		}
		if version != "" {
			body["version"] = version
		}
		if author != "" {
			body["author"] = author
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post("/api/community/workflows", body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var marketImportCmd = &cobra.Command{
	Use:   "import <id>",
	Short: "Import a community workflow",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		wsID, _ := cmd.Flags().GetString("workspace")
		if wsID == "" {
			return fmt.Errorf("--workspace is required")
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/community/workflows/%s/import", args[0]),
			map[string]string{"workspace_id": wsID}, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

func init() {
	marketPublishCmd.Flags().String("name", "", "workflow name (required)")
	marketPublishCmd.Flags().String("description", "", "workflow description")
	marketPublishCmd.Flags().String("version", "", "workflow version (default: 1.0.0)")
	marketPublishCmd.Flags().String("author", "", "workflow author")
	marketPublishCmd.Flags().String("definition", "", "workflow definition JSON, or @path/to/file.json (required)")
	marketImportCmd.Flags().String("workspace", "", "workspace ID")

	marketCmd.AddCommand(marketListCmd)
	marketCmd.AddCommand(marketGetCmd)
	marketCmd.AddCommand(marketPublishCmd)
	marketCmd.AddCommand(marketImportCmd)
	rootCmd.AddCommand(marketCmd)
}
