// auth.go 提供 HTTP 请求的身份认证与授权中间件，支持 JWT Bearer Token 和 API Key 两种认证方式。
// 包含工作区隔离检查、项目级访问控制、Agent 权限验证等功能。
// 安全特性：
//   - JWT jti 吊销检查（通过 Redis 验证令牌是否已失效）
//   - API Key 使用 bcrypt 慢哈希验证，SHA-256 仅用于高效索引查询
//   - 会话令牌（st_ 前缀）优先匹配，降级查找 API 令牌（tm_ 前缀）
//   - 工作区隔离：用户只能访问所属工作区的资源，防止跨工作区数据泄露
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

// contextKey 是本包内部使用的上下文键类型，采用自定义类型避免与其他包的字符串键冲突。
type contextKey int

const (
	// authContextKey 用于在请求上下文中存储已认证的身份信息（AuthClaims）。
	authContextKey contextKey = iota
	// workspaceContextKey 用于在请求上下文中存储当前资源所属工作区下的授权上下文。
	workspaceContextKey
	// taskContextKey 用于在请求上下文中存储任务访问中间件注入的任务对象。
	taskContextKey
	// nodeContextKey 用于在请求上下文中存储节点访问中间件注入的节点对象。
	nodeContextKey
)

// AuthClaims 保存从 JWT 或 API Key 中提取的已认证身份信息。
// 该结构体在认证中间件成功后被注入到请求上下文中，供后续中间件和 handler 使用。
type AuthClaims struct {
	// UserID 是用户（人类成员或 AI 代理）的唯一标识符。
	UserID uuid.UUID
	// UserType 标识用户类型："member"（人类成员）或 "agent"（AI 代理）。
	UserType string // "member" 或 "agent"
	// WorkspaceID 和 Role 只能由资源作用域中间件基于数据库状态派生。
	// JWT/API key 解析阶段不得填充这两个字段。
	WorkspaceID uuid.UUID
	Role        string
}

// APIKeyAuthenticator 验证 API key 或 session token，并返回已认证的身份。
// server 层注入该函数，使中间件不依赖数据库访问。
type APIKeyAuthenticator func(ctx context.Context, apiKey string) (AuthClaims, error)

// WorkspaceContext 保存已认证身份对当前资源工作区的实时授权信息。
// 它只能由工作区/项目/任务/节点访问中间件根据数据库状态注入，不能来自 JWT。
type WorkspaceContext struct {
	WorkspaceID uuid.UUID
	Role        string
}

// GetAuthFromContext 从请求上下文中获取已认证的身份信息。
//
// 参数：
//   - ctx: 请求上下文，由认证中间件注入 AuthClaims
//
// 返回：
//   - AuthClaims: 已认证的身份信息
//   - bool: 是否存在有效的身份信息
func GetAuthFromContext(ctx context.Context) (AuthClaims, bool) {
	claims, ok := ctx.Value(authContextKey).(AuthClaims)
	return claims, ok
}

// GetWorkspaceFromContext 从请求上下文中获取当前资源工作区的授权上下文。
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

// AuthMiddleware 返回一个 chi 兼容的认证中间件，支持 JWT Bearer Token 和 X-API-Key 两种认证方式。
// 认证成功后将身份信息（AuthClaims）存入请求上下文，供后续中间件和 handler 使用。
//
// 认证流程：
//  1. 优先检查 X-API-Key 头，使用 API Key 认证
//  2. 若无 API Key，降级检查 Authorization 头中的 Bearer Token
//  3. 对于 member 类型的 JWT，额外检查 Redis 中的 jti 是否已吊销（登录/登出机制）
//  4. 认证失败返回 401 Unauthorized
//
// 安全说明：
//   - Redis 吊销检查失败时放行请求，避免 Redis 故障导致所有认证不可用
//   - API Key 使用 SHA-256 哈希索引 + bcrypt 慢哈希验证双重机制
//
// 参数：
//   - jwtSecret: JWT 签名密钥，用于验证 Bearer Token 的签名
//   - db: 数据库连接，用于 API Key 查找和验证
//   - rdb: Redis 客户端，用于 JWT jti 吊销检查（可为 nil，此时跳过吊销检查）
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
func AuthMiddleware(jwtSecret string, authenticateAPIKey APIKeyAuthenticator, rdb *redis.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var claims AuthClaims
			var jti string
			var err error

			// 优先尝试 API Key 认证（X-API-Key 头）
			if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
				if authenticateAPIKey == nil {
					response.Unauthorized(w, "invalid or expired token")
					return
				}
				claims, err = authenticateAPIKey(r.Context(), apiKey)
			} else {
				// 降级到 JWT Bearer Token 认证
				claims, jti, err = authenticateJWT(r, jwtSecret)
			}

			if err != nil {
				response.Unauthorized(w, "invalid or expired token")
				return
			}

			// 对于 member 类型的 JWT，检查 jti 是否在 Redis 中存在（吊销检查）
			// 安全说明：登录失败锁定、密码修改、用户禁用等操作会将 jti 写入 Redis 过期键，
			// 从而使当前令牌失效。Redis 故障时放行请求以保证可用性。
			if claims.UserType == "member" && jti != "" && rdb != nil {
				exists, err := rdb.Exists(r.Context(), "jwt:"+jti).Result()
				if err != nil {
					// Redis 故障时放行请求，避免 Redis 问题阻塞所有认证
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

// RequireRole 返回一个中间件，检查当前用户是否拥有指定的角色之一。
// 该中间件仅适用于人类用户（member 类型），Agent 必须使用 RequireAgentPermission 进行权限检查。
//
// 角色层级（从高到低）：owner > admin > member > viewer
// 只要用户角色等于或高于允许列表中的最低角色要求，即视为通过。
//
// 失败处理：
//   - 未认证：返回 403 Forbidden
//   - Agent 用户：返回 403（Agent 必须使用基于权限的访问控制）
//   - 角色不足：返回 403 Forbidden
//
// 参数：
//   - allowedRoles: 允许访问的角色列表，如 []string{"owner", "admin"}
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
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

			// Agent 必须使用基于权限的访问控制，不能使用角色检查
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

// AgentPermissionCheckerFunc 是检查 Agent 权限的函数签名。
// 该函数通过数据库查询验证指定 Agent 是否拥有特定权限。
type AgentPermissionCheckerFunc func(ctx context.Context, agentID uuid.UUID, permission string) (bool, error)

// RequireAgentPermissionWithChecker 返回一个中间件，使用提供的检查器函数验证 Agent 是否拥有指定权限。
// 对于人类用户（member 类型），该中间件直接放行，由路由级别的 RequireRole 负责权限检查。
//
// 参数：
//   - permission: 所需的 Agent 权限标识符，如 "task:approve"、"git:push"
//   - checker: 权限检查函数，用于查询 Agent 是否拥有指定权限
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
func RequireAgentPermissionWithChecker(permission string, checker AgentPermissionCheckerFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := GetAuthFromContext(r.Context())
			if !ok {
				response.Forbidden(w, "authentication required")
				return
			}

			// 人类用户直接放行，由路由级别的 RequireRole 检查
			if claims.UserType != "agent" {
				next.ServeHTTP(w, r)
				return
			}

			// 检查 Agent 权限
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

// RequireAccessWithChecker 是统一的授权中间件，在一次检查中同时处理人类角色验证和 Agent 权限验证。
//
// 授权逻辑：
//   - 人类用户：根据角色层级检查是否在允许的角色列表中（owner > admin > member > viewer）
//   - Agent：通过检查器验证是否拥有指定权限（细粒度权限表）
//   - 如果 agentPermission 为空字符串，则拒绝所有 Agent 访问（仅限人类用户的路由）
//
// 安全说明：该中间件是推荐的统一授权入口，避免在路由中混用 RequireRole 和 RequireAgentPermission。
//
// 参数：
//   - allowedRoles: 人类用户允许的角色列表
//   - agentPermission: Agent 所需的权限标识符，为空则拒绝所有 Agent
//   - checker: Agent 权限检查函数
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
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
				// Agent 路径：检查权限
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
				// 人类用户路径：检查当前工作区内的实时角色层级
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

// authenticateJWT 从 Authorization 头中提取并验证 Bearer Token。
// 解析 JWT 签名、过期时间、用户身份等声明，并提取 jti 用于 Redis 吊销检查。
//
// 验证流程：
//  1. 检查 Authorization 头格式是否为 "Bearer <token>"
//  2. 使用 HMAC-SHA256 算法验证 JWT 签名
//  3. 提取 user_id、user_type、jti 等身份声明
//  4. 验证 user_type 是否为合法值（"member" 或 "agent"）
//
// 参数：
//   - r: HTTP 请求对象
//   - secret: JWT 签名密钥
//
// 返回：
//   - AuthClaims: 解析后的身份信息
//   - string: JWT 的 jti（JWT ID），用于吊销检查
//   - error: 解析或验证失败时返回错误
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

// WorkspaceAccessCheckerFunc 根据具体工作区解析已认证身份。
// 它必须查询持久化的服务端状态；JWT 声明绝不能作为工作区授权的依据。
type WorkspaceAccessCheckerFunc func(ctx context.Context, userID uuid.UUID, userType string, workspaceID uuid.UUID) (string, error)

// WorkspaceAuthMiddlewareWithChecker 确保已认证用户属于 URL 参数 {workspaceId} 指定的工作区。
// 通过参数注入 checker，所有工作区授权均来自持久化服务端状态。
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

// ProjectAccessCheckerFunc 验证项目级访问权限并返回解析后的工作区上下文。
type ProjectAccessCheckerFunc func(ctx context.Context, userID uuid.UUID, userType string, projectID uuid.UUID) (WorkspaceContext, error)

// ProjectMemberMiddlewareWithChecker 检查已认证用户是否可以访问 URL 参数 {projectId} 指定的项目。
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

// --- 任务访问中间件 ---

// TaskWorkspaceCheckerFunc 验证任务存在并返回任务对象及其所属工作区。
type TaskWorkspaceCheckerFunc func(ctx context.Context, taskID int32) (interface{}, uuid.UUID, error)

// GetTaskFromContext 从请求上下文中获取任务对象（由 TaskAccessMiddleware 注入）。
//
// 返回：
//   - interface{}: 任务对象
//   - bool: 是否存在有效的任务对象
func GetTaskFromContext(ctx context.Context) (interface{}, bool) {
	task := ctx.Value(taskContextKey)
	return task, task != nil
}

// TaskAccessMiddlewareWithChecker 验证 URL 参数 {taskId} 中的任务是否属于已认证用户的工作区。
// 验证通过后将任务注入上下文，可通过 GetTaskFromContext 获取。
// 该中间件防止用户通过 URL 篡改访问其他工作区的任务。
//
// 参数：
//   - checker: 任务工作区验证函数，验证任务归属并返回任务对象
//   - workspaceChecker: 工作区访问检查函数，验证当前身份是否可访问任务所属工作区
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
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

// --- 节点访问中间件 ---

// NodeWorkspaceCheckerFunc 验证节点存在并返回节点对象及其所属工作区。
type NodeWorkspaceCheckerFunc func(ctx context.Context, nodeID uuid.UUID) (interface{}, uuid.UUID, error)

// GetNodeFromContext 从请求上下文中获取节点对象（由 NodeAccessMiddleware 注入）。
//
// 返回：
//   - interface{}: 节点对象
//   - bool: 是否存在有效的节点对象
func GetNodeFromContext(ctx context.Context) (interface{}, bool) {
	node := ctx.Value(nodeContextKey)
	return node, node != nil
}

// NodeAccessMiddlewareWithChecker 验证 URL 参数 {id} 中的节点是否属于已认证用户的工作区。
// 验证通过后将节点注入上下文，可通过 GetNodeFromContext 获取。
// 该中间件防止用户通过 URL 篡改访问其他工作区的任务节点。
//
// 参数：
//   - checker: 节点工作区验证函数，验证节点归属并返回节点对象
//   - workspaceChecker: 工作区访问检查函数，验证当前身份是否可访问节点所属工作区
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
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
				// 父级测试路由在节点子路由匹配前，可能使用 {id} 作为任务 ID。
				// 节点处理器仍会在具体的节点路由上校验格式错误的节点 ID。
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

// --- 项目角色中间件 ---

// ProjectRoleCheckerFunc 检查成员是否拥有指定的项目级角色。
// 项目角色层级（从高到低）：lead > developer > reviewer
type ProjectRoleCheckerFunc func(ctx context.Context, userID uuid.UUID, userType string, workspaceRole string, projectID uuid.UUID, requiredRole string) error

// RequireProjectRoleWithChecker 返回一个中间件，检查已认证用户是否拥有指定的项目级角色。
// 工作区 owner/admin 始终绕过项目角色检查（拥有工作区级别的完全权限）。
// Agent 被拒绝（项目角色仅限人类用户）。
//
// 参数：
//   - requiredRole: 所需的项目角色，如 "lead"、"developer"、"reviewer"
//   - checker: 项目角色检查函数
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
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
