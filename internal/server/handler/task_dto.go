// task_dto.go provides domain type aliases, constants, and parameter builders for task.go and subtask.go.
// Convention: type aliases use types.Xxx, parameter builders return types.XxxParams,
// response conversion reads types.Task fields directly (does not use sql.NullXxx).
package handler

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ---- domain type aliases ----

type Task = types.Task
type TaskType = string
type TaskPriority = string
type TaskStatus = string

// ---- constants ----

const TaskPriorityMedium = types.TaskPriorityMedium
const TaskTypeTask = types.TaskTypeTask
const TaskStatusActive = types.TaskStatusActive
const TaskStatusCompleted = types.TaskStatusCompleted
const TaskStatusCancelled = types.TaskStatusCancelled

// ---- request structs ----

// createTaskRequest create task request body.
type createTaskRequest struct {
	Title              string          `json:"title"`               // task title
	Description        string          `json:"description"`         // task description
	Constraints        string          `json:"constraints"`         // constraints
	Type               TaskType        `json:"type"`                // task type
	Priority           TaskPriority    `json:"priority"`            // priority
	DueDate            *string         `json:"due_date"`            // due date
	Labels             []string        `json:"labels"`              // label list
	WorkflowTemplateID uuid.UUID       `json:"workflow_template_id"` // workflow template ID
}

// updateTaskRequest update task request body.
type updateTaskRequest struct {
	Title       string       `json:"title"`        // task title
	Description string       `json:"description"`  // task description
	Priority    TaskPriority `json:"priority"`     // priority
	Labels      []string     `json:"labels"`       // label list
	DueDate     *string      `json:"due_date"`     // due date
	Constraints string       `json:"constraints"`  // constraints
	Status      TaskStatus   `json:"status"`       // task status
}

// ---- response conversion functions ----

// taskResponse task response DTO, converts domain fields to a JSON-serialization-friendly format.
type taskResponse struct {
	ID           int32    `json:"id"`            // task ID
	ProjectID    string   `json:"project_id"`    // project ID
	WorkflowName string   `json:"workflow_name"` // workflow name
	Title        string   `json:"title"`         // task title
	Description  string   `json:"description"`   // task description
	Constraints  string   `json:"constraints"`   // constraints
	Type         string   `json:"type"`          // task type
	Priority     string   `json:"priority"`      // priority
	Status       string   `json:"status"`        // task status
	AuthorType   string   `json:"author_type"`   // author type
	AuthorID     string   `json:"author_id"`     // author ID
	DueDate      *string  `json:"due_date"`      // due date
	Labels       []string `json:"labels"`        // label list
	Sequence     int32    `json:"sequence"`      // sequence number
	ParentTaskID *int32   `json:"parent_task_id"` // parent task ID
	GitBranch    string   `json:"git_branch"`    // Git branch
	CreatedAt    string   `json:"created_at"`    // creation time
	UpdatedAt    string   `json:"updated_at"`    // update time
}

// taskToResponse converts a domain task record to an API response.
func taskToResponse(t Task) taskResponse {
	var dueDate *string
	if t.DueDate != nil {
		s := t.DueDate.Format(time.RFC3339)
		dueDate = &s
	}
	var gitBranch string
	if t.GitBranch != nil {
		gitBranch = *t.GitBranch
	}
	return taskResponse{
		ID:           t.ID,
		ProjectID:    t.ProjectID,
		WorkflowName: t.WorkflowName,
		Title:        t.Title,
		Description:  t.Description,
		Constraints:  t.Constraints,
		Type:         t.Type,
		Priority:     t.Priority,
		Status:       t.Status,
		AuthorType:   t.AuthorType,
		AuthorID:     t.AuthorID,
		DueDate:      dueDate,
		Labels:       t.Labels,
		Sequence:     int32(t.Sequence),
		ParentTaskID: t.ParentTaskID,
		GitBranch:    gitBranch,
		CreatedAt:    t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    t.UpdatedAt.Format(time.RFC3339),
	}
}

// tasksToResponse converts a task list to the response format.
func tasksToResponse(tasks []Task) []taskResponse {
	result := make([]taskResponse, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, taskToResponse(t))
	}
	return result
}

// ---- parameter builders ----

// buildCreateTaskParams builds types.CreateTaskParams from request fields.
func buildCreateTaskParams(
	projectID uuid.UUID,
	title string,
	description string,
	constraints string,
	taskType TaskType,
	priority TaskPriority,
	status TaskStatus,
	authorType string,
	authorID uuid.UUID,
	dueDate sql.NullTime,
	labels []string,
	workflowName string,
) types.CreateTaskParams {
	var descPtr *string
	if description != "" {
		descPtr = &description
	}
	var constraintsPtr *string
	if constraints != "" {
		constraintsPtr = &constraints
	}
	var dueDatePtr *time.Time
	if dueDate.Valid {
		t := dueDate.Time
		dueDatePtr = &t
	}
	return types.CreateTaskParams{
		ProjectID:    projectID.String(),
		Title:        title,
		Description:  descPtr,
		Constraints:  constraintsPtr,
		Type:         taskType,
		Priority:     priority,
		Status:       status,
		AuthorType:   authorType,
		AuthorID:     authorID.String(),
		DueDate:      dueDatePtr,
		Labels:       labels,
		Sequence:     0,
		WorkflowName: workflowName,
	}
}

// buildUpdateTaskParams builds types.UpdateTaskParams from request fields.
func buildUpdateTaskParams(
	taskID int32,
	title string,
	description string,
	priority TaskPriority,
	labels []string,
	dueDate sql.NullTime,
	constraints string,
	status TaskStatus,
) types.UpdateTaskParams {
	var descPtr *string
	if description != "" {
		descPtr = &description
	}
	var constraintsPtr *string
	if constraints != "" {
		constraintsPtr = &constraints
	}
	var dueDatePtr *time.Time
	if dueDate.Valid {
		t := dueDate.Time
		dueDatePtr = &t
	}
	return types.UpdateTaskParams{
		ID:          taskID,
		Title:       title,
		Description: descPtr,
		Priority:    priority,
		Labels:      labels,
		DueDate:     dueDatePtr,
		Constraints: constraintsPtr,
		Status:      status,
	}
}

// buildListTasksParams builds types.ListTasksParams from request fields.
func buildListTasksParams(projectID uuid.UUID, status TaskStatus) types.ListTasksParams {
	return types.ListTasksParams{
		WorkspaceID: projectID.String(),
	}
}

