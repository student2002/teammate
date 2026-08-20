// workspace.go 提供工作区的 CRUD 管理、成员邀请/移除/角色变更及所有权转移等 HTTP API 端点。
//
// 本文件提供以下 HTTP API 端点：
//   - POST /workspaces: 创建新的工作区
//   - GET /workspaces: 列出所有工作区
//   - GET /workspaces/{id}: 查询指定工作区的详细信息
//   - PUT /workspaces/{id}: 更新工作区的名称和描述
//   - DELETE /workspaces/{id}: 删除指定工作区（默认工作区不允许删除）
//   - POST /workspaces/{id}/members: 邀请新成员加入工作区
//   - GET /workspaces/{id}/members: 列出工作区的所有成员
//   - DELETE /workspaces/{id}/members/{memberId}: 移除工作区成员
//   - PUT /workspaces/{id}/members/{memberId}/role: 变更工作区成员的角色
//   - POST /workspaces/{id}/transfer-ownership: 将工作区所有权转移给其他成员
//
// 成员角色层级为 owner > admin > member > viewer，操作权限遵循层级约束：
// 只能创建/修改/删除比自身角色更低的成员，所有权转移仅限 Owner 操作。
// 所有敏感操作（邀请、移除、角色变更、所有权转移）均记录审计日志。

package handler

import (
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// validRoles 是允许的成员角色白名单。
var validRoles = map[string]int{
	"owner":  4,
	"admin":  3,
	"member": 2,
	"viewer": 1,
}

// WorkspaceHandler 处理工作区管理的 HTTP 请求，包括工作区的 CRUD、成员管理及所有权转移。
type WorkspaceHandler struct {
	Svc *service.Service
}

// NewWorkspaceHandler 创建 WorkspaceHandler 实例。
//
// 参数:
//   - svc: 业务逻辑服务实例，提供工作区和成员管理能力
//
// 返回:
//   - *WorkspaceHandler: 工作区处理器实例
func NewWorkspaceHandler(svc *service.Service) *WorkspaceHandler {
	return &WorkspaceHandler{Svc: svc}
}

// Routes 返回工作区的路由表。
//
// 返回:
//   - chi.Router: 包含工作区 CRUD、成员管理和所有权转移端点的路由
func (h *WorkspaceHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateWorkspace)
	r.Get("/", h.ListWorkspaces)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetWorkspace)
		r.Put("/", h.UpdateWorkspace)
		r.Delete("/", h.DeleteWorkspace)
		r.Post("/members", h.CreateMember)
		r.Get("/members", h.ListMembers)
		r.Delete("/members/{memberId}", h.DeleteMember)
		r.Put("/members/{memberId}/role", h.UpdateMemberRole)
	})

	return r
}


// CreateWorkspace 处理 POST /workspaces 端点，创建新的工作区。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，请求体包含工作区名称、描述和 Issue 前缀
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应（201 Created），包含创建的工作区或错误信息
func (h *WorkspaceHandler) CreateWorkspace(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if claims.UserType != "member" {
		response.Forbidden(w, "only members can create workspaces")
		return
	}

	var req createWorkspaceRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	workspace, err := wsSvc.CreateForMember(r.Context(), claims.UserID, buildCreateWorkspaceParams(
		req.Name,
		req.Description,
		req.IssuePrefix,
		false,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, workspace)
}

// ListWorkspaces 处理 GET /workspaces 端点，列出所有工作区。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含工作区列表或错误信息
func (h *WorkspaceHandler) ListWorkspaces(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if claims.UserType != "member" {
		response.Forbidden(w, "only members can list workspaces")
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	workspaces, err := wsSvc.ListForMember(r.Context(), claims.UserID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, workspaces)
}

// GetWorkspace 处理 GET /workspaces/{id} 端点，查询指定工作区的详细信息。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含工作区详情或错误信息
func (h *WorkspaceHandler) GetWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	workspace, err := wsSvc.Get(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "workspace not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, workspace)
}


// UpdateWorkspace 处理 PUT /workspaces/{id} 端点，更新工作区的名称和描述。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID，请求体包含更新后的名称和描述
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含更新后的工作区或错误信息
func (h *WorkspaceHandler) UpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	var req updateWorkspaceRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	workspace, err := wsSvc.Update(r.Context(), buildUpdateWorkspaceParams(
		id,
		req.Name,
		req.Description,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "workspace not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, workspace)
}

// DeleteWorkspace 处理 DELETE /workspaces/{id} 端点，删除指定工作区（默认工作区不允许删除）。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID
//
// 返回:
//   - 无返回值，成功时返回 204 No Content，失败时返回错误信息
func (h *WorkspaceHandler) DeleteWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	wsCtx, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return
	}
	if claims.UserType != "member" || wsCtx.Role != "owner" {
		response.Forbidden(w, "only workspace owner can delete workspace")
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	if _, err := wsSvc.Get(r.Context(), id); err != nil {
		response.NotFound(w, "workspace not found")
		return
	}
	workspaces, err := wsSvc.ListForMember(r.Context(), claims.UserID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	if len(workspaces) <= 1 {
		response.Forbidden(w, "cannot delete your last workspace")
		return
	}

	if err := wsSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}


// CreateMember 处理 POST /workspaces/{id}/members 端点，邀请新成员加入工作区，需要比目标角色更高的权限。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID，请求体包含成员姓名、邮箱和角色
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应（201 Created），包含邀请信息和令牌或错误信息
func (h *WorkspaceHandler) CreateMember(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req createMemberRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 校验角色是否在白名单中
	targetLevel, ok := validRoles[req.Role]
	if !ok {
		response.BadRequest(w, "invalid role: must be one of owner, admin, member, viewer")
		return
	}

	// 只允许创建低于自己等级的角色
	actorLevel, ok := validRoles[claims.Role]
	if !ok {
		response.Forbidden(w, "invalid actor role")
		return
	}
	if targetLevel >= actorLevel {
		response.Forbidden(w, "cannot create a member with role equal to or higher than your own")
		return
	}

	invSvc := service.NewInvitationService(h.Svc)
	inv, token, err := invSvc.Create(r.Context(), workspaceID, req.Email, req.Role, claims.UserID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"invitation":       inv,
		"invitation_token": token,
	})

	auditSvc := service.NewAuditService(h.Svc)
	if err := auditSvc.Log(r.Context(), service.AuditLogEntry{
		WorkspaceID:  workspaceID,
		ActorType:    claims.UserType,
		ActorID:      claims.UserID,
		Action:       "member.invite",
		ResourceType: "member",
		ResourceID:   inv.ID,
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	}); err != nil {
		slog.Warn("audit log write failed", "err", err)
	}
}

// ListMembers 处理 GET /workspaces/{id}/members 端点，列出工作区的所有成员。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含成员列表或错误信息
func (h *WorkspaceHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	members, err := wsSvc.ListMembers(r.Context(), workspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, members)
}

// DeleteMember 处理 DELETE /workspaces/{id}/members/{memberId} 端点，移除工作区成员，不可移除 Owner 和自身。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID，memberId 为成员 UUID
//
// 返回:
//   - 无返回值，成功时返回 204 No Content，失败时返回错误信息
func (h *WorkspaceHandler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		response.BadRequest(w, "invalid member id")
		return
	}

	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// 不能删除自己
	if claims.UserID == memberID {
		response.BadRequest(w, "cannot delete yourself")
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	_, err = wsSvc.GetMember(r.Context(), memberID)
	if err != nil {
		response.NotFound(w, "member not found")
		return
	}

	// 获取成员在该工作区中的角色
	workspaceID, _ := uuid.Parse(chi.URLParam(r, "workspaceId"))
	wm, err := wsSvc.GetMembership(r.Context(), workspaceID, memberID)
	if err != nil {
		response.NotFound(w, "member not found in this workspace")
		return
	}

	// 不能删除 Owner
	if wm.Role == "owner" {
		response.Forbidden(w, "cannot delete owner, transfer ownership first")
		return
	}

	// 只允许删除角色等级低于自己的成员
	actorLevel, ok := validRoles[claims.Role]
	if !ok {
		response.Forbidden(w, "invalid actor role")
		return
	}
	targetLevel, ok := validRoles[wm.Role]
	if !ok {
		response.Forbidden(w, "invalid target role")
		return
	}
	if targetLevel >= actorLevel {
		response.Forbidden(w, "cannot delete a member with role equal to or higher than your own")
		return
	}

	if err := wsSvc.DeleteMember(r.Context(), memberID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	workspaceID, _ = uuid.Parse(chi.URLParam(r, "workspaceId"))
	auditSvc := service.NewAuditService(h.Svc)
	if err := auditSvc.Log(r.Context(), service.AuditLogEntry{
		WorkspaceID:  workspaceID,
		ActorType:    claims.UserType,
		ActorID:      claims.UserID,
		Action:       "member.remove",
		ResourceType: "member",
		ResourceID:   memberID.String(),
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	}); err != nil {
		slog.Warn("audit log write failed", "err", err)
	}

	w.WriteHeader(http.StatusNoContent)
}


// UpdateMemberRole 处理 PUT /workspaces/{id}/members/{memberId}/role 端点，变更工作区成员的角色。
//
// 参数:
//   - w: HTTP 响应写入器
//   - r: HTTP 请求，路径参数 workspaceId 为工作区 UUID，memberId 为成员 UUID，请求体包含新角色
//
// 返回:
//   - 无返回值，通过 w 写入 JSON 响应，包含更新后的成员角色信息或错误信息
func (h *WorkspaceHandler) UpdateMemberRole(w http.ResponseWriter, r *http.Request) {
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		response.BadRequest(w, "invalid member id")
		return
	}

	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req updateMemberRoleRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 校验新角色是否在白名单中
	newLevel, ok := validRoles[req.Role]
	if !ok {
		response.BadRequest(w, "invalid role: must be one of owner, admin, member, viewer")
		return
	}

	// 不能修改自己的角色
	if claims.UserID == memberID {
		response.BadRequest(w, "cannot change your own role")
		return
	}

	wsSvc := service.NewWorkspaceService(h.Svc)
	_, err = wsSvc.GetMember(r.Context(), memberID)
	if err != nil {
		response.NotFound(w, "member not found")
		return
	}

	// 获取成员在该工作区中的当前角色
	workspaceID, _ := uuid.Parse(chi.URLParam(r, "workspaceId"))
	wm, err := wsSvc.GetMembership(r.Context(), workspaceID, memberID)
	if err != nil {
		response.NotFound(w, "member not found in this workspace")
		return
	}

	// 只有 Owner 可以将角色变更到/变更自 Owner
	if req.Role == "owner" || wm.Role == "owner" {
		response.BadRequest(w, "use transfer-ownership to change owner role")
		return
	}

	// 只允许设置低于自己等级的角色
	actorLevel, ok := validRoles[claims.Role]
	if !ok {
		response.Forbidden(w, "invalid actor role")
		return
	}
	currentLevel, ok := validRoles[wm.Role]
	if !ok {
		response.Forbidden(w, "invalid target role")
		return
	}

	// 只能修改角色等级低于自己的成员
	if currentLevel >= actorLevel {
		response.Forbidden(w, "cannot modify a member with role equal to or higher than your own")
		return
	}
	// 只能分配低于自己等级的角色
	if newLevel >= actorLevel {
		response.Forbidden(w, "cannot assign a role equal to or higher than your own")
		return
	}

	wmResult, err := wsSvc.UpdateMemberRole(r.Context(), buildUpdateMemberRoleParams(
		workspaceID,
		req.Role,
		memberID,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	auditSvc := service.NewAuditService(h.Svc)
	if err := auditSvc.Log(r.Context(), service.AuditLogEntry{
		WorkspaceID:  workspaceID,
		ActorType:    claims.UserType,
		ActorID:      claims.UserID,
		Action:       "role.change",
		ResourceType: "member",
		ResourceID:   memberID.String(),
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
	}); err != nil {
		slog.Warn("audit log write failed", "err", err)
	}

	response.JSON(w, r, wmResult)
}

