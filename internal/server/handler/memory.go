// memory.go 提供共享记忆（Memory）的创建、列表查询、删除和文本搜索等 HTTP API 端点。
//
// 共享记忆是 Agent 之间共享的知识库，当前通过 ILIKE 文本搜索检索记忆。
// 数据库已预留 embedding vector(1536) 字段，pgvector 语义检索待接入 embedding 生成服务后启用。
// Agent 需要 memory:create 权限才能创建记忆，resource:delete 权限才能删除记忆。

package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// MemoryHandler 处理共享记忆相关的 HTTP 请求，包括创建、列表查询、删除和语义搜索。
type MemoryHandler struct {
	Svc     *service.Service
	Checker svcmw.WorkspaceAccessCheckerFunc // 工作区访问检查器（注入，非全局）
}

// NewMemoryHandler 创建 MemoryHandler 实例。
func NewMemoryHandler(svc *service.Service, checker svcmw.WorkspaceAccessCheckerFunc) *MemoryHandler {
	return &MemoryHandler{Svc: svc, Checker: checker}
}

// createMemoryRequest 创建记忆请求体。
type createMemoryRequest struct {
	WorkspaceID  string          `json:"workspace_id"`   // 工作区 ID
	SourceTaskID string          `json:"source_task_id"` // 来源任务 ID
	Type         string          `json:"type"`           // 记忆类型
	Title        string          `json:"title"`          // 记忆标题
	Content      string          `json:"content"`        // 记忆内容
	Tags         []string        `json:"tags"`           // 标签列表
	Confidence   float32         `json:"confidence"`     // 置信度（0-1）
	Verified     bool            `json:"verified"`       // 是否已验证
	Metadata     json.RawMessage `json:"metadata"`       // 元数据（JSON）
}

func (h *MemoryHandler) resolveWorkspaceForRequest(w http.ResponseWriter, r *http.Request, workspaceIDStr string) (uuid.UUID, string, bool) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return uuid.Nil, "", false
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if workspaceIDStr == "" || err != nil {
		response.BadRequest(w, "valid workspace_id is required")
		return uuid.Nil, "", false
	}
	if h.Checker == nil {
		response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
		return uuid.Nil, "", false
	}
	role, err := h.Checker(r.Context(), claims.UserID, claims.UserType, workspaceID)
	if err != nil {
		response.Forbidden(w, "workspace access denied")
		return uuid.Nil, "", false
	}
	return workspaceID, role, true
}

// CreateMemory 处理 POST /memories 端点，创建新的共享记忆条目，Agent 需要 memory:create 权限。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - source_task_id: string，来源任务 ID
//   - type: string，记忆类型
//   - title: string，记忆标题
//   - content: string，记忆内容
//   - tags: string[]，标签列表
//   - confidence: float，置信度（默认 0.5）
//   - verified: bool，是否已验证（Agent 不能设置为 true）
//   - metadata: object，元数据
//
// 响应：
//   - 201: 成功创建记忆
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 权限不足（Agent 需要 memory:create，人类需要 member+ 角色）
func (h *MemoryHandler) CreateMemory(w http.ResponseWriter, r *http.Request) {
	// 解析请求体
	var req createMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	workspaceID, role, ok := h.resolveWorkspaceForRequest(w, r, req.WorkspaceID)
	if !ok {
		return
	}

	// 权限检查：Agent 和人类用户使用不同的权限模型
	if claims.UserType == "agent" {
		// Agent 必须具有 memory:create 权限
		permSvc := service.NewAgentPermissionService(h.Svc)
		has, err := permSvc.HasPermission(r.Context(), claims.UserID, types.PermMemoryCreate)
		if err != nil || !has {
			response.Forbidden(w, "agent lacks memory:create permission")
			return
		}
		// Agent 不能设置 verified=true
		req.Verified = false
	} else {
		// 人类用户需要 member 及以上角色
		if types.MemberRoleLevel(role) < 2 {
			response.Forbidden(w, "insufficient permissions: member role or higher required")
			return
		}
	}

	// 转换来源任务 ID
	var sourceTaskID sql.NullInt32
	if req.SourceTaskID != "" {
		var tid int32
		if _, err := fmt.Sscanf(req.SourceTaskID, "%d", &tid); err == nil {
			sourceTaskID = sql.NullInt32{Int32: tid, Valid: true}
		}
	}

	// 设置默认标签
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	// 设置默认置信度
	confidence := req.Confidence
	if confidence == 0 {
		confidence = 0.5
	}

	// 转换元数据
	var metadata pqtype.NullRawMessage
	if req.Metadata != nil {
		metadata = pqtype.NullRawMessage{RawMessage: req.Metadata, Valid: true}
	} else {
		metadata = pqtype.NullRawMessage{RawMessage: json.RawMessage(`{}`), Valid: true}
	}

	// 调用 service 创建记忆
	memSvc := service.NewMemoryService(h.Svc)
	memory, err := memSvc.Create(r.Context(), buildCreateMemoryParams(
		workspaceID,
		sourceTaskID,
		req.Type,
		req.Title,
		req.Content,
		tags,
		confidence,
		req.Verified,
		metadata,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, memory)
}

// ListMemories 处理 GET /memories 端点，列出工作区下的所有共享记忆，支持按验证状态、置信度过滤。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 查询参数：
//   - verified: bool，按验证状态过滤
//   - min_confidence: float，最小置信度过滤
//   - limit: int，返回数量限制
//
// 响应：
//   - 200: 成功返回记忆列表
//   - 400: 查询参数错误
//   - 401: 未认证
func (h *MemoryHandler) ListMemories(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.resolveWorkspaceForRequest(w, r, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}

	memSvc := service.NewMemoryService(h.Svc)

	// 解析可选的过滤参数
	var verified *bool
	var minConfidence *float32
	var limit *int32

	// 解析 verified 参数
	if v := r.URL.Query().Get("verified"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			response.BadRequest(w, "invalid verified parameter")
			return
		}
		verified = &parsed
	}

	// 解析 min_confidence 参数
	if mc := r.URL.Query().Get("min_confidence"); mc != "" {
		parsed, err := strconv.ParseFloat(mc, 32)
		if err != nil {
			response.BadRequest(w, "invalid min_confidence parameter")
			return
		}
		f := float32(parsed)
		minConfidence = &f
	}

	// 解析 limit 参数
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.ParseInt(l, 10, 32)
		if err != nil {
			response.BadRequest(w, "invalid limit parameter")
			return
		}
		n := int32(parsed)
		limit = &n
	}

	// 调用 service 查询记忆
	memories, err := memSvc.ListByWorkspace(r.Context(), workspaceID, verified, minConfidence, limit)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, memories)
}

// DeleteMemory 处理 DELETE /memories/{id} 端点，删除指定的共享记忆条目，Agent 需要 resource:delete 权限。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功删除
//   - 400: 记忆 ID 无效
//   - 401: 未认证
//   - 403: 权限不足（Agent 需要 resource:delete，人类需要 member+ 角色）
//   - 404: 记忆不存在
func (h *MemoryHandler) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	// 解析记忆 ID
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "invalid memory id")
		return
	}

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 验证记忆属于当前工作区
	memSvc := service.NewMemoryService(h.Svc)
	memory, err := memSvc.Get(r.Context(), id)
	if err != nil {
		response.NotFound(w, "memory not found")
		return
	}
	if h.Checker == nil {
		response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
		return
	}
	// workspace ID 是 domain 幜格 string，需解析回 uuid.UUID 传给 Checker
	memoryWsID, _ := uuid.Parse(memory.WorkspaceID)
	role, err := h.Checker(r.Context(), claims.UserID, claims.UserType, memoryWsID)
	if err != nil {
		response.NotFound(w, "memory not found")
		return
	}

	// 权限检查：谁能删除这条记忆？
	if claims.UserType == "agent" {
		// Agent 必须具有 resource:delete 权限
		permSvc := service.NewAgentPermissionService(h.Svc)
		has, err := permSvc.HasPermission(r.Context(), claims.UserID, types.PermResourceDelete)
		if err != nil || !has {
			response.Forbidden(w, "agent lacks resource:delete permission")
			return
		}
	} else {
		// 人类用户需要 member 及以上角色（viewer 不能删除）
		if types.MemberRoleLevel(role) < 2 {
			response.Forbidden(w, "insufficient permissions: member role or higher required")
			return
		}
	}

	// 调用 service 删除记忆
	if err := memSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SearchMemories 处理 GET /memories/search 端点，使用 pgvector 语义搜索匹配的共享记忆条目。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 查询参数：
//   - q: string，搜索关键词
//
// 响应：
//   - 200: 成功返回搜索结果
//   - 401: 未认证
func (h *MemoryHandler) SearchMemories(w http.ResponseWriter, r *http.Request) {
	// 获取搜索关键词
	q := r.URL.Query().Get("q")

	workspaceID, _, ok := h.resolveWorkspaceForRequest(w, r, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}

	// 如果有搜索关键词，执行语义搜索
	if q != "" {
		memSvc := service.NewMemoryService(h.Svc)
		results, err := memSvc.Search(r.Context(), q, workspaceID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		response.JSON(w, r, results)
		return
	}

	// 无搜索关键词时返回空列表
	response.JSON(w, r, []interface{}{})
}
