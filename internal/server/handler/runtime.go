// runtime.go 提供 Agent 守护进程运行时的注册、心跳、同步及公钥上传等 HTTP API 端点。
//
// 运行时（Runtime）是 Agent 守护进程的实例，通过心跳维持在线状态。
// Agent 只能操作自己的运行时，人类用户可操作工作区内所有运行时。

package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// RuntimeHandler 处理 Agent 守护进程运行时管理的 HTTP 请求，包括注册、心跳、同步和公钥上传。
type RuntimeHandler struct {
	Svc *service.Service
}

// NewRuntimeHandler 创建 RuntimeHandler 实例。
func NewRuntimeHandler(svc *service.Service) *RuntimeHandler {
	return &RuntimeHandler{Svc: svc}
}

// Routes 返回运行时的完整路由表（包含读写操作）。
func (h *RuntimeHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.RegisterRuntime)
	r.Post("/{id}/heartbeat", h.Heartbeat)

	return r
}

// ReadRoutes 返回运行时只读路由（viewer+ 可访问）。
func (h *RuntimeHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	return r
}

// WriteRoutes 返回运行时写入路由（member+ 或 Agent 自身可访问）。
func (h *RuntimeHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.RegisterRuntime)
	r.Post("/{id}/heartbeat", h.Heartbeat)

	return r
}


// RegisterRuntime 处理 POST /workspaces/{workspaceId}/runtimes 端点，注册 Agent 守护进程运行时实例。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - agent_id: string，Agent ID（必填）
//   - daemon_id: string，守护进程 ID
//   - provider: string，Agent 提供者
//   - version: string，守护进程版本
//   - status: string，初始状态，默认 "online"
//   - session_token_hash: string，会话 Token 哈希
//   - session_expires_at: string，会话过期时间
//   - public_key: string，RSA 公钥
//
// 响应：
//   - 201: 成功注册运行时
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: Agent 只能注册自己的运行时，人类需要 admin+ 角色
func (h *RuntimeHandler) RegisterRuntime(w http.ResponseWriter, r *http.Request) {
	// 解析请求体
	var req registerRuntimeRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// Agent ID 已是 uuid.UUID（registerRuntimeRequest.AgentID 字段类型）
	agentID := req.AgentID
	if agentID == uuid.Nil {
		response.BadRequest(w, "invalid agent_id: must be a valid UUID")
		return
	}

	// 验证 Agent 属于 URL 中的工作区
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 权限检查
	if claims.UserType == "agent" {
		// Agent 只能为自己注册运行时
		if claims.UserID != agentID {
			response.Forbidden(w, "agents can only register runtimes for themselves")
			return
		}
	}

	if claims.UserType == "member" {
		// 仅 admin+ 角色可代 Agent 注册运行时
		if types.MemberRoleLevel(claims.Role) < 3 {
			response.Forbidden(w, "only admins can register runtimes on behalf of agents")
			return
		}
	}

	// 验证 Agent 存在且属于工作区
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), agentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "agent not found")
		return
	}

	// 设置默认状态
	status := req.Status
	if status == "" {
		status = RuntimeStatusOnline
	}

	// 调用 service 注册运行时
	rtSvc := service.NewRuntimeService(h.Svc)
	runtime, err := rtSvc.Register(r.Context(), buildCreateRuntimeParams(
		agentID, req.DaemonID, req.Provider, req.Version, status, req.SessionTokenHash, req.SessionExpiresAt, req.PublicKey,
	))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.BadRequest(w, "agent not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, runtime)
}

// Heartbeat 处理 POST /workspaces/{workspaceId}/runtimes/{id}/heartbeat 端点，Agent 定期发送心跳维持在线状态。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回运行时信息
//   - 400: 运行时 ID 无效
//   - 401: 未认证
//   - 403: Agent 只能操作自己的运行时
//   - 404: 运行时不存在
func (h *RuntimeHandler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	// 解析运行时 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid runtime id")
		return
	}

	// 验证运行时归属（Agent 只能操作自己的运行时）
	if h.checkRuntimeOwnership(w, r, id) == nil {
		return
	}

	// 调用 service 发送心跳
	rtSvc := service.NewRuntimeService(h.Svc)
	runtime, err := rtSvc.Heartbeat(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "runtime not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, runtime)
}

// --- 公钥上传 ---
func (h *RuntimeHandler) checkRuntimeWorkspace(w http.ResponseWriter, r *http.Request, runtimeID uuid.UUID) bool {
	// 解析工作区 ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return false
	}

	// 查询运行时
	runtime, err := service.NewRuntimeService(h.Svc).GetRuntimeByID(r.Context(), runtimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "runtime not found")
			return false
		}
		response.InternalServerError(w, err)
		return false
	}

	// 验证 Agent 属于工作区
	rtAgentID, _ := uuid.Parse(runtime.AgentID)
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), rtAgentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "runtime not found")
		return false
	}

	return true
}

// checkRuntimeOwnership 验证运行时是否属于当前认证 Agent 的自有运行时。
// 对于 Agent：运行时必须属于认证的 Agent（防止跨 Agent 干扰）。
// 对于人类用户：运行时的 Agent 必须属于 URL 工作区。
func (h *RuntimeHandler) checkRuntimeOwnership(w http.ResponseWriter, r *http.Request, runtimeID uuid.UUID) *Runtime {
	// 解析工作区 ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return nil
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return nil
	}

	// 查询运行时
	runtime, err := service.NewRuntimeService(h.Svc).GetRuntimeByID(r.Context(), runtimeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "runtime not found")
			return nil
		}
		response.InternalServerError(w, err)
		return nil
	}

	// 验证 Agent 属于工作区
	rAgentID, _ := uuid.Parse(runtime.AgentID)
	agent, err := service.NewAgentService(h.Svc).Get(r.Context(), rAgentID)
	if err != nil || agent.WorkspaceID != workspaceID.String() {
		response.NotFound(w, "runtime not found")
		return nil
	}

	// Agent 只能操作自己的运行时
	if claims.UserType == "agent" && rAgentID != claims.UserID {
		response.Forbidden(w, "agents can only operate on their own runtimes")
		return nil
	}

	return &runtime
}
