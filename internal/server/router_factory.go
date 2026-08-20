// router_factory.go 提供路由器构造函数，供生产服务器和测试共用。
// 通过 RouterDeps 注入依赖，确保测试路由器与生产路由器使用相同的路由配置，
// 防止路由漂移。
package server

import (
	"database/sql"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/clock"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
)

// RouterDeps 持有路由器构造所需的依赖。
// 生产环境和测试环境共用此结构体，确保路由配置一致。
type RouterDeps struct {
	// Config 是服务器配置。
	Config Config
	// DB 是 PostgreSQL 数据库连接。
	DB *sql.DB
	// Redis 是 Redis 客户端，为 nil 时速率限制使用内存降级方案。
	Redis *redis.Client
	// Hub 是 SSE 事件中心。
	Hub *ws.Hub
	// Gateway 是 WebSocket 日志网关。
	Gateway *ws.Gateway

	// Clock 是可选的自定义时钟，用于测试时间相关逻辑。
	// 为 nil 时使用默认系统时钟。
	Clock clock.Clock
}

// NewRouter 使用生产路由配置创建 chi.Router。
// 生产服务器和测试都应使用此函数，确保路由不会漂移。
//
// 参数：
//   - deps: 路由器依赖
//
// 返回：
//   - chi.Router: 配置好的路由器
func NewRouter(deps RouterDeps) chi.Router {
	cfg := deps.Config
	s := &Server{
		Config:  &cfg,
		DB:      deps.DB,
		Redis:   deps.Redis,
		Hub:     deps.Hub,
		Gateway: deps.Gateway,
	}
	var svc *service.Service
	if deps.Clock != nil {
		svc = service.NewWithClock(s.DB, s.Hub, s.Redis, deps.Clock)
	} else {
		svc = service.New(s.DB, s.Hub, s.Redis)
	}
	return s.buildRouter(svc)
}
