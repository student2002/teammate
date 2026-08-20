// community.go 提供社区工作流模板的创建、列表查询和导入等 HTTP API 端点。
//
// 社区工作流模板是全局共享的，可被导入到任意工作区。

package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// CommunityHandler 处理社区工作流模板的 HTTP 请求，包括创建、查询和导入社区工作流。
type CommunityHandler struct {
	Svc *service.Service
}

// NewCommunityHandler 创建 CommunityHandler 实例。
func NewCommunityHandler(svc *service.Service) *CommunityHandler {
	return &CommunityHandler{Svc: svc}
}

// Routes 返回社区工作流的路由表。
func (h *CommunityHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateCommunityWorkflow)
	r.Get("/", h.ListCommunityWorkflows)
	r.Post("/{id}/import", h.ImportCommunityWorkflow)

	return r
}

// createCommunityWorkflowRequest 创建社区工作流请求体。
type createCommunityWorkflowRequest struct {
	Name                         string          `json:"name"`                          // 工作流名称
	Description                  string          `json:"description"`                   // 工作流描述
	Author                       string          `json:"author"`                        // 作者
	Version                      string          `json:"version"`                       // 版本号
	WorkflowDefinition           json.RawMessage `json:"workflow_definition"`            // 工作流定义（JSON）
	RequiredSkills               json.RawMessage `json:"required_skills"`               // 所需技能（JSON）
	RequiredMcpServers           json.RawMessage `json:"required_mcp_servers"`           // 所需 MCP 服务器（JSON）
	RecommendedAgentInstructions json.RawMessage `json:"recommended_agent_instructions"` // 推荐代理指令（JSON）
	IsOfficial                   bool            `json:"is_official"`                   // 是否官方模板
}

// CreateCommunityWorkflow 处理 POST /community-workflows 端点，创建新的社区工作流模板。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，工作流名称（必填）
//   - description: string，工作流描述
//   - author: string，作者
//   - version: string，版本号，默认 "1.0.0"
//   - workflow_definition: object，工作流定义
//   - required_skills: object，所需技能
//   - required_mcp_servers: object，所需 MCP 服务器
//   - recommended_agent_instructions: object，推荐代理指令
//   - is_official: bool，是否官方模板
//
// 响应：
//   - 201: 成功创建社区工作流
//   - 400: 参数错误
func (h *CommunityHandler) CreateCommunityWorkflow(w http.ResponseWriter, r *http.Request) {
	// 解析请求体
	var req createCommunityWorkflowRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 设置默认版本
	version := req.Version
	if version == "" {
		version = "1.0.0"
	}

	// 转换可选的 JSON 字段
	var requiredSkills pqtype.NullRawMessage
	if req.RequiredSkills != nil {
		requiredSkills = pqtype.NullRawMessage{RawMessage: req.RequiredSkills, Valid: true}
	}
	var requiredMcpServers pqtype.NullRawMessage
	if req.RequiredMcpServers != nil {
		requiredMcpServers = pqtype.NullRawMessage{RawMessage: req.RequiredMcpServers, Valid: true}
	}
	var recommendedAgentInstructions pqtype.NullRawMessage
	if req.RecommendedAgentInstructions != nil {
		recommendedAgentInstructions = pqtype.NullRawMessage{RawMessage: req.RecommendedAgentInstructions, Valid: true}
	}

	// 调用 service 创建社区工作流
	commSvc := service.NewCommunityService(h.Svc)
	workflow, err := commSvc.Create(r.Context(), buildCreateCommunityWorkflowParams(
		req.Name, req.Description, req.Author, version, req.WorkflowDefinition,
		requiredSkills, requiredMcpServers, recommendedAgentInstructions, req.IsOfficial,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, workflow)
}

// ListCommunityWorkflows 处理 GET /community-workflows 端点，列出所有社区工作流模板。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回社区工作流列表
func (h *CommunityHandler) ListCommunityWorkflows(w http.ResponseWriter, r *http.Request) {
	// 调用 service 查询社区工作流
	commSvc := service.NewCommunityService(h.Svc)
	workflows, err := commSvc.List(r.Context())
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, workflows)
}

// importCommunityWorkflowRequest 导入社区工作流请求体。
type importCommunityWorkflowRequest struct {
	WorkspaceID uuid.UUID `json:"workspace_id"` // 目标工作区 ID
}

// ImportCommunityWorkflow 处理 POST /community-workflows/{id}/import 端点，将社区工作流导入到指定工作区。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - workspace_id: UUID，目标工作区 ID（必填）
//
// 响应：
//   - 201: 成功导入，返回创建的模板和源工作流
//   - 400: 参数错误
//   - 404: 社区工作流不存在
func (h *CommunityHandler) ImportCommunityWorkflow(w http.ResponseWriter, r *http.Request) {
	// 解析工作流 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid workflow id")
		return
	}

	// 解析请求体
	var req importCommunityWorkflowRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 导入工作流
	commSvc := service.NewCommunityService(h.Svc)
	result, err := commSvc.ImportWorkflow(r.Context(), id, req.WorkspaceID)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "not found") {
			response.NotFound(w, errMsg)
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// 返回导入结果
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"template":        result.Template,
		"source_workflow": result.SourceWorkflow,
	})
}
