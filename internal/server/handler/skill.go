// skill.go 提供技能（Skill）的创建、列表查询、更新和删除等 HTTP API 端点。
//
// 技能是 AI 代理可执行的特定能力，包含提示模板和分类信息。

package handler

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	apitypes "github.com/teammate/server/internal/types"
)

// SkillHandler 处理技能管理的 HTTP 请求，包括创建、查询、更新和删除技能。
type SkillHandler struct {
	Svc *service.Service
}

// NewSkillHandler 创建 SkillHandler 实例。
func NewSkillHandler(svc *service.Service) *SkillHandler {
	return &SkillHandler{Svc: svc}
}

// Routes 返回技能的完整路由表（包含读写操作）。
func (h *SkillHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateSkill)
	r.Get("/", h.ListSkills)
	r.Put("/{id}", h.UpdateSkill)
	r.Delete("/{id}", h.DeleteSkill)

	return r
}

// ReadRoutes 返回技能的只读路由表。
func (h *SkillHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListSkills)

	return r
}

// WriteRoutes 返回技能的写入路由表。
func (h *SkillHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateSkill)
	r.Put("/{id}", h.UpdateSkill)
	r.Delete("/{id}", h.DeleteSkill)

	return r
}

// createSkillRequest 创建技能请求体。
type createSkillRequest struct {
	Name           string `json:"name"`            // 技能名称
	Description    string `json:"description"`     // 技能描述
	Category       string `json:"category"`        // 技能分类
	PromptTemplate string `json:"prompt_template"` // 提示模板
}

// CreateSkill 处理 POST /workspaces/{workspaceId}/skills 端点，创建新的技能条目。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，技能名称（必填）
//   - description: string，技能描述
//   - category: string，技能分类
//   - prompt_template: string，提示模板
//
// 响应：
//   - 201: 成功创建技能
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
func (h *SkillHandler) CreateSkill(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态和写入权限
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析工作区 ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// 解析请求体
	var req createSkillRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 输入校验
	if err := validateCreateSkill(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 创建技能
	skillSvc := service.NewSkillService(h.Svc)
	skill, err := skillSvc.Create(r.Context(), buildCreateSkillParams(
		workspaceID, req.Name, req.Description, req.Category, req.PromptTemplate,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, skillResponse(skill))
}

// ListSkills 处理 GET /workspaces/{workspaceId}/skills 端点，列出工作区下的所有技能。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回技能列表
//   - 400: 工作区 ID 无效
func (h *SkillHandler) ListSkills(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// 调用 service 查询技能
	skillSvc := service.NewSkillService(h.Svc)
	skills, err := skillSvc.List(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	items := make([]apitypes.SkillResponse, 0, len(skills))
	for _, skill := range skills {
		items = append(items, skillResponse(skill))
	}
	response.JSON(w, r, items)
}

// DeleteSkill 处理 DELETE /workspaces/{workspaceId}/skills/{id} 端点，删除指定的技能条目。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 204: 成功删除
//   - 400: 技能 ID 无效
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 技能不存在
func (h *SkillHandler) DeleteSkill(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态和写入权限
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析技能 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid skill id")
		return
	}

	// 验证技能属于当前工作区
	if checkSkillWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// 调用 service 删除技能
	skillSvc := service.NewSkillService(h.Svc)
	if err := skillSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// updateSkillRequest 更新技能请求体。
type updateSkillRequest struct {
	Name           *string `json:"name,omitempty"`            // 技能名称（nil=保持）
	Description    *string `json:"description,omitempty"`     // 技能描述（nil=保持）
	Category       *string `json:"category,omitempty"`        // 技能分类（nil=保持）
	PromptTemplate *string `json:"prompt_template,omitempty"` // 提示模板（nil=保持）
}

// UpdateSkill 处理 PUT /workspaces/{workspaceId}/skills/{id} 端点，更新技能的配置信息。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - name: string，技能名称
//   - description: string，技能描述
//   - category: string，技能分类
//   - prompt_template: string，提示模板
//
// 响应：
//   - 200: 成功返回更新后的技能信息
//   - 400: 参数错误
//   - 401: 未认证
//   - 403: 无权限
//   - 404: 技能不存在
func (h *SkillHandler) UpdateSkill(w http.ResponseWriter, r *http.Request) {
	// 验证认证状态和写入权限
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// 解析技能 ID
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid skill id")
		return
	}

	// 验证技能属于当前工作区
	if checkSkillWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	// 解析请求体
	var req updateSkillRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 输入校验
	if err := validateUpdateSkill(req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 调用 service 更新技能
	skillSvc := service.NewSkillService(h.Svc)
	skill, err := skillSvc.Update(r.Context(), id, req.Name, req.Description, req.Category, req.PromptTemplate)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, skillResponse(skill))
}

// ---- 输入校验函数 ----

// validateCreateSkill 验证创建技能请求的输入合法性。
func validateCreateSkill(req createSkillRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(req.Name) > 200 {
		return fmt.Errorf("name must be at most 200 characters")
	}
	if len(req.PromptTemplate) > 50000 {
		return fmt.Errorf("prompt_template must be at most 50000 characters")
	}
	return nil
}

// validateUpdateSkill 验证更新技能请求的输入合法性。
func validateUpdateSkill(req updateSkillRequest) error {
	if req.Name != nil {
		if strings.TrimSpace(*req.Name) == "" {
			return fmt.Errorf("name must not be empty")
		}
		if len(*req.Name) > 200 {
			return fmt.Errorf("name must be at most 200 characters")
		}
	}
	if req.PromptTemplate != nil && len(*req.PromptTemplate) > 50000 {
		return fmt.Errorf("prompt_template must be at most 50000 characters")
	}
	return nil
}
