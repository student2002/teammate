// task.go provides data access operations for tasks and workflow nodes.
//
// A Task is a unit of work in a project, instantiated from a workflow template
// into a graph of task nodes by the workflow engine.
// This file contains task CRUD, node queries, subtask management, etc.
package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateTask creates the task main record. Node instantiation from the
// template graph is the workflow engine's responsibility (Start), not part of
// task creation.
func (s *Store) CreateTask(ctx context.Context, params types.CreateTaskParams) (types.Task, error) {
	dbParams, err := FromDomainCreateTaskParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert create task params: %w", err)
	}

	task, err := s.q.CreateTask(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("create task: %w", err)
	}

	// If sequence is not explicitly provided, set sequence = id (defaults to 0)
	if dbParams.Sequence == 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE tasks SET sequence = $1 WHERE id = $1`, task.ID)
		if err != nil {
			return types.Task{}, fmt.Errorf("set task sequence: %w", err)
		}
		task.Sequence = task.ID
	}

	return ToDomainTask(task)
}

// GetTask queries a single task record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: integer ID of the task
//
// Returns:
//   - types.Task: the task record
//   - error: error if the query fails
func (s *Store) GetTask(ctx context.Context, id int32) (types.Task, error) {
	task, err := s.q.GetTask(ctx, id)
	if err != nil {
		return types.Task{}, fmt.Errorf("get task: %w", err)
	}
	return ToDomainTask(task)
}

// ListTasks paginated query of tasks within the specified project.
//
// Parameters:
//   - ctx: request context
//   - params: pagination query parameters
//
// Returns:
//   - []types.Task: list of tasks
//   - error: error if the query fails
func (s *Store) ListTasks(ctx context.Context, params types.ListTasksParams) ([]types.Task, error) {
	dbParams, err := FromDomainListTasksParams(params)
	if err != nil {
		return nil, fmt.Errorf("convert list tasks params: %w", err)
	}
	tasks, err := s.q.ListTasks(ctx, dbParams)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// ListAllTasks queries all tasks within the specified project (no pagination).
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - []types.Task: list of tasks
//   - error: error if the query fails
func (s *Store) ListAllTasks(ctx context.Context, projectID uuid.UUID) ([]types.Task, error) {
	tasks, err := s.q.ListAllTasks(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list all tasks: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// UpdateTask updates the basic information of a task.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including the task ID and fields to update
//
// Returns:
//   - types.Task: the updated task record
//   - error: error if the update fails
func (s *Store) UpdateTask(ctx context.Context, params types.UpdateTaskParams) (types.Task, error) {
	dbParams, err := FromDomainUpdateTaskParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert update task params: %w", err)
	}
	task, err := s.q.UpdateTask(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("update task: %w", err)
	}
	return ToDomainTask(task)
}

// DeleteTask cancels a task and cancels its unfinished nodes.
//
// Execution steps:
//  1. Set the task status to cancelled
//  2. Cancel the running nodes (they are hidden by the cancelled task status)
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//
// Returns:
//   - error: error if deletion fails
func (s *Store) DeleteTask(ctx context.Context, taskID int32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Set the task status to cancelled
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status = 'cancelled', updated_at = NOW() WHERE id = $1`, taskID); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	// Cancel the running nodes (they will be hidden by the cancelled task status)
	if _, err := tx.ExecContext(ctx,
		`UPDATE task_nodes SET status = 'cancelled', updated_at = NOW()
		 WHERE task_id = $1 AND status = 'running'`, taskID); err != nil {
		return fmt.Errorf("cancel task nodes on delete: %w", err)
	}

	return tx.Commit()
}

// CancelTaskNodes cancels the running nodes of the specified task: a cancelled
// task's half-done work is terminated, and the terminal state of the rest of
// the graph is decided by the engine when the task is re-activated.
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//
// Returns:
//   - error: error if the update fails
func (s *Store) CancelTaskNodes(ctx context.Context, taskID int32) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE task_nodes SET status = 'cancelled', updated_at = NOW()
		 WHERE task_id = $1 AND status = 'running'`, taskID)
	if err != nil {
		return fmt.Errorf("cancel task nodes: %w", err)
	}
	return nil
}

// GetTaskNode queries a single workflow node record by ID.
//
// Parameters:
//   - ctx: request context, supports timeout and cancellation
//   - id: UUID identifier of the node
//
// Returns:
//   - types.TaskNode: node record, including status, reservation, version, etc.
//   - error: error returned when the query fails (e.g. node does not exist)
func (s *Store) GetTaskNode(ctx context.Context, id uuid.UUID) (types.TaskNode, error) {
	node, err := s.q.GetTaskNode(ctx, id)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ListTaskNodes queries all workflow nodes of the specified task.
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//
// Returns:
//   - []types.TaskNode: list of workflow nodes
//   - error: error if the query fails
func (s *Store) ListTaskNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListTaskNodesByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// ListTaskNodesByProject queries all workflow nodes of all tasks within the specified project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - []types.TaskNode: list of workflow nodes
//   - error: error if the query fails
func (s *Store) ListTaskNodesByProject(ctx context.Context, projectID uuid.UUID) ([]types.TaskNode, error) {
	nodes, err := s.q.ListTaskNodesByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes by project: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// CreateSubtask creates a subtask record.
//
// Parameters:
//   - ctx: request context
//   - params: subtask creation parameters
//
// Returns:
//   - types.Task: the created subtask
//   - error: error if creation fails
func (s *Store) CreateSubtask(ctx context.Context, params types.CreateSubtaskParams) (types.Task, error) {
	dbParams, err := FromDomainCreateSubtaskParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert create subtask params: %w", err)
	}
	task, err := s.q.CreateSubtask(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("create subtask: %w", err)
	}

	// If sequence is not explicitly provided, set sequence = id (defaults to 0)
	if dbParams.Sequence == 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE tasks SET sequence = $1 WHERE id = $1`, task.ID)
		if err != nil {
			return types.Task{}, fmt.Errorf("set subtask sequence: %w", err)
		}
		task.Sequence = task.ID
	}

	return ToDomainTask(task)
}

// ListSubtasks queries all subtasks of the specified parent task.
//
// Parameters:
//   - ctx: request context
//   - parentTaskID: parent task ID (may be null)
//
// Returns:
//   - []types.Task: list of subtasks
//   - error: error if the query fails
func (s *Store) ListSubtasks(ctx context.Context, parentTaskID sql.NullInt32) ([]types.Task, error) {
	tasks, err := s.q.ListSubtasks(ctx, parentTaskID)
	if err != nil {
		return nil, fmt.Errorf("list subtasks: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// UpdateTaskGitBranch updates the Git branch name associated with a task.
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//   - gitBranch: Git branch name
//
// Returns:
//   - error: error if the update fails
func (s *Store) UpdateTaskGitBranch(ctx context.Context, taskID int32, gitBranch string) error {
	if err := s.q.UpdateTaskGitBranch(ctx, db.UpdateTaskGitBranchParams{
		ID:        taskID,
		GitBranch: sql.NullString{String: gitBranch, Valid: gitBranch != ""},
	}); err != nil {
		return fmt.Errorf("update task git branch: %w", err)
	}
	return nil
}

// ListTasksPaginated queries tasks within the specified project (pagination + search), without filtering out historical tasks.
//
// Parameters:
//   - ctx: request context
//   - params: pagination query parameters, including project ID, status filter, search keyword, limit and offset
//
// Returns:
//   - []types.Task: list of tasks on the current page
//   - error: error if the query fails
func (s *Store) ListTasksPaginated(ctx context.Context, params types.ListTasksPaginatedParams) ([]types.Task, error) {
	dbParams, err := FromDomainListTasksPaginatedParams(params)
	if err != nil {
		return nil, fmt.Errorf("convert list tasks paginated params: %w", err)
	}
	tasks, err := s.q.ListTasksPaginated(ctx, dbParams)
	if err != nil {
		return nil, fmt.Errorf("list tasks paginated: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// CountTasksByStatus counts tasks under the specified project and status (supports search).
//
// Parameters:
//   - ctx: request context
//   - params: count parameters, including project ID, status filter and search keyword
//
// Returns:
//   - int64: total number of matching tasks
//   - error: error if the query fails
func (s *Store) CountTasksByStatus(ctx context.Context, params types.CountTasksByStatusParams) (int64, error) {
	dbParams, err := FromDomainCountTasksByStatusParams(params)
	if err != nil {
		return 0, fmt.Errorf("convert count tasks by status params: %w", err)
	}
	count, err := s.q.CountTasksByStatus(ctx, dbParams)
	if err != nil {
		return 0, fmt.Errorf("count tasks by status: %w", err)
	}
	return count, nil
}
