// cmd/teammate-server —— Teammate Server 独立部署二进制（部署工具链入口）。
//
// 与 `teammate` 管理 CLI 解耦：CLI 定位为纯 HTTP API（零直连 DB），启动服务与
// schema 迁移属运维前置操作，由本二进制承担。用法：
//
//	teammate-server                 # 启动 HTTP 服务（被测 ./internal/server）
//	teammate-server migrate --path <dir>   # 执行数据库迁移
//
// 详见 docs/命令行工具设计.md §5.10。配置经 TEAMS_* 环境变量注入（server.LoadConfig）。
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/teammate/server/internal/db"
	"github.com/teammate/server/internal/server"
)

var migratePath string

var rootCmd = &cobra.Command{
	Use:   "teammate-server",
	Short: "Teammate Server（部署工具链）：启动服务 / 执行数据库迁移",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := server.LoadConfig()

		srv, err := server.New(cfg)
		if err != nil {
			return err
		}

		return srv.Start()
	},
}

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Run database migrations (requires direct DB access)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg := server.LoadConfig()

		mp := migratePath
		if mp == "" {
			mp = "./internal/db/migrations"
		}

		if err := db.Migrate(cfg.DatabaseURL, mp); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}

		fmt.Println("migrations applied successfully")
		return nil
	},
}

func init() {
	migrateCmd.Flags().StringVar(&migratePath, "path", "", "migrations directory path")
	rootCmd.AddCommand(migrateCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}