// root.go 定义 CLI 根命令，提供子命令注册、服务连接和公共初始化。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	outputFmt string
)

// 退出码
const (
	ExitSuccess   = 0
	ExitGeneral   = 1
	ExitParam     = 2
	ExitAuth      = 10
	ExitForbidden = 11
	ExitNotFound  = 20
	ExitConflict  = 30
	ExitDB        = 40
	ExitInternal  = 50
)

// skipAuthCommands 不需要认证的命令（完整 CommandPath）。
var skipAuthCommands = map[string]bool{
	"teammate auth login":            true,
	"teammate auth register":         true,
	"teammate completion bash":       true,
	"teammate completion zsh":        true,
	"teammate completion fish":       true,
	"teammate completion powershell": true,
	"teammate":                       true, // root 本身
}

var rootCmd = &cobra.Command{
	Use:   "teammate",
	Short: "Teammate - AI-powered team collaboration platform",
	Long:  "Teammate Server - AI-powered team collaboration platform.\nManage workspaces, projects, agents, tasks, and more.",
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&outputFmt, "output", "o", "table", "output format: table, json, yaml")
	rootCmd.PersistentFlags().String("token", "", "JWT token (overrides stored credentials)")

	// 全局认证拦截
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if skipAuthCommands[cmd.CommandPath()] {
			return nil
		}
		// 验证 token 有效（未登录或过期则退出）
		requireAuth()
		return nil
	}
}

// getOutputFormat 返回当前输出格式。
func getOutputFormat() OutputFormat {
	return parseOutputFormat(outputFmt)
}

// exitError 打印错误信息并以指定退出码退出。
func exitError(code int, msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, msg+"\n", args...)
	os.Exit(code)
}
