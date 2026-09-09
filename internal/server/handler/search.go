// search.go provides HTTP API endpoints for keyword search of tasks and agents.
//
// Supports keyword search of tasks and Agents; task search can be filtered by project ID, and Agent search can be filtered by workspace ID.

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// SearchHandler handles HTTP requests related to search, supporting keyword search of tasks and agents.
type SearchHandler struct {
	Svc *service.Service
}

// NewSearchHandler creates a SearchHandler instance.
func NewSearchHandler(svc *service.Service) *SearchHandler {
	return &SearchHandler{Svc: svc}
}

// Routes returns the route table for search.
func (h *SearchHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Get("/tasks", h.SearchTasks)
	r.Get("/agents", h.SearchAgents)

	return r
}

// SearchTasks handles the GET /workspaces/{workspaceId}/search/tasks endpoint, searching tasks by keyword, supports filtering by project ID.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Query parameters:
//   - q: string, search keyword (required)
//   - projectId: UUID, project ID (optional, filter by project)
//
// Response:
//   - 200: successfully returns search results
//   - 400: missing search keyword or invalid project ID
//   - 401: not authenticated
func (h *SearchHandler) SearchTasks(w http.ResponseWriter, r *http.Request) {
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return
	}

	// get the search keyword
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		response.BadRequest(w, "missing query parameter: q")
		return
	}

	// parse the optional project ID
	projectIDStr := r.URL.Query().Get("projectId")
	var projectID *uuid.UUID
	if projectIDStr != "" {
		parsed, err := uuid.Parse(projectIDStr)
		if err != nil {
			response.BadRequest(w, "invalid project id")
			return
		}
		// verify the project belongs to the current workspace
		if checkProjectWorkspace(h.Svc, w, r, parsed) == nil {
			return
		}
		projectID = &parsed
	}

	// call service to search tasks (now returns []types.Task directly)
	searchSvc := service.NewSearchService(h.Svc)
	tasks, err := searchSvc.SearchTasks(r.Context(), keyword, ws.WorkspaceID, projectID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// convert to response format
	response.JSON(w, r, tasksToResponse(tasks))
}

// SearchAgents handles the GET /workspaces/{workspaceId}/search/agents endpoint, searching AI agents by keyword.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Query parameters:
//   - q: string, search keyword (required)
//
// Response:
//   - 200: successfully returns search results
//   - 400: missing search keyword or invalid workspace ID
//   - 401: not authenticated
func (h *SearchHandler) SearchAgents(w http.ResponseWriter, r *http.Request) {
	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return
	}

	// get the search keyword
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		response.BadRequest(w, "missing query parameter: q")
		return
	}

	// call service to search Agents
	searchSvc := service.NewSearchService(h.Svc)
	agents, err := searchSvc.SearchAgents(r.Context(), keyword, ws.WorkspaceID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, agents)
}
