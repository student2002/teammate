// server.go 提供 Teammate HTTP 服务器的初始化和生命周期管理。
// 服务器整合了所有组件：数据库连接、Redis 缓存、SSE Hub、WebSocket Gateway 和定时任务调度器。
// 支持优雅关闭：监听 SIGINT/SIGTERM 信号，依次关闭各组件后退出。
//
// 路由配置在 routes.go 和 routes_*.go 中，通过 routeRegistrar 注册。
package server

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/db"
	"github.com/teammate/server/internal/scheduler"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/store"
)

// Server 是 Teammate HTTP 服务器的核心结构体，管理所有服务器组件的生命周期。
type Server struct {
	// Config 是服务器配置。
	Config *Config
	// DB 是 PostgreSQL 数据库连接。
	DB *sql.DB
	// Redis 是 Redis 客户端，用于缓存、Pub/Sub、分布式锁和速率限制。
	Redis *redis.Client
	// Router 是 Chi 路由器，管理所有 HTTP 路由和中间件。
	Router chi.Router
	// Hub 是 SSE 事件中心，管理 Agent 与 Server 之间的实时事件推送。
	Hub *ws.Hub
	// Gateway 是 WebSocket 日志网关，管理任务执行日志的实时推送。
	Gateway *ws.Gateway
	// Scheduler 是定时任务调度器，负责节点超时检测等周期性任务。
	Scheduler *scheduler.Scheduler
	// http 是底层的 HTTP 服务器实例。
	http *http.Server
}

// New 创建一个新的 Server 实例，初始化数据库、Redis、SSE Hub、WebSocket Gateway 和调度器。
//
// 安全检查：
//   - 生产环境禁止使用默认 JWT 密钥（dev-secret-change-me）
//   - 生产环境禁止使用通配符 CORS 源（*）
//   - 加密密钥必须已初始化（通过 TEAMMATE_ENCRYPTION_KEY_BASE64 环境变量）
//
// 参数：
//   - cfg: 服务器配置
//
// 返回：
//   - *Server: 初始化后的服务器实例
//   - error: 初始化失败时返回错误
func New(cfg Config) (*Server, error) {
	// 安全检查：生产环境禁止使用默认 JWT 密钥
	if cfg.JWTSecret == "dev-secret-change-me" && os.Getenv("TEAMMATE_DEV") != "true" {
		return nil, fmt.Errorf("TEAMS_JWT_SECRET must be changed from the default value for security")
	}

	if os.Getenv("TEAMMATE_DEV") == "true" {
		slog.Warn("running in development mode, security features relaxed")
	}

	// 安全检查：生产环境必须显式配置 CORS 源
	if cfg.AllowedOrigins == "*" && os.Getenv("TEAMMATE_DEV") != "true" {
		return nil, fmt.Errorf("TEAMS_ALLOWED_ORIGINS must be configured in production (no wildcard *)")
	}

	// 从环境变量初始化加密密钥
	if err := crypto.InitEncryptionKey(); err != nil {
		return nil, fmt.Errorf("encryption key initialization failed: %w", err)
	}

	pgDB, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr: redisAddr(cfg.RedisURL),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		pgDB.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}

	s := &Server{
		Config: &cfg,
		DB:     pgDB,
		Redis:  rdb,
	}

	s.Hub = ws.NewHub(rdb)
	s.Gateway = ws.NewGateway(rdb)

	st := store.New(s.DB)
	s.Scheduler = scheduler.NewScheduler(st, s.Hub, s.Redis)

	s.Router = s.setupRoutes()

	s.http = &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      s.Router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	return s, nil
}

// Start 启动 HTTP 服务器，同时启动 SSE Hub、WebSocket Gateway 和调度器。
// 监听 SIGINT/SIGTERM 信号实现优雅关闭。
//
// 启动的后台组件：
//   - SSE Hub：监听 Redis Pub/Sub，分发事件给本地订阅者
//   - WebSocket Gateway：监听 Redis Pub/Sub，分发日志给本地订阅者
//   - Scheduler：执行节点超时检测等定时任务
//
// 返回：
//   - error: 服务器启动失败或接收到终止信号时返回错误
func (s *Server) Start() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动 SSE Hub（Redis Pub/Sub 监听器）
	go s.Hub.Start(ctx)

	// 启动 WebSocket Gateway（Redis Pub/Sub 日志监听器）
	go s.Gateway.Start(ctx)

	// 启动调度器
	go s.Scheduler.Start(ctx)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		slog.Info("server starting", "addr", s.http.Addr)
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		cancel()
		return err
	case sig := <-quit:
		slog.Info("shutting down", "signal", sig)
	}

	cancel()
	return s.Stop()
}

// Stop 优雅关闭 HTTP 服务器，依次关闭 SSE Hub、WebSocket Gateway、Redis 和数据库连接。
// 使用 10 秒超时确保关闭过程不会无限阻塞。
//
// 关闭顺序：
//  1. HTTP 服务器（等待活跃连接完成）
//  2. SSE Hub（关闭所有客户端通道）
//  3. WebSocket Gateway（关闭所有客户端通道）
//  4. Redis 连接
//  5. PostgreSQL 数据库连接
//
// 返回：
//   - error: 关闭过程中发生错误时返回
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := s.http.Shutdown(ctx); err != nil {
		slog.Error("http shutdown error", "err", err)
	}
	s.Hub.Close()
	s.Gateway.Close()
	if err := s.Redis.Close(); err != nil {
		slog.Error("redis close error", "err", err)
	}
	if s.DB != nil {
		s.DB.Close()
	}
	return nil
}
