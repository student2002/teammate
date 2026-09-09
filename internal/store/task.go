// task.go provides data access operations for tasks and workflow nodes.
//
// A Task is a unit of work in a project, instantiated from a workflow template
// into an ordered set of workflow nodes.
// This file contains task CRUD, node queries, subtask management, due date parsing, etc.
//
// Task creation flow:
//  1. Create the task record within a transaction
//  2. Set sequence (defaults to the task ID)
//  3. Fetch the project's max_review_cycles configuration
//  4. Iterate over template nodes to create task_nodes
//  5. Update depends_on dependencies
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateTask creates a task within a transaction and instantiates workflow nodes from the template nodes.
//
// Execution steps:
//  1. Create the task record
//  2. Set sequence (defaults to the task ID)
//  3. Fetch the project's max_review_cycles configuration
//  4. Iterate over template nodes to create task_nodes (the first node auto-starts based on assignee type)
//  5. Update depends_on dependencies
//
// First-node auto-start rules:
//   - specific_agent: mark as in_progress, reserved for that Agent
//   - human: mark directly as completed
//   - auto: mark as in_progress
//   - any_agent: stay pending, waiting for an Agent to claim
//
// Parameters:
//   - ctx: request context
//   - params: task creation parameters
//   - templateNodes: list of workflow template nodes
//
// Returns:
//   - types.Task: the created task record
//   - []types.TaskNode: list of created workflow nodes
//   - error: error if creation fails
func (s *Store) CreateTask(ctx context.Context, params types.CreateTaskParams, templateNodes []types.WorkflowTemplateNode) (types.Task, []types.TaskNode, error) {
	dbParams, err := FromDomainCreateTaskParams(params)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("convert create task params: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	task, err := qtx.CreateTask(ctx, dbParams)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("create task: %w", err)
	}

	// If sequence is not explicitly provided, set sequence = id (defaults to 0)
	if dbParams.Sequence == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET sequence = $1 WHERE id = $1`, task.ID)
		if err != nil {
			return types.Task{}, nil, fmt.Errorf("set task sequence: %w", err)
		}
		task.Sequence = task.ID
	}

	// Fetch the project's max_review_cycles to propagate to task nodes
	project, err := qtx.GetProject(ctx, dbParams.ProjectID)
	if err != nil {
		tx.Rollback()
		return types.Task{}, nil, fmt.Errorf("get project: %w", err)
	}

	dbTaskNodes := make([]db.TaskNode, 0, len(templateNodes))

	// Build a mapping from template node ID to task node ID, used to resolve depends_on.
	// We create nodes by sort_order, so collect IDs first, then update depends_on.
	templateToTaskNodeID := make(map[uuid.UUID]uuid.UUID, len(templateNodes))

	for i, tn := range templateNodes {
		status := db.TaskNodeStatusPending
		assigneeType := db.AssigneeType(tn.AssigneeType)
		var assigneeID uuid.NullUUID
		if tn.AssigneeID != nil {
			if u, err := uuid.Parse(*tn.AssigneeID); err == nil {
				assigneeID = uuid.NullUUID{UUID: u, Valid: true}
			}
		}

		// Auto-start the first node (index 0, regardless of sort_order value)
		if i == 0 {
			if assigneeType == db.AssigneeTypeSpecificAgent && assigneeID.Valid {
				// Specific Agent: mark as in_progress and reserve for that Agent
				status = db.TaskNodeStatusInProgress
			} else if assigneeType == db.AssigneeTypeHuman {
				// Human step: auto-complete as the first node
				status = db.TaskNodeStatusCompleted
			} else if assigneeType == db.AssigneeTypeAuto {
				// Auto-assignee: mark as in_progress so the system auto-starts it
				status = db.TaskNodeStatusInProgress
			}
			// For the "any_agent" assignee type, keep pending so an Agent can claim it
		}

		// If the template node sets MaxRejectCycles use it, otherwise fall back to the project's MaxReviewCycles
		maxRejectCycles := int32(tn.MaxRejectCycles)
		if maxRejectCycles == 0 {
			maxRejectCycles = project.MaxReviewCycles
		}

		dbNodeParams, err := FromDomainCreateTaskNodeParams(types.CreateTaskNodeParams{
			TaskID:             task.ID,
			Name:               tn.Name,
			Description:        nullStringToPtr(sql.NullString{String: tn.Description, Valid: tn.Description != ""}),
			SortOrder:          int32(tn.SortOrder),
			NodeType:           string(tn.NodeType),
			Status:             string(status),
			AssigneeType:       string(assigneeType),
			AssigneeID:         nullUUIDToString(assigneeID),
			ReservedForAgentID: nullUUIDToString(assigneeID),
			MaxRejectCycles:    maxRejectCycles,
			TimeoutMinutes:     int32(tn.TimeoutMinutes),
			ReadonlyDirs:       tn.ReadonlyDirs,
			FullControlDirs:    tn.FullControlDirs,
			DependsOn:          []string{}, // placeholder, updated later
		})
		if err != nil {
			return types.Task{}, nil, fmt.Errorf("convert create task node params: %w", err)
		}
		taskNode, err := qtx.CreateTaskNode(ctx, dbNodeParams)
		if err != nil {
			return types.Task{}, nil, fmt.Errorf("create task node: %w", err)
		}
		dbTaskNodes = append(dbTaskNodes, taskNode)
		tplID, _ := uuid.Parse(tn.ID)
		templateToTaskNodeID[tplID] = taskNode.ID
	}

	// Now update each task node's depends_on, mapping template node IDs to task node IDs
	for i, tn := range templateNodes {
		if len(tn.DependsOn) == 0 {
			continue
		}
		// domain DependsOn is []string, needs parsing into []uuid.UUID
		resolvedDeps := make([]uuid.UUID, 0, len(tn.DependsOn))
		for _, depTemplateIDStr := range tn.DependsOn {
			depTemplateID, err := uuid.Parse(depTemplateIDStr)
			if err != nil {
				continue
			}
			if taskNodeID, ok := templateToTaskNodeID[depTemplateID]; ok {
				resolvedDeps = append(resolvedDeps, taskNodeID)
			}
		}
		if len(resolvedDeps) > 0 {
			_, err := tx.ExecContext(ctx,
				`UPDATE task_nodes SET depends_on = $1 WHERE id = $2`,
				pq.Array(resolvedDeps), dbTaskNodes[i].ID)
			if err != nil {
				return types.Task{}, nil, fmt.Errorf("update task node depends_on: %w", err)
			}
			dbTaskNodes[i].DependsOn = resolvedDeps
		}
	}

	if err := tx.Commit(); err != nil {
		return types.Task{}, nil, fmt.Errorf("commit tx: %w", err)
	}

	domainTask, err := ToDomainTask(task)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("convert task to domain: %w", err)
	}
	domainNodes, err := ToDomainTaskNodeSlice(dbTaskNodes)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("convert task nodes to domain: %w", err)
	}
	return domainTask, domainNodes, nil
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

// DeleteTask soft-deletes a task (sets status to cancelled) and, within a transaction, also resets in_progress nodes to pending.
//
// Execution steps:
//  1. Set the task status to cancelled
//  2. Reset in_progress nodes to pending and clear their assignee_id
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

	// Reset in-progress nodes to pending (they will be hidden by the cancelled task status)
	if _, err := tx.ExecContext(ctx,
		`UPDATE task_nodes SET status = 'pending', assignee_id = NULL, updated_at = NOW()
		 WHERE task_id = $1 AND status = 'in_progress'`, taskID); err != nil {
		return fmt.Errorf("reset task nodes on delete: %w", err)
	}

	return tx.Commit()
}

// CancelTaskNodes resets all in_progress nodes of the specified task to pending.
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//
// Returns:
//   - error: error if the update fails
func (s *Store) CancelTaskNodes(ctx context.Context, taskID int32) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE task_nodes SET status = 'pending', assignee_id = NULL, updated_at = NOW()
		 WHERE task_id = $1 AND status = 'in_progress'`, taskID)
	if err != nil {
		return fmt.Errorf("cancel task nodes: %w", err)
	}
	return nil
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
	nodes, err := s.q.ListTaskNodes(ctx, taskID)
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
	rows, err := s.db.QueryContext(ctx, `
		SELECT tn.id, tn.task_id, tn.name, tn.description, tn.sort_order,
		       tn.node_type, tn.status, tn.assignee_type, tn.assignee_id, tn.reserved_for_agent_id,
		       tn.reject_count, tn.max_reject_cycles, tn.timeout_minutes, tn.version,
		       tn.completed_at, tn.completed_by, tn.summary, tn.previous_summary,
		       tn.reservation_expires_at, tn.created_at, tn.updated_at, tn.depends_on
		FROM task_nodes tn
		WHERE tn.task_id IN (SELECT id FROM tasks WHERE project_id = $1)
		ORDER BY tn.sort_order
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes by project: %w", err)
	}
	defer rows.Close()

	var dbNodes []db.TaskNode
	for rows.Next() {
		var n db.TaskNode
		if scanErr := rows.Scan(
			&n.ID, &n.TaskID, &n.Name, &n.Description, &n.SortOrder,
			&n.NodeType, &n.Status, &n.AssigneeType, &n.AssigneeID, &n.ReservedForAgentID,
			&n.RejectCount, &n.MaxRejectCycles, &n.TimeoutMinutes, &n.Version,
			&n.CompletedAt, &n.CompletedBy, &n.Summary, &n.PreviousSummary,
			&n.ReservationExpiresAt, &n.CreatedAt, &n.UpdatedAt, pq.Array(&n.DependsOn),
		); scanErr != nil {
			return nil, fmt.Errorf("scan task node: %w", scanErr)
		}
		dbNodes = append(dbNodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task node rows error: %w", err)
	}
	return ToDomainTaskNodeSlice(dbNodes)
}

// ListNodeTransitions queries all state transition records of the specified node.
//
// Parameters:
//   - ctx: request context
//   - nodeID: UUID of the node
//
// Returns:
//   - []types.NodeTransition: list of transition records
//   - error: error if the query fails
func (s *Store) ListNodeTransitions(ctx context.Context, nodeID uuid.UUID) ([]types.NodeTransition, error) {
	transitions, err := s.q.ListNodeTransitions(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node transitions: %w", err)
	}
	return ToDomainNodeTransitionSlice(transitions)
}

// CreateSubtask creates a subtask record.
//
// Parameters:
//   - ctx: request context
//   - params: subtask creation parameters
//
// Returns:
//   - types.Task: the created subtask record
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

// ParseDueDate parses a date string into sql.NullTime, supporting RFC3339 and YYYY-MM-DD formats.
//
// Parameters:
//   - dateStr: pointer to the date string (may be nil)
//
// Returns:
//   - sql.NullTime: parsed time; returns Valid=false if parsing fails
func ParseDueDate(dateStr *string) sql.NullTime {
	if dateStr == nil || *dateStr == "" {
		return sql.NullTime{}
	}
	parsed, err := time.Parse(time.RFC3339, *dateStr)
	if err != nil {
		parsed, err = time.Parse("2006-01-02", *dateStr)
	}
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: parsed, Valid: true}
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
