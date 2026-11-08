// workflow_run.go provides the transactional data-access surface of the V2
// workflow run engine. RunTx hands the service a Tx whose methods expose only
// domain types: the store layer keeps converting between sqlc-generated types
// and types.* at the boundary (see converter.go).
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// ErrNodeVersionConflict marks an optimistic-lock miss in UpdateTaskNodeRun:
// the node row changed between the caller's load and its write.
var ErrNodeVersionConflict = errors.New("task node version conflict")

// Tx is a transaction-bound query handle for the run engine. It is created by
// RunTx and cannot outlive the transaction's commit/rollback.
type Tx struct {
	q *db.Queries
}

// RunTx runs fn inside one database transaction and commits on success. The
// run engine uses it to make multi-statement advances atomic.
func (s *Store) RunTx(ctx context.Context, fn func(tx *Tx) error) error {
	sqlTx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer sqlTx.Rollback()

	tx := &Tx{q: s.q.WithTx(sqlTx)}
	if err := fn(tx); err != nil {
		return err
	}
	return sqlTx.Commit()
}

// AcquireTaskRunLock takes the transaction-scoped advisory lock that
// serializes concurrent advances of one task.
func (t *Tx) AcquireTaskRunLock(ctx context.Context, taskID int32) error {
	if err := t.q.AcquireTaskRunLock(ctx, int64(taskID)); err != nil {
		return fmt.Errorf("acquire task run lock: %w", err)
	}
	return nil
}

// GetTask loads one task row inside the transaction.
func (t *Tx) GetTask(ctx context.Context, taskID int32) (types.Task, error) {
	task, err := t.q.GetTask(ctx, taskID)
	if err != nil {
		return types.Task{}, fmt.Errorf("get task: %w", err)
	}
	return ToDomainTask(task)
}

// GetTaskNode loads one run node inside the transaction.
func (t *Tx) GetTaskNode(ctx context.Context, nodeID uuid.UUID) (types.TaskNode, error) {
	node, err := t.q.GetTaskNode(ctx, nodeID)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node: %w", err)
	}
	return ToDomainTaskNode(node)
}

// GetProjectNextTaskSequence returns MAX(sequence)+1 of the project's tasks.
func (t *Tx) GetProjectNextTaskSequence(ctx context.Context, projectID uuid.UUID) (int, error) {
	seq, err := t.q.GetProjectNextTaskSequence(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("get project next task sequence: %w", err)
	}
	return int(seq), nil
}

// CreateTaskForRun inserts the task main record of a workflow run.
func (t *Tx) CreateTaskForRun(ctx context.Context, params types.CreateTaskForRunParams) (types.Task, error) {
	dbParams, err := fromDomainCreateTaskForRunParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert create task for run params: %w", err)
	}
	task, err := t.q.CreateTaskForRun(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("create task for run: %w", err)
	}
	return ToDomainTask(task)
}

// UpdateTaskRunState writes the engine-owned task fields (status, context,
// failure) inside the transaction.
func (t *Tx) UpdateTaskRunState(ctx context.Context, taskID int32, status string, context, failure json.RawMessage) (types.Task, error) {
	task, err := t.q.UpdateTaskRunState(ctx, db.UpdateTaskRunStateParams{
		ID:      taskID,
		Status:  db.TaskStatus(status),
		Context: context,
		Failure: failure,
	})
	if err != nil {
		return types.Task{}, fmt.Errorf("update task run state: %w", err)
	}
	return ToDomainTask(task)
}

// ListTemplateNodes loads the template nodes for run instantiation.
func (t *Tx) ListTemplateNodes(ctx context.Context, templateID uuid.UUID) ([]types.WorkflowTemplateNode, error) {
	nodes, err := t.q.ListTemplateNodes(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}
	return ToDomainWorkflowTemplateNodeSlice(nodes)
}

// ListTemplateEdges loads the template edges for run instantiation.
func (t *Tx) ListTemplateEdges(ctx context.Context, templateID uuid.UUID) ([]types.WorkflowTemplateEdge, error) {
	edges, err := t.q.ListTemplateEdges(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template edges: %w", err)
	}
	return ToDomainWorkflowTemplateEdgeSlice(edges)
}

// InsertTaskNodeForRun copies one template node into the task graph.
func (t *Tx) InsertTaskNodeForRun(ctx context.Context, params types.InsertTaskNodeParams) (types.TaskNode, error) {
	dbParams, err := fromDomainInsertTaskNodeParams(params)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("convert insert task node params: %w", err)
	}
	node, err := t.q.InsertTaskNodeForRun(ctx, dbParams)
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("insert task node for run: %w", err)
	}
	return ToDomainTaskNode(node)
}

// InsertTaskEdgeForRun copies one template edge into the task graph.
func (t *Tx) InsertTaskEdgeForRun(ctx context.Context, params types.InsertTaskEdgeParams) (types.TaskEdge, error) {
	dbParams, err := fromDomainInsertTaskEdgeParams(params)
	if err != nil {
		return types.TaskEdge{}, fmt.Errorf("convert insert task edge params: %w", err)
	}
	edge, err := t.q.InsertTaskEdgeForRun(ctx, dbParams)
	if err != nil {
		return types.TaskEdge{}, fmt.Errorf("insert task edge for run: %w", err)
	}
	return ToDomainTaskEdge(edge)
}

// ListTaskNodes loads all run nodes of one task inside the transaction.
func (t *Tx) ListTaskNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	nodes, err := t.q.ListTaskNodesByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// ListTaskEdges loads all run edges of one task inside the transaction.
func (t *Tx) ListTaskEdges(ctx context.Context, taskID int32) ([]types.TaskEdge, error) {
	edges, err := t.q.ListTaskEdgesByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task edges: %w", err)
	}
	return ToDomainTaskEdgeSlice(edges)
}

// UpdateTaskNodeRun writes the full run state of one node with an optimistic
// version check and returns the new version.
func (t *Tx) UpdateTaskNodeRun(ctx context.Context, params types.UpdateTaskNodeRunParams) (int, error) {
	nodeID, err := stringToUUID(params.ID)
	if err != nil {
		return 0, fmt.Errorf("convert node id: %w", err)
	}
	var reserved uuid.NullUUID
	if params.ReservedForAgentID != nil {
		if u, parseErr := uuid.Parse(*params.ReservedForAgentID); parseErr == nil {
			reserved = uuid.NullUUID{UUID: u, Valid: true}
		}
	}
	version, err := t.q.UpdateTaskNodeRun(ctx, db.UpdateTaskNodeRunParams{
		ID:                 nodeID,
		Status:             db.TaskNodeStatus(params.Status),
		Input:              params.Input,
		Output:             params.Output,
		Error:              params.Error,
		AttemptCount:       int32(params.AttemptCount),
		MaxAttempts:        int32(params.MaxAttempts),
		NextRetryAt:        ptrToNullTime(params.NextRetryAt),
		WaitKey:            params.WaitKey,
		WaitExpiresAt:      ptrToNullTime(params.WaitExpiresAt),
		CompletedAt:        ptrToNullTime(params.CompletedAt),
		ReservedForAgentID: reserved,
		Version:            int32(params.Version),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrNodeVersionConflict
		}
		return 0, fmt.Errorf("update task node run: %w", err)
	}
	return int(version), nil
}

// CancelNonTerminalRunNodes cancels the waiting/ready siblings of a task that
// just reached a terminal state; it returns the cancelled node ids.
func (t *Tx) CancelNonTerminalRunNodes(ctx context.Context, taskID int32) ([]uuid.UUID, error) {
	ids, err := t.q.CancelNonTerminalRunNodes(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("cancel non-terminal run nodes: %w", err)
	}
	return ids, nil
}

// UpdateTaskEdgeResolution persists one edge resolution; only pending edges
// resolve, so the write is idempotent under the task advisory lock.
func (t *Tx) UpdateTaskEdgeResolution(ctx context.Context, edgeID string, resolution string) error {
	id, err := stringToUUID(edgeID)
	if err != nil {
		return fmt.Errorf("convert edge id: %w", err)
	}
	if err := t.q.UpdateTaskEdgeResolution(ctx, db.UpdateTaskEdgeResolutionParams{
		ID:         id,
		Resolution: db.EdgeResolution(resolution),
	}); err != nil {
		return fmt.Errorf("update task edge resolution: %w", err)
	}
	return nil
}

// CreateTaskAttempt upserts one attempt record keyed by (node, attempt_no).
func (t *Tx) CreateTaskAttempt(ctx context.Context, params types.CreateTaskAttemptParams) (types.TaskNodeAttempt, error) {
	dbParams, err := fromDomainCreateTaskAttemptParams(params)
	if err != nil {
		return types.TaskNodeAttempt{}, fmt.Errorf("convert create task attempt params: %w", err)
	}
	attempt, err := t.q.CreateTaskAttempt(ctx, dbParams)
	if err != nil {
		return types.TaskNodeAttempt{}, fmt.Errorf("create task attempt: %w", err)
	}
	return ToDomainTaskNodeAttempt(attempt)
}

// GetTaskAttemptsByTask loads every attempt record of one task.
func (t *Tx) GetTaskAttemptsByTask(ctx context.Context, taskID int32) ([]types.TaskNodeAttempt, error) {
	attempts, err := t.q.GetTaskAttemptsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task attempts by task: %w", err)
	}
	return ToDomainTaskNodeAttemptSlice(attempts)
}

// GetTaskAttempt loads one attempt record by id inside the transaction.
func (t *Tx) GetTaskAttempt(ctx context.Context, attemptID uuid.UUID) (types.TaskNodeAttempt, error) {
	attempt, err := t.q.GetTaskAttempt(ctx, attemptID)
	if err != nil {
		return types.TaskNodeAttempt{}, fmt.Errorf("get task attempt: %w", err)
	}
	return ToDomainTaskNodeAttempt(attempt)
}

// UpdateTaskNodeSummary writes the node summary written by the agentd
// completion receipt; the engine's full-state node write never touches it.
func (t *Tx) UpdateTaskNodeSummary(ctx context.Context, nodeID uuid.UUID, summary string) error {
	if err := t.q.UpdateTaskNodeSummary(ctx, db.UpdateTaskNodeSummaryParams{ID: nodeID, Summary: summary}); err != nil {
		return fmt.Errorf("update task node summary: %w", err)
	}
	return nil
}

// CreateNodeTransition appends one node transition-history record.
func (t *Tx) CreateNodeTransition(ctx context.Context, params types.CreateNodeTransitionParams) error {
	dbParams, err := FromDomainCreateNodeTransitionParams(params)
	if err != nil {
		return fmt.Errorf("convert create node transition params: %w", err)
	}
	if _, err := t.q.CreateNodeTransition(ctx, dbParams); err != nil {
		return fmt.Errorf("create node transition: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Non-transactional reads used by the scheduler and the graph endpoint
// ---------------------------------------------------------------------------

// ListRetryDueNodes returns waiting nodes whose retry time has passed.
func (s *Store) ListRetryDueNodes(ctx context.Context, now time.Time, limit int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListRetryDueNodes(ctx, db.ListRetryDueNodesParams{NextRetryAt: sql.NullTime{Time: now, Valid: true}, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list retry due nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// ListWaitDueNodes returns waiting duration-wait nodes whose wake time passed.
func (s *Store) ListWaitDueNodes(ctx context.Context, now time.Time, limit int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListWaitDueNodes(ctx, db.ListWaitDueNodesParams{WaitExpiresAt: sql.NullTime{Time: now, Valid: true}, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list wait due nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// GetTaskAttemptsByTask loads every attempt record of one task (graph view).
func (s *Store) GetTaskAttemptsByTask(ctx context.Context, taskID int32) ([]types.TaskNodeAttempt, error) {
	attempts, err := s.q.GetTaskAttemptsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task attempts by task: %w", err)
	}
	return ToDomainTaskNodeAttemptSlice(attempts)
}

// ListTaskEdges loads all run edges of one task (graph view).
func (s *Store) ListTaskEdges(ctx context.Context, taskID int32) ([]types.TaskEdge, error) {
	edges, err := s.q.ListTaskEdgesByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task edges: %w", err)
	}
	return ToDomainTaskEdgeSlice(edges)
}

// AgentInProgressAttempt is one running agentd attempt with the node and
// project context the recovery view needs.
type AgentInProgressAttempt struct {
	Node        types.TaskNode
	ExecutionID string
	ProjectID   string
}

// ListAgentInProgressAttempts loads the running agentd attempts of one agent
// instance with their node and project context (in-progress-nodes view).
func (s *Store) ListAgentInProgressAttempts(ctx context.Context, agentID uuid.UUID) ([]AgentInProgressAttempt, error) {
	rows, err := s.q.ListAgentInProgressAttempts(ctx, agentID.String())
	if err != nil {
		return nil, fmt.Errorf("list agent in progress attempts: %w", err)
	}
	out := make([]AgentInProgressAttempt, 0, len(rows))
	for _, row := range rows {
		node, err := ToDomainTaskNode(row.TaskNode)
		if err != nil {
			return nil, fmt.Errorf("convert in progress node: %w", err)
		}
		out = append(out, AgentInProgressAttempt{
			Node:        node,
			ExecutionID: row.ExecutionID.String(),
			ProjectID:   row.ProjectID.String(),
		})
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Parameter converters (domain → db)
// ---------------------------------------------------------------------------

func fromDomainCreateTaskForRunParams(p types.CreateTaskForRunParams) (db.CreateTaskForRunParams, error) {
	projectUUID, err := stringToUUID(p.ProjectID)
	if err != nil {
		return db.CreateTaskForRunParams{}, fmt.Errorf("convert project id: %w", err)
	}
	authorUUID, err := stringToUUID(p.AuthorID)
	if err != nil {
		return db.CreateTaskForRunParams{}, fmt.Errorf("convert author id: %w", err)
	}
	var templateID uuid.NullUUID
	if p.WorkflowTemplateID != nil {
		id, err := uuid.Parse(*p.WorkflowTemplateID)
		if err != nil {
			return db.CreateTaskForRunParams{}, fmt.Errorf("convert template id: %w", err)
		}
		templateID = uuid.NullUUID{UUID: id, Valid: true}
	}
	return db.CreateTaskForRunParams{
		ProjectID:          projectUUID,
		WorkflowName:       p.WorkflowName,
		WorkflowTemplateID: templateID,
		WorkflowVersion:    int32(p.WorkflowVersion),
		TriggerPayload:     p.TriggerPayload,
		Context:            p.Context,
		Title:              p.Title,
		AuthorType:         p.AuthorType,
		AuthorID:           authorUUID,
		Sequence:           int32(p.Sequence),
	}, nil
}

func fromDomainInsertTaskNodeParams(p types.InsertTaskNodeParams) (db.InsertTaskNodeForRunParams, error) {
	templateNodeID, err := stringToUUID(p.TemplateNodeID)
	if err != nil {
		return db.InsertTaskNodeForRunParams{}, fmt.Errorf("convert template node id: %w", err)
	}
	return db.InsertTaskNodeForRunParams{
		TaskID:          p.TaskID,
		TemplateNodeID:  templateNodeID,
		Name:            p.Name,
		SortOrder:       int32(p.SortOrder),
		TypeKey:         p.TypeKey,
		Config:          p.Config,
		Status:          db.TaskNodeStatus(p.Status),
		ActivationMode:  p.ActivationMode,
		Output:          p.Output,
		AttemptCount:    int32(p.AttemptCount),
		MaxAttempts:     int32(p.MaxAttempts),
		ReadonlyDirs:    rawToNullRaw(p.ReadonlyDirs),
		FullControlDirs: rawToNullRaw(p.FullControlDirs),
		CompletedAt:     ptrToNullTime(p.CompletedAt),
	}, nil
}

func fromDomainInsertTaskEdgeParams(p types.InsertTaskEdgeParams) (db.InsertTaskEdgeForRunParams, error) {
	sourceNodeID, err := stringToUUID(p.SourceNodeID)
	if err != nil {
		return db.InsertTaskEdgeForRunParams{}, fmt.Errorf("convert source node id: %w", err)
	}
	targetNodeID, err := stringToUUID(p.TargetNodeID)
	if err != nil {
		return db.InsertTaskEdgeForRunParams{}, fmt.Errorf("convert target node id: %w", err)
	}
	return db.InsertTaskEdgeForRunParams{
		TaskID:       p.TaskID,
		SourceNodeID: sourceNodeID,
		SourcePort:   p.SourcePort,
		TargetNodeID: targetNodeID,
		TargetPort:   p.TargetPort,
		Label:        p.Label,
		Config:       p.Config,
	}, nil
}

func fromDomainCreateTaskAttemptParams(p types.CreateTaskAttemptParams) (db.CreateTaskAttemptParams, error) {
	taskNodeID, err := stringToUUID(p.TaskNodeID)
	if err != nil {
		return db.CreateTaskAttemptParams{}, fmt.Errorf("convert task node id: %w", err)
	}
	return db.CreateTaskAttemptParams{
		TaskID:       p.TaskID,
		TaskNodeID:   taskNodeID,
		AttemptNo:    int32(p.AttemptNo),
		ExecutorKind: db.ExecutorKind(p.ExecutorKind),
		ExecutorID:   p.ExecutorID,
		Status:       db.AttemptStatus(p.Status),
		Input:        p.Input,
		Output:       p.Output,
		Error:        p.Error,
		CompletedAt:  ptrToNullTime(p.CompletedAt),
	}, nil
}

// ---------------------------------------------------------------------------
// S6 async-node queries: ready-node scans, spawned tasks and sub-run linkage
// ---------------------------------------------------------------------------

// CreateSpawnedTask inserts the plain task of an action.create_task node
// inside the transaction.
func (t *Tx) CreateSpawnedTask(ctx context.Context, params types.CreateSpawnedTaskParams) (types.Task, error) {
	projectUUID, err := stringToUUID(params.ProjectID)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert project id: %w", err)
	}
	authorUUID, err := stringToUUID(params.AuthorID)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert author id: %w", err)
	}
	task, err := t.q.CreateSpawnedTask(ctx, db.CreateSpawnedTaskParams{
		ProjectID:   projectUUID,
		Title:       params.Title,
		Description: sql.NullString{String: params.Description, Valid: true},
		AuthorType:  params.AuthorType,
		AuthorID:    authorUUID,
		Sequence:    int32(params.Sequence),
	})
	if err != nil {
		return types.Task{}, fmt.Errorf("create spawned task: %w", err)
	}
	return ToDomainTask(task)
}

// SetTaskParent links a sub-run task to its parent task row.
func (t *Tx) SetTaskParent(ctx context.Context, taskID, parentTaskID int32) error {
	if err := t.q.SetTaskParent(ctx, db.SetTaskParentParams{
		ID:           taskID,
		ParentTaskID: sql.NullInt32{Int32: parentTaskID, Valid: true},
	}); err != nil {
		return fmt.Errorf("set task parent: %w", err)
	}
	return nil
}

// CancelRunTask moves one active task to cancelled inside the transaction.
func (t *Tx) CancelRunTask(ctx context.Context, taskID int32) error {
	if err := t.q.CancelRunTask(ctx, taskID); err != nil {
		return fmt.Errorf("cancel run task: %w", err)
	}
	return nil
}

// ListReadyAsyncActionNodes returns ready action.http / action.create_task /
// workflow.call nodes awaiting execution by the scheduler worker.
func (s *Store) ListReadyAsyncActionNodes(ctx context.Context, limit int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListReadyAsyncActionNodes(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list ready async action nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// GetTaskNodeByWaitKey returns the node of one task registered under a wake
// key, e.g. the workflow.call node waiting for "call:{childTaskID}".
func (s *Store) GetTaskNodeByWaitKey(ctx context.Context, taskID int32, waitKey string) (types.TaskNode, error) {
	node, err := s.q.GetTaskNodeByWaitKey(ctx, db.GetTaskNodeByWaitKeyParams{TaskID: taskID, WaitKey: waitKey})
	if err != nil {
		return types.TaskNode{}, fmt.Errorf("get task node by wait key: %w", err)
	}
	return ToDomainTaskNode(node)
}

// ListCallWaitNodes returns waiting workflow.call nodes whose child task
// reached a terminal state; each wakes its parent's graph.
func (s *Store) ListCallWaitNodes(ctx context.Context, limit int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListCallWaitNodes(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("list call wait nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// ListActiveChildTaskIDs returns the still-active sub-run tasks of one parent.
func (s *Store) ListActiveChildTaskIDs(ctx context.Context, parentTaskID int32) ([]int32, error) {
	ids, err := s.q.ListActiveChildTaskIDs(ctx, sql.NullInt32{Int32: parentTaskID, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("list active child task ids: %w", err)
	}
	return ids, nil
}
