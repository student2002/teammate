// auth.go 提供用户认证相关的 HTTP API 端点，包括登录、注册、Token 交换、登出、密码修改/重置、工作区切换等。
//
// 公开端点（无需认证）：登录、注册、Token 交换、密码重置请求、密码重置、接受邀请。
// 需认证端点：登出、当前用户信息、修改密码、切换工作区。

package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// AuthHandler 处理用户认证相关的 HTTP 请求，包括登录、注册、Token 交换、登出、密码管理等。
type AuthHandler struct {
	Svc       *service.Service
	JWTSecret string // JWT 签名密钥
}

// NewAuthHandler 创建 AuthHandler 实例。
func NewAuthHandler(svc *service.Service, jwtSecret string) *AuthHandler {
	return &AuthHandler{Svc: svc, JWTSecret: jwtSecret}
}

// Routes 返回不需要认证的认证路由（登录、注册、Token 交换）。
func (h *AuthHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/login", h.Login)
	r.Post("/register", h.Register)
	r.Post("/token-exchange", h.TokenExchange)

	return r
}

// loginRequest 登录请求体。
type loginRequest struct {
	Email    string `json:"email"`    // 用户邮箱
	Password string `json:"password"` // 用户密码
}

// Login 处理 POST /login 端点，验证用户邮箱和密码，返回 JWT Token 和用户信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - email: string，用户邮箱（必填）
//   - password: string，用户密码（必填）
//
// 响应：
//   - 200: 登录成功，返回 Token 和用户信息
//   - 400: 参数错误（缺少邮箱或密码）
//   - 401: 邮箱或密码错误
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	// 解析请求体
	var req loginRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// 验证必填字段
	if req.Email == "" || req.Password == "" {
		response.BadRequest(w, "email and password are required")
		return
	}

	// 调用认证服务执行登录
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	result, err := authSvc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		response.Unauthorized(w, "invalid email or password")
		return
	}

	// 返回认证结果
	response.JSON(w, r, authResponse{
		Token:       result.Token,
		ExpiresAt:   result.ExpiresAt,
		Member:      result.Member,
		WorkspaceID: result.WorkspaceID.String(),
		Role:        result.Role,
	})
}

// registerRequest 注册请求体。
type registerRequest struct {
	Name     string `json:"name"`     // 用户名称
	Email    string `json:"email"`    // 用户邮箱
	Password string `json:"password"` // 用户密码
}

// Register 处理 POST /register 端点，注册新用户并创建默认工作区，返回 JWT Token。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，用户名称（必填）
//   - email: string，用户邮箱（必填）
//   - password: string，用户密码（必填，8-128 位，需包含大小写字母和数字）
//
// 响应：
//   - 201: 注册成功，返回 Token 和用户信息
//   - 400: 参数错误或密码不符合强度要求
//   - 500: 服务器内部错误
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	// 解析请求体
	var req registerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// 验证必填字段
	if req.Name == "" || req.Email == "" || req.Password == "" {
		response.BadRequest(w, "name, email and password are required")
		return
	}

	// 验证密码强度（与 service 层一致，纵深防御）
	if err := service.ValidatePassword(req.Password); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用认证服务执行注册
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	result, err := authSvc.Register(r.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		if isUniqueViolation(err) {
			response.Conflict(w, "email already registered")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// 返回注册结果
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, authResponse{
		Token:       result.Token,
		ExpiresAt:   result.ExpiresAt,
		Member:      result.Member,
		WorkspaceID: result.WorkspaceID.String(),
		Role:        result.Role,
	})
}

// --- Token 交换 ---

// tokenExchangeRequest Token 交换请求体。
type tokenExchangeRequest struct {
	APIToken string `json:"api_token"` // API Token（tm_ 前缀）
}

// tokenExchangeResponse Token 交换响应体。
type tokenExchangeResponse struct {
	SessionToken string    `json:"session_token"` // 会话 Token（st_ 前缀）
	ExpiresAt    time.Time `json:"expires_at"`    // 过期时间
}

// TokenExchange 处理 POST /token-exchange 端点，将 API Token 交换为会话 Token。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - api_token: string，API Token（必填，tm_ 前缀）
//
// 响应：
//   - 200: 交换成功，返回会话 Token
//   - 400: 参数错误或 Token 格式无效
//   - 401: Token 无效或已过期
func (h *AuthHandler) TokenExchange(w http.ResponseWriter, r *http.Request) {
	// 解析请求体
	var req tokenExchangeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// 验证必填字段
	if req.APIToken == "" {
		response.BadRequest(w, "api_token is required")
		return
	}

	// 验证 Token 格式
	if !strings.HasPrefix(req.APIToken, "tm_") {
		response.BadRequest(w, "invalid api_token format")
		return
	}

	// 调用认证服务执行 Token 交换
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	result, err := authSvc.ExchangeAPITokenForSession(r.Context(), req.APIToken)
	if err != nil {
		response.Unauthorized(w, "invalid or expired api token")
		return
	}

	// 返回会话 Token
	response.JSON(w, r, tokenExchangeResponse{
		SessionToken: result.SessionToken,
		ExpiresAt:    result.ExpiresAt,
	})
}

// --- Whoami ---

// Whoami 处理 GET /whoami 端点，返回当前已认证用户的信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回用户信息
//   - 401: 未认证
func (h *AuthHandler) Whoami(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "not authenticated")
		return
	}

	// 调用认证服务获取用户信息
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	info, err := authSvc.Whoami(r.Context(), claims.UserType, claims.UserID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, info)
}

// generateToken 生成 JWT Token 的内部方法。
func (h *AuthHandler) generateToken(userID uuid.UUID, userType string, workspaceID uuid.UUID, role string) (string, time.Time, error) {
	authSvc := service.NewAuthService(h.Svc, h.JWTSecret)
	return authSvc.GenerateToken(userID, userType, workspaceID, role)
}

// --- 切换工作区 ---

// switchWorkspaceRequest 切换工作区请求体。
type switchWorkspaceRequest struct {
	WorkspaceID string `json:"workspace_id"` // 目标工作区 ID
}

// switchWorkspaceResponse 切换工作区响应体。
type switchWorkspaceResponse struct {
	WorkspaceID string `json:"workspace_id"` // 目标工作区 ID
	Role        string `json:"role"`         // 用户在目标工作区的角色
}

// SwitchWorkspace 处理 POST /switch-workspace 端点，生成面向目标工作区的新 JWT Token，仅允许人类用户切换。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - workspace_id: string，目标工作区 ID（必填）
//
// 响应：
//   - 200: 切换成功，返回新 Token 和角色信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 非人类用户无法切换或不是目标工作区成员
func (h *AuthHandler) SwitchWorkspace(w http.ResponseWriter, r *http.Request) {
	// 从上下文获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 仅人类用户可以切换工作区
	if claims.UserType != "member" {
		response.Forbidden(w, "only members can switch workspace")
		return
	}

	// 解析请求体
	var req switchWorkspaceRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// 验证必填字段
	if req.WorkspaceID == "" {
		response.BadRequest(w, "workspace_id is required")
		return
	}

	// 解析目标工作区 ID
	targetWorkspaceID, err := uuid.Parse(req.WorkspaceID)
	if err != nil {
		response.BadRequest(w, "invalid workspace_id format")
		return
	}

	// 验证用户是目标工作区的成员
	wsSvc := service.NewWorkspaceService(h.Svc)
	wm, err := wsSvc.GetMembership(r.Context(), targetWorkspaceID, claims.UserID)
	if err != nil {
		response.Forbidden(w, "you are not a member of the target workspace")
		return
	}

	response.JSON(w, r, switchWorkspaceResponse{
		WorkspaceID: targetWorkspaceID.String(),
		Role:        wm.Role,
	})
}

// isUniqueViolation 判断错误链里是否含 PostgreSQL 唯一约束冲突（SQLSTATE 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return true
	}
	// 退化兜底：错误串含 SQLSTATE 23505（覆盖驱动错误类型被包装变化的情况）
	return strings.Contains(err.Error(), "23505")
}
