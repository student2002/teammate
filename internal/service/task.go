// task.go implements the business logic for Task management, including Task creation, query, update, delete,
// as well as the state transitions of task nodes (task_nodes) and subtask management.
//
// This file is the Service layer entry point for the Task domain, relying on data access methods
// provided by the Store layer. All types use the domain types from internal/types; only a few
// nullable enum references such as db.NullTaskStatus remain (can be cleaned up once
// types.NullTaskStatus is introduced in the future).
package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TaskService provides the business logic related to Task management.
type TaskService struct {
	svc *Service
}

// ParseDueDate parses a due date string, supporting RFC3339 and YYYY-MM-DD formats.
// Returns sql.NullTime with Valid=false when parsing fails.
// Allows the handler layer to avoid importing the store package directly.
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

// NewTaskService creates a new TaskService instance.
func NewTaskService(svc *Service) *TaskService {
	return &TaskService{svc: svc}
}

// CreateTaskResult stores the result of a create Task operation.
type CreateTaskResult struct {
	Task  types.Task       // the created Task
	Nodes []types.TaskNode // Workflow nodes generated from the Workflow template
}

// Create creates a Task and generates ordered nodes from the Workflow template.
// After creation, it notifies agents via SSE events that there are nodes to claim.
//
// Steps:
//  1. Query the node definitions of the Workflow template
//  2. Call the Store to create the Task and generate Workflow nodes (within a transaction)
//  3. Publish node:pending SSE events to notify agents
//
// Parameters:
//   - ctx: request context
//   - projectID: Project ID
//   - params: parameters for creating the Task, including title, description, priority, Workflow name, etc.
//   - workflowTemplateID: Workflow template ID
//
// Returns:
//   - *CreateTaskResult: contains the Task and the generated node list
//   - error: possible errors (template not found, database write failure)
func (s *TaskService) Create(ctx context.Context, projectID uuid.UUID, params types.CreateTaskParams, workflowTemplateID uuid.UUID) (*CreateTaskResult, error) {
	templateNodes, err := s.svc.Store.ListTemplateNodes(ctx, workflowTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}

	task, nodes, err := s.svc.Store.CreateTask(ctx, params, templateNodes)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	s.publishNodePendingEvents(ctx, projectID, nodes)

	return &CreateTaskResult{Task: task, Nodes: nodes}, nil
}

// publishNodePendingEvents publishes node:pending or
// node:continuation_invite SSE events for newly created task nodes.
//
// Steps:
//  1. Query project info to get the Workspace ID
//  2. Iterate nodes to find the first node that needs handling:
//     - pending status: publish node:pending event (broadcast to Workspace)
//     - in_progress with continuation rights: publish node:continuation_invite event (directed)
//  3. A single event is enough to trigger all agents to poll, so return after the first one
//
// Parameters:
//   - ctx: request context
//   - projectID: Project ID
//   - nodes: the newly created node list
func (s *TaskService) publishNodePendingEvents(ctx context.Context, projectID uuid.UUID, nodes []types.TaskNode) {
	for _, node := range nodes {
		if node.Status == types.TaskNodeStatusPending {
			s.svc.publishToProject(ctx, projectID, types.EventNodePending, map[string]interface{}{
				"task_id":    node.TaskID,
				"node_id":    node.ID,
				"project_id": projectID.String(),
			})
			return
		}
		if node.Status == types.TaskNodeStatusInProgress && node.ReservedForAgentID != nil {
			reservedID, err := uuid.Parse(*node.ReservedForAgentID)
			if err != nil {
				continue
			}
			s.svc.publishToAgent(ctx, reservedID, types.EventNodeContinuationInvite, map[string]interface{}{
				"task_id":    node.TaskID,
				"node_id":    node.ID,
				"project_id": projectID.String(),
			})
			return
		}
	}
}

// Get retrieves Task info by ID.
//
// Parameters:
//   - ctx: request context
//   - id: Task ID
//
// Returns:
//   - types.Task: Task info
//   - error: possible errors (Task not found)
func (s *TaskService) Get(ctx context.Context, id int32) (types.Task, error) {
	return s.svc.Store.GetTask(ctx, id)
}

// TaskWithNodes contains a Task along with its Workflow nodes and enriched metadata.
type TaskWithNodes struct {
	Task         types.Task                `json:"task"`          // basic Task info
	Nodes        []types.TaskNode          `json:"nodes"`         // Workflow node list
	WorkflowName string                   `json:"workflow_name"` // Workflow template name
	GitBranch    string                   `json:"git_branch"`    // associated Git branch
	NodeTokens   map[string]NodeTokenUsage `json:"node_tokens"`   // Token usage per node
}

// NodeTokenUsage wraps the Token usage of a single node.
type NodeTokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// PaginatedTaskResult contains the result of a paginated Task query.
type PaginatedTaskResult struct {
	Tasks  []TaskWithNodes `json:"tasks"`  // Task list for the current page
	Total  int64           `json:"total"`  // total number of Tasks matching the criteria
	Limit  int32           `json:"limit"`  // page size
	Offset int32           `json:"offset"` // offset
}

// ListWithNodes lists Tasks in a Project along with their nodes, filtered by status.
//
// Parameters:
//   - ctx: request context
//   - params: query parameters, including Project ID and status filter
//
// Returns:
//   - []TaskWithNodes: Task list (with nodes and metadata)
//   - error: possible errors (database query failure)
func (s *TaskService) ListWithNodes(ctx context.Context, params types.ListTasksParams) ([]TaskWithNodes, error) {
	tasks, err := s.svc.Store.ListTasks(ctx, params)
	if err != nil {
		return nil, err
	}
	projectID, _ := uuid.Parse(params.WorkspaceID)
	return s.enrichWithNodes(ctx, tasks, projectID)
}

// ListAllWithNodes lists all Tasks in a Project (regardless of status) along with their nodes.
//
// Parameters:
//   - ctx: request context
//   - projectID: Project ID
//
// Returns:
//   - []TaskWithNodes: Task list (with nodes and metadata)
//   - error: possible errors (database query failure)
func (s *TaskService) ListAllWithNodes(ctx context.Context, projectID uuid.UUID) ([]TaskWithNodes, error) {
	tasks, err := s.svc.Store.ListAllTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.enrichWithNodes(ctx, tasks, projectID)
}

// ListTasksPaginatedWithNodes paginates Tasks in a Project along with their nodes,
// supporting status filtering and search. Does not filter historical Tasks;
// used by the historical Tasks page.
//
// Parameters:
//   - ctx: request context
//   - projectID: Project ID
//   - status: Task status filter (empty string means no status filter)
//   - searchQuery: search keyword (matches title or description; empty string means no search)
//   - limit: page size
//   - offset: offset
//
// Returns:
//   - *PaginatedTaskResult: paginated result, including the Task list for the current page and the total count
//   - error: possible errors (database query failure)
//
// Note: This method uses nullable enums such as db.NullTaskStatus to construct
// db.CountTasksByStatusParams and db.ListTasksPaginatedParams. Once types.NullTaskStatus
// is introduced in the future, the types can be unified.
func (s *TaskService) ListTasksPaginatedWithNodes(ctx context.Context, projectID uuid.UUID, status string, searchQuery string, limit, offset int32) (*PaginatedTaskResult, error) {
	var statuses []string
	if status != "" {
		statuses = append(statuses, status)
	}

	countParams := types.CountTasksByStatusParams{
		WorkspaceID: projectID.String(),
		Statuses:    statuses,
	}
	total, err := s.svc.Store.CountTasksByStatus(ctx, countParams)
	if err != nil {
		return nil, fmt.Errorf("count tasks: %w", err)
	}

	listParams := types.ListTasksPaginatedParams{
		WorkspaceID: projectID.String(),
		Statuses:    statuses,
		Limit:       limit,
		Offset:      offset,
	}
	tasks, err := s.svc.Store.ListTasksPaginated(ctx, listParams)
	if err != nil {
		return nil, fmt.Errorf("list tasks paginated: %w", err)
	}

	enriched, err := s.enrichWithNodes(ctx, tasks, projectID)
	if err != nil {
		return nil, fmt.Errorf("enrich with nodes: %w", err)
	}

	return &PaginatedTaskResult{
		Tasks:  enriched,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

// enrichWithNodes batch-loads nodes for all Tasks in a Project and groups them by task_id.
// It also fills in the Workflow name and Git branch info for each Task.
//
// Steps:
//  1. Batch query nodes for all Tasks under the Project (avoids N+1 queries)
//  2. Group them into a map by task_id
//  3. Iterate the Task list to assemble the TaskWithNodes structure
//
// Parameters:
//   - ctx: request context
//   - tasks: Task list
//   - projectID: Project ID
//
// Returns:
//   - []TaskWithNodes: Task list (with nodes and metadata)
//   - error: possible errors (database query failure)
func (s *TaskService) enrichWithNodes(ctx context.Context, tasks []types.Task, projectID uuid.UUID) ([]TaskWithNodes, error) {
	allNodes, err := s.svc.Store.ListTaskNodesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	nodesByTask := make(map[int32][]types.TaskNode, len(tasks))
	for _, n := range allNodes {
		nodesByTask[n.TaskID] = append(nodesByTask[n.TaskID], n)
	}

	// Batch query Token usage for all nodes (single query)
	var allNodeIDs []uuid.UUID
	for _, n := range allNodes {
		if id, err := uuid.Parse(n.ID); err == nil {
			allNodeIDs = append(allNodeIDs, id)
		}
	}
	tokenMap, err := s.svc.Store.GetTokenUsageByTaskNodes(ctx, allNodeIDs)
	if err != nil {
		return nil, err
	}

	result := make([]TaskWithNodes, 0, len(tasks))
	for _, t := range tasks {
		nodes := nodesByTask[t.ID]
		wfName := t.WorkflowName
		gitBranch := ""
		if t.GitBranch != nil {
			gitBranch = *t.GitBranch
		}

		// Build the node Token usage map for this Task
		nodeTokens := make(map[string]NodeTokenUsage, len(nodes))
		for _, n := range nodes {
			if tu, ok := tokenMap[uuid.MustParse(n.ID)]; ok && tu.TotalTokens > 0 {
				nodeTokens[n.ID] = NodeTokenUsage{
					InputTokens:  tu.InputTokens,
					OutputTokens: tu.OutputTokens,
				}
			}
		}

		result = append(result, TaskWithNodes{
			Task:         t,
			Nodes:        nodes,
			WorkflowName: wfName,
			GitBranch:    gitBranch,
			NodeTokens:   nodeTokens,
		})
	}
	return result, nil
}

// List lists the tasks in a project, filtered by status.
//
// Parameters:
//   - ctx: request context
//   - params: query parameters, including the project ID and status filter conditions
//
// Returns:
//   - []types.Task: task list
//   - error: possible errors (database query failure)
func (s *TaskService) List(ctx context.Context, params types.ListTasksParams) ([]types.Task, error) {
	return s.svc.Store.ListTasks(ctx, params)
}

// ListAll lists all tasks (regardless of status) in a project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - []types.Task: task list
//   - error: possible errors (database query failure)
func (s *TaskService) ListAll(ctx context.Context, projectID uuid.UUID) ([]types.Task, error) {
	return s.svc.Store.ListAllTasks(ctx, projectID)
}

// Update updates task info.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for updating the task, including the ID and the fields to update
//
// Returns:
//   - types.Task: updated task info
//   - error: possible errors (task not found, database update failure)
func (s *TaskService) Update(ctx context.Context, params types.UpdateTaskParams) (types.Task, error) {
	return s.svc.Store.UpdateTask(ctx, params)
}

// Delete soft-deletes a task and cancels all its unfinished nodes.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//
// Returns:
//   - error: possible errors (task not found, database deletion failure)
func (s *TaskService) Delete(ctx context.Context, taskID int32) error {
	return s.svc.Store.DeleteTask(ctx, taskID)
}

// CancelTaskNodes cancels all unfinished/uncancelled nodes in a task.
// It sets the node status to cancelled, terminating the in-progress workflow.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//
// Returns:
//   - error: possible errors (database update failure)
func (s *TaskService) CancelTaskNodes(ctx context.Context, taskID int32) error {
	return s.svc.Store.CancelTaskNodes(ctx, taskID)
}

// ListTaskNodes lists all workflow nodes of the specified task.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//
// Returns:
//   - []types.TaskNode: node list
//   - error: possible errors (database query failure)
func (s *TaskService) ListTaskNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	return s.svc.Store.ListTaskNodes(ctx, taskID)
}

// ListNodeTransitions lists all state-transition records of the specified node.
// Transition records are used to track the full state-change history of a node.
//
// Parameters:
//   - ctx: request context
//   - nodeID: node ID
//
// Returns:
//   - []types.NodeTransition: state-transition record list
//   - error: possible errors (database query failure)
func (s *TaskService) ListNodeTransitions(ctx context.Context, nodeID uuid.UUID) ([]types.NodeTransition, error) {
	return s.svc.Store.ListNodeTransitions(ctx, nodeID)
}

// CreateSubtask creates a subtask under a parent task.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating a subtask, including the parent task ID, title, description, etc.
//
// Returns:
//   - types.Task: the created subtask
//   - error: possible errors (parent task not found, database write failure)
func (s *TaskService) CreateSubtask(ctx context.Context, params types.CreateSubtaskParams) (types.Task, error) {
	return s.svc.Store.CreateSubtask(ctx, params)
}

// UpdateTaskGitBranch updates the Git branch name associated with the task.
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//   - gitBranch: Git branch name
//
// Returns:
//   - error: possible errors (database update failure)
func (s *TaskService) UpdateTaskGitBranch(ctx context.Context, taskID int32, gitBranch string) error {
	return s.svc.Store.UpdateTaskGitBranch(ctx, taskID, gitBranch)
}

// ListSubtasks lists all subtasks of a parent task.
//
// Parameters:
//   - ctx: request context
//   - parentTaskID: parent task ID
//
// Returns:
//   - []types.Task: subtask list
//   - error: possible errors (database query failure)
func (s *TaskService) ListSubtasks(ctx context.Context, parentTaskID int32) ([]types.Task, error) {
	return s.svc.Store.ListSubtasks(ctx, sql.NullInt32{Int32: parentTaskID, Valid: true})
}
