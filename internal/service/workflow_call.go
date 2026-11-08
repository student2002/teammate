// workflow_call.go implements the sub-workflow mechanism of the V2 engine
// (plan §9.5): StartSubRun instantiates a child template under the calling
// task (parent_task_id linkage, call_depth/call_chain context roots, recursion
// guards), the scheduler worker parks the calling workflow.call node on
// wait_key "call:{childTaskID}", and WakeWorkflowCallParent completes that
// node — and so the parent's graph — once the child run reaches a terminal
// state. CancelChildRuns cascades a parent task cancellation onto still
// active child runs.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// maxCallDepth bounds context.call_depth (plan §9.5): the child of a depth-5
// run would be depth 6 and is rejected.
const maxCallDepth = 5

// ErrWorkflowCallInvalid marks a workflow.call whose target is not runnable:
// unknown or disabled template, workspace mismatch, depth limit exceeded, or
// a recursive call (the template is already on the call chain).
var ErrWorkflowCallInvalid = errors.New("workflow call is invalid")

// StartSubRun instantiates templateID as a child run of parentTaskID and
// parks the calling workflow.call node on wait_key "call:{childTaskID}" (plan
// §9.5). Child creation, the parent linkage and the parking commit in one
// transaction, so a parked node always has its committed child run. The child
// carries parent_task_id and the call context roots: call_depth = parent
// depth + 1 and call_chain = parent chain + this template id. A call exceeding
// maxCallDepth or re-entering a template of the call chain — the parent's own
// template id included, so a root run cannot call itself — fails with
// ErrWorkflowCallInvalid. The call node must be claimed (running); a node
// moved by another writer rolls the whole call back.
func (s *WorkflowRunService) StartSubRun(ctx context.Context, templateID uuid.UUID, parentTaskID int32, parentNodeID uuid.UUID, triggerPayload json.RawMessage, title string) (int32, error) {
	var (
		childID   int32
		projectID uuid.UUID
		ready     []string
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		var err error
		childID, projectID, ready, err = s.startSubRunTx(ctx, tx, templateID, parentTaskID, parentNodeID, triggerPayload, title)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.publishReadyNodes(ctx, projectID, childID, ready)
	return childID, nil
}

// startSubRunTx is the transactional core of StartSubRun: validation, task
// instantiation (the Start core reused inside the same transaction), the
// sub-run linkage — parent task row plus the call context roots — and the
// calling node parked on the child's wake key.
func (s *WorkflowRunService) startSubRunTx(ctx context.Context, tx *store.Tx, templateID uuid.UUID, parentTaskID int32, parentNodeID uuid.UUID, triggerPayload json.RawMessage, title string) (int32, uuid.UUID, []string, error) {
	template, err := s.svc.Store.GetWorkflowTemplate(ctx, templateID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: template %s does not exist: %w", templateID, ErrWorkflowCallInvalid)
		}
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: get workflow template: %w", err)
	}
	if !template.Enabled {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: template %s is disabled: %w", templateID, ErrWorkflowCallInvalid)
	}

	parent, err := tx.GetTask(ctx, parentTaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: parent task %d does not exist: %w", parentTaskID, ErrWorkflowCallInvalid)
		}
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: get parent task: %w", err)
	}
	// A cancelled parent must not spawn child runs even if its call node was
	// claimed in the window before the cancel landed.
	if parent.Status != types.TaskStatusActive {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: parent task %d is %s: %w", parentTaskID, parent.Status, ErrWorkflowCallInvalid)
	}
	// The caller must be the claimed node of the parent task.
	node, err := tx.GetTaskNode(ctx, parentNodeID)
	if err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: get parent node: %w", err)
	}
	if node.TaskID != parentTaskID || node.TypeKey != "workflow.call" {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: node %s is not the workflow.call node of task %d: %w", parentNodeID, parentTaskID, ErrWorkflowCallInvalid)
	}

	projectUUID, err := uuid.Parse(parent.ProjectID)
	if err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: parse parent project id: %w", err)
	}
	project, err := s.svc.Store.GetProject(ctx, projectUUID)
	if err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: get project: %w", err)
	}
	if project.WorkspaceID != template.WorkspaceID {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: template %s is not in the parent's workspace: %w", templateID, ErrWorkflowCallInvalid)
	}

	parentCtx := decodeRunContext(parent.Context)
	childDepth := contextCallDepth(parentCtx) + 1
	if childDepth > maxCallDepth {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: call depth %d exceeds the limit of %d: %w", childDepth, maxCallDepth, ErrWorkflowCallInvalid)
	}
	chainGuard := contextCallChain(parentCtx)
	if parent.WorkflowTemplateID != nil {
		// The parent's own template sits on the chain even though a root run's
		// context carries no call_chain: this is what rejects a root run
		// calling itself (plan §9.5 禁止递归).
		chainGuard = append(slices.Clone(chainGuard), *parent.WorkflowTemplateID)
	}
	if slices.Contains(chainGuard, templateID.String()) {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: template %s is already on the call chain: %w", templateID, ErrWorkflowCallInvalid)
	}
	// The child inherits the full guard chain (which already contains the
	// parent task's own template id): A→B→A must be rejected at B, not at the
	// depth limit.
	childChain := append(slices.Clone(chainGuard), templateID.String())

	if len(triggerPayload) == 0 {
		triggerPayload = json.RawMessage("{}")
	}

	// The Start instantiation core runs inside this transaction; the sub-run
	// linkage below patches the task it created before the commit.
	childID, ready, err := s.startTx(ctx, tx, template, project, triggerPayload, title, systemAuthorID)
	if err != nil {
		return 0, uuid.UUID{}, nil, err
	}
	child, err := tx.GetTask(ctx, childID)
	if err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: reload child task: %w", err)
	}
	ctxRoot := decodeRunContext(child.Context)
	ctxRoot["call_depth"] = childDepth
	ctxRoot["call_chain"] = childChain
	contextJSON, err := json.Marshal(ctxRoot)
	if err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: marshal call context: %w", err)
	}
	if _, err := tx.UpdateTaskRunState(ctx, childID, child.Status, contextJSON, ensureJSON(child.Failure)); err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: write call context: %w", err)
	}
	if err := tx.SetTaskParent(ctx, childID, parentTaskID); err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: link parent task: %w", err)
	}

	// Park the calling node on the child's wake key inside the same
	// transaction; a version conflict rolls the child back with the park.
	parked, err := tx.GetTaskNode(ctx, parentNodeID)
	if err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: reload call node: %w", err)
	}
	if parked.Status != types.TaskNodeStatusRunning {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: call node %s is no longer running", parentNodeID)
	}
	parked.Status = types.TaskNodeStatusWaiting
	parked.WaitKey = callWaitKey(childID)
	parked.WaitExpiresAt = nil
	if err := s.writeNodeRun(ctx, tx, &parked); err != nil {
		return 0, uuid.UUID{}, nil, fmt.Errorf("start sub run: park call node: %w", err)
	}
	return childID, projectUUID, ready, nil
}

// executeCallNode runs one claimed workflow.call node: the config's
// template_id is resolved against the run context and StartSubRun instantiates
// the child run, links it to this task and parks the node waiting for the
// child's terminal state — all in one transaction.
func (s *WorkflowAsyncNodeService) executeCallNode(ctx context.Context, node types.TaskNode, attemptNo int) {
	nodeID, err := uuid.Parse(node.ID)
	if err != nil {
		return
	}
	runSvc := NewWorkflowRunService(s.svc)
	fail := func(code, format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		slog.Error("workflow.call node failed", "node_id", node.ID, "code", code, "message", message)
		if err := runSvc.FailNode(ctx, nodeID, int32(attemptNo), NodeFailure{Code: code, Message: message}); err != nil {
			slog.Error("fail workflow.call node", "node_id", node.ID, "err", err)
		}
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		fail("node_execution_failed", "load task: %v", err)
		return
	}
	resolve := resolveTaskContextFor(task, node.TaskID)

	var raw struct {
		TemplateID string `json:"template_id"`
	}
	if err := json.Unmarshal(node.Config, &raw); err != nil {
		fail("invalid_config", "decode workflow.call config: %v", err)
		return
	}
	templateID, err := uuid.Parse(strings.TrimSpace(resolveNodeString(raw.TemplateID, resolve)))
	if err != nil {
		fail("workflow_call_invalid", "template_id %q is not a UUID", raw.TemplateID)
		return
	}

	childID, err := runSvc.StartSubRun(ctx, templateID, node.TaskID, nodeID, node.Input, node.Name)
	switch {
	case err == nil:
	case errors.Is(err, ErrWorkflowCallInvalid):
		fail("workflow_call_invalid", "%v", err)
		return
	default:
		fail("workflow_call_failed", "%v", err)
		return
	}
	slog.Info("workflow.call started child run", "node_id", node.ID, "child_task_id", childID)
}

// WakeWorkflowCallParent completes the workflow.call node waiting for one
// child run and advances the parent's graph (plan §9.5): a completed child
// emits the success port, a failed child fails the node and emits the failed
// port. A node already woken by another instance is skipped (idempotent).
func (s *WorkflowRunService) WakeWorkflowCallParent(ctx context.Context, childTaskID int32) error {
	child, err := s.svc.Store.GetTask(ctx, childTaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("wake call parent: get child task: %w", err)
	}
	if child.ParentTaskID == nil || !isCallWakeStatus(child.Status) {
		return nil
	}
	waitKey := callWaitKey(childTaskID)
	node, err := s.svc.Store.GetTaskNodeByWaitKey(ctx, *child.ParentTaskID, waitKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("wake call parent: find call node: %w", err)
	}
	nodeID, err := uuid.Parse(node.ID)
	if err != nil {
		return fmt.Errorf("wake call parent: parse node id: %w", err)
	}

	var (
		st     advanceState
		taskID int32
	)
	err = s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		fresh, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if fresh.Status != types.TaskNodeStatusWaiting || fresh.WaitKey != waitKey {
			return nil
		}
		now := s.svc.Store.Clock.Now()
		fresh.AttemptCount++
		fresh.CompletedAt = &now
		fresh.WaitKey = ""
		fresh.WaitExpiresAt = nil
		output, err := json.Marshal(map[string]any{
			"child_task_id": childTaskID,
			"child_status":  child.Status,
		})
		if err != nil {
			return fmt.Errorf("wake call parent: marshal output: %w", err)
		}
		attemptStatus := "succeeded"
		fresh.Status = types.TaskNodeStatusSucceeded
		fresh.Output = output
		errorJSON := json.RawMessage("{}")
		if child.Status != types.TaskStatusCompleted {
			attemptStatus = "failed"
			fresh.Status = types.TaskNodeStatusFailed
			errorJSON, err = json.Marshal(map[string]string{
				"code":    "workflow_call_child_failed",
				"message": fmt.Sprintf("child task %d ended as %s", childTaskID, child.Status),
			})
			if err != nil {
				return fmt.Errorf("wake call parent: marshal error: %w", err)
			}
			fresh.Error = errorJSON
		}
		if err := s.writeNodeRun(ctx, tx, &fresh); err != nil {
			if errors.Is(err, store.ErrNodeVersionConflict) {
				return nil
			}
			return err
		}
		if err := s.writeAttempt(ctx, tx, &fresh, fresh.AttemptCount, attemptStatus, fresh.Input, output, errorJSON, now); err != nil {
			return err
		}
		taskID = fresh.TaskID
		st, err = s.advanceTx(ctx, tx, fresh.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	if taskID != 0 {
		s.publishReadyNodes(ctx, st.projectID, taskID, st.readyAgentd)
	}
	return nil
}

// isCallWakeStatus reports whether a child task state completes the waiting
// call node: completed and failed wake the parent (plan §9.5); a cancelled
// child does not — the parent cancellation cascade owns that path.
func isCallWakeStatus(status string) bool {
	return status == types.TaskStatusCompleted || status == types.TaskStatusFailed
}

// FailWorkflowCallParentOnChildCancel releases the parent of a manually
// cancelled child run (plan §9.5): a cancelled child is not a call wake
// status, so the workflow.call node parked on "call:{childTaskID}" would wait
// forever. The parked node fails with a child-cancelled error and the parent
// advances through the same wake path as WakeWorkflowCallParent. Tasks without
// a parent, non-cancelled tasks, parents without a parked call node and
// already woken nodes are skipped (idempotent).
func (s *WorkflowRunService) FailWorkflowCallParentOnChildCancel(ctx context.Context, childTaskID int32) error {
	child, err := s.svc.Store.GetTask(ctx, childTaskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("fail call parent on cancel: get child task: %w", err)
	}
	if child.ParentTaskID == nil || child.Status != types.TaskStatusCancelled {
		return nil
	}
	waitKey := callWaitKey(childTaskID)
	node, err := s.svc.Store.GetTaskNodeByWaitKey(ctx, *child.ParentTaskID, waitKey)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("fail call parent on cancel: find call node: %w", err)
	}
	nodeID, err := uuid.Parse(node.ID)
	if err != nil {
		return fmt.Errorf("fail call parent on cancel: parse node id: %w", err)
	}

	var (
		st     advanceState
		taskID int32
	)
	err = s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		fresh, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if fresh.Status != types.TaskNodeStatusWaiting || fresh.WaitKey != waitKey {
			return nil
		}
		now := s.svc.Store.Clock.Now()
		fresh.AttemptCount++
		fresh.CompletedAt = &now
		fresh.WaitKey = ""
		fresh.WaitExpiresAt = nil
		fresh.Status = types.TaskNodeStatusFailed
		errorJSON, err := json.Marshal(map[string]string{
			"code":    "workflow_call_child_cancelled",
			"message": "子运行已取消",
		})
		if err != nil {
			return fmt.Errorf("fail call parent on cancel: marshal error: %w", err)
		}
		fresh.Error = errorJSON
		output, err := json.Marshal(map[string]any{
			"child_task_id": childTaskID,
			"child_status":  child.Status,
		})
		if err != nil {
			return fmt.Errorf("fail call parent on cancel: marshal output: %w", err)
		}
		fresh.Output = output
		if err := s.writeNodeRun(ctx, tx, &fresh); err != nil {
			if errors.Is(err, store.ErrNodeVersionConflict) {
				return nil
			}
			return err
		}
		if err := s.writeAttempt(ctx, tx, &fresh, fresh.AttemptCount, "failed", fresh.Input, output, errorJSON, now); err != nil {
			return err
		}
		taskID = fresh.TaskID
		st, err = s.advanceTx(ctx, tx, fresh.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	if taskID != 0 {
		s.publishReadyNodes(ctx, st.projectID, taskID, st.readyAgentd)
	}
	return nil
}

// CancelChildRuns explicitly cancels every still-active child run of a
// cancelled parent task, recursively (plan §9.5 父任务取消时显式取消子任务).
func (s *WorkflowRunService) CancelChildRuns(ctx context.Context, parentTaskID int32) error {
	childIDs, err := s.svc.Store.ListActiveChildTaskIDs(ctx, parentTaskID)
	if err != nil {
		return fmt.Errorf("cancel child runs: list children: %w", err)
	}
	for _, childID := range childIDs {
		if err := s.cancelRunTaskTree(ctx, childID); err != nil {
			slog.Error("cancel child run", "parent_task_id", parentTaskID, "child_task_id", childID, "err", err)
			continue
		}
		// Agents mid-execution on the cancelled child are told to stop, the
		// same way the top-level manual cancel notifies its running nodes;
		// otherwise they would finish work nobody will consume.
		if err := s.interruptRunningAgents(ctx, childID); err != nil {
			slog.Error("interrupt child agents", "task_id", childID, "err", err)
		}
		if err := s.CancelChildRuns(ctx, childID); err != nil {
			slog.Error("cancel grandchild runs", "task_id", childID, "err", err)
		}
	}
	return nil
}

// interruptRunningAgents pushes task:interrupt to every agent executing a
// running reserved node of the task.
func (s *WorkflowRunService) interruptRunningAgents(ctx context.Context, taskID int32) error {
	nodes, err := s.svc.Store.ListTaskNodes(ctx, taskID)
	if err != nil {
		return fmt.Errorf("list nodes: %w", err)
	}
	for _, node := range nodes {
		if node.Status != types.TaskNodeStatusRunning || node.ReservedForAgentID == nil {
			continue
		}
		assigneeID, parseErr := uuid.Parse(*node.ReservedForAgentID)
		if parseErr != nil {
			continue
		}
		s.svc.PublishControlEvent(ctx, assigneeID, "task:interrupt", map[string]interface{}{
			"task_id": taskID,
			"node_id": node.ID,
		})
	}
	return nil
}

// cancelRunTaskTree cancels one run task and its non-terminal nodes in one
// transaction.
func (s *WorkflowRunService) cancelRunTaskTree(ctx context.Context, taskID int32) error {
	return s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		if err := tx.CancelRunTask(ctx, taskID); err != nil {
			return err
		}
		if _, err := tx.CancelNonTerminalRunNodes(ctx, taskID); err != nil {
			return err
		}
		return nil
	})
}

// contextCallDepth reads context.call_depth; a run without one is a root run.
func contextCallDepth(ctxRoot map[string]any) int {
	if depth, ok := ctxRoot["call_depth"].(float64); ok {
		return int(depth)
	}
	return 0
}

// contextCallChain reads context.call_chain as template id strings.
func contextCallChain(ctxRoot map[string]any) []string {
	raw, ok := ctxRoot["call_chain"].([]any)
	if !ok {
		return nil
	}
	chain := make([]string, 0, len(raw))
	for _, item := range raw {
		if id, ok := item.(string); ok {
			chain = append(chain, id)
		}
	}
	return chain
}
