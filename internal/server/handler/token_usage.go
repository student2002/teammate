// token_usage.go 提供 AI 代理 Token 用量上报和查询的 HTTP API 端点。
//
// 本文件提供以下 HTTP API 端点：
//   - POST /tasks/{taskId}/token-usage: Agent 上报执行节点的 Token 用量数据（仅限 Agent 身份）
//   - GET /tasks/{taskId}/token-usage: 查询指定任务的 Token 用量汇总
//
// 上报接口强制要求请求者为 Agent 身份，且必须是目标节点的 assignee。
// Agent ID 从认证声明中提取，防止伪造。节点与任务的归属关系通过 URL 参数和数据库校验确保工作区隔离。

package handler

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// TokenUsageHandler 处理 Token 用量上报和查询的 HTTP 请求。
type TokenUsageHandler struct {
	Svc *service.Service
}

// NewTokenUsageHandler 创建 TokenUsageHandler 实例。
//
// 参数:
//   - svc: 业务逻辑服务实例，提供 Token 用量管理能力
//
// 返回:
//   - *TokenUsageHandler: Token 用量处理器实例
func NewTokenUsageHandler(svc *service.Service) *TokenUsageHandler {
	return &TokenUsageHandler{Svc: svc}
}

// Routes 返回 Token 用量的路由表。
//
// 返回:
//   - chi.Router: 包含 Token 用量上报和查询端点的路由
func (h *TokenUsageHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.ReportTokenUsage)

	return r
}

// ReportTokenUsage 处理 POST /tasks/{taskId}/token-usage 端点，Agent 上报执行节点的 Token 用量数据。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 taskId 为任务 ID，请求体包含 Token 用量数据
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应（201 Created），包含创建的用量记录或错误信息
func (h *TokenUsageHandler) ReportTokenUsage(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 只有 Agent 可以上报 Token 用量——Member 的 UUID 会违反
	// token_usage.agent_id 外键约束（REFERENCES agents(id)）。
	if claims.UserType != "agent" {
		response.Forbidden(w, "only agents can report token usage")
		return
	}

	var req types.ReportTokenUsageReq
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	// 从认证声明（而非请求体）中推导 agent_id，以防止伪造
	agentID := claims.UserID

	// 验证该 Agent 是否为节点的 assignee
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), req.TaskNodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "node not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}
	if node.AssigneeID == nil || *node.AssigneeID != claims.UserID.String() {
		response.Forbidden(w, "only the assigned agent can report token usage for this node")
		return
	}

	// 验证节点是否属于 URL 中的任务（工作区隔离）
	urlTaskIDStr := chi.URLParam(r, "taskId")
	var urlTaskID int32
	if _, err := fmt.Sscanf(urlTaskIDStr, "%d", &urlTaskID); err != nil || node.TaskID != urlTaskID {
		response.BadRequest(w, "node does not belong to the specified task")
		return
	}

	tuSvc := service.NewTokenUsageService(h.Svc)
	usage, err := tuSvc.Create(r.Context(), buildCreateTokenUsageParams(
		req.TaskNodeID, agentID, req.InputTokens, req.OutputTokens, req.TotalTokens, req.CostEstimate,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, usage)
}
