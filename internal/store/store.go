// store.go 提供数据访问层（DAL）的核心定义和初始化方法。
//
// Store 是数据访问的统一入口，封装了 sqlc 生成的查询方法和自定义的事务性操作。
// 所有业务模块通过 Store 访问数据库，不直接操作底层连接。
//
// 设计原则：
//   - 简单 CRUD 操作委托给 sqlc Queries
//   - 需要事务的操作在 Store 方法内实现（如 ApproveNodeInTx、CreateTask）
//   - 所有方法接收 context.Context 参数，支持超时和取消
//   - 错误使用 fmt.Errorf 包装，保留原始错误链
package store

import (
	"database/sql"

	"github.com/teammate/server/internal/clock"
	db "github.com/teammate/server/internal/db/generated"
)

// Store 封装数据库连接和 sqlc 查询对象，提供统一的数据访问入口。
//
// 使用方式：
//
//	s := store.New(pgDB)
//	node, err := s.GetTaskNode(ctx, nodeID)
//
// 对于需要自定义时钟的测试场景，使用 NewWithClock 创建 Store。
type Store struct {
	// q 是 sqlc 自动生成的查询对象（私有，强制通过 Store 封装方法访问）。
	q *db.Queries

	// db 是底层数据库连接，用于 Store 内部自定义 SQL 查询和事务操作。
	db *sql.DB

	// Clock 提供时间抽象，用于测试时注入假时钟。
	Clock clock.Clock
}

// New 创建一个新的 Store 实例，使用系统时钟。
//
// 参数：
//   - pgDB: 已连接的 PostgreSQL 数据库连接
//
// 返回：
//   - 初始化完成的 Store 实例
func New(pgDB *sql.DB) *Store {
	return &Store{
		q:     db.New(pgDB),
		db:    pgDB,
		Clock: clock.RealClock{},
	}
}

// NewWithClock 创建一个使用自定义时钟的 Store 实例。
//
// 主要用于测试场景，允许控制时间流逝以测试超时、过期等时间相关逻辑。
//
// 参数：
//   - pgDB: 已连接的 PostgreSQL 数据库连接
//   - c: 自定义的时钟实现（如 FakeClock）
//
// 返回：
//   - 使用自定义时钟的 Store 实例
func NewWithClock(pgDB *sql.DB, c clock.Clock) *Store {
	return &Store{
		q:     db.New(pgDB),
		db:    pgDB,
		Clock: c,
	}
}
