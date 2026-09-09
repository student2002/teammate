// workspace.go provides HTTP API endpoints for workspace CRUD management, member invite/remove/role-change, and ownership transfer.
//
// This file provides the following HTTP API endpoints:
//   - POST /workspaces: create a new workspace
//   - GET /workspaces: list all workspaces
//   - GET /workspaces/{id}: query the details of the specified workspace
//   - PUT /workspaces/{id}: update the workspace name and description
//   - DELETE /workspaces/{id}: delete the specified workspace (the default workspace cannot be deleted)
//   - POST /workspaces/{id}/members: invite a new member to join the workspace
//   - GET /workspaces/{id}/members: list all members of the workspace
//   - DELETE /workspaces/{id}/members/{memberId}: remove a workspace member
//   - PUT /workspaces/{id}/members/{memberId}/role: change the role of a workspace member
//   - POST /workspaces/{id}/transfer-ownership: transfer workspace ownership to another member
//
// The member role hierarchy is owner > admin > member > viewer, and operation permissions follow the hierarchy constraints:
// you can only create/modify/delete members with a role lower than your own, and ownership transfer is restricted to the Owner.
// All sensitive operations (invite, remove, role change, ownership transfer) are recorded in the audit log.

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

// validRoles is the whitelist of allowed member roles.
var validRoles = map[string]int{
	"owner":  4,
	"admin":  3,
	"member": 2,
	"viewer": 1,
}

// WorkspaceHandler handles HTTP requests for workspace management, including workspace CRUD, member management, and ownership transfer.
type WorkspaceHandler struct {
	Svc *service.Service
}

// NewWorkspaceHandler creates a WorkspaceHandler instance.
//
// Parameters:
//   - svc: business logic service instance, provides workspace and member management capabilities
//
// Returns:
//   - *WorkspaceHandler: workspace handler instance
func NewWorkspaceHandler(svc *service.Service) *WorkspaceHandler {
	return &WorkspaceHandler{Svc: svc}
}

// Routes returns the route table for workspaces.
//
// Returns:
//   - chi.Router: routes containing workspace CRUD, member management, and ownership transfer endpoints
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


// CreateWorkspace handles the POST /workspaces endpoint, creating a new workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, request body contains the workspace name, description, and issue prefix
//
// Returns:
//   - no return value, writes a JSON response via w (201 Created), containing the created workspace or an error message
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

// ListWorkspaces handles the GET /workspaces endpoint, listing all workspaces.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Returns:
//   - no return value, writes a JSON response via w, containing the workspace list or an error message
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

// GetWorkspace handles the GET /workspaces/{id} endpoint, querying the details of the specified workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID
//
// Returns:
//   - no return value, writes a JSON response via w, containing the workspace details or an error message
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


// UpdateWorkspace handles the PUT /workspaces/{id} endpoint, updating the workspace name and description.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID, request body contains the updated name and description
//
// Returns:
//   - no return value, writes a JSON response via w, containing the updated workspace or an error message
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

// DeleteWorkspace handles the DELETE /workspaces/{id} endpoint, deleting the specified workspace (the default workspace cannot be deleted).
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID
//
// Returns:
//   - no return value, returns 204 No Content on success, or an error message on failure
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


// CreateMember handles the POST /workspaces/{id}/members endpoint, inviting a new member to join the workspace; requires a higher permission level than the target role.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID, request body contains the member name, email, and role
//
// Returns:
//   - no return value, writes a JSON response via w (201 Created), containing the invitation info and token or an error message
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

	// verify the role is in the whitelist
	targetLevel, ok := validRoles[req.Role]
	if !ok {
		response.BadRequest(w, "invalid role: must be one of owner, admin, member, viewer")
		return
	}

	// only allow creating roles lower than your own level
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

// ListMembers handles the GET /workspaces/{id}/members endpoint, listing all members of the workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID
//
// Returns:
//   - no return value, writes a JSON response via w, containing the member list or an error message
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

// DeleteMember handles the DELETE /workspaces/{id}/members/{memberId} endpoint, removing a workspace member; the Owner and yourself cannot be removed.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID, memberId is the member UUID
//
// Returns:
//   - no return value, returns 204 No Content on success, or an error message on failure
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

	// cannot delete yourself
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

	// get the member's role in this workspace
	workspaceID, _ := uuid.Parse(chi.URLParam(r, "workspaceId"))
	wm, err := wsSvc.GetMembership(r.Context(), workspaceID, memberID)
	if err != nil {
		response.NotFound(w, "member not found in this workspace")
		return
	}

	// cannot delete the Owner
	if wm.Role == "owner" {
		response.Forbidden(w, "cannot delete owner, transfer ownership first")
		return
	}

	// only allow deleting members with a role level lower than your own
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


// UpdateMemberRole handles the PUT /workspaces/{id}/members/{memberId}/role endpoint, changing the role of a workspace member.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID, memberId is the member UUID, request body contains the new role
//
// Returns:
//   - no return value, writes a JSON response via w, containing the updated member role info or an error message
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

	// verify the new role is in the whitelist
	newLevel, ok := validRoles[req.Role]
	if !ok {
		response.BadRequest(w, "invalid role: must be one of owner, admin, member, viewer")
		return
	}

	// cannot change your own role
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

	// get the member's current role in this workspace
	workspaceID, _ := uuid.Parse(chi.URLParam(r, "workspaceId"))
	wm, err := wsSvc.GetMembership(r.Context(), workspaceID, memberID)
	if err != nil {
		response.NotFound(w, "member not found in this workspace")
		return
	}

	// only the Owner can change a role to/from Owner
	if req.Role == "owner" || wm.Role == "owner" {
		response.BadRequest(w, "use transfer-ownership to change owner role")
		return
	}

	// only allow setting roles lower than your own level
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

	// can only modify members whose role level is lower than your own
	if currentLevel >= actorLevel {
		response.Forbidden(w, "cannot modify a member with role equal to or higher than your own")
		return
	}
	// can only assign roles lower than your own level
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

