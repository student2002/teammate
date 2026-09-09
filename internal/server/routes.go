// routes.go defines the route registrar and the shared dependencies required for route registration.
// setupRoutes delegates route registration to the methods in each routes_*.go file.
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

// routeRegistrar holds the shared dependencies required for route registration.
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

// newRouteRegistrar creates the route registrar, initializing all shared permission-check functions.
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
	// The checkers are injected into handlers and middleware via the registrar, no longer set as global variables

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

// setupRoutes configures all HTTP routes and middleware (using the default service).
func (s *Server) setupRoutes() chi.Router {
	svc := service.New(s.DB, s.Hub, s.Redis)
	return s.buildRouter(svc)
}

// buildRouter configures all HTTP routes and middleware with the given service.
// This method is the core implementation of route configuration, shared by setupRoutes and NewRouter.
func (s *Server) buildRouter(svc *service.Service) chi.Router {
	r := chi.NewRouter()

	// Global middleware
	r.Use(svcmw.RequestID())
	r.Use(svcmw.Recovery())
	r.Use(svcmw.Logger())
	r.Use(svcmw.CORS(s.Config.AllowedOrigins))
	r.Use(svcmw.BodyLimitMiddleware())

	reg := newRouteRegistrar(s, svc)

	// Public routes (health check, SSE, WebSocket)
	reg.registerPublicRoutes(r)

	// API routes (with Timeout middleware)
	r.Route("/api", func(r chi.Router) {
		r.Use(chimw.Timeout(60 * time.Second))

		// Authentication routes (no auth required)
		reg.registerAuthRoutes(r)

		// Authenticated route group
		r.Group(func(r chi.Router) {
			r.Use(svcmw.AuthMiddleware(s.Config.JWTSecret, reg.apiKeyAuth, s.Redis))
			r.Use(svcmw.RateLimitMiddleware(s.Redis, svcmw.APIRateLimit, svcmw.UserKeyFunc))

			// Authenticated auth routes
			reg.registerAuthProtectedRoutes(r)

			// Search, workspaces, projects, tasks, Agents, memories, templates, community, etc.
			reg.registerWorkspaceRoutes(r)
			reg.registerProjectRoutes(r)
			reg.registerTaskRoutes(r)
			reg.registerMiscRoutes(r)
		})
	})

	return r
}

// redisAddr extracts the host address from a Redis URL.
func redisAddr(rawURL string) string {
	opts, err := redis.ParseURL(rawURL)
	if err != nil {
		return rawURL
	}
	return opts.Addr
}
