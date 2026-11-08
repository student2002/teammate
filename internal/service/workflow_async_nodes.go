// workflow_async_nodes.go implements the scheduler-driven executor of ready
// server-side async nodes (S6): action.http, action.create_task and
// workflow.call. The scan claims one node at a time (ready → running with an
// optimistic version check), dispatches by type_key and closes the attempt
// through the engine's CompleteNode/FailNode paths (retry budget and backoff
// included). The same worker tick runs the sub-workflow wake scan: waiting
// workflow.call nodes whose child task reached a terminal state wake their
// parent's graph.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

// asyncNodeScanLimit bounds one scan of ready async nodes / call-wait nodes.
const asyncNodeScanLimit int32 = 100

// asyncNodeTypes is the closed set of node types this worker executes.
var asyncNodeTypes = map[string]bool{
	"action.http":        true,
	"action.create_task": true,
	"workflow.call":      true,
}

// WorkflowAsyncNodeService executes ready async action nodes and drives the
// sub-workflow parent wake-ups.
type WorkflowAsyncNodeService struct {
	svc *Service
}

func NewWorkflowAsyncNodeService(svc *Service) *WorkflowAsyncNodeService {
	return &WorkflowAsyncNodeService{svc: svc}
}

// asyncNodeConcurrency bounds how many claimed nodes execute at once inside
// one scan.
const asyncNodeConcurrency = 4

// ProcessWorkflowAsyncNodes is the scheduler's entry point (task
// "workflow_async_nodes"): execute ready async nodes, then wake parents whose
// child run reached a terminal state.
func (s *WorkflowAsyncNodeService) ProcessWorkflowAsyncNodes(ctx context.Context) error {
	if err := s.processReadyNodes(ctx); err != nil {
		return err
	}
	return s.wakeCallParents(ctx)
}

// processReadyNodes claims and executes every ready async node of one scan.
func (s *WorkflowAsyncNodeService) processReadyNodes(ctx context.Context) error {
	nodes, err := s.svc.Store.ListReadyAsyncActionNodes(ctx, asyncNodeScanLimit)
	if err != nil {
		return fmt.Errorf("list ready async action nodes: %w", err)
	}
	// Claims run sequentially (the version CAS serializes them); executions
	// run concurrently within a small bounded pool so one slow HTTP node does
	// not stall the rest of the scan (plan §17 review: blocking scan).
	sem := make(chan struct{}, asyncNodeConcurrency)
	var wg sync.WaitGroup
	for i := range nodes {
		nodeID, err := uuid.Parse(nodes[i].ID)
		if err != nil {
			continue
		}
		claimed, node, attemptNo, err := s.claimAsyncNode(ctx, nodeID)
		if err != nil {
			slog.Error("claim async action node", "node_id", nodes[i].ID, "err", err)
			continue
		}
		if !claimed {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(node types.TaskNode, attemptNo int) {
			defer wg.Done()
			defer func() { <-sem }()
			s.dispatchAsyncNode(ctx, node, attemptNo)
		}(node, attemptNo)
	}
	wg.Wait()
	return nil
}

// dispatchAsyncNode routes one claimed node to its executor.
func (s *WorkflowAsyncNodeService) dispatchAsyncNode(ctx context.Context, node types.TaskNode, attemptNo int) {
	switch node.TypeKey {
	case "action.http":
		s.executeHTTPNode(ctx, node, attemptNo)
	case "action.create_task":
		s.executeCreateTaskNode(ctx, node, attemptNo)
	case "workflow.call":
		s.executeCallNode(ctx, node, attemptNo)
	default:
		slog.Error("async node executor missing", "node_id", node.ID, "type_key", node.TypeKey)
	}
}

// claimAsyncNode moves one ready async node to running and opens its running
// attempt inside one transaction. A node already claimed by another instance
// misses the version check and reports claimed=false.
func (s *WorkflowAsyncNodeService) claimAsyncNode(ctx context.Context, nodeID uuid.UUID) (bool, types.TaskNode, int, error) {
	var (
		node      types.TaskNode
		attemptNo int
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		loaded, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if loaded.Status != types.TaskNodeStatusReady || !asyncNodeTypes[loaded.TypeKey] {
			return nil
		}
		// Defence in depth: skip nodes of tasks cancelled after discovery.
		task, err := tx.GetTask(ctx, loaded.TaskID)
		if err != nil {
			return err
		}
		if task.Status != types.TaskStatusActive {
			return nil
		}
		loaded.Status = types.TaskNodeStatusRunning
		loaded.AttemptCount++
		if err := NewWorkflowRunService(s.svc).writeNodeRun(ctx, tx, &loaded); err != nil {
			if errors.Is(err, store.ErrNodeVersionConflict) {
				return nil
			}
			return err
		}
		attempt, err := tx.CreateTaskAttempt(ctx, types.CreateTaskAttemptParams{
			TaskID:       loaded.TaskID,
			TaskNodeID:   loaded.ID,
			AttemptNo:    loaded.AttemptCount,
			ExecutorKind: "server",
			Status:       "running",
			Input:        ensureJSON(loaded.Input),
			Output:       json.RawMessage("{}"),
			Error:        json.RawMessage("{}"),
		})
		if err != nil {
			return err
		}
		node = loaded
		attemptNo = int(attempt.AttemptNo)
		return nil
	})
	if err != nil {
		return false, types.TaskNode{}, 0, err
	}
	return attemptNo > 0, node, attemptNo, nil
}

// wakeCallParents scans waiting workflow.call nodes whose child task is
// terminal and wakes each parent's graph through WakeWorkflowCallParent.
func (s *WorkflowAsyncNodeService) wakeCallParents(ctx context.Context) error {
	nodes, err := s.svc.Store.ListCallWaitNodes(ctx, asyncNodeScanLimit)
	if err != nil {
		return fmt.Errorf("list call wait nodes: %w", err)
	}
	for i := range nodes {
		childID, err := childTaskIDFromCallKey(nodes[i].WaitKey)
		if err != nil {
			slog.Error("parse call wait key", "node_id", nodes[i].ID, "wait_key", nodes[i].WaitKey, "err", err)
			continue
		}
		if err := NewWorkflowRunService(s.svc).WakeWorkflowCallParent(ctx, childID); err != nil {
			slog.Error("wake workflow call parent", "node_id", nodes[i].ID, "child_task_id", childID, "err", err)
		}
	}
	return nil
}

// childTaskIDFromCallKey parses the child task id out of a "call:{id}" wake
// key.
func childTaskIDFromCallKey(waitKey string) (int32, error) {
	id, err := strconv.Atoi(strings.TrimPrefix(waitKey, "call:"))
	if err != nil {
		return 0, fmt.Errorf("invalid call wait key %q: %w", waitKey, err)
	}
	return int32(id), nil
}

// callWaitKey builds the wake key under which a workflow.call node waits for
// one child run.
func callWaitKey(childTaskID int32) string {
	return fmt.Sprintf("call:%d", childTaskID)
}

// resolveTaskContextFor builds the expression resolver of one node's task
// context (plan §3.5): the decoded context roots with the task id backfilled.
func resolveTaskContextFor(task types.Task, taskID int32) func(string) (any, bool) {
	ctxRoot := decodeRunContext(task.Context)
	if taskRoot, ok := ctxRoot["task"].(map[string]any); ok {
		taskRoot["id"] = taskID
	}
	return func(path string) (any, bool) { return workflow.ResolvePath(ctxRoot, path) }
}
