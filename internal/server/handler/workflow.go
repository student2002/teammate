// workflow.go provides HTTP API endpoints for creating, listing, updating, and deleting workflow templates.
//
// This file provides the following HTTP API endpoints:
//   - POST /workspaces/{workspaceId}/templates: create a new workflow template and its ordered node definitions
//   - GET /workspaces/{workspaceId}/templates: list all workflow templates under the workspace
//   - GET /workspaces/{workspaceId}/templates/{id}: query the details of the specified workflow template
//   - PUT /workspaces/{workspaceId}/templates/{id}: update the workflow template and its node definitions
//   - DELETE /workspaces/{workspaceId}/templates/{id}: delete the specified workflow template
//
// Workflow templates define the ordered node flow of task execution (e.g. implement -> self-test -> review -> deploy).
// Node types include standard (AI execution), review (review), and manual (human execution).
// All write operations require write permission, and template ownership is verified through workspace isolation.

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

// WorkflowHandler handles HTTP requests for workflow template management, including creating, querying, updating, and deleting templates.
type WorkflowHandler struct {
	Svc *service.Service
}

// NewWorkflowHandler creates a WorkflowHandler instance.
//
// Parameters:
//   - svc: business logic service instance, provides workflow template management capabilities
//
// Returns:
//   - *WorkflowHandler: workflow handler instance
func NewWorkflowHandler(svc *service.Service) *WorkflowHandler {
	return &WorkflowHandler{Svc: svc}
}

// Routes returns the complete route table for workflow templates (including read and write operations).
//
// Returns:
//   - chi.Router: routes containing workflow template CRUD endpoints
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

// ReadRoutes returns the read-only route table for workflow templates.
//
// Returns:
//   - chi.Router: routes containing only query endpoints
func (h *WorkflowHandler) ReadRoutes() chi.Router {
	r := chi.NewRouter()

	r.Get("/", h.ListWorkflowTemplates)

	return r
}

// WriteRoutes returns the write route table for workflow templates.
//
// Returns:
//   - chi.Router: routes containing only create, update, and delete endpoints
func (h *WorkflowHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateWorkflowTemplate)
	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateWorkflowTemplate)
		r.Delete("/", h.DeleteWorkflowTemplate)
	})

	return r
}

// CreateWorkflowTemplate handles the POST /workspaces/{workspaceId}/templates endpoint, creating a new workflow template and its ordered node definitions.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID, request body contains the template and node definitions
//
// Returns:
//   - no return value, writes a JSON response via w (201 Created), containing the created template and node data or an error message
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

	// build node parameters
	// fallback-fix missing/duplicate sort_order to avoid hitting UNIQUE(template_id, sort_order) (deviation #8)
	normalizeTemplateSortOrder(req.Nodes)
	nodeParams := make([]CreateTemplateNodeParams, 0, len(req.Nodes))
	for _, n := range req.Nodes {
		var assigneeID uuid.NullUUID
		if n.AssigneeID != nil {
			// verify the assignee belongs to the current workspace
			if n.AssigneeType == AssigneeTypeSpecificAgent {
				agent := checkAgentWorkspace(h.Svc, w, r, *n.AssigneeID)
				if agent == nil {
					return
				}
			} else if n.AssigneeType == AssigneeTypeHuman {
				// verify the member belongs to the current workspace
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

// ListWorkflowTemplates handles the GET /workspaces/{workspaceId}/templates endpoint, listing all workflow templates and their nodes under the workspace.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID
//
// Returns:
//   - no return value, writes a JSON response via w, containing the template list or an error message
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

	// convert to the response format
	resp := make([]templateWithNodes, 0, len(results))
	for _, r := range results {
		resp = append(resp, templateWithNodes{
			templateResponse: toTemplateResponse(r.Template),
			Nodes:            r.Nodes,
		})
	}

	response.JSON(w, r, resp)
}

// GetWorkflowTemplate handles the GET /workspaces/{workspaceId}/templates/{id} endpoint, querying the details of the specified workflow template and its nodes.
//
// Parameters:
// UpdateWorkflowTemplate handles the PUT /workspaces/{workspaceId}/templates/{id} endpoint, updating the workflow template and its node definitions.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter id is the template UUID, request body contains the updated template and node definitions
//
// Returns:
//   - no return value, writes a JSON response via w, containing the updated template and node data or an error message
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

	// verify the workflow template belongs to the authenticated user's workspace
	if checkWorkflowTemplateWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	var req updateWorkflowTemplateRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// if nodes are provided, build the node parameters
	var nodeParams []CreateTemplateNodeParams
	if req.Nodes != nil {
		// fallback-fix missing/duplicate sort_order to avoid hitting UNIQUE(template_id, sort_order) (deviation #8)
		normalizeTemplateSortOrder(req.Nodes)
		nodeParams = make([]CreateTemplateNodeParams, 0, len(req.Nodes))
		for _, n := range req.Nodes {
			var assigneeID uuid.NullUUID
			if n.AssigneeID != nil {
				// verify the assignee belongs to the current workspace
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

// DeleteWorkflowTemplate handles the DELETE /workspaces/{workspaceId}/templates/{id} endpoint, deleting the specified workflow template.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter id is the template UUID
//
// Returns:
//   - no return value, returns 204 No Content on success, or an error message on failure
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

	// verify the workflow template belongs to the authenticated user's workspace
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
