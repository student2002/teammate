// routes.go 定义路由注册器和路由注册所需的共享依赖。
// setupRoutes 将路由注册委托给各 routes_*.go 文件中的方法。
package server

import (
	"context"
	"fmt"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/service"
)

// routeRegistrar 持有路由注册所需的共享依赖。
type routeRegistrar struct {
	server     *Server
	svc        *service.Service
	apiKeyAuth svcmw.APIKeyAuthenticator
	agentPerm  func(ctx context.Context, agentID uuid.UUID, permission string) (bool, error)
	wsChk      svcmw.WorkspaceAccessCheckerFunc
	projectChk func(ctx context.Context, userID uuid.UUID, userType string, projectID uuid.UUID) (svcmw.WorkspaceContext, error)
	taskChk    func(ctx context.Context, taskID int32) (interface{}, uuid.UUID, error)
	nodeChk    func(ctx context.Context, nodeID uuid.UUID) (interface{}, uuid.UUID, error)
}

// newRouteRegistrar 创建路由注册器，初始化所有共享的权限检查函数。
func newRouteRegistrar(s *Server, svc *service.Service) *routeRegistrar {
	agentPermSvc := service.NewAgentPermissionService(svc)
	authSvc := service.NewAuthService(svc, s.Config.JWTSecret)
	apiKeyAuthenticator := func(ctx context.Context, apiKey string) (svcmw.AuthClaims, error) {
		result, err := authSvc.AuthenticateAPIKey(ctx, apiKey)
		if err != nil {
			return svcmw.AuthClaims{}, err
		}
		return svcmw.AuthClaims{
			UserID:   result.UserID,
			UserType: result.UserType,
		}, nil
	}
	agentPerm := func(ctx context.Context, agentID uuid.UUID, permission string) (bool, error) {
		return agentPermSvc.HasPermission(ctx, agentID, permission)
	}

	projSvc := service.NewProjectService(svc)
	wsSvc := service.NewWorkspaceService(svc)
	agentSvc := service.NewAgentService(svc)
	wsAccessChecker := func(ctx context.Context, userID uuid.UUID, userType string, workspaceID uuid.UUID) (string, error) {
		switch userType {
		case "member":
			role, err := wsSvc.GetWorkspaceMemberRole(ctx, userID, workspaceID)
			if err != nil {
				return "", fmt.Errorf("workspace membership not found: %w", err)
			}
			return role, nil
		case "agent":
			agent, err := agentSvc.Get(ctx, userID)
			if err != nil {
				return "", fmt.Errorf("agent not found: %w", err)
			}
			if agent.WorkspaceID != workspaceID.String() {
				return "", fmt.Errorf("agent workspace mismatch")
			}
			return "agent", nil
		default:
			return "", fmt.Errorf("unknown user type")
		}
	}
	// checker 通过 registrar 注入到 handler 和中间件，不再设置全局变量

	projectAccessChecker := func(ctx context.Context, userID uuid.UUID, userType string, projectID uuid.UUID) (svcmw.WorkspaceContext, error) {
		project, err := projSvc.Get(ctx, projectID)
		if err != nil {
			return svcmw.WorkspaceContext{}, fmt.Errorf("project not found")
		}
		wsUUID, _ := uuid.Parse(project.WorkspaceID)
		role, err := wsAccessChecker(ctx, userID, userType, wsUUID)
		if err != nil {
			return svcmw.WorkspaceContext{}, err
		}
		if userType == "agent" {
			if err := projSvc.CheckAgentProjectAccess(ctx, userID, projectID); err != nil {
				return svcmw.WorkspaceContext{}, err
			}
		} else if err := projSvc.CheckMemberProjectAccess(ctx, userID, projectID, role); err != nil {
			return svcmw.WorkspaceContext{}, err
		}
		return svcmw.WorkspaceContext{WorkspaceID: wsUUID, Role: role}, nil
	}

	taskWorkspaceChecker := func(ctx context.Context, taskID int32) (interface{}, uuid.UUID, error) {
		taskSvc := service.NewTaskService(svc)
		task, err := taskSvc.Get(ctx, taskID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		taskProjectID, _ := uuid.Parse(task.ProjectID)
		project, err := projSvc.Get(ctx, taskProjectID)
		if err != nil {
			return nil, uuid.Nil, fmt.Errorf("task project not found: %w", err)
		}
		wsUUID, _ := uuid.Parse(project.WorkspaceID)
		return task, wsUUID, nil
	}

	nodeWorkspaceChecker := func(ctx context.Context, nodeID uuid.UUID) (interface{}, uuid.UUID, error) {
		nodeSvc := service.NewNodeService(svc)
		node, err := nodeSvc.GetTaskNode(ctx, nodeID)
		if err != nil {
			return nil, uuid.Nil, err
		}
		taskSvc := service.NewTaskService(svc)
		task, err := taskSvc.Get(ctx, node.TaskID)
		if err != nil {
			return nil, uuid.Nil, fmt.Errorf("node task not found: %w", err)
		}
		taskProjectID, _ := uuid.Parse(task.ProjectID)
		project, err := projSvc.Get(ctx, taskProjectID)
		if err != nil {
			return nil, uuid.Nil, fmt.Errorf("node project not found: %w", err)
		}
		wsUUID, _ := uuid.Parse(project.WorkspaceID)
		return node, wsUUID, nil
	}

	return &routeRegistrar{
		server:     s,
		svc:        svc,
		apiKeyAuth: apiKeyAuthenticator,
		agentPerm:  agentPerm,
		wsChk:      wsAccessChecker,
		projectChk: projectAccessChecker,
		taskChk:    taskWorkspaceChecker,
		nodeChk:    nodeWorkspaceChecker,
	}
}

// setupRoutes 配置所有 HTTP 路由和中间件（使用默认服务）。
func (s *Server) setupRoutes() chi.Router {
	svc := service.New(s.DB, s.Hub, s.Redis)
	return s.buildRouter(svc)
}

// buildRouter 使用给定的 service 配置所有 HTTP 路由和中间件。
// 此方法是路由配置的核心实现，供 setupRoutes 和 NewRouter 共用。
func (s *Server) buildRouter(svc *service.Service) chi.Router {
	r := chi.NewRouter()

	// 全局中间件
	r.Use(svcmw.RequestID())
	r.Use(svcmw.Recovery())
	r.Use(svcmw.Logger())
	r.Use(svcmw.CORS(s.Config.AllowedOrigins))
	r.Use(svcmw.BodyLimitMiddleware())

	reg := newRouteRegistrar(s, svc)

	// 公开路由（健康检查、SSE、WebSocket）
	reg.registerPublicRoutes(r)

	// API 路由（带 Timeout 中间件）
	r.Route("/api", func(r chi.Router) {
		r.Use(chimw.Timeout(60 * time.Second))

		// 认证路由（无需认证）
		reg.registerAuthRoutes(r)

		// 需要认证的路由组
		r.Group(func(r chi.Router) {
			r.Use(svcmw.AuthMiddleware(s.Config.JWTSecret, reg.apiKeyAuth, s.Redis))
			r.Use(svcmw.RateLimitMiddleware(s.Redis, svcmw.APIRateLimit, svcmw.UserKeyFunc))

			// 需要认证的认证路由
			reg.registerAuthProtectedRoutes(r)

			// 搜索、工作区、项目、任务、Agent、记忆、模板、社区等
			reg.registerWorkspaceRoutes(r)
			reg.registerProjectRoutes(r)
			reg.registerTaskRoutes(r)
			reg.registerMiscRoutes(r)
		})
	})

	return r
}

// redisAddr 从 Redis URL 中提取主机地址。
func redisAddr(rawURL string) string {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return rawURL
	}
	return opts.Addr
}
