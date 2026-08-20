// task_dto.go 为 task.go 和 subtask.go 提供 domain 类型别名、常量和参数构建器。
// 约定：类型别名使用 types.Xxx，参数构建器返回 types.XxxParams，
// 响应转换直接读 types.Task 字段（不使用 sql.NullXxx）。
package handler

import (
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ---- domain 类型别名 ----

type Task = types.Task
type TaskType = string
type TaskPriority = string
type TaskStatus = string

// ---- 常量 ----

const TaskPriorityMedium = types.TaskPriorityMedium
const TaskTypeTask = types.TaskTypeTask
const TaskStatusActive = types.TaskStatusActive
const TaskStatusCompleted = types.TaskStatusCompleted
const TaskStatusCancelled = types.TaskStatusCancelled

// ---- 请求结构体 ----

// createTaskRequest 创建任务请求体。
type createTaskRequest struct {
	Title              string          `json:"title"`               // 任务标题
	Description        string          `json:"description"`         // 任务描述
	Constraints        string          `json:"constraints"`         // 约束条件
	Type               TaskType        `json:"type"`                // 任务类型
	Priority           TaskPriority    `json:"priority"`            // 优先级
	DueDate            *string         `json:"due_date"`            // 截止日期
	Labels             []string        `json:"labels"`              // 标签列表
	WorkflowTemplateID uuid.UUID       `json:"workflow_template_id"` // 工作流模板 ID
}

// updateTaskRequest 更新任务请求体。
type updateTaskRequest struct {
	Title       string       `json:"title"`        // 任务标题
	Description string       `json:"description"`  // 任务描述
	Priority    TaskPriority `json:"priority"`     // 优先级
	Labels      []string     `json:"labels"`       // 标签列表
	DueDate     *string      `json:"due_date"`     // 截止日期
	Constraints string       `json:"constraints"`  // 约束条件
	Status      TaskStatus   `json:"status"`       // 任务状态
}

// ---- 响应转换函数 ----

// taskResponse 任务响应 DTO，将 domain 字段转换为 JSON 序列化友好格式。
type taskResponse struct {
	ID           int32    `json:"id"`            // 任务 ID
	ProjectID    string   `json:"project_id"`    // 项目 ID
	WorkflowName string   `json:"workflow_name"` // 工作流名称
	Title        string   `json:"title"`         // 任务标题
	Description  string   `json:"description"`   // 任务描述
	Constraints  string   `json:"constraints"`   // 约束条件
	Type         string   `json:"type"`          // 任务类型
	Priority     string   `json:"priority"`      // 优先级
	Status       string   `json:"status"`        // 任务状态
	AuthorType   string   `json:"author_type"`   // 作者类型
	AuthorID     string   `json:"author_id"`     // 作者 ID
	DueDate      *string  `json:"due_date"`      // 截止日期
	Labels       []string `json:"labels"`        // 标签列表
	Sequence     int32    `json:"sequence"`      // 序号
	ParentTaskID *int32   `json:"parent_task_id"` // 父任务 ID
	GitBranch    string   `json:"git_branch"`    // Git 分支
	CreatedAt    string   `json:"created_at"`    // 创建时间
	UpdatedAt    string   `json:"updated_at"`    // 更新时间
}

// taskToResponse 将 domain 任务记录转换为 API 响应。
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

// tasksToResponse 将任务列表转换为响应格式。
func tasksToResponse(tasks []Task) []taskResponse {
	result := make([]taskResponse, 0, len(tasks))
	for _, t := range tasks {
		result = append(result, taskToResponse(t))
	}
	return result
}

// ---- 参数构建器 ----

// buildCreateTaskParams 从请求字段构建 types.CreateTaskParams。
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

// buildUpdateTaskParams 从请求字段构建 types.UpdateTaskParams。
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

// buildListTasksParams 从请求字段构建 types.ListTasksParams。
func buildListTasksParams(projectID uuid.UUID, status TaskStatus) types.ListTasksParams {
	return types.ListTasksParams{
		WorkspaceID: projectID.String(),
	}
}

