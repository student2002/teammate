// workflow.go provides HTTP API endpoints for workflow template management.
//
// This file provides the following HTTP API endpoints:
//   - POST /workspaces/{workspaceId}/workflows: create a workflow template with its full graph
//   - GET /workspaces/{workspaceId}/workflows: list all workflow templates under the workspace
//   - GET /workspaces/{workspaceId}/workflows/{id}: query the details of the specified workflow template
//   - PUT /workspaces/{workspaceId}/workflows/{id}: replace the template's whole graph (version bumps)
//   - GET /workspaces/{workspaceId}/workflows/{id}/stats: query usage statistics of the specified workflow template
//   - DELETE /workspaces/{workspaceId}/workflows/{id}: delete the specified workflow template
//
// A workflow template is an event-driven graph of typed nodes (type_key) and
// port-based edges; create/update validate the whole graph against the node
// registry before any write.
// All write operations require write permission, and template ownership is verified through workspace isolation.

package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// WorkflowHandler handles HTTP requests for workflow template management, including querying and deleting templates.
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
//   - chi.Router: routes containing workflow template endpoints
func (h *WorkflowHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateWorkflowTemplate)
	r.Get("/", h.ListWorkflowTemplates)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetWorkflowTemplate)
		r.Put("/", h.UpdateWorkflowTemplate)
		r.Get("/stats", h.GetWorkflowTemplateStats)
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
//   - chi.Router: routes containing the create/update/delete endpoints
func (h *WorkflowHandler) WriteRoutes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateWorkflowTemplate)
	r.Route("/{id}", func(r chi.Router) {
		r.Put("/", h.UpdateWorkflowTemplate)
		r.Delete("/", h.DeleteWorkflowTemplate)
	})

	return r
}

// CreateWorkflowTemplate handles the POST /workspaces/{workspaceId}/workflows
// endpoint, validating and creating a workflow template with its full graph.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter workspaceId is the workspace UUID; body is the full graph
//
// Returns:
//   - no return value, writes 201 with {template, nodes, edges} or an error message
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

	var req templateGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	detail, err := service.NewWorkflowService(h.Svc).Create(r.Context(), workspaceID, toTemplateGraphInput(req))
	if err != nil {
		if errors.Is(err, service.ErrInvalidGraph) {
			response.BadRequest(w, err.Error())
			return
		}
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, toTemplateGraphResponse(*detail))
}

// UpdateWorkflowTemplate handles the PUT /workspaces/{workspaceId}/workflows/{id}
// endpoint, atomically replacing the template's whole graph; the template
// version bumps on every save.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameters workspaceId/id; body is the full graph
//
// Returns:
//   - no return value, writes 200 with {template, nodes, edges} or an error message
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
	if checkWorkflowTemplateWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	var req templateGraphRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	detail, err := service.NewWorkflowService(h.Svc).Update(r.Context(), id, toTemplateGraphInput(req))
	if err != nil {
		if errors.Is(err, service.ErrInvalidGraph) {
			response.BadRequest(w, err.Error())
			return
		}
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "workflow template not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, toTemplateGraphResponse(*detail))
}

// ListWorkflowTemplates handles the GET /workspaces/{workspaceId}/workflows endpoint, listing all workflow templates with their nodes and edges under the workspace.
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
	resp := make([]templateGraphResponse, 0, len(results))
	for _, result := range results {
		resp = append(resp, templateGraphResponse{
			Template: toTemplateResponse(result.Template),
			Nodes:    result.Nodes,
			Edges:    result.Edges,
		})
	}

	response.JSON(w, r, resp)
}

// GetWorkflowTemplate handles the GET /workspaces/{workspaceId}/workflows/{id} endpoint, querying the specified workflow template with its nodes and edges.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter id is the template UUID
//
// Returns:
//   - no return value, writes a JSON response via w, containing the template, nodes and edges or an error message
func (h *WorkflowHandler) GetWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid template id")
		return
	}
	if checkWorkflowTemplateWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	result, err := service.NewWorkflowService(h.Svc).Get(r.Context(), id)
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "workflow template not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, templateGraphResponse{
		Template: toTemplateResponse(result.Template),
		Nodes:    result.Nodes,
		Edges:    result.Edges,
	})
}

// toTemplateGraphInput maps the API request to the service input payload.
func toTemplateGraphInput(req templateGraphRequest) service.TemplateGraphInput {
	return service.TemplateGraphInput{
		Name:        req.Name,
		Description: req.Description,
		Enabled:     req.Enabled,
		Nodes:       req.Nodes,
		Edges:       req.Edges,
	}
}

// GetWorkflowTemplateStats handles the GET /workspaces/{workspaceId}/workflows/{id}/stats endpoint,
// returning usage count and average completion time for the template.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request, path parameter id is the template UUID
//
// Returns:
//   - no return value, writes a JSON response via w, containing the template statistics or an error message
func (h *WorkflowHandler) GetWorkflowTemplateStats(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid template id")
		return
	}
	if checkWorkflowTemplateWorkspace(h.Svc, w, r, id) == nil {
		return
	}

	stats, err := service.NewTemplateStatsService(h.Svc).GetStats(r.Context(), id)
	if err != nil {
		if errors.Is(err, service.ErrTemplateNotFound) {
			response.NotFound(w, "workflow template not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, stats)
}

// DeleteWorkflowTemplate handles the DELETE /workspaces/{workspaceId}/workflows/{id} endpoint, deleting the specified workflow template.
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
