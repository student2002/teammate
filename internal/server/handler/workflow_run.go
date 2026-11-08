// workflow_run.go provides HTTP endpoints of the workflow V2 run engine:
// manual runs (POST .../runs) and the run view data source (GET /tasks/{id}/graph).
package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// WorkflowRunHandler handles the HTTP requests of workflow runs.
type WorkflowRunHandler struct {
	Svc *service.Service
}

func NewWorkflowRunHandler(svc *service.Service) *WorkflowRunHandler {
	return &WorkflowRunHandler{Svc: svc}
}

// startRunRequest is the body of the manual run endpoint (plan §12.3).
type startRunRequest struct {
	Input json.RawMessage `json:"input"`
	Title string          `json:"title"`
}

// graphNodeEntry is one node of the run view: the V2 node fields plus its
// attempt history.
type graphNodeEntry struct {
	types.TaskNode
	Attempts []types.TaskNodeAttempt `json:"attempts"`
}

// StartRun handles POST /projects/{projectId}/workflows/{templateId}/runs.
// It instantiates the template into a new task run and returns the task id.
//
// Response:
//   - 201: {"task_id": ..., "title": ...}
//   - 400: invalid parameters
//   - 401/403: authentication/permission
//   - 404: project or template not found in the caller's workspace
func (h *WorkflowRunHandler) StartRun(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}
	templateID, err := uuid.Parse(chi.URLParam(r, "templateId"))
	if err != nil {
		response.BadRequest(w, "invalid template id")
		return
	}

	var req startRunRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// The workspace-context checks are the HTTP boundary of the same
	// validation the service repeats inside Start.
	project := checkProjectWorkspace(h.Svc, w, r, projectID)
	if project == nil {
		return
	}
	template := checkWorkflowTemplateWorkspace(h.Svc, w, r, templateID)
	if template == nil {
		return
	}

	authorID := claims.UserID
	if authorID == uuid.Nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	runSvc := service.NewWorkflowRunService(h.Svc)
	taskID, err := runSvc.Start(r.Context(), templateID, projectID, req.Input, req.Title, authorID)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrTemplateDisabled):
			w.WriteHeader(http.StatusConflict)
			response.JSON(w, r, map[string]string{"error": err.Error()})
		case errors.Is(err, service.ErrWorkspaceMismatch):
			response.BadRequest(w, err.Error())
		default:
			response.InternalServerError(w, err)
		}
		return
	}

	title := req.Title
	if title == "" {
		title = template.Name
	}
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"task_id": taskID,
		"title":   title,
	})
}

// GetTaskGraph handles GET /tasks/{taskId}/graph (plan §13.5): the run view
// data source with the task snapshot, every V2 node with its attempt history,
// and every edge with its resolution.
func (h *WorkflowRunHandler) GetTaskGraph(w http.ResponseWriter, r *http.Request) {
	taskID, err := parseTaskIDParam(r)
	if err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	graph, err := service.NewWorkflowRunService(h.Svc).Graph(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			response.NotFound(w, "task not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	attemptsByNode := make(map[string][]types.TaskNodeAttempt, len(graph.Nodes))
	for _, a := range graph.Attempts {
		attemptsByNode[a.TaskNodeID] = append(attemptsByNode[a.TaskNodeID], a)
	}
	entries := make([]graphNodeEntry, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		attempts, ok := attemptsByNode[n.ID]
		if !ok {
			attempts = []types.TaskNodeAttempt{}
		}
		entries = append(entries, graphNodeEntry{TaskNode: n, Attempts: attempts})
	}

	response.JSON(w, r, map[string]interface{}{
		"task":     graph.Task,
		"nodes":    entries,
		"edges":    graph.Edges,
		"attempts": graph.Attempts,
	})
}

// parseTaskIDParam parses the {taskId} URL parameter.
func parseTaskIDParam(r *http.Request) (int32, error) {
	id, err := strconv.Atoi(chi.URLParam(r, "taskId"))
	if err != nil {
		return 0, err
	}
	return int32(id), nil
}
