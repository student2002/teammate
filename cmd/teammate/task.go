// task.go 实现任务子命令，管理任务的增删改查和状态流转。
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{
	Use:   "task",
	Short: "Manage tasks",
}

var taskListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List tasks in a project",
	Aliases: []string{"ls"},
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		if projectID == "" {
			return fmt.Errorf("--project is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/tasks", projectID), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var taskGetCmd = &cobra.Command{
	Use:   "get <id>",
	Short: "Get task details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		if projectID == "" {
			return fmt.Errorf("--project is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/tasks/%s", projectID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var taskCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new task",
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		title, _ := cmd.Flags().GetString("title")
		desc, _ := cmd.Flags().GetString("description")
		taskType, _ := cmd.Flags().GetString("type")
		priority, _ := cmd.Flags().GetString("priority")
		workflowID, _ := cmd.Flags().GetString("workflow")

		if projectID == "" || title == "" {
			return fmt.Errorf("--project and --title are required")
		}

		body := map[string]string{
			"title":    title,
			"type":     taskType,
			"priority": priority,
		}
		if desc != "" {
			body["description"] = desc
		}
		if workflowID != "" {
			body["workflow_template_id"] = workflowID
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/projects/%s/tasks", projectID), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var taskUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		if projectID == "" {
			return fmt.Errorf("--project is required")
		}
		title, _ := cmd.Flags().GetString("title")
		desc, _ := cmd.Flags().GetString("description")
		priority, _ := cmd.Flags().GetString("priority")
		status, _ := cmd.Flags().GetString("status")

		body := map[string]string{}
		if title != "" {
			body["title"] = title
		}
		if desc != "" {
			body["description"] = desc
		}
		if priority != "" {
			body["priority"] = priority
		}
		if status != "" {
			body["status"] = status
		}

		client := newAPIClient()
		var result interface{}
		if err := client.Put(fmt.Sprintf("/api/projects/%s/tasks/%s", projectID, args[0]), body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var taskDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		if projectID == "" {
			return fmt.Errorf("--project is required")
		}
		client := newAPIClient()
		if err := client.Delete(fmt.Sprintf("/api/projects/%s/tasks/%s", projectID, args[0])); err != nil {
			return err
		}
		fmt.Println("Task deleted.")
		return nil
	},
}

var taskCancelCmd = &cobra.Command{
	Use:   "cancel <id>",
	Short: "Cancel a task and its nodes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		if projectID == "" {
			return fmt.Errorf("--project is required")
		}
		client := newAPIClient()
		if err := client.Put(fmt.Sprintf("/api/projects/%s/tasks/%s", projectID, args[0]),
			map[string]string{"status": "cancelled"}, nil); err != nil {
			return err
		}
		fmt.Println("Task cancelled.")
		return nil
	},
}

var taskInterruptCmd = &cobra.Command{
	Use:   "interrupt <task-id>",
	Short: "Interrupt a task (marks in-progress nodes as manual intervention)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		comment, _ := cmd.Flags().GetString("comment")
		body := map[string]string{}
		if comment != "" {
			body["comment"] = comment
		}

		// interrupt 是 task 级别操作，handler 从 {taskId} 获取 taskID，
		// {id} 段仅用于路由匹配，handler 内部不读取该值。
		client := newAPIClient()
		var result interface{}
		if err := client.Post(fmt.Sprintf("/api/tasks/%s/nodes/_/interrupt", args[0]),
			body, &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var taskNodeCmd = &cobra.Command{
	Use:   "node",
	Short: "Manage task nodes",
}

var taskNodeListCmd = &cobra.Command{
	Use:   "list <task-id>",
	Short: "List nodes for a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		projectID, _ := cmd.Flags().GetString("project")
		if projectID == "" {
			return fmt.Errorf("--project is required")
		}
		client := newAPIClient()
		var result interface{}
		if err := client.Get(fmt.Sprintf("/api/projects/%s/tasks/%s/nodes", projectID, args[0]), &result); err != nil {
			return err
		}
		return printOutput(result, getOutputFormat())
	},
}

var taskLogsCmd = &cobra.Command{
	Use:   "logs <task-id>",
	Short: "Show buffered execution logs for a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		nodeID, _ := cmd.Flags().GetString("node")
		path := fmt.Sprintf("/api/tasks/%s/logs", args[0])
		if nodeID != "" {
			path += "?node_id=" + nodeID
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
	taskListCmd.Flags().String("project", "", "project ID")
	taskGetCmd.Flags().String("project", "", "project ID")
	taskCreateCmd.Flags().String("project", "", "project ID")
	taskCreateCmd.Flags().String("title", "", "task title")
	taskCreateCmd.Flags().String("description", "", "task description")
	taskCreateCmd.Flags().String("type", "task", "task type (story, bug, task)")
	taskCreateCmd.Flags().String("priority", "medium", "task priority (urgent, high, medium, low)")
	taskCreateCmd.Flags().String("workflow", "", "workflow template ID")
	taskUpdateCmd.Flags().String("project", "", "project ID")
	taskUpdateCmd.Flags().String("title", "", "task title")
	taskUpdateCmd.Flags().String("description", "", "task description")
	taskUpdateCmd.Flags().String("priority", "", "task priority")
	taskUpdateCmd.Flags().String("status", "", "task status")
	taskDeleteCmd.Flags().String("project", "", "project ID")
	taskCancelCmd.Flags().String("project", "", "project ID")
	taskInterruptCmd.Flags().String("comment", "", "interruption comment")
	taskNodeListCmd.Flags().String("project", "", "project ID")
	taskLogsCmd.Flags().String("node", "", "filter logs by node ID")

	taskNodeCmd.AddCommand(taskNodeListCmd)
	taskCmd.AddCommand(taskListCmd)
	taskCmd.AddCommand(taskGetCmd)
	taskCmd.AddCommand(taskCreateCmd)
	taskCmd.AddCommand(taskUpdateCmd)
	taskCmd.AddCommand(taskDeleteCmd)
	taskCmd.AddCommand(taskCancelCmd)
	taskCmd.AddCommand(taskInterruptCmd)
	taskCmd.AddCommand(taskLogsCmd)
	taskCmd.AddCommand(taskNodeCmd)
	rootCmd.AddCommand(taskCmd)
}
