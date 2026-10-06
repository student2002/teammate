// subtask.go provides HTTP API endpoints for task subtasks.
//
// This file provides the following HTTP API endpoints (mounted under /api/tasks/{taskId}):
//   - POST /subtasks: create a subtask under the parent task
//   - GET /subtasks: list the parent task's subtasks
package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/teammate/server/internal/types"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
)

// subtaskParamsFromRequest builds the CreateSubtaskParams domain struct from
// the request fields and the parent task.
func subtaskParamsFromRequest(parent types.Task, parentID int32, title, priority string, labels []string, authorType, authorID string) types.CreateSubtaskParams {
	return types.CreateSubtaskParams{
		ProjectID:    parent.ProjectID,
		Title:        title,
		Type:         "task",
		Priority:     priority,
		Status:       TaskStatusActive,
		AuthorType:   authorType,
		AuthorID:     authorID,
		Labels:       labels,
		ParentTaskID: &parentID,
	}
}

// SubtaskHandler handles HTTP requests related to task subtasks.
type SubtaskHandler struct {
	Svc *service.Service
}

// NewSubtaskHandler creates a SubtaskHandler instance.
func NewSubtaskHandler(svc *service.Service) *SubtaskHandler {
	return &SubtaskHandler{Svc: svc}
}

// Routes returns the route table for subtasks.
func (h *SubtaskHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateSubtask)
	r.Get("/", h.ListSubtasks)

	return r
}

// createSubtaskRequest create subtask request body.
type createSubtaskRequest struct {
	Title       string   `json:"title"`       // subtask title (required)
	Description *string  `json:"description"` // subtask description
	Priority    string   `json:"priority"`    // priority
	DueDate     *string  `json:"due_date"`    // due date (RFC3339 or YYYY-MM-DD)
	Labels      []string `json:"labels"`      // labels
}

// CreateSubtask handles the POST /tasks/{taskId}/subtasks endpoint, creating
// a subtask under the parent task.
//
// Request body:
//   - title: string, subtask title (required)
//   - description / priority / due_date / labels: optional task fields
//
// Response:
//   - 201: the created subtask
//   - 400/401/403/404: validation / auth / permission / parent not found
func (h *SubtaskHandler) CreateSubtask(w http.ResponseWriter, r *http.Request) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if err := requireWriteAccess(claims); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 32)
	if err != nil || taskID <= 0 {
		response.BadRequest(w, "invalid task id")
		return
	}

	var req createSubtaskRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	if req.Title == "" {
		response.BadRequest(w, "title is required")
		return
	}

	parent, err := service.NewTaskService(h.Svc).GetTask(r.Context(), int32(taskID))
	if err != nil {
		response.NotFound(w, "parent task not found")
		return
	}

	priority := req.Priority
	if priority == "" {
		priority = TaskPriorityMedium
	}
	labels := req.Labels
	if labels == nil {
		labels = []string{}
	}
	parentID := int32(taskID)
	authorType := claims.UserType
	if authorType == "" {
		authorType = "human"
	}

	dueDate, err := service.ParseDueDate(req.DueDate)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}
	params := subtaskParamsFromRequest(parent, parentID, req.Title, priority, labels, authorType, claims.UserID.String())
	if dueDate.Valid {
		t := dueDate.Time
		params.DueDate = &t
	}

	taskSvc := service.NewTaskService(h.Svc)
	subtask, err := taskSvc.CreateSubtask(r.Context(), params)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, subtask)
}

// ListSubtasks handles the GET /tasks/{taskId}/subtasks endpoint, listing the
// parent task's subtasks.
//
// Response:
//   - 200: subtask list
//   - 400: invalid task id
func (h *SubtaskHandler) ListSubtasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := svcmw.GetAuthFromContext(r.Context()); !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	taskID, err := strconv.ParseInt(chi.URLParam(r, "taskId"), 10, 32)
	if err != nil || taskID <= 0 {
		response.BadRequest(w, "invalid task id")
		return
	}

	taskSvc := service.NewTaskService(h.Svc)
	subtasks, err := taskSvc.ListSubtasks(r.Context(), int32(taskID))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, subtasks)
}
