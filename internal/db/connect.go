// connect.go 提供 PostgreSQL 数据库连接池管理和数据库迁移功能。
//
// 本文件包含：
//   - Connect：使用 pgx 驱动通过 database/sql 打开 PostgreSQL 连接池
//   - Migrate：使用 golang-migrate 从指定路径执行 SQL 迁移文件
//   - ensurePgxScheme：内部辅助函数，将 DSN 的 scheme 转换为 pgx 驱动兼容格式
//
// 连接池默认配置：
//   - 最大打开连接数：25
//   - 最大空闲连接数：5
//   - 连接最大生命周期：5 分钟
//
// 设计决策：
//   - 使用 pgx 的 stdlib 包装器，兼容 database/sql 标准接口
//   - 连接前自动 ping 验证连通性，确保启动时尽早发现数据库问题
//   - DSN 自动转换 postgres:// 前缀为 pgx://，适配 golang-migrate 要求
package db

import (
	"database/sql"
	"fmt"
	"time"

	// 通过 stdlib 方式注册 pgx 驱动
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Connect 使用 pgx 驱动通过 database/sql 打开 PostgreSQL 连接池。
// 连接前会 ping 数据库以验证连通性。
//
// 连接池配置：
//   - 最大打开连接数: 25
//   - 最大空闲连接数: 5
//   - 连接最大生命周期: 5 分钟
//
// 参数：
//   - databaseURL: PostgreSQL 连接字符串，格式如 "postgres://user:pass@host:port/dbname"
//
// 返回：
//   - *sql.DB: 已验证连通性的数据库连接池
//   - error: 连接失败时返回错误信息
func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

// Migrate 使用 golang-migrate 从指定文件系统路径执行数据库迁移。
// migrationsPath 应指向包含 .sql 文件的目录。
// 自动将 "postgres://" 或 "postgresql://" DSN 转换为 pgx 驱动所需的 "pgx://" scheme。
//
// 参数：
//   - databaseURL: PostgreSQL 连接字符串
//   - migrationsPath: SQL 迁移文件目录路径
//
// 返回：
//   - error: 迁移失败时返回错误信息
func Migrate(databaseURL string, migrationsPath string) error {
	// golang-migrate 要求数据库 URL scheme 与驱动名匹配
	// 将 "postgres://" 转换为 "pgx://" 以适配 pgx 驱动
	dsn := ensurePgxScheme(databaseURL)

	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

// ensurePgxScheme 将 "postgres://" 或 "postgresql://" DSN 转换为
// golang-migrate pgx 数据库驱动所需的 "pgx://" scheme。
//
// 参数：
//   - dsn: 原始数据库连接字符串
//
// 返回：
//   - string: 转换后的连接字符串
func ensurePgxScheme(dsn string) string {
	if len(dsn) >= 11 && dsn[:11] == "postgres://" {
		return "pgx://" + dsn[11:]
	}
	if len(dsn) >= 13 && dsn[:13] == "postgresql://" {
		return "pgx://" + dsn[13:]
	}
	return dsn
}
