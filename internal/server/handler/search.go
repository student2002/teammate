// search.go 提供任务和代理的关键词搜索 HTTP API 端点。
//
// 支持按关键词搜索任务和 Agent，任务搜索可按项目 ID 过滤，Agent 搜索可按工作区 ID 过滤。

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// SearchHandler 处理搜索相关的 HTTP 请求，支持任务和代理的关键词搜索。
type SearchHandler struct {
	Svc *service.Service
}

// NewSearchHandler 创建 SearchHandler 实例。
func NewSearchHandler(svc *service.Service) *SearchHandler {
	return &SearchHandler{Svc: svc}
}

// Routes 返回搜索的路由表。
func (h *SearchHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/tasks", h.SearchTasks)
	r.Get("/agents", h.SearchAgents)

	return r
}

// SearchTasks 处理 GET /workspaces/{workspaceId}/search/tasks 端点，按关键词搜索任务，支持按项目 ID 过滤。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 查询参数：
//   - q: string，搜索关键词（必填）
//   - projectId: UUID，项目 ID（可选，按项目过滤）
//
// 响应：
//   - 200: 成功返回搜索结果
//   - 400: 缺少搜索关键词或项目 ID 无效
//   - 401: 未认证
func (h *SearchHandler) SearchTasks(w http.ResponseWriter, r *http.Request) {
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return
	}

	// 获取搜索关键词
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		response.BadRequest(w, "missing query parameter: q")
		return
	}

	// 解析可选的项目 ID
	projectIDStr := r.URL.Query().Get("projectId")
	var projectID *uuid.UUID
	if projectIDStr != "" {
		parsed, err := uuid.Parse(projectIDStr)
		if err != nil {
			response.BadRequest(w, "invalid project id")
			return
		}
		// 验证项目属于当前工作区
		if checkProjectWorkspace(h.Svc, w, r, parsed) == nil {
			return
		}
		projectID = &parsed
	}

	// 调用 service 搜索任务（现已直接返回 []types.Task）
	searchSvc := service.NewSearchService(h.Svc)
	tasks, err := searchSvc.SearchTasks(r.Context(), keyword, ws.WorkspaceID, projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 转换为响应格式
	response.JSON(w, r, tasksToResponse(tasks))
}

// SearchAgents 处理 GET /workspaces/{workspaceId}/search/agents 端点，按关键词搜索 AI 代理。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 查询参数：
//   - q: string，搜索关键词（必填）
//
// 响应：
//   - 200: 成功返回搜索结果
//   - 400: 缺少搜索关键词或工作区 ID 无效
//   - 401: 未认证
func (h *SearchHandler) SearchAgents(w http.ResponseWriter, r *http.Request) {
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return
	}

	// 获取搜索关键词
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		response.BadRequest(w, "missing query parameter: q")
		return
	}

	// 调用 service 搜索 Agent
	searchSvc := service.NewSearchService(h.Svc)
	agents, err := searchSvc.SearchAgents(r.Context(), keyword, ws.WorkspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, agents)
}
