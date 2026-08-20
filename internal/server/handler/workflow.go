// workflow.go 提供工作流模板的创建、列表查询、更新和删除等 HTTP API 端点。
//
// 本文件提供以下 HTTP API 端点：
//   - POST /workspaces/{workspaceId}/templates: 创建新的工作流模板及其有序节点定义
//   - GET /workspaces/{workspaceId}/templates: 列出工作区下的所有工作流模板
//   - GET /workspaces/{workspaceId}/templates/{id}: 查询指定工作流模板的详细信息
//   - PUT /workspaces/{workspaceId}/templates/{id}: 更新工作流模板及其节点定义
//   - DELETE /workspaces/{workspaceId}/templates/{id}: 删除指定工作流模板
//
// 工作流模板定义了任务执行的有序节点流程（如 实现→自测→审查→部署），节点类型包括
// standard（AI 执行）、review（审查）和 manual（人工执行）。
// 所有写入操作需要 write 权限，模板归属通过工作区隔离校验。

package handler

import (
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// WorkflowHandler 处理工作流模板管理的 HTTP 请求，包括创建、查询、更新和删除模板。
type WorkflowHandler struct {
	Svc *service.Service
}

// NewWorkflowHandler 创建 WorkflowHandler 实例。
//
// 参数:
//   - svc: 业务逻辑服务实例，提供工作流模板管理能力
//
// 返回:
//   - *WorkflowHandler: 工作流处理器实例
func NewWorkflowHandler(svc *service.Service) *WorkflowHandler {
	return &WorkflowHandler{Svc: svc}
}

// Routes 返回工作流模板的完整路由表（包含读写操作）。
//
// 返回:
//   - chi.Router: 包含工作流模板 CRUD 端点的路由
func (h *WorkflowHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateWorkflowTemplate)
	r.Get("/", h.ListWorkflowTemplates)
	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateWorkflowTemplate)
		r.Delete("/", h.DeleteWorkflowTemplate)
	})

	return r
}

// ReadRoutes 返回工作流模板的只读路由表。
//
// 返回:
//   - chi.Router: 仅包含查询类端点的路由
func (h *WorkflowHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListWorkflowTemplates)

	return r
}

// WriteRoutes 返回工作流模板的写入路由表。
//
// 返回:
//   - chi.Router: 仅包含创建、更新、删除端点的路由
func (h *WorkflowHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateWorkflowTemplate)
	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateWorkflowTemplate)
		r.Delete("/", h.DeleteWorkflowTemplate)
	})

	return r
}

// CreateWorkflowTemplate 处理 POST /workspaces/{workspaceId}/templates 端点，创建新的工作流模板及其有序节点定义。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID，请求体包含模板和节点定义
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应（201 Created），包含创建的模板和节点数据或错误信息
func (h *WorkflowHandler) CreateWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	var req createWorkflowTemplateRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 构建节点参数
	// 兜底修正缺失/重复的 sort_order，避免命中 UNIQUE(template_id, sort_order)（偏差 #8）
	normalizeTemplateSortOrder(req.Nodes)
	nodeParams := make([]CreateTemplateNodeParams, 0, len(req.Nodes))
	for _, n := range req.Nodes {
		var assigneeID uuid.NullUUID
		if n.AssigneeID != nil {
			// 验证 assignee 是否属于当前工作区
			if n.AssigneeType == AssigneeTypeSpecificAgent {
				agent := checkAgentWorkspace(h.Svc, w, r, *n.AssigneeID)
				if agent == nil {
					return
				}
			} else if n.AssigneeType == AssigneeTypeHuman {
				// 验证成员是否属于当前工作区
				wsSvc := service.NewWorkspaceService(h.Svc)
				member, err := wsSvc.GetMember(r.Context(), *n.AssigneeID)
				if err != nil {
					response.BadRequest(w, "assignee not found in this workspace")
					return
				}
				memberUUID, _ := uuid.Parse(member.ID)
				_, err = wsSvc.GetMembership(r.Context(), workspaceID, memberUUID)
				if err != nil {
					response.BadRequest(w, "assignee not found in this workspace")
					return
				}
			}
			assigneeID = uuid.NullUUID{UUID: *n.AssigneeID, Valid: true}
		}

		var readonlyDirs pqtype.NullRawMessage
		if n.ReadonlyDirs != nil {
			readonlyDirs = pqtype.NullRawMessage{RawMessage: n.ReadonlyDirs, Valid: true}
		}
		var fullControlDirs pqtype.NullRawMessage
		if n.FullControlDirs != nil {
			fullControlDirs = pqtype.NullRawMessage{RawMessage: n.FullControlDirs, Valid: true}
		}
		var artifact pqtype.NullRawMessage
		if n.Artifact != nil {
			artifact = pqtype.NullRawMessage{RawMessage: n.Artifact, Valid: true}
		}

		maxRejectCycles := n.MaxRejectCycles
		if maxRejectCycles <= 0 {
			maxRejectCycles = 5
		}

		nodeParams = append(nodeParams, buildCreateTemplateNodeParams(
			n.Name,
			n.Description,
			n.SortOrder,
			n.NodeType,
			n.AssigneeType,
			assigneeID,
			n.TimeoutMinutes,
			readonlyDirs,
			fullControlDirs,
			artifact,
			maxRejectCycles,
			n.DependsOn,
		))
	}

	wfSvc := service.NewWorkflowService(h.Svc)
	result, err := wfSvc.Create(r.Context(), buildCreateWorkflowTemplateParams(
		workspaceID,
		req.Name,
		req.Description,
		req.IsBuiltin,
		req.TriggerType,
		req.TriggerConfig,
		req.TriggerEnabled,
		req.NextRunAt,
	), nodeParams)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"template": toTemplateResponse(result.Template),
		"nodes":    result.Nodes,
	})
}

// ListWorkflowTemplates 处理 GET /workspaces/{workspaceId}/templates 端点，列出工作区下的所有工作流模板及其节点。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含模板列表或错误信息
func (h *WorkflowHandler) ListWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	wfSvc := service.NewWorkflowService(h.Svc)
	results, err := wfSvc.List(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// 转换为响应格式
	resp := make([]templateWithNodes, 0, len(results))
	for _, r := range results {
		resp = append(resp, templateWithNodes{
			templateResponse: toTemplateResponse(r.Template),
			Nodes:            r.Nodes,
		})
	}

	response.JSON(w, r, resp)
}

// GetWorkflowTemplate 处理 GET /workspaces/{workspaceId}/templates/{id} 端点，查询指定工作流模板的详细信息及其节点。
//
// 参数:
// UpdateWorkflowTemplate 处理 PUT /workspaces/{workspaceId}/templates/{id} 端点，更新工作流模板及其节点定义。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 id 为模板 UUID，请求体包含更新后的模板和节点定义
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含更新后的模板和节点数据或错误信息
func (h *WorkflowHandler) UpdateWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid template id")
		return
	}

	// 验证工作流模板是否属于已认证用户的工作区
	if checkWorkflowTemplateWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	var req updateWorkflowTemplateRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 如果提供了节点，则构建节点参数
	var nodeParams []CreateTemplateNodeParams
	if req.Nodes != nil {
		// 兜底修正缺失/重复的 sort_order，避免命中 UNIQUE(template_id, sort_order)（偏差 #8）
		normalizeTemplateSortOrder(req.Nodes)
		nodeParams = make([]CreateTemplateNodeParams, 0, len(req.Nodes))
		for _, n := range req.Nodes {
			var assigneeID uuid.NullUUID
			if n.AssigneeID != nil {
				// 验证 assignee 是否属于当前工作区
				claims, _ := svcmw.GetAuthFromContext(r.Context())
				if claims.WorkspaceID != uuid.Nil {
					if n.AssigneeType == AssigneeTypeSpecificAgent {
						agent := checkAgentWorkspace(h.Svc, w, r, *n.AssigneeID)
						if agent == nil {
							return
						}
					} else if n.AssigneeType == AssigneeTypeHuman {
						wsSvc := service.NewWorkspaceService(h.Svc)
						member, err := wsSvc.GetMember(r.Context(), *n.AssigneeID)
						if err != nil {
							response.BadRequest(w, "assignee not found in this workspace")
							return
						}
						memberUUID, _ := uuid.Parse(member.ID)
					_, err = wsSvc.GetMembership(r.Context(), claims.WorkspaceID, memberUUID)
						if err != nil {
							response.BadRequest(w, "assignee not found in this workspace")
							return
						}
					}
				}
				assigneeID = uuid.NullUUID{UUID: *n.AssigneeID, Valid: true}
			}

			var readonlyDirs pqtype.NullRawMessage
			if n.ReadonlyDirs != nil {
				readonlyDirs = pqtype.NullRawMessage{RawMessage: n.ReadonlyDirs, Valid: true}
			}
			var fullControlDirs pqtype.NullRawMessage
			if n.FullControlDirs != nil {
				fullControlDirs = pqtype.NullRawMessage{RawMessage: n.FullControlDirs, Valid: true}
			}
			var artifact pqtype.NullRawMessage
			if n.Artifact != nil {
				artifact = pqtype.NullRawMessage{RawMessage: n.Artifact, Valid: true}
			}

			maxRejectCycles := n.MaxRejectCycles
			if maxRejectCycles <= 0 {
				maxRejectCycles = 5
			}

			nodeParams = append(nodeParams, buildCreateTemplateNodeParams(
				n.Name,
				n.Description,
				n.SortOrder,
				n.NodeType,
				n.AssigneeType,
				assigneeID,
				n.TimeoutMinutes,
				readonlyDirs,
				fullControlDirs,
				artifact,
				maxRejectCycles,
				n.DependsOn,
			))
		}
	}

	wfSvc := service.NewWorkflowService(h.Svc)
	result, err := wfSvc.UpdateWithNodes(r.Context(), buildUpdateWorkflowTemplateParams(
		id,
		req.Name,
		req.Description,
		req.TriggerType,
		req.TriggerConfig,
		req.TriggerEnabled,
		req.NextRunAt,
	), nodeParams)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "workflow template not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, map[string]interface{}{
		"template": toTemplateResponse(result.Template),
		"nodes":    result.Nodes,
	})
}

// DeleteWorkflowTemplate 处理 DELETE /workspaces/{workspaceId}/templates/{id} 端点，删除指定的工作流模板。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 id 为模板 UUID
//
// 返回:
//   - 无返回值，成功时返回 204 No Content，失败时返回错误信息
func (h *WorkflowHandler) DeleteWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid template id")
		return
	}

	// 验证工作流模板是否属于已认证用户的工作区
	if checkWorkflowTemplateWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	wfSvc := service.NewWorkflowService(h.Svc)
	if err := wfSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
