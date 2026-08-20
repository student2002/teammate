// cleanup.go 提供测试数据库清理辅助函数。
package testdb

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/jackc/pgx/v5/stdlib"
)

const testDBName = "teammate_test"

// GetTestDSN 返回测试数据库的连接字符串。
// 它使用独立的 "teammate_test" 数据库，避免污染开发数据。
// 设置 TEAMS_TEST_DATABASE_URL 可覆盖默认连接配置。
func GetTestDSN() string {
	if dsn := os.Getenv("TEAMS_TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return fmt.Sprintf("postgres://postgres:teammate@localhost:15432/%s?sslmode=disable", testDBName)
}

// getAdminDSN 返回管理员数据库的连接字符串（用于创建/删除测试数据库）。
func getAdminDSN() string {
	if dsn := os.Getenv("TEAMS_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return "postgres://postgres:teammate@localhost:15432/teammate?sslmode=disable"
}

// SetupTestDB 确保 teammate_test 数据库存在并运行迁移。
// 返回测试数据库的连接。完成后调用 CleanupTestDB 清理。
func SetupTestDB() (*sql.DB, error) {
	// 连接到管理员数据库以确保测试数据库存在
	adminDB, err := sql.Open("pgx", getAdminDSN())
	if err != nil {
		return nil, fmt.Errorf("connect admin db: %w", err)
	}
	defer adminDB.Close()

	// 如果测试数据库不存在则创建（若已存在则忽略错误）
	var exists bool
	err = adminDB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", testDBName).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check test db existence: %w", err)
	}
	if !exists {
		if _, err := adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", testDBName)); err != nil {
			return nil, fmt.Errorf("create test db: %w", err)
		}
	}

	// 连接到测试数据库
	testDB, err := sql.Open("pgx", GetTestDSN())
	if err != nil {
		return nil, fmt.Errorf("connect test db: %w", err)
	}
	if err := testDB.Ping(); err != nil {
		testDB.Close()
		return nil, fmt.Errorf("ping test db: %w", err)
	}

	// 如果架构不存在则运行迁移
	var tableExists bool
	err = testDB.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'workspaces')").Scan(&tableExists)
	if err != nil {
		testDB.Close()
		return nil, fmt.Errorf("check schema existence: %w", err)
	}
	if !tableExists {
		migrationPath := findMigrationPath()
		if migrationPath != "" {
			sqlBytes, err := os.ReadFile(migrationPath)
			if err != nil {
				testDB.Close()
				return nil, fmt.Errorf("read migration file: %w", err)
			}
			if _, err := testDB.Exec(string(sqlBytes)); err != nil {
				// 并发测试包可能已运行过迁移；
				// 再次检查后确认失败
				var existsNow bool
				if checkErr := testDB.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'workspaces')").Scan(&existsNow); checkErr == nil && existsNow {
					return testDB, nil
				}
				testDB.Close()
				return nil, fmt.Errorf("run migration: %w", err)
			}
		}
	}
	if err := ensureSchemaCompatibility(testDB); err != nil {
		testDB.Close()
		return nil, err
	}

	return testDB, nil
}

func ensureSchemaCompatibility(db *sql.DB) error {
	statements := []string{
		`DO $$ BEGIN
			CREATE TYPE workflow_trigger_type AS ENUM ('manual', 'schedule', 'github_issue');
		EXCEPTION WHEN duplicate_object THEN NULL;
		END $$`,
		`ALTER TABLE workflow_templates ADD COLUMN IF NOT EXISTS trigger_type workflow_trigger_type NOT NULL DEFAULT 'manual'`,
		`ALTER TABLE workflow_templates ADD COLUMN IF NOT EXISTS trigger_config JSONB NOT NULL DEFAULT '{}'`,
		`ALTER TABLE workflow_templates ADD COLUMN IF NOT EXISTS trigger_enabled BOOLEAN NOT NULL DEFAULT true`,
		`ALTER TABLE workflow_templates ADD COLUMN IF NOT EXISTS next_run_at TIMESTAMPTZ`,
		`ALTER TABLE workflow_templates ADD COLUMN IF NOT EXISTS last_triggered_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_wf_templates_due_triggers ON workflow_templates(next_run_at)
			WHERE trigger_enabled = true AND trigger_type = 'schedule' AND next_run_at IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS workflow_trigger_runs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
			project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
			workflow_template_id UUID NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
			trigger_type workflow_trigger_type NOT NULL,
			external_key TEXT NOT NULL DEFAULT '',
			status VARCHAR(20) NOT NULL,
			task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
			payload JSONB NOT NULL DEFAULT '{}',
			error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
			UNIQUE(workflow_template_id, external_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_trigger_runs_workspace ON workflow_trigger_runs(workspace_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_workflow_trigger_runs_template ON workflow_trigger_runs(workflow_template_id, created_at DESC)`,
		`ALTER TABLE comments ADD COLUMN IF NOT EXISTS node_id UUID REFERENCES task_nodes(id) ON DELETE CASCADE`,
		`ALTER TABLE comments ADD COLUMN IF NOT EXISTS source_node_id UUID REFERENCES task_nodes(id) ON DELETE SET NULL`,
		`ALTER TABLE comments ADD COLUMN IF NOT EXISTS parent_id UUID REFERENCES comments(id) ON DELETE CASCADE`,
		`ALTER TABLE comments ADD COLUMN IF NOT EXISTS metadata JSONB DEFAULT '{}'`,
		`ALTER TABLE comments ADD COLUMN IF NOT EXISTS mentions UUID[] DEFAULT '{}'`,
		`ALTER TABLE comments ADD COLUMN IF NOT EXISTS edited_at TIMESTAMPTZ`,
		`CREATE INDEX IF NOT EXISTS idx_comments_node ON comments(node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_source_node ON comments(source_node_id)`,
		`CREATE INDEX IF NOT EXISTS idx_comments_parent ON comments(parent_id)`,
		`CREATE TABLE IF NOT EXISTS task_logs (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			task_id INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
			node_id UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
			type TEXT NOT NULL CHECK (type IN ('stdout', 'stderr', 'system')),
			content TEXT NOT NULL,
			timestamp TIMESTAMPTZ NOT NULL DEFAULT now(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_task_logs_task_timestamp ON task_logs(task_id, timestamp, id)`,
		`CREATE INDEX IF NOT EXISTS idx_task_logs_task_node_timestamp ON task_logs(task_id, node_id, timestamp, id)`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("ensure schema compatibility: %w", err)
		}
	}
	return nil
}

// findMigrationPath 定位相对于测试二进制文件的初始化迁移文件。
func findMigrationPath() string {
	// 从工作目录尝试常见的相对路径
	candidates := []string{
		"internal/db/migrations/001_init.up.sql",
		"../internal/db/migrations/001_init.up.sql",
		"../../internal/db/migrations/001_init.up.sql",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	// 回退：如果可用则使用模块根目录
	if root := findModuleRoot(); root != "" {
		p := filepath.Join(root, "internal/db/migrations/001_init.up.sql")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// findModuleRoot 从当前目录向上查找 go.mod 文件。
func findModuleRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// TruncateAll 删除测试数据库中所有业务表的数据，
// 保留表结构。按依赖顺序截断表。
// 仅在 TestMain 中用于最终的安全网清理。
func TruncateAll(db *sql.DB) error {
	tables := []string{
		"token_usage",
		"workflow_trigger_runs",
		"execution_sessions",
		"task_logs",
		"task_log_chunks",
		"node_transitions",
		"comments",
		"task_nodes",
		"tasks",
		"agent_mcp_servers",
		"agent_skills",
		"agent_permissions",
		"project_reviewers",
		"project_members",
		"git_credentials",
		"runtimes",
		"agents",
		"workflow_template_nodes",
		"workflow_templates",
		"skills",
		"mcp_servers",
		"projects",
		"workspace_members",
		"invitations",
		"audit_logs",
		"auth_tokens",
		"sse_event_buffer",
		"memories",
		"community_workflows",
		"workspaces",
		"members",
	}

	for _, table := range tables {
		if _, err := db.Exec(fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table)); err != nil {
			return fmt.Errorf("truncate %s: %w", table, err)
		}
	}
	return nil
}

// DeleteWorkspace 根据 ID 删除工作空间。所有相关数据由 PostgreSQL 外键约束级联删除
// （项目、智能体、任务、记忆等）。
// 此操作绕过业务逻辑（如 is_default 检查）以进行测试清理。
func DeleteWorkspace(db *sql.DB, workspaceID string) error {
	_, err := db.Exec("DELETE FROM workspaces WHERE id = $1", workspaceID)
	return err
}

// DeleteMember 根据 ID 删除成员。相关的 workspace_members 和 project_members
// 被级联删除。该成员拥有的认证令牌也会被删除。
func DeleteMember(db *sql.DB, memberID string) error {
	// 认证令牌引用 owner_id 但没有外键约束，需要手动清理
	_, _ = db.Exec("DELETE FROM auth_tokens WHERE owner_type = 'member' AND owner_id = $1", memberID)
	_, err := db.Exec("DELETE FROM members WHERE id = $1", memberID)
	return err
}
