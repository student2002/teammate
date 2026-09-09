// auth.go provides HTTP request authentication and authorization middleware,
// supporting both JWT Bearer Token and API Key authentication.
// It includes workspace isolation checks, project-level access control, and agent permission verification.
//
// Security features:
//   - JWT jti revocation check (verifies whether the token has been revoked via Redis)
//   - API Key verified with bcrypt slow hashing; SHA-256 used only for efficient index lookups
//   - Session tokens (st_ prefix) are matched first, falling back to API tokens (tm_ prefix)
//   - Workspace isolation: users can only access resources within their workspace, preventing cross-workspace data leakage
package middleware

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/types"
)

// contextKey is the context key type used internally by this package.
// A custom type is used to avoid collisions with string keys from other packages.
type contextKey int

const (
	// authContextKey stores the authenticated identity (AuthClaims) in the request context.
	authContextKey contextKey = iota
	// workspaceContextKey stores the authorization context under the current resource's workspace in the request context.
	workspaceContextKey
	// taskContextKey stores the task object injected by the task access middleware in the request context.
	taskContextKey
	// nodeContextKey stores the node object injected by the node access middleware in the request context.
	nodeContextKey
)

// AuthClaims holds the authenticated identity extracted from a JWT or API Key.
// After the authentication middleware succeeds, this struct is injected into the request context
// for use by subsequent middleware and handlers.
type AuthClaims struct {
	// UserID is the unique identifier of the user (a human member or an AI agent).
	UserID uuid.UUID
	// UserType identifies the user type: "member" (human member) or "agent" (AI agent).
	UserType string // "member" or "agent"
	// WorkspaceID and Role can only be derived by the resource-scoped middleware based on database state.
	// The JWT/API key parsing phase must NOT populate these two fields.
	WorkspaceID uuid.UUID
	Role        string
}

// APIKeyAuthenticator validates an API key or session token and returns the authenticated identity.
// The server layer injects this function so the middleware does not depend on database access.
type APIKeyAuthenticator func(ctx context.Context, apiKey string) (AuthClaims, error)

// WorkspaceContext holds the real-time authorization information of the authenticated identity
// for the current resource's workspace. It can only be injected by the workspace/project/task/node
// access middleware based on database state, never from the JWT.
type WorkspaceContext struct {
	WorkspaceID uuid.UUID
	Role        string
}

// GetAuthFromContext retrieves the authenticated identity from the request context.
//
// Parameters:
//   - ctx: request context, into which the authentication middleware injects AuthClaims
//
// Returns:
//   - AuthClaims: the authenticated identity
//   - bool: whether valid identity information exists
func GetAuthFromContext(ctx context.Context) (AuthClaims, bool) {
	claims, ok := ctx.Value(authContextKey).(AuthClaims)
	return claims, ok
}

// GetWorkspaceFromContext retrieves the authorization context for the current resource's workspace from the request context.
func GetWorkspaceFromContext(ctx context.Context) (WorkspaceContext, bool) {
	ws, ok := ctx.Value(workspaceContextKey).(WorkspaceContext)
	return ws, ok
}

func withWorkspaceContext(ctx context.Context, ws WorkspaceContext) context.Context {
	ctx = context.WithValue(ctx, workspaceContextKey, ws)
	if claims, ok := GetAuthFromContext(ctx); ok {
		claims.WorkspaceID = ws.WorkspaceID
		claims.Role = ws.Role
		ctx = context.WithValue(ctx, authContextKey, claims)
	}
	return ctx
}

// AuthMiddleware returns a chi-compatible authentication middleware supporting
// both JWT Bearer Token and X-API-Key authentication.
// On success it stores the identity (AuthClaims) in the request context for subsequent middleware and handlers.
//
// Authentication flow:
//  1. Check the X-API-Key header first and authenticate with the API Key
//  2. If no API Key, fall back to the Bearer Token in the Authorization header
//  3. For member-type JWTs, additionally check whether the jti has been revoked in Redis (login/logout mechanism)
//  4. On authentication failure return 401 Unauthorized
//
// Security notes:
//   - When the Redis revocation check fails the request is allowed through, so a Redis outage does not disable all authentication
//   - API Key uses a dual mechanism: SHA-256 hash index + bcrypt slow-hash verification
//
// Parameters:
//   - jwtSecret: JWT signing secret, used to verify the Bearer Token signature
//   - authenticateAPIKey: API Key lookup and verification function
//   - rdb: Redis client for JWT jti revocation checks (may be nil, in which case revocation checks are skipped)
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func AuthMiddleware(jwtSecret string, authenticateAPIKey APIKeyAuthenticator, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var claims AuthClaims
			var jti string
			var err error

			// Try API Key authentication first (X-API-Key header)
			if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
				if authenticateAPIKey == nil {
					response.Unauthorized(w, "invalid or expired token")
					return
				}
				claims, err = authenticateAPIKey(r.Context(), apiKey)
			} else {
				// Fall back to JWT Bearer Token authentication
				claims, jti, err = authenticateJWT(r, jwtSecret)
			}

			if err != nil {
				response.Unauthorized(w, "invalid or expired token")
				return
			}

			// For member-type JWTs, check whether the jti exists in Redis (revocation check).
			// Security note: login-failure lockout, password change, user disable, and similar operations
			// write the jti to a Redis expiring key, thereby invalidating the current token.
			// When Redis fails the request is allowed through to preserve availability.
			if claims.UserType == "member" && jti != "" && rdb != nil {
				exists, err := rdb.Exists(r.Context(), "jwt:"+jti).Result()
				if err != nil {
					// Allow the request through when Redis fails, so a Redis issue does not block all authentication
					slog.Warn("redis JWT revocation check failed, allowing request", "jti", jti, "err", err)
				} else if exists == 0 {
					response.Unauthorized(w, "token revoked or invalidated")
					return
				}
			}

			ctx := context.WithValue(r.Context(), authContextKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireRole returns a middleware that checks whether the current user has one of the specified roles.
// This middleware applies only to human users (member type); agents must use RequireAgentPermission for permission checks.
//
// Role hierarchy (high to low): owner > admin > member > viewer.
// As long as the user's role is equal to or higher than the lowest required role in the allowed list, it passes.
//
// Failure handling:
//   - Not authenticated: returns 403 Forbidden
//   - Agent user: returns 403 (agents must use permission-based access control)
//   - Insufficient role: returns 403 Forbidden
//
// Parameters:
//   - allowedRoles: list of allowed roles, e.g. []string{"owner", "admin"}
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func RequireRole(allowedRoles ...string) func(http.Handler) http.Handler {
	minLevel := 999
	for _, r := range allowedRoles {
		if l := types.MemberRoleLevel(r); l > 0 && l < minLevel {
			minLevel = l
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			// Agents must use permission-based access control, not role checks
			if claims.UserType == "agent" {
				response.Forbidden(w, "agents must use permission-based access control")
				return
			}

			ws, ok := GetWorkspaceFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "workspace context required")
				return
			}

			userLevel := types.MemberRoleLevel(ws.Role)
			if userLevel == 0 || userLevel < minLevel {
				response.Forbidden(w, "insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AgentPermissionCheckerFunc is the signature of a function that checks agent permissions.
// It verifies, via a database query, whether the specified agent has a particular permission.
type AgentPermissionCheckerFunc func(ctx context.Context, agentID uuid.UUID, permission string) (bool, error)

// RequireAgentPermissionWithChecker returns a middleware that uses the provided checker function
// to verify whether the agent has the specified permission.
// For human users (member type) it passes through directly; permission checks for them are handled by the route-level RequireRole.
//
// Parameters:
//   - permission: the required agent permission identifier, e.g. "task:approve", "git:push"
//   - checker: permission-check function, used to query whether the agent has the specified permission
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func RequireAgentPermissionWithChecker(permission string, checker AgentPermissionCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			// Human users pass through directly; checked by route-level RequireRole
			if claims.UserType != "agent" {
				next.ServeHTTP(w, r)
				return
			}

			// Check the agent permission
			if checker == nil {
				response.InternalServerError(w, fmt.Errorf("permission checker not configured"))
				return
			}

			has, err := checker(r.Context(), claims.UserID, permission)
			if err != nil {
				response.InternalServerError(w, err)
				return
			}

			if !has {
				response.Forbidden(w, "insufficient permissions")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAccessWithChecker is the unified authorization middleware that handles both human role verification
// and agent permission verification in a single check.
//
// Authorization logic:
//   - Human users: check whether the role is in the allowed list according to the role hierarchy (owner > admin > member > viewer)
//   - Agents: verify via the checker whether the specified permission is held (fine-grained permission table)
//   - If agentPermission is an empty string, all agent access is denied (routes restricted to human users only)
//
// Security note: this middleware is the recommended unified authorization entry point,
// to avoid mixing RequireRole and RequireAgentPermission in routes.
//
// Parameters:
//   - allowedRoles: list of allowed roles for human users
//   - agentPermission: the permission identifier required for agents; empty denies all agents
//   - checker: agent permission-check function
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func RequireAccessWithChecker(allowedRoles []string, agentPermission string, checker AgentPermissionCheckerFunc) func(http.Handler) http.Handler {
	minLevel := 999
	for _, r := range allowedRoles {
		if l := types.MemberRoleLevel(r); l > 0 && l < minLevel {
			minLevel = l
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			if claims.UserType == "agent" {
				// Agent path: check permission
				if agentPermission == "" {
					response.Forbidden(w, "agents cannot access this resource")
					return
				}
				if checker == nil {
					response.InternalServerError(w, fmt.Errorf("permission checker not configured"))
					return
				}
				has, err := checker(r.Context(), claims.UserID, agentPermission)
				if err != nil {
					response.InternalServerError(w, err)
					return
				}
				if !has {
					response.Forbidden(w, "insufficient permissions")
					return
				}
			} else {
				// Human user path: check the real-time role hierarchy within the current workspace
				ws, ok := GetWorkspaceFromContext(r.Context())
				if !ok {
					response.Forbidden(w, "workspace context required")
					return
				}
				userLevel := types.MemberRoleLevel(ws.Role)
				if userLevel == 0 || userLevel < minLevel {
					response.Forbidden(w, "insufficient permissions")
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// authenticateJWT extracts and validates the Bearer Token from the Authorization header.
// It parses the JWT signature, expiration, user identity and other claims,
// and extracts the jti for Redis revocation checks.
//
// Verification flow:
//  1. Check whether the Authorization header has the format "Bearer <token>"
//  2. Verify the JWT signature using the HMAC-SHA256 algorithm
//  3. Extract identity claims such as user_id, user_type, and jti
//  4. Verify that user_type is a legal value ("member" or "agent")
//
// Parameters:
//   - r: HTTP request object
//   - secret: JWT signing secret
//
// Returns:
//   - AuthClaims: the parsed identity
//   - string: the JWT jti (JWT ID), used for revocation checks
//   - error: returned when parsing or verification fails
func authenticateJWT(r *http.Request, secret string) (AuthClaims, string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return AuthClaims{}, "", errors.New("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return AuthClaims{}, "", errors.New("invalid authorization header format")
	}

	tokenStr := parts[1]
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return []byte(secret), nil
	})
	if err != nil {
		return AuthClaims{}, "", fmt.Errorf("invalid token: %w", err)
	}

	mapClaims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return AuthClaims{}, "", errors.New("invalid token claims")
	}

	userIDStr, ok := mapClaims["user_id"].(string)
	if !ok {
		return AuthClaims{}, "", errors.New("missing user_id in token")
	}
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return AuthClaims{}, "", fmt.Errorf("invalid user_id: %w", err)
	}

	userType, ok := mapClaims["user_type"].(string)
	if !ok {
		return AuthClaims{}, "", errors.New("missing user_type in token")
	}
	if userType != "member" && userType != "agent" {
		return AuthClaims{}, "", errors.New("invalid user_type in token")
	}

	jti, ok := mapClaims["jti"].(string)
	if !ok {
		return AuthClaims{}, "", errors.New("missing jti in token")
	}

	return AuthClaims{
		UserID:   userID,
		UserType: userType,
	}, jti, nil
}

// WorkspaceAccessCheckerFunc resolves the authenticated identity for a specific workspace.
// It must query persisted server-side state; JWT claims must never be used as the basis for workspace authorization.
type WorkspaceAccessCheckerFunc func(ctx context.Context, userID uuid.UUID, userType string, workspaceID uuid.UUID) (string, error)

// WorkspaceAuthMiddlewareWithChecker ensures the authenticated user belongs to the workspace
// specified by the URL parameter {workspaceId}. The checker is injected via a parameter,
// so all workspace authorization comes from persisted server-side state.
func WorkspaceAuthMiddlewareWithChecker(checker WorkspaceAccessCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			wsIDStr := chi.URLParam(r, "workspaceId")
			if wsIDStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			wsID, err := uuid.Parse(wsIDStr)
			if err != nil {
				response.BadRequest(w, "invalid workspace id")
				return
			}

			if checker == nil {
				response.Forbidden(w, "workspace access checker not configured")
				return
			}
			role, err := checker(r.Context(), claims.UserID, claims.UserType, wsID)
			if err != nil {
				response.Forbidden(w, "user does not belong to this workspace")
				return
			}

			ctx := withWorkspaceContext(r.Context(), WorkspaceContext{WorkspaceID: wsID, Role: role})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ProjectAccessCheckerFunc verifies project-level access and returns the resolved workspace context.
type ProjectAccessCheckerFunc func(ctx context.Context, userID uuid.UUID, userType string, projectID uuid.UUID) (WorkspaceContext, error)

// ProjectMemberMiddlewareWithChecker checks whether the authenticated user can access the project
// specified by the URL parameter {projectId}.
func ProjectMemberMiddlewareWithChecker(checker ProjectAccessCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			projectIDStr := chi.URLParam(r, "projectId")
			if projectIDStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				response.BadRequest(w, "invalid project id")
				return
			}

			if checker == nil {
				response.InternalServerError(w, fmt.Errorf("project access checker not configured"))
				return
			}

			ws, err := checker(r.Context(), claims.UserID, claims.UserType, projectID)
			if err != nil {
				response.Forbidden(w, "access denied")
				return
			}

			ctx := withWorkspaceContext(r.Context(), ws)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// --- Task access middleware ---

// TaskWorkspaceCheckerFunc verifies that a task exists and returns the task object and its workspace.
type TaskWorkspaceCheckerFunc func(ctx context.Context, taskID int32) (interface{}, uuid.UUID, error)

// GetTaskFromContext retrieves the task object from the request context (injected by TaskAccessMiddleware).
//
// Returns:
//   - interface{}: the task object
//   - bool: whether a valid task object exists
func GetTaskFromContext(ctx context.Context) (interface{}, bool) {
	task := ctx.Value(taskContextKey)
	return task, task != nil
}

// TaskAccessMiddlewareWithChecker verifies that the task in URL parameter {taskId} belongs to the authenticated user's workspace.
// On success it injects the task into the context, retrievable via GetTaskFromContext.
// This middleware prevents users from accessing tasks of other workspaces by tampering with the URL.
//
// Parameters:
//   - checker: task workspace verification function, verifies task ownership and returns the task object
//   - workspaceChecker: workspace access check function, verifies the current identity can access the task's workspace
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func TaskAccessMiddlewareWithChecker(checker TaskWorkspaceCheckerFunc, workspaceChecker WorkspaceAccessCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			taskIDStr := chi.URLParam(r, "taskId")
			if taskIDStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			var taskID int32
			if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
				response.BadRequest(w, "invalid task id")
				return
			}

			if checker == nil {
				response.InternalServerError(w, fmt.Errorf("task access checker not configured"))
				return
			}

			task, workspaceID, err := checker(r.Context(), taskID)
			if err != nil {
				response.NotFound(w, "task not found")
				return
			}

			if workspaceChecker == nil {
				response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
				return
			}
			role, err := workspaceChecker(r.Context(), claims.UserID, claims.UserType, workspaceID)
			if err != nil {
				response.Forbidden(w, "access denied")
				return
			}

			ctx := withWorkspaceContext(r.Context(), WorkspaceContext{WorkspaceID: workspaceID, Role: role})
			ctx = context.WithValue(ctx, taskContextKey, task)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// --- Node access middleware ---

// NodeWorkspaceCheckerFunc verifies that a node exists and returns the node object and its workspace.
type NodeWorkspaceCheckerFunc func(ctx context.Context, nodeID uuid.UUID) (interface{}, uuid.UUID, error)

// GetNodeFromContext retrieves the node object from the request context (injected by NodeAccessMiddleware).
//
// Returns:
//   - interface{}: the node object
//   - bool: whether a valid node object exists
func GetNodeFromContext(ctx context.Context) (interface{}, bool) {
	node := ctx.Value(nodeContextKey)
	return node, node != nil
}

// NodeAccessMiddlewareWithChecker verifies that the node in URL parameter {id} belongs to the authenticated user's workspace.
// On success it injects the node into the context, retrievable via GetNodeFromContext.
// This middleware prevents users from accessing task nodes of other workspaces by tampering with the URL.
//
// Parameters:
//   - checker: node workspace verification function, verifies node ownership and returns the node object
//   - workspaceChecker: workspace access check function, verifies the current identity can access the node's workspace
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func NodeAccessMiddlewareWithChecker(checker NodeWorkspaceCheckerFunc, workspaceChecker WorkspaceAccessCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			nodeIDStr := chi.URLParam(r, "id")
			if nodeIDStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			nodeID, err := uuid.Parse(nodeIDStr)
			if err != nil {
				// The parent test route may use {id} as a task ID before the node subroutes match.
				// The node handler will still validate a malformed node ID on the concrete node route.
				next.ServeHTTP(w, r)
				return
			}

			if checker == nil {
				response.InternalServerError(w, fmt.Errorf("node access checker not configured"))
				return
			}

			node, workspaceID, err := checker(r.Context(), nodeID)
			if err != nil {
				response.NotFound(w, "node not found")
				return
			}

			if workspaceChecker == nil {
				response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
				return
			}
			role, err := workspaceChecker(r.Context(), claims.UserID, claims.UserType, workspaceID)
			if err != nil {
				response.Forbidden(w, "access denied")
				return
			}

			ctx := withWorkspaceContext(r.Context(), WorkspaceContext{WorkspaceID: workspaceID, Role: role})
			ctx = context.WithValue(ctx, nodeContextKey, node)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// --- Project role middleware ---

// ProjectRoleCheckerFunc checks whether a member holds the specified project-level role.
// Project role hierarchy (high to low): lead > developer > reviewer.
type ProjectRoleCheckerFunc func(ctx context.Context, userID uuid.UUID, userType string, workspaceRole string, projectID uuid.UUID, requiredRole string) error

// RequireProjectRoleWithChecker returns a middleware that checks whether the authenticated user has the specified project-level role.
// Workspace owner/admin always bypass the project role check (they have full workspace-level permissions).
// Agents are denied (project roles are restricted to human users).
//
// Parameters:
//   - requiredRole: the required project role, e.g. "lead", "developer", "reviewer"
//   - checker: project role check function
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func RequireProjectRoleWithChecker(requiredRole string, checker ProjectRoleCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			projectIDStr := chi.URLParam(r, "projectId")
			if projectIDStr == "" {
				next.ServeHTTP(w, r)
				return
			}

			projectID, err := uuid.Parse(projectIDStr)
			if err != nil {
				response.BadRequest(w, "invalid project id")
				return
			}

			if checker == nil {
				response.InternalServerError(w, fmt.Errorf("project role checker not configured"))
				return
			}

			ws, ok := GetWorkspaceFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "workspace context required")
				return
			}

			if err := checker(r.Context(), claims.UserID, claims.UserType, ws.Role, projectID, requiredRole); err != nil {
				response.Forbidden(w, "insufficient project role")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
