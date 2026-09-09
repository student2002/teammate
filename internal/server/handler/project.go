// project.go provides HTTP API endpoints for project CRUD management, member/reviewer management, and Git credential management.
//
// A project is a container for tasks and supports Git repository integration.
// Project roles: lead (project lead), developer (developer), reviewer (reviewer).

package handler

import (
	"database/sql"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	"github.com/teammate/server/internal/crypto"
	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// ProjectHandler handles HTTP requests for project management, including project CRUD, member and reviewer management, and Git credential management.
type ProjectHandler struct {
	Svc *service.Service
}

// NewProjectHandler creates a ProjectHandler instance.
func NewProjectHandler(svc *service.Service) *ProjectHandler {
	return &ProjectHandler{Svc: svc}
}

// checkProjectRole verifies that the currently authenticated user has the specified project-level role permission.
// On success it returns the project ID; on failure it writes an error response and returns uuid.Nil.
func checkProjectRole(svc *service.Service, w http.ResponseWriter, r *http.Request, requiredRole string) uuid.UUID {
	return checkProjectRoleByParam(svc, w, r, "id", requiredRole)
}

// checkProjectRoleByParam works the same as checkProjectRole, but reads the project ID from the specified URL parameter name.
func checkProjectRoleByParam(svc *service.Service, w http.ResponseWriter, r *http.Request, param string, requiredRole string) uuid.UUID {
	// parse project ID
	projectID, err := uuid.Parse(chi.URLParam(r, param))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return uuid.Nil
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return uuid.Nil
	}

	// Agents are not allowed to manage project settings
	if claims.UserType == "agent" {
		response.Forbidden(w, "agents cannot manage project settings")
		return uuid.Nil
	}

	// verify the project belongs to the current workspace
	if checkProjectWorkspace(svc, w, r, projectID) == nil {
		return uuid.Nil
	}

	// check project role permission
	projSvc := service.NewProjectService(svc)
	if err := projSvc.CheckMemberProjectAccess(r.Context(), claims.UserID, projectID, claims.Role, requiredRole); err != nil {
		response.Forbidden(w, err.Error())
		return uuid.Nil
	}

	return projectID
}

// Routes returns the complete route table for projects (including read and write operations).
func (h *ProjectHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateProject)
	r.Get("/", h.ListProjects)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetProject)
		r.Put("/", h.UpdateProject)
		r.Delete("/", h.DeleteProject)
		r.Post("/members", h.AddProjectMember)
		r.Get("/members", h.ListProjectMembers)
		r.Delete("/members/{memberId}", h.RemoveProjectMember)
		r.Post("/reviewers", h.AddProjectReviewer)
		r.Get("/reviewers", h.ListProjectReviewers)
		r.Delete("/reviewers/{reviewerId}", h.RemoveProjectReviewer)
	})

	return r
}

// ReadRoutes returns the read-only route table for projects.
func (h *ProjectHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListProjects)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetProject)
		r.Get("/members", h.ListProjectMembers)
		r.Get("/reviewers", h.ListProjectReviewers)
	})

	return r
}

// WriteRoutes returns the write route table for projects.
func (h *ProjectHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateProject)
	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateProject)
		r.Delete("/", h.DeleteProject)
		r.Post("/members", h.AddProjectMember)
		r.Delete("/members/{memberId}", h.RemoveProjectMember)
		r.Post("/reviewers", h.AddProjectReviewer)
		r.Delete("/reviewers/{reviewerId}", h.RemoveProjectReviewer)
	})

	return r
}

// CreateProject handles the POST /workspaces/{workspaceId}/projects endpoint, creating a new project and automatically setting the creator as the project lead.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, project name (required)
//   - description: string, project description
//   - icon: string, project icon
//   - status: string, project status, default "planned"
//   - repo_url: string, Git repository URL
//   - context: string, project context
//
// Response:
//   - 201: project created successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
func (h *ProjectHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	// verify authentication status and write permission
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	// parse workspace ID
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	// parse request body
	var req createProjectRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// project name is required
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(w, "name is required")
		return
	}

	// Git repository URL is required
	if req.RepoUrl == "" {
		response.BadRequest(w, "repo_url is required")
		return
	}

	// set default status
	status := req.Status
	if status == "" {
		status = ProjectStatusPlanned
	}

	// call service to create the project
	projSvc := service.NewProjectService(h.Svc)
	project, err := projSvc.Create(r.Context(), buildCreateProjectParams(
		workspaceID,
		req.Name,
		req.Description,
		req.Icon,
		status,
		req.RepoUrl,
		req.Context,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// automatically add the creator as the project lead
	if claims, ok := svcmw.GetAuthFromContext(r.Context()); ok && claims.UserType == "member" {
		projSvc2 := service.NewProjectService(h.Svc)
		projectUUID, _ := uuid.Parse(project.ID)
		_, _ = projSvc2.AddMember(r.Context(), buildCreateProjectMemberParams(
			projectUUID,
			"human",
			uuid.NullUUID{},
			uuid.NullUUID{UUID: claims.UserID, Valid: true},
			"lead",
		))
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, project)
}

// ListProjects handles the GET /workspaces/{workspaceId}/projects endpoint, listing projects under the workspace; Agents can only see projects they participate in.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the project list
//   - 400: invalid workspace ID
//   - 401: not authenticated
func (h *ProjectHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := uuid.Parse(chi.URLParam(r, "workspaceId"))
	if err != nil {
		response.BadRequest(w, "invalid workspace id")
		return
	}

	projSvc := service.NewProjectService(h.Svc)

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	var projects []Project
	if claims.UserType == "agent" {
		// Agents can only see projects they participate in
		projects, err = projSvc.ListByAgentMembership(r.Context(), workspaceID, claims.UserID)
	} else {
		// human users can see all projects
		projects, err = projSvc.List(r.Context(), workspaceID)
	}
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, projects)
}

// GetProject handles the GET /workspaces/{workspaceId}/projects/{id} endpoint, querying the detailed information of the specified project.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the project details
//   - 400: invalid project ID
//   - 401: not authenticated
//   - 404: project does not exist
func (h *ProjectHandler) GetProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// verify the project belongs to the current workspace
	project := checkProjectWorkspace(h.Svc, w, r, id)
	if project == nil {
		return
	}

	response.JSON(w, r, project)
}

// UpdateProject handles the PUT /workspaces/{workspaceId}/projects/{id} endpoint, updating the project configuration; requires the lead role permission.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - name: string, project name
//   - description: string, project description
//   - status: string, project status
//   - repo_url: string, Git repository URL
//   - context: string, project context
//
// Response:
//   - 200: successfully returns the updated project info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no lead role permission
//   - 404: project does not exist
func (h *ProjectHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	// verify project role permission
	id := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if id == uuid.Nil {
		return
	}

	// parse request body
	var req updateProjectRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// project name is required
	if strings.TrimSpace(req.Name) == "" {
		response.BadRequest(w, "name is required")
		return
	}

	// if status is not provided, fetch the current project to preserve the existing status
	status := req.Status
	if status == "" {
		projSvc := service.NewProjectService(h.Svc)
		existing, err := projSvc.Get(r.Context(), id)
		if err != nil {
			response.NotFound(w, "project not found")
			return
		}
		status = existing.Status
	}

	// call service to update the project
	projSvc := service.NewProjectService(h.Svc)
	project, err := projSvc.Update(r.Context(), buildUpdateProjectParams(
		id,
		req.Name,
		req.Description,
		status,
		req.RepoUrl,
		req.Context,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "project not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, project)
}

// DeleteProject handles the DELETE /workspaces/{workspaceId}/projects/{id} endpoint, deleting the specified project; requires the lead role permission.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: deleted successfully
//   - 401: not authenticated
//   - 403: no lead role permission
//   - 404: project does not exist
func (h *ProjectHandler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	// verify project role permission
	id := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if id == uuid.Nil {
		return
	}

	// call service to delete the project
	projSvc := service.NewProjectService(h.Svc)
	if err := projSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddProjectMember handles the POST /workspaces/{workspaceId}/projects/{id}/members endpoint, adding a member (human or Agent) to the project.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - member_type: string, member type ("human" or "agent")
//   - agent_id: UUID, Agent ID (required when type is agent)
//   - member_id: UUID, human user ID (required when type is human)
//   - role: string, project role (lead/developer/reviewer)
//
// Response:
//   - 201: member added successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no lead role permission
func (h *ProjectHandler) AddProjectMember(w http.ResponseWriter, r *http.Request) {
	// verify project role permission
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// parse request body
	var req addProjectMemberRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// convert optional ID fields
	var agentID uuid.NullUUID
	if req.AgentID != nil {
		agentID = uuid.NullUUID{UUID: *req.AgentID, Valid: true}
	}
	var memberID uuid.NullUUID
	if req.MemberID != nil {
		memberID = uuid.NullUUID{UUID: *req.MemberID, Valid: true}
	}

	// call service to add the member
	projSvc := service.NewProjectService(h.Svc)
	member, err := projSvc.AddMember(r.Context(), buildCreateProjectMemberParams(
		projectID,
		req.MemberType,
		agentID,
		memberID,
		req.Role,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, member)
}

// ListProjectMembers handles the GET /workspaces/{workspaceId}/projects/{id}/members endpoint, listing the project's members.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the member list
//   - 400: invalid project ID
//   - 401: not authenticated
//   - 404: project does not exist
func (h *ProjectHandler) ListProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// verify the project belongs to the current workspace
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}

	// call service to query members
	projSvc := service.NewProjectService(h.Svc)
	members, err := projSvc.ListMembers(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, members)
}

// RemoveProjectMember handles the DELETE /workspaces/{workspaceId}/projects/{id}/members/{memberId} endpoint, removing a project member.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: removed successfully
//   - 400: invalid member ID
//   - 401: not authenticated
//   - 403: no lead role permission
//   - 404: member does not exist
func (h *ProjectHandler) RemoveProjectMember(w http.ResponseWriter, r *http.Request) {
	// verify project role permission
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// parse member ID
	memberID, err := uuid.Parse(chi.URLParam(r, "memberId"))
	if err != nil {
		response.BadRequest(w, "invalid member id")
		return
	}

	// verify the member record belongs to this project
	projSvc := service.NewProjectService(h.Svc)
	member, err := projSvc.GetProjectMember(r.Context(), memberID)
	if err != nil {
		response.NotFound(w, "project member not found")
		return
	}
	if member.ProjectID != projectID.String() {
		response.NotFound(w, "project member not found")
		return
	}

	// call service to remove the member
	if err := projSvc.RemoveMember(r.Context(), memberID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// AddProjectReviewer handles the POST /workspaces/{workspaceId}/projects/{id}/reviewers endpoint, adding a reviewer to the project.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - member_type: string, member type ("human" or "agent")
//   - agent_id: UUID, Agent ID (required when type is agent)
//   - member_id: UUID, human user ID (required when type is human)
//
// Response:
//   - 201: reviewer added successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no lead role permission
func (h *ProjectHandler) AddProjectReviewer(w http.ResponseWriter, r *http.Request) {
	// verify project role permission
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// parse request body
	var req addProjectReviewerRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// convert optional ID fields
	var agentID uuid.NullUUID
	if req.AgentID != nil {
		agentID = uuid.NullUUID{UUID: *req.AgentID, Valid: true}
	}
	var memberID uuid.NullUUID
	if req.MemberID != nil {
		memberID = uuid.NullUUID{UUID: *req.MemberID, Valid: true}
	}

	// call service to add the reviewer
	projSvc := service.NewProjectService(h.Svc)
	reviewer, err := projSvc.AddReviewer(r.Context(), buildCreateProjectReviewerParams(
		projectID,
		req.MemberType,
		agentID,
		memberID,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, reviewer)
}

// ListProjectReviewers handles the GET /workspaces/{workspaceId}/projects/{id}/reviewers endpoint, listing the project's reviewers.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the reviewer list
//   - 400: invalid project ID
//   - 401: not authenticated
//   - 404: project does not exist
func (h *ProjectHandler) ListProjectReviewers(w http.ResponseWriter, r *http.Request) {
	projectID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// verify the project belongs to the current workspace
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}

	// call service to query reviewers
	projSvc := service.NewProjectService(h.Svc)
	reviewers, err := projSvc.ListReviewers(r.Context(), projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, reviewers)
}

// RemoveProjectReviewer handles the DELETE /workspaces/{workspaceId}/projects/{id}/reviewers/{reviewerId} endpoint, removing a project reviewer.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: removed successfully
//   - 400: invalid reviewer ID
//   - 401: not authenticated
//   - 403: no lead role permission
//   - 404: reviewer does not exist
func (h *ProjectHandler) RemoveProjectReviewer(w http.ResponseWriter, r *http.Request) {
	// verify project role permission
	projectID := checkProjectRole(h.Svc, w, r, types.ProjectRoleLead)
	if projectID == uuid.Nil {
		return
	}

	// parse reviewer ID
	reviewerID, err := uuid.Parse(chi.URLParam(r, "reviewerId"))
	if err != nil {
		response.BadRequest(w, "invalid reviewer id")
		return
	}

	// verify the reviewer record belongs to this project
	reviewer, err := service.NewProjectService(h.Svc).GetProjectReviewerByReviewerID(r.Context(), reviewerID)
	if err != nil {
		response.NotFound(w, "project reviewer not found")
		return
	}
	if reviewer.ProjectID != projectID.String() {
		response.NotFound(w, "project reviewer not found")
		return
	}

	// call service to remove the reviewer
	projSvc := service.NewProjectService(h.Svc)
	if err := projSvc.RemoveReviewer(r.Context(), reviewerID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- Git credential management ---

// maskPAT returns a masked PAT, showing only the last 4 characters.
func maskPAT(pat string) string {
	if len(pat) <= 4 {
		return "****"
	}
	return "****" + pat[len(pat)-4:]
}

// isFineGrainedPAT checks whether the PAT is a fine-grained/repository-scoped token.
// GitHub fine-grained PATs start with "github_pat_".
// GitHub classic PATs start with "ghp_".
// GitLab project access tokens start with "glpts-" (repository scope).
// GitLab personal access tokens start with "glpat-" (account scope).
func isFineGrainedPAT(pat string, patType string) bool {
	// explicit type declaration takes precedence
	if patType == "fine_grained" {
		return true
	}
	if patType == "classic" {
		return false
	}
	// auto-detect from the token prefix
	prefix := strings.ToLower(pat)
	return strings.HasPrefix(prefix, "github_pat_") || strings.HasPrefix(prefix, "glpts-")
}

// GitCredentialsHandler handles the GET /projects/{projectId}/git-credentials endpoint, returning the project's Git credentials.
// Agents get the encrypted version; humans get the masked version.
func GitCredentialsHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// parse project ID
		projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
		if err != nil {
			response.BadRequest(w, "invalid project id")
			return
		}

		// get auth info
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}

		// Agent permission verification
		if claims.UserType == "agent" {
			// the Agent must be an explicit member of the project
			projSvc := service.NewProjectService(svc)
			if err := projSvc.CheckAgentProjectAccess(r.Context(), claims.UserID, projectID); err != nil {
				response.Forbidden(w, err.Error())
				return
			}
			// the Agent must have the git:push permission to read credentials
			permSvc := service.NewAgentPermissionService(svc)
			has, err := permSvc.HasPermission(r.Context(), claims.UserID, types.PermGitPush)
			if err != nil || !has {
				response.Forbidden(w, "agent lacks git:push permission")
				return
			}
		}

		// query Git credentials
		authSvc := service.NewAuthService(svc, "")
		credentials, err := authSvc.GetGitCredentialsByProject(r.Context(), projectID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		// Agent path: decrypt the AES-encrypted PAT, then re-encrypt with the Agent's public key
		if claims.UserType == "agent" {
			agentID := claims.UserID

			// get the Agent's latest public key
			publicKeyPEM, err := authSvc.GetLatestPublicKeyForAgent(r.Context(), agentID)
			if err != nil {
				response.NotFound(w, fmt.Sprintf("no public key found for agent: %v", err))
				return
			}

			// parse the public key
			pubKey, err := crypto.ParsePublicKey([]byte(publicKeyPEM))
			if err != nil {
				response.InternalServerError(w, err)
				return
			}


			// process each credential
			result := make([]encryptedCredential, 0, len(credentials))
			for _, cred := range credentials {
				// first decrypt the stored AES-encrypted PAT
				plainPAT, err := crypto.DecryptPAT(cred.EncryptedPAT)
				if err != nil {
					response.InternalServerError(w, err)
					return
				}

				// then re-encrypt with the Agent's RSA public key
				encrypted, err := crypto.EncryptWithPublicKey(pubKey, []byte(plainPAT))
				if err != nil {
					response.InternalServerError(w, err)
					return
				}

				credID, _ := uuid.Parse(cred.ID)
				result = append(result, encryptedCredential{
					ID:           credID,
					RepoUrl:      cred.RepoURL,
					Username:     cred.Username,
					EncryptedPAT: base64.StdEncoding.EncodeToString(encrypted),
				})
			}

			// return the encrypted credentials
			response.JSON(w, r, map[string]interface{}{
				"credentials": result,
			})
			return
		}

		// human user path: decrypt the PAT for masked display

		// process each credential
		result := make([]maskedCredential, 0, len(credentials))
		for _, cred := range credentials {
			var createdBy *uuid.UUID
			if cred.CreatedBy != nil {
				if cb, err := uuid.Parse(*cred.CreatedBy); err == nil {
					createdBy = &cb
				}
			}
			// decrypt the AES-encrypted PAT for masking
			plainPAT, err := crypto.DecryptPAT(cred.EncryptedPAT)
			if err != nil {
				response.InternalServerError(w, err)
				return
			}
			credID, _ := uuid.Parse(cred.ID)
			result = append(result, maskedCredential{
				ID:        credID,
				RepoUrl:   cred.RepoURL,
				Username:  cred.Username,
				MaskedPAT: maskPAT(plainPAT),
				CreatedBy: createdBy,
				CreatedAt: cred.CreatedAt,
				UpdatedAt: cred.UpdatedAt,
			})
		}

		// return the masked credentials
		response.JSON(w, r, map[string]interface{}{
			"credentials": result,
		})
	}
}

// CreateGitCredentialHandler handles the POST /projects/{projectId}/git-credentials endpoint, creating the project's Git credentials; the PAT is stored encrypted with AES-256-GCM.
func CreateGitCredentialHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// verify project role permission
		projectID := checkProjectRoleByParam(svc, w, r, "projectId", types.ProjectRoleDeveloper)
		if projectID == uuid.Nil {
			return
		}

		// get auth info
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}

		// Agents are not allowed to create Git credentials
		if claims.UserType == "agent" {
			response.Forbidden(w, "agents cannot create git credentials")
			return
		}

		// parse request body
		var req createGitCredentialRequest
		if err := render.Decode(r, &req); err != nil {
			response.BadRequest(w, err.Error())
			return
		}

		// verify required fields
		if req.RepoUrl == "" {
			response.BadRequest(w, "repo_url is required")
			return
		}
		if req.PAT == "" {
			response.BadRequest(w, "pat is required")
			return
		}

		// validate PAT scope: warn if using a classic (account-scoped) token
		if !isFineGrainedPAT(req.PAT, req.PATType) {
			log.Printf("[git-credentials] WARNING: project %s is using a classic/broad-scope PAT for %s — "+
				"this token can access ALL repositories. Consider using a fine-grained/project-scoped token instead.",
				projectID, req.RepoUrl)
		}

		// set default username
		username := req.Username
		if username == "" {
			username = "git"
		}

		// encrypt the PAT with AES-256-GCM
		encryptedPAT, err := crypto.EncryptPAT(req.PAT)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		// call service to create the credential
		authSvc := service.NewAuthService(svc, "")
		credential, err := authSvc.CreateGitCredential(r.Context(), buildCreateGitCredentialParams(
			projectID,
			req.RepoUrl,
			username,
			encryptedPAT,
			uuid.NullUUID{UUID: claims.UserID, Valid: true},
		))
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		// prepare the scope warning
		scopeWarning := ""
		if !isFineGrainedPAT(req.PAT, req.PATType) {
			scopeWarning = "This token has account-wide access. For better security, use a fine-grained/project-scoped token that only has access to this repository."
		}


		var createdBy *uuid.UUID
		if credential.CreatedBy != nil {
			if cb, err := uuid.Parse(*credential.CreatedBy); err == nil {
				createdBy = &cb
			}
		}

		// decrypt the PAT for masked display
		plainPAT, _ := crypto.DecryptPAT(credential.EncryptedPAT)

		// return the creation result
		w.WriteHeader(http.StatusCreated)
		credID, _ := uuid.Parse(credential.ID)
		credPID, _ := uuid.Parse(credential.ProjectID)
		response.JSON(w, r, credentialResponse{
			ID:           credID,
			ProjectID:    credPID,
			RepoUrl:      credential.RepoURL,
			Username:     credential.Username,
			MaskedPAT:    maskPAT(plainPAT),
			ScopeWarning: scopeWarning,
			CreatedBy:    createdBy,
			CreatedAt:    credential.CreatedAt,
			UpdatedAt:    credential.UpdatedAt,
		})
	}
}

// UpdateGitCredentialHandler handles the PUT /projects/{projectId}/git-credentials/{credentialId} endpoint, updating the project's Git credentials.
func UpdateGitCredentialHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// verify project role permission
		projectID := checkProjectRoleByParam(svc, w, r, "projectId", types.ProjectRoleDeveloper)
		if projectID == uuid.Nil {
			return
		}

		// get auth info
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "not authenticated")
			return
		}

		// Agents are not allowed to update Git credentials
		if claims.UserType == "agent" {
			response.Forbidden(w, "agents cannot update git credentials")
			return
		}

		// parse credential ID
		credentialID, err := uuid.Parse(chi.URLParam(r, "credentialId"))
		if err != nil {
			response.BadRequest(w, "invalid credential id")
			return
		}

		// verify the credential belongs to this project
		authSvc := service.NewAuthService(svc, "")
		existing, err := authSvc.GetGitCredential(r.Context(), credentialID)
		if err != nil {
			response.NotFound(w, "git credential not found")
			return
		}
		if existing.ProjectID != projectID.String() {
			response.NotFound(w, "git credential not found")
			return
		}

		// parse request body
		var req updateGitCredentialRequest
		if err := render.Decode(r, &req); err != nil {
			response.BadRequest(w, err.Error())
			return
		}

		// use existing values as defaults
		repoUrl := existing.RepoURL
		if req.RepoUrl != "" {
			repoUrl = req.RepoUrl
		}
		username := existing.Username
		if req.Username != "" {
			username = req.Username
		}
		encryptedPAT := existing.EncryptedPAT
		if req.PAT != "" {
			// encrypt the new PAT with AES-256-GCM
			var err error
			encryptedPAT, err = crypto.EncryptPAT(req.PAT)
			if err != nil {
				response.InternalServerError(w, err)
				return
			}
		}

		// call service to update the credential
		credential, err := authSvc.UpdateGitCredential(r.Context(), buildUpdateGitCredentialParams(
			credentialID,
			repoUrl,
			username,
			encryptedPAT,
		))
		if err != nil {
			response.InternalServerError(w, err)
			return
		}


		var createdBy *uuid.UUID
		if credential.CreatedBy != nil {
			if cb, err := uuid.Parse(*credential.CreatedBy); err == nil {
				createdBy = &cb
			}
		}

		// decrypt the PAT for masked display
		plainPAT, _ := crypto.DecryptPAT(credential.EncryptedPAT)

		// return the update result
		updID, _ := uuid.Parse(credential.ID)
		updPID, _ := uuid.Parse(credential.ProjectID)
		response.JSON(w, r, credentialResponse{
			ID:        updID,
			ProjectID: updPID,
			RepoUrl:   credential.RepoURL,
			Username:  credential.Username,
			MaskedPAT: maskPAT(plainPAT),
			CreatedBy: createdBy,
			CreatedAt: credential.CreatedAt,
			UpdatedAt: credential.UpdatedAt,
		})
	}
}
