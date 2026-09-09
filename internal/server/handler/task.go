// task.go provides HTTP API endpoints for task CRUD management, node list queries, and Git branch updates.
//
// Tasks are defined by workflow templates and contain ordered nodes. Creating a task automatically generates the nodes.

package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
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

// TaskHandler handles HTTP requests for task management, including task CRUD, node queries, and Git branch updates.
type TaskHandler struct {
	Svc *service.Service
}

// NewTaskHandler creates a TaskHandler instance.
func NewTaskHandler(svc *service.Service) *TaskHandler {
	return &TaskHandler{Svc: svc}
}

// Routes returns the route table for tasks.
func (h *TaskHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateTask)
	r.Get("/", h.ListTasks)
	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", h.GetTask)
		r.Put("/", h.UpdateTask)
		r.Delete("/", h.DeleteTask)
	})

	return r
}

// CreateTask handles the POST /projects/{projectId}/tasks endpoint, creating a new task and generating ordered nodes based on the workflow template.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - title: string, task title (required)
//   - description: string, task description
//   - constraints: string, constraints
//   - type: string, task type
//   - priority: string, priority
//   - due_date: string, due date (RFC3339 or YYYY-MM-DD format)
//   - labels: string[], label list
//   - workflow_template_id: UUID, workflow template ID (required)
//
// Response:
//   - 201: task created successfully, returns task and node info
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: no permission
//   - 404: project does not exist
//
// Processing flow:
//  1. verify authentication status and write permission
//  2. verify the project and workflow template belong to the current workspace
//  3. call service to create the task and generate nodes
//  4. return task and node info
func (h *TaskHandler) CreateTask(w http.ResponseWriter, r *http.Request) {
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

	// parse project ID
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// parse request body
	var req createTaskRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// verify workflow template ID is required
	if req.WorkflowTemplateID == uuid.Nil {
		response.BadRequest(w, "workflow_template_id is required")
		return
	}

	taskSvc := service.NewTaskService(h.Svc)

	// verify the project belongs to the current workspace
	project := checkProjectWorkspace(h.Svc, w, r, projectID)
	if project == nil {
		return
	}

	// verify the workflow template belongs to the current workspace and get the template name
	template := checkWorkflowTemplateWorkspace(h.Svc, w, r, req.WorkflowTemplateID)
	if template == nil {
		return
	}

	// set default priority
	priority := req.Priority
	if priority == "" {
		priority = TaskPriorityMedium
	}

	// set default task type
	taskType := req.Type
	if taskType == "" {
		taskType = TaskTypeTask
	}

	// derive author identity from auth info (not the request body) to prevent forgery
	authorType := claims.UserType
	if authorType == "" {
		authorType = "human"
	}
	authorID := claims.UserID

	// parse the due date
	dueDate := service.ParseDueDate(req.DueDate)

	// set default labels
	labels := req.Labels
	if labels == nil {
		labels = []string{}
	}

	// call service to create the task
	result, err := taskSvc.Create(r.Context(), projectID, buildCreateTaskParams(
		projectID, req.Title, req.Description, req.Constraints, taskType, priority,
		TaskStatusActive, authorType, authorID, dueDate, labels, template.Name,
	), req.WorkflowTemplateID)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	// return task and node info
	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, map[string]interface{}{
		"task":  result.Task,
		"nodes": result.Nodes,
	})
}

// ListTasks handles the GET /projects/{projectId}/tasks endpoint, listing the project's tasks, supports filtering by status, pagination, and search.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Query parameters:
//   - status: string, filter by status (active/completed/cancelled/all)
//   - q: string, search keyword (matches title or description)
//   - limit: int, page size (1-100, default 50)
//   - offset: int, offset (default 0)
//
// Behavior:
//   - when no limit/offset/q parameters are passed, maintains backward compatibility (ListTasks/ListAllTasks already excludes historical tasks)
//   - when any of limit/offset/q is passed, uses a paginated query (does not filter historical tasks), returns PaginatedTaskResult
//
// Response:
//   - 200: successfully returns the task list (array or paginated result)
//   - 400: invalid project ID
//   - 401: not authenticated
//   - 404: project does not exist
func (h *TaskHandler) ListTasks(w http.ResponseWriter, r *http.Request) {
	// parse project ID
	projectID, err := uuid.Parse(chi.URLParam(r, "projectId"))
	if err != nil {
		response.BadRequest(w, "invalid project id")
		return
	}

	// verify the project belongs to the current workspace
	if checkProjectWorkspace(h.Svc, w, r, projectID) == nil {
		return
	}

	// get query parameters
	statusFilter := r.URL.Query().Get("status")
	searchQuery := r.URL.Query().Get("q")
	limit := int32(50)
	offset := int32(0)

	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 100 {
		limit = int32(l)
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = int32(o)
	}

	taskSvc := service.NewTaskService(h.Svc)

	// if the request includes pagination or search parameters, use the paginated query (does not filter historical tasks)
	hasPagination := r.URL.Query().Get("limit") != "" || r.URL.Query().Get("offset") != "" || searchQuery != ""

	if hasPagination {
		// paginated mode: an empty string means do not filter status
		var status string
		if statusFilter != "" && statusFilter != "all" {
			status = statusFilter
		}
		result, err := taskSvc.ListTasksPaginatedWithNodes(r.Context(), projectID, status, searchQuery, limit, offset)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		response.JSON(w, r, result)
		return
	}

	// original logic: maintains backward compatibility when no pagination parameters are present (ListTasks/ListAllTasks already excludes historical tasks)
	if statusFilter == "all" {
		result, err := taskSvc.ListAllWithNodes(r.Context(), projectID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		response.JSON(w, r, result)
		return
	}

	// build query parameters
	status := TaskStatusActive
	if statusFilter != "" {
		status = TaskStatus(statusFilter)
	}
	params := buildListTasksParams(projectID, status)

	// call service to query tasks
	result, err := taskSvc.ListWithNodes(r.Context(), params)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, result)
}

// GetTask handles the GET /projects/{projectId}/tasks/{id} endpoint, querying the detailed information of the specified task.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the task details
//   - 400: invalid task ID
//   - 401: not authenticated
//   - 404: task does not exist
func (h *TaskHandler) GetTask(w http.ResponseWriter, r *http.Request) {
	// parse task ID
	taskIDStr := chi.URLParam(r, "id")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// verify the task belongs to the current workspace
	task := checkTaskWorkspace(h.Svc, w, r, taskID)
	if task == nil {
		return
	}

	// verify the task belongs to the project in the URL
	if err := checkTaskBelongsToProject(h.Svc, w, r, task); err != nil {
		return
	}

	// return task details
	response.JSON(w, r, taskToResponse(*task))
}

// UpdateTask handles the PUT /projects/{projectId}/tasks/{id} endpoint, updating task info; canceling a task requires admin/owner permission.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - title: string, task title (at most 200 characters)
//   - description: string, task description (at most 50000 characters)
//   - priority: string, priority
//   - labels: string[], label list (at most 10)
//   - due_date: string, due date
//   - constraints: string, constraints (at most 2000 characters)
//   - status: string, task status
//
// Response:
//   - 200: successfully returns the updated task info
//   - 400: parameter error or task status does not allow editing
//   - 401: not authenticated
//   - 403: no permission (canceling a task requires admin/owner)
//   - 404: task does not exist
func (h *TaskHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
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

	// parse task ID
	taskIDStr := chi.URLParam(r, "id")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// parse request body
	var req updateTaskRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// validate field lengths
	if len(req.Title) > 200 {
		response.BadRequest(w, "title must be at most 200 characters")
		return
	}
	if len(req.Description) > 50000 {
		response.BadRequest(w, "description must be at most 50000 characters")
		return
	}
	if len(req.Constraints) > 2000 {
		response.BadRequest(w, "constraints must be at most 2000 characters")
		return
	}
	if len(req.Labels) > 10 {
		response.BadRequest(w, "at most 10 labels allowed")
		return
	}

	taskSvc := service.NewTaskService(h.Svc)

	// verify the task belongs to the current workspace and check status
	existingTask := checkTaskWorkspace(h.Svc, w, r, taskID)
	if existingTask == nil {
		return
	}

	// verify the task belongs to the project in the URL
	if err := checkTaskBelongsToProject(h.Svc, w, r, existingTask); err != nil {
		return
	}

	// completed or cancelled tasks cannot be edited
	if existingTask.Status == TaskStatusCompleted || existingTask.Status == TaskStatusCancelled {
		response.BadRequest(w, fmt.Sprintf("cannot edit task in '%s' status", existingTask.Status))
		return
	}

	// for fields not provided, use the existing values
	title := req.Title
	if title == "" {
		title = existingTask.Title
	}
	description := req.Description
	if description == "" {
		description = existingTask.Description
	}
	priority := req.Priority
	if priority == "" {
		priority = existingTask.Priority
	}
	labels := req.Labels
	if labels == nil {
		labels = existingTask.Labels
	}
	constraints := req.Constraints
	if constraints == "" {
		constraints = existingTask.Constraints
	}
	status := req.Status
	if status == "" {
		status = existingTask.Status
	}

	// parse the due date
	var dueDate sql.NullTime
	if req.DueDate != nil && *req.DueDate != "" {
		dueDate = service.ParseDueDate(req.DueDate)
	} else if existingTask.DueDate != nil {
		dueDate = sql.NullTime{Time: *existingTask.DueDate, Valid: true}
	}

	// if the task is being canceled, admin/owner permission must be verified
	if status == TaskStatusCancelled {
		userInfo, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok || (userInfo.Role != "owner" && userInfo.Role != "admin") {
			response.Forbidden(w, "only admin or owner can cancel tasks")
			return
		}
	}

	// call service to update the task
	task, err := taskSvc.Update(r.Context(), buildUpdateTaskParams(
		taskID, title, description, priority, labels, dueDate, constraints, status,
	))
	if err != nil {
		if err == sql.ErrNoRows {
			response.NotFound(w, "task not found")
			return
		}
		response.InternalServerError(w, err)
		return
	}

	// if the task is canceled, cancel all in-progress nodes and notify the Agents
	if status == TaskStatusCancelled {
		// first fetch the node list to send interrupt events
		nodes, _ := taskSvc.ListTaskNodes(r.Context(), taskID)

		if err := taskSvc.CancelTaskNodes(r.Context(), taskID); err != nil {
			response.InternalServerError(w, err)
			return
		}

		// send a task:interrupt control event to the Agents currently executing
		for _, node := range nodes {
			if node.Status == types.TaskNodeStatusInProgress && node.AssigneeID != nil {
				assigneeID, _ := uuid.Parse(*node.AssigneeID)
				h.Svc.PublishControlEvent(r.Context(), assigneeID, "task:interrupt", map[string]interface{}{
					"task_id": fmt.Sprintf("%d", taskID),
					"node_id": node.ID,
				})
			}
		}
	}

	// return the updated task
	response.JSON(w, r, taskToResponse(task))
}

// DeleteTask handles the DELETE /projects/{projectId}/tasks/{id} endpoint, deleting the specified task and its associated nodes.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: deleted successfully
//   - 400: invalid task ID
//   - 401: not authenticated
//   - 403: no permission
//   - 404: task does not exist
func (h *TaskHandler) DeleteTask(w http.ResponseWriter, r *http.Request) {
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

	// parse task ID
	taskIDStr := chi.URLParam(r, "id")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// verify the task belongs to the current workspace
	task := checkTaskWorkspace(h.Svc, w, r, taskID)
	if task == nil {
		return
	}

	// verify the task belongs to the project in the URL
	if err := checkTaskBelongsToProject(h.Svc, w, r, task); err != nil {
		return
	}

	// call service to delete the task
	taskSvc := service.NewTaskService(h.Svc)
	if err := taskSvc.Delete(r.Context(), taskID); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// NewUpdateGitBranchHandler handles the PUT /tasks/{taskId}/git-branch endpoint; after an Agent initializes the Git working directory it updates the task's Git branch info.
func NewUpdateGitBranchHandler(svc *service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// get auth info
		claims, ok := svcmw.GetAuthFromContext(r.Context())
		if !ok {
			response.Unauthorized(w, "authentication required")
			return
		}
		// only Agents can update the Git branch
		if claims.UserType != "agent" {
			response.Forbidden(w, "only agents can update git branch")
			return
		}

		// parse task ID
		taskIDStr := chi.URLParam(r, "taskId")
		taskID, err := strconv.Atoi(taskIDStr)
		if err != nil {
			response.BadRequest(w, "invalid task id")
			return
		}

		// parse request body
		var req struct {
			GitBranch string `json:"git_branch"` // Git branch name
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			response.BadRequest(w, "invalid request body")
			return
		}
		// validate the branch name
		if req.GitBranch == "" {
			response.BadRequest(w, "git_branch is required")
			return
		}
		if len(req.GitBranch) > 200 {
			response.BadRequest(w, "git_branch too long (max 200 characters)")
			return
		}

		// call service to update the Git branch
		taskSvc := service.NewTaskService(svc)
		if err := taskSvc.UpdateTaskGitBranch(r.Context(), int32(taskID), req.GitBranch); err != nil {
			response.InternalServerError(w, err)
			return
		}

		// return success
		response.JSON(w, r, map[string]string{"status": "ok"})
	}
}
