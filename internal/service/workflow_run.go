// workflow_run.go implements the V2 run engine (plan §9): template
// instantiation (Start), graph advancement (Advance), node completion and
// failure callbacks (CompleteNode/FailNode) and due-node waking for the
// scheduler (WakeDueNode).
//
// Every state change runs inside one transaction that holds the task's
// pg_advisory_xact_lock (via store.Tx), so concurrent advances of the same
// task serialize without row locks. Pure graph reasoning lives in
// internal/workflow; this service organizes transactions and persists results.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

// ErrNodeNotRunning marks a completion/failure callback for a node that is not
// in running state; the handler layer maps it to 409 (S4 agentd protocol).
var ErrNodeNotRunning = errors.New("workflow node is not running")

// ErrNodeConflict marks an optimistic-lock miss on a node write.
var ErrNodeConflict = store.ErrNodeVersionConflict

// ErrTemplateDisabled marks a Start call against a disabled template.
var ErrTemplateDisabled = errors.New("workflow template is disabled")

// ErrWorkspaceMismatch marks a Start call whose project and template live in
// different workspaces.
var ErrWorkspaceMismatch = errors.New("project and workflow template are not in the same workspace")

// maxAdvanceSteps bounds the synchronous node executions inside one advance;
// exceeding it means the graph or the engine is wrong (plan §9.2).
const maxAdvanceSteps = 100

// NodeResult is the completion payload of one node attempt.
type NodeResult struct {
	Output json.RawMessage
	Branch string
}

// NodeFailure is the failure payload of one node attempt.
type NodeFailure struct {
	Code    string
	Message string
}

// WorkflowRunService implements the business logic of workflow runs.
type WorkflowRunService struct {
	svc *Service
}

func NewWorkflowRunService(svc *Service) *WorkflowRunService {
	return &WorkflowRunService{svc: svc}
}

// Start instantiates a template into a new task run (plan §9.1): the task main
// record, one waiting node per template node (the trigger terminal), one
// pending edge per template edge, then the first advance in the same
// transaction. After commit it wakes the project's agents for ready agentd
// nodes. Returns the new task id.
func (s *WorkflowRunService) Start(ctx context.Context, templateID, projectID uuid.UUID, triggerPayload json.RawMessage, title string, authorID uuid.UUID) (int32, error) {
	template, err := s.svc.Store.GetWorkflowTemplate(ctx, templateID)
	if err != nil {
		return 0, fmt.Errorf("get workflow template: %w", err)
	}
	if !template.Enabled {
		return 0, fmt.Errorf("start run: %w", ErrTemplateDisabled)
	}
	project, err := s.svc.Store.GetProject(ctx, projectID)
	if err != nil {
		return 0, fmt.Errorf("get project: %w", err)
	}
	if project.WorkspaceID != template.WorkspaceID {
		return 0, fmt.Errorf("start run: %w", ErrWorkspaceMismatch)
	}
	if len(triggerPayload) == 0 {
		triggerPayload = json.RawMessage("{}")
	}
	if strings.TrimSpace(title) == "" {
		title = template.Name
	}

	var (
		taskID     int32
		readyNodes []string
	)
	err = s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		taskID, readyNodes, err = s.startTx(ctx, tx, template, project, triggerPayload, title, authorID)
		return err
	})
	if err != nil {
		return 0, err
	}
	s.publishReadyNodes(ctx, projectID, taskID, readyNodes)
	return taskID, nil
}

func (s *WorkflowRunService) startTx(ctx context.Context, tx *store.Tx, template types.WorkflowTemplate, project types.Project, triggerPayload json.RawMessage, title string, authorID uuid.UUID) (int32, []string, error) {
	templateID, err := uuid.Parse(template.ID)
	if err != nil {
		return 0, nil, fmt.Errorf("parse template id: %w", err)
	}
	projectUUID, err := uuid.Parse(project.ID)
	if err != nil {
		return 0, nil, fmt.Errorf("parse project id: %w", err)
	}
	templateNodes, err := tx.ListTemplateNodes(ctx, templateID)
	if err != nil {
		return 0, nil, err
	}
	templateEdges, err := tx.ListTemplateEdges(ctx, templateID)
	if err != nil {
		return 0, nil, err
	}

	// Run context (§3.5): trigger/task/vars/nodes roots. task.id is backfilled
	// after the insert below.
	ctxRoot := map[string]any{
		"trigger": decodeJSONValue(triggerPayload),
		"task": map[string]any{
			"project_id":    project.ID,
			"workspace_id":  project.WorkspaceID,
			"title":         title,
			"workflow_name": template.Name,
		},
		"vars":  map[string]any{},
		"nodes": map[string]any{},
	}
	contextJSON, err := json.Marshal(ctxRoot)
	if err != nil {
		return 0, nil, fmt.Errorf("marshal initial context: %w", err)
	}

	sequence, err := tx.GetProjectNextTaskSequence(ctx, projectUUID)
	if err != nil {
		return 0, nil, err
	}

	task, err := tx.CreateTaskForRun(ctx, types.CreateTaskForRunParams{
		ProjectID:          project.ID,
		WorkflowName:       template.Name,
		WorkflowTemplateID: &template.ID,
		WorkflowVersion:    template.Version,
		TriggerPayload:     triggerPayload,
		Context:            contextJSON,
		Title:              title,
		AuthorType:         "human",
		AuthorID:           authorID.String(),
		Sequence:           sequence,
	})
	if err != nil {
		return 0, nil, err
	}
	ctxRoot["task"].(map[string]any)["id"] = task.ID

	now := s.svc.Store.Clock.Now()
	// The run graph is self-contained: task_edges reference the task node
	// instances created below, so the engine, the run view and agentd all work
	// in one id space.
	taskNodeIDs := make(map[string]string, len(templateNodes))
	for _, tn := range templateNodes {
		status := types.TaskNodeStatusWaiting
		output := json.RawMessage("{}")
		attemptCount := 0
		var completedAt *time.Time
		if strings.HasPrefix(tn.TypeKey, "trigger.") {
			// The trigger is completed by Start itself (§9.1 step 4).
			status = types.TaskNodeStatusSucceeded
			output = triggerPayload
			attemptCount = 1
			completedAt = &now
		}
		node, err := tx.InsertTaskNodeForRun(ctx, types.InsertTaskNodeParams{
			TaskID:          task.ID,
			TemplateNodeID:  tn.ID,
			Name:            tn.Name,
			SortOrder:       tn.SortOrder,
			TypeKey:         tn.TypeKey,
			Config:          tn.Config,
			Status:          status,
			ActivationMode:  activationModeFor(tn.TypeKey, tn.Config),
			Output:          output,
			AttemptCount:    attemptCount,
			MaxAttempts:     maxAttemptsFromConfig(tn.Config),
			ReadonlyDirs:    configArrayField(tn.Config, "readonly_dirs"),
			FullControlDirs: configArrayField(tn.Config, "full_control_dirs"),
			CompletedAt:     completedAt,
		})
		if err != nil {
			return 0, nil, err
		}
		taskNodeIDs[tn.ID] = node.ID
		if status == types.TaskNodeStatusSucceeded {
			if err := s.writeAttempt(ctx, tx, &node, 1, "succeeded", json.RawMessage("{}"), output, json.RawMessage("{}"), now); err != nil {
				return 0, nil, err
			}
		}
	}

	for _, te := range templateEdges {
		sourceID, ok := taskNodeIDs[te.SourceNodeID]
		if !ok {
			return 0, nil, fmt.Errorf("copy edge: template source node %s has no task node", te.SourceNodeID)
		}
		targetID, ok := taskNodeIDs[te.TargetNodeID]
		if !ok {
			return 0, nil, fmt.Errorf("copy edge: template target node %s has no task node", te.TargetNodeID)
		}
		if _, err := tx.InsertTaskEdgeForRun(ctx, types.InsertTaskEdgeParams{
			TaskID:       task.ID,
			SourceNodeID: sourceID,
			SourcePort:   te.SourcePort,
			TargetNodeID: targetID,
			TargetPort:   te.TargetPort,
			Label:        te.Label,
			Config:       te.Config,
		}); err != nil {
			return 0, nil, err
		}
	}

	st, err := s.advanceTx(ctx, tx, task.ID)
	if err != nil {
		return 0, nil, err
	}
	return task.ID, st.readyAgentd, nil
}

// Advance runs one advance pass of a task inside a transaction and publishes
// node:ready events for agentd nodes that became ready.
func (s *WorkflowRunService) Advance(ctx context.Context, taskID int32) error {
	var st advanceState
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		var err error
		st, err = s.advanceTx(ctx, tx, taskID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishReadyNodes(ctx, st.projectID, taskID, st.readyAgentd)
	return nil
}

// CompleteNode closes a running node attempt as succeeded and advances the
// task (plan §9.3). A concurrent or duplicate completion misses the node's
// optimistic version and fails with ErrNodeConflict.
func (s *WorkflowRunService) CompleteNode(ctx context.Context, nodeID uuid.UUID, attemptNo int32, result NodeResult) error {
	var (
		st     advanceState
		taskID int32
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			return err
		}
		taskID = node.TaskID
		if node.Status != types.TaskNodeStatusRunning {
			return fmt.Errorf("complete node %s: %w", nodeID, ErrNodeNotRunning)
		}
		now := s.svc.Store.Clock.Now()
		output, err := mergeBranchIntoOutput(result.Output, result.Branch)
		if err != nil {
			return fmt.Errorf("complete node %s: %w", nodeID, err)
		}
		node.Status = types.TaskNodeStatusSucceeded
		node.Output = output
		node.CompletedAt = &now
		if int(attemptNo) > node.AttemptCount {
			node.AttemptCount = int(attemptNo)
		}
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		if err := s.writeAttempt(ctx, tx, &node, int(attemptNo), "succeeded", node.Input, output, json.RawMessage("{}"), now); err != nil {
			return err
		}
		st, err = s.advanceTx(ctx, tx, node.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishReadyNodes(ctx, st.projectID, taskID, st.readyAgentd)
	return nil
}

// FailNode closes a running node attempt as failed (plan §6.3, §9.3): before
// the attempt budget is exhausted the node returns to waiting with a
// scheduled retry, after it the node fails. The subsequent advance activates
// the failed port's edge; the engine's failed-node invariant (below) fails the
// task when no failure edge is connected.
func (s *WorkflowRunService) FailNode(ctx context.Context, nodeID uuid.UUID, attemptNo int32, failure NodeFailure) error {
	var (
		st     advanceState
		taskID int32
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			return err
		}
		taskID = node.TaskID
		if node.Status != types.TaskNodeStatusRunning {
			return fmt.Errorf("fail node %s: %w", nodeID, ErrNodeNotRunning)
		}
		now := s.svc.Store.Clock.Now()
		errorJSON, err := json.Marshal(map[string]string{"code": failure.Code, "message": failure.Message})
		if err != nil {
			return fmt.Errorf("fail node %s: %w", nodeID, err)
		}
		if int(attemptNo) > node.AttemptCount {
			node.AttemptCount = int(attemptNo)
		}
		if int(attemptNo) < node.MaxAttempts {
			backoff := backoffSeconds(node.Config)
			node.Status = types.TaskNodeStatusWaiting
			nextRetry := now.Add(backoff)
			node.NextRetryAt = &nextRetry
		} else {
			node.Status = types.TaskNodeStatusFailed
			node.Error = errorJSON
			node.CompletedAt = &now
		}
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		if err := s.writeAttempt(ctx, tx, &node, int(attemptNo), "failed", node.Input, json.RawMessage("{}"), errorJSON, now); err != nil {
			return err
		}
		st, err = s.advanceTx(ctx, tx, node.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishReadyNodes(ctx, st.projectID, taskID, st.readyAgentd)
	return nil
}

// CancelTaskRun cancels the non-terminal (waiting/ready) nodes of a manually
// cancelled task and resolves its live event waiters. The running nodes keep
// their interrupt flow; the terminal-node cleanup path owns engine-driven
// terminal states. A cancelled child run additionally fails its parent's
// parked workflow.call node, which would otherwise wait forever.
func (s *WorkflowRunService) CancelTaskRun(ctx context.Context, taskID int32) error {
	if err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		if _, err := tx.CancelNonTerminalRunNodes(ctx, taskID); err != nil {
			return err
		}
		return tx.ResolveWaitersByTask(ctx, taskID)
	}); err != nil {
		return err
	}
	return s.FailWorkflowCallParentOnChildCancel(ctx, taskID)
}

// WakeDueNode transitions one due waiting node back to ready and advances its
// task; it is the scheduler's entry point for retry-due and wait-due scans.
// A node already woken by another instance is skipped (idempotent).
func (s *WorkflowRunService) WakeDueNode(ctx context.Context, nodeID uuid.UUID) error {
	var (
		st     advanceState
		taskID int32
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if node.Status != types.TaskNodeStatusWaiting {
			return nil
		}
		node.Status = types.TaskNodeStatusReady
		// A woken node leaves the retry schedule behind; the stale timestamp
		// must not survive onto the ready/terminal row.
		node.NextRetryAt = nil
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			if errors.Is(err, store.ErrNodeVersionConflict) {
				return nil
			}
			return err
		}
		taskID = node.TaskID
		st, err = s.advanceTx(ctx, tx, node.TaskID)
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

// RunGraph is the data source of the run view (plan §13.5): the task
// snapshot, every run node, every run edge with its resolution, and all
// attempt records.
type RunGraph struct {
	Task     types.Task
	Nodes    []types.TaskNode
	Edges    []types.TaskEdge
	Attempts []types.TaskNodeAttempt
}

// Graph loads the run view data of one task.
func (s *WorkflowRunService) Graph(ctx context.Context, taskID int32) (*RunGraph, error) {
	task, err := s.svc.Store.GetTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	nodes, err := s.svc.Store.ListTaskNodes(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes: %w", err)
	}
	edges, err := s.svc.Store.ListTaskEdges(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task edges: %w", err)
	}
	attempts, err := s.svc.Store.GetTaskAttemptsByTask(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get task attempts: %w", err)
	}
	return &RunGraph{Task: task, Nodes: nodes, Edges: edges, Attempts: attempts}, nil
}

// ---------------------------------------------------------------------------
// Advance
// ---------------------------------------------------------------------------

// advanceState carries the mutable parts of one advance pass.
type advanceState struct {
	projectID   uuid.UUID
	status      string
	failure     json.RawMessage
	ctxRoot     map[string]any
	readyAgentd []string
}

// advanceTx is the advance core shared by Start/Advance/CompleteNode/FailNode/
// WakeDueNode. Advance reasoning serializes on the task's advisory lock while
// individual node writes made before the lock are guarded by the version CAS;
// it plans the next graph
// transitions, applies them and executes ready synchronous nodes until the
// graph is quiescent or the step limit is hit.
func (s *WorkflowRunService) advanceTx(ctx context.Context, tx *store.Tx, taskID int32) (advanceState, error) {
	st := advanceState{status: types.TaskStatusActive, failure: json.RawMessage("{}")}
	if err := tx.AcquireTaskRunLock(ctx, taskID); err != nil {
		return st, err
	}
	task, err := tx.GetTask(ctx, taskID)
	if err != nil {
		return st, err
	}
	if task.Status != types.TaskStatusActive {
		// The run already reached a terminal state; nothing to advance.
		return st, nil
	}
	projectID, err := uuid.Parse(task.ProjectID)
	if err != nil {
		return st, fmt.Errorf("parse project id: %w", err)
	}
	st.projectID = projectID
	if len(task.Failure) > 0 {
		st.failure = task.Failure
	}

	st.ctxRoot = decodeRunContext(task.Context)
	// task.id is backfilled on every decode: Start persists the initial
	// context before the generated task id exists (§3.5 task root).
	if taskRoot, ok := st.ctxRoot["task"].(map[string]any); ok {
		taskRoot["id"] = taskID
	} else {
		st.ctxRoot["task"] = map[string]any{"id": taskID}
	}
	resolve := func(path string) (any, bool) { return workflow.ResolvePath(st.ctxRoot, path) }

	nodes, err := tx.ListTaskNodes(ctx, taskID)
	if err != nil {
		return st, err
	}
	edges, err := tx.ListTaskEdges(ctx, taskID)
	if err != nil {
		return st, err
	}
	nodeByID := make(map[string]*types.TaskNode, len(nodes))
	for i := range nodes {
		nodeByID[nodes[i].ID] = &nodes[i]
	}
	edgeByID := make(map[string]*types.TaskEdge, len(edges))
	for i := range edges {
		edgeByID[edges[i].ID] = &edges[i]
	}

	// Nodes the scheduler already moved to ready (retry/wait wake-ups) join
	// the work queue alongside the ones this pass discovers.
	worklist := make([]*types.TaskNode, 0, len(nodes))
	for i := range nodes {
		if syncRunnable(&nodes[i]) {
			worklist = append(worklist, &nodes[i])
		}
	}

	now := s.svc.Store.Clock.Now()
	steps := 0
	for {
		// Rebuild the nodes root every pass: expressions must resolve outputs
		// of nodes that reached a terminal state earlier in this same advance
		// (trigger→first node, agent completion→downstream, plan §3.5/§4.1).
		st.ctxRoot["nodes"] = terminalNodesRoot(nodes)
		plan, err := workflow.PlanAdvance(s.runStates(nodes), edgeRunStates(edges))
		if err != nil {
			return st, fmt.Errorf("plan advance for task %d: %w", taskID, err)
		}
		if len(plan.EdgeUpdates) == 0 && len(plan.NodeUpdates) == 0 && len(worklist) == 0 {
			break
		}

		for _, u := range plan.EdgeUpdates {
			if err := tx.UpdateTaskEdgeResolution(ctx, u.EdgeID, u.Resolution); err != nil {
				return st, err
			}
			if e := edgeByID[u.EdgeID]; e != nil {
				e.Resolution = u.Resolution
				e.ResolvedAt = &now
			}
		}

		for _, u := range plan.NodeUpdates {
			n := nodeByID[u.NodeID]
			if n == nil {
				continue
			}
			if u.Status == workflow.NodeReady {
				if err := s.applyNodeReady(ctx, tx, n, &st, resolve, &worklist); err != nil {
					return st, err
				}
			} else {
				n.Status = types.TaskNodeStatusSkipped
				n.CompletedAt = &now
				if err := s.writeNodeRun(ctx, tx, n); err != nil {
					return st, err
				}
			}
		}

		for len(worklist) > 0 {
			if steps >= maxAdvanceSteps {
				// More than 100 synchronous steps: the graph or the engine is
				// wrong — fail the task (plan §9.2).
				st.status = types.TaskStatusFailed
				st.failure = json.RawMessage(`{"code":"engine_step_limit"}`)
				worklist = nil
				break
			}
			n := worklist[0]
			worklist = worklist[1:]
			if err := s.executeReadyNode(ctx, tx, n, &st, edges, resolve); err != nil {
				return st, err
			}
			steps++
			if st.status != types.TaskStatusActive {
				// flow.end finished the run; stop executing further nodes.
				worklist = nil
				break
			}
		}
		if st.status != types.TaskStatusActive {
			break
		}
	}

	// Failed-node invariant: a failed node without an activated outbound edge
	// has no failure path to flow.end, so the task fails with the node's error
	// (plan §6.3 "没有失败边则任务 failed").
	if st.status == types.TaskStatusActive {
		for i := range nodes {
			n := &nodes[i]
			if n.Status != types.TaskNodeStatusFailed {
				continue
			}
			if !hasActivatedOutbound(edges, n.ID) {
				st.status = types.TaskStatusFailed
				st.failure = ensureJSON(n.Error)
				break
			}
		}
	}

	// A terminal task cancels its non-terminal siblings (§6.2 cancelled) and
	// resolves its live event waiters so late events cannot wake the run.
	if st.status != types.TaskStatusActive {
		if _, err := tx.CancelNonTerminalRunNodes(ctx, taskID); err != nil {
			return st, err
		}
		if err := tx.ResolveWaitersByTask(ctx, taskID); err != nil {
			return st, err
		}
	}

	// Persist the context: terminal node outputs feed expressions and the run
	// view; the status/failure snapshot reflects any end/step-limit transition.
	st.ctxRoot["nodes"] = terminalNodesRoot(nodes)
	contextJSON, err := json.Marshal(st.ctxRoot)
	if err != nil {
		return st, fmt.Errorf("marshal run context: %w", err)
	}
	if _, err := tx.UpdateTaskRunState(ctx, taskID, st.status, contextJSON, ensureJSON(st.failure)); err != nil {
		return st, err
	}
	return st, nil
}

// applyNodeReady transitions a waiting node planned as ready: wait/human nodes
// go back to waiting with a wake key, synchronous nodes join the execution
// queue, async nodes stay ready (agentd ones are published after commit).
func (s *WorkflowRunService) applyNodeReady(ctx context.Context, tx *store.Tx, n *types.TaskNode, st *advanceState, resolve func(string) (any, bool), worklist *[]*types.TaskNode) error {
	now := s.svc.Store.Clock.Now()
	n.Input = resolveInputMap(n.Config, resolve)
	switch n.TypeKey {
	case "flow.wait":
		var cfg struct {
			Mode    string  `json:"mode"`
			Minutes float64 `json:"minutes"`
			Event   string  `json:"event"`
		}
		_ = json.Unmarshal(n.Config, &cfg)
		n.Status = types.TaskNodeStatusWaiting
		if cfg.Mode == "duration" {
			n.WaitKey = fmt.Sprintf("duration:%d:%s", n.TaskID, n.ID)
			expires := now.Add(time.Duration(cfg.Minutes * float64(time.Minute)))
			n.WaitExpiresAt = &expires
		} else {
			// github_event waits are woken by the event service (S5); no
			// expiry is tracked in this stage.
			n.WaitKey = "event:" + cfg.Event
			n.WaitExpiresAt = nil
			// Register the waiter the event service matches deliveries
			// against (plan §9.4).
			if err := s.createEventWaiterTx(ctx, tx, n); err != nil {
				return err
			}
		}
	case "human.input":
		// 停靠即进入"需人工"状态:监控/待办/提交入口都以此状态点亮
		n.Status = types.TaskNodeStatusManualIntervention
		n.WaitKey = fmt.Sprintf("human:%d:%s", n.TaskID, n.ID)
		n.WaitExpiresAt = nil
	default:
		n.Status = types.TaskNodeStatusReady
		if s.executorKind(n.TypeKey) == "agentd" {
			st.readyAgentd = append(st.readyAgentd, n.ID)
		}
		if workflow.IsSyncNode(n.TypeKey) || n.TypeKey == "flow.end" {
			*worklist = append(*worklist, n)
		}
	}
	return s.writeNodeRun(ctx, tx, n)
}

// executeReadyNode completes one ready synchronous node.
func (s *WorkflowRunService) executeReadyNode(ctx context.Context, tx *store.Tx, n *types.TaskNode, st *advanceState, edges []types.TaskEdge, resolve func(string) (any, bool)) error {
	now := s.svc.Store.Clock.Now()
	switch {
	case n.TypeKey == "flow.end":
		// The activated inbound port decides the task outcome; a failed port
		// wins when both are activated in the same pass. When the ports
		// activate in different passes, the first one decides — later passes
		// stop at the terminal task state.
		failedPort := false
		for _, e := range edges {
			if e.TargetNodeID != n.ID || e.Resolution != workflow.EdgeActivated {
				continue
			}
			if e.TargetPort == "failed" {
				failedPort = true
			}
		}
		n.Status = types.TaskNodeStatusSucceeded
		n.Output = json.RawMessage("{}")
		n.CompletedAt = &now
		n.AttemptCount++
		if err := s.writeNodeRun(ctx, tx, n); err != nil {
			return err
		}
		if err := s.writeAttempt(ctx, tx, n, n.AttemptCount, "succeeded", n.Input, n.Output, json.RawMessage("{}"), now); err != nil {
			return err
		}
		if failedPort {
			st.status = types.TaskStatusFailed
			st.failure = json.RawMessage(`{"code":"workflow_failed"}`)
		} else {
			st.status = types.TaskStatusCompleted
			st.failure = json.RawMessage("{}")
		}
		return nil

	case n.TypeKey == "flow.wait" && strings.HasPrefix(n.WaitKey, "duration:"):
		// Woken by the scheduler: the wait is over, emit the resumed port.
		n.Status = types.TaskNodeStatusSucceeded
		n.Output = json.RawMessage(`{"outcome":"resumed"}`)
		n.CompletedAt = &now
		n.WaitKey = ""
		n.WaitExpiresAt = nil
		n.AttemptCount++
		if err := s.writeNodeRun(ctx, tx, n); err != nil {
			return err
		}
		return s.writeAttempt(ctx, tx, n, n.AttemptCount, "succeeded", n.Input, n.Output, json.RawMessage("{}"), now)

	default:
		output, _, err := workflow.ExecuteSyncNode(n.TypeKey, n.Config, resolve, func(name string, value any) {
			st.ctxRoot["vars"].(map[string]any)[name] = value
		})
		n.AttemptCount++
		if err != nil {
			// Sync nodes are validated at save time, so an execution error is
			// an engine-level defect: fail the node and let the invariant /
			// failed port decide the task outcome.
			errorJSON, marshalErr := json.Marshal(map[string]string{"code": "node_execution_failed", "message": err.Error()})
			if marshalErr != nil {
				return fmt.Errorf("marshal node error: %w", marshalErr)
			}
			n.Status = types.TaskNodeStatusFailed
			n.Error = errorJSON
			n.CompletedAt = &now
			if err := s.writeNodeRun(ctx, tx, n); err != nil {
				return err
			}
			return s.writeAttempt(ctx, tx, n, n.AttemptCount, "failed", n.Input, json.RawMessage("{}"), errorJSON, now)
		}
		n.Status = types.TaskNodeStatusSucceeded
		n.Output = ensureJSON(output)
		n.CompletedAt = &now
		if err := s.writeNodeRun(ctx, tx, n); err != nil {
			return err
		}
		return s.writeAttempt(ctx, tx, n, n.AttemptCount, "succeeded", n.Input, n.Output, json.RawMessage("{}"), now)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (s *WorkflowRunService) runStates(nodes []types.TaskNode) []workflow.NodeRunState {
	states := make([]workflow.NodeRunState, 0, len(nodes))
	for _, n := range nodes {
		states = append(states, workflow.NodeRunState{
			ID:             n.ID,
			TypeKey:        n.TypeKey,
			Status:         workflow.NodeLifecycleStatus(n.Status),
			ActivationMode: n.ActivationMode,
			WaitKey:        n.WaitKey,
			RetryPending:   n.NextRetryAt != nil,
			Config:         n.Config,
			Output:         n.Output,
			Definition:     s.nodeDefinition(n.TypeKey),
		})
	}
	return states
}

func (s *WorkflowRunService) nodeDefinition(typeKey string) workflow.NodeDefinition {
	def, _ := s.svc.WorkflowRegistry.Get(typeKey)
	return def
}

func (s *WorkflowRunService) executorKind(typeKey string) string {
	return s.nodeDefinition(typeKey).ExecutorKind
}

func edgeRunStates(edges []types.TaskEdge) []workflow.EdgeRunState {
	states := make([]workflow.EdgeRunState, 0, len(edges))
	for _, e := range edges {
		states = append(states, workflow.EdgeRunState{
			ID:           e.ID,
			SourceNodeID: e.SourceNodeID,
			SourcePort:   e.SourcePort,
			TargetNodeID: e.TargetNodeID,
			TargetPort:   e.TargetPort,
			Resolution:   e.Resolution,
		})
	}
	return states
}

// syncRunnable reports whether a ready node is executed inline by the engine:
// synchronous nodes, flow.end, and duration waits woken by the scheduler.
func syncRunnable(n *types.TaskNode) bool {
	if n.Status != types.TaskNodeStatusReady {
		return false
	}
	if n.TypeKey == "flow.wait" {
		return strings.HasPrefix(n.WaitKey, "duration:")
	}
	return workflow.IsSyncNode(n.TypeKey) || n.TypeKey == "flow.end"
}

// writeNodeRun persists the full run state of one node with the optimistic
// version check and refreshes the caller's copy.
func (s *WorkflowRunService) writeNodeRun(ctx context.Context, tx *store.Tx, n *types.TaskNode) error {
	version, err := tx.UpdateTaskNodeRun(ctx, types.UpdateTaskNodeRunParams{
		ID:                 n.ID,
		Status:             n.Status,
		Input:              ensureJSON(n.Input),
		Output:             ensureJSON(n.Output),
		Error:              ensureJSON(n.Error),
		AttemptCount:       n.AttemptCount,
		MaxAttempts:        n.MaxAttempts,
		NextRetryAt:        n.NextRetryAt,
		WaitKey:            n.WaitKey,
		WaitExpiresAt:      n.WaitExpiresAt,
		CompletedAt:        n.CompletedAt,
		ReservedForAgentID: n.ReservedForAgentID,
		Version:            n.Version,
	})
	if err != nil {
		return fmt.Errorf("update node %s: %w", n.ID, err)
	}
	n.Version = version
	return nil
}

// writeAttempt upserts one attempt record in its terminal state. The executor
// kind follows the node type; a claim-time row keeps its original value.
func (s *WorkflowRunService) writeAttempt(ctx context.Context, tx *store.Tx, n *types.TaskNode, attemptNo int, status string, input, output, errorJSON json.RawMessage, completedAt time.Time) error {
	_, err := tx.CreateTaskAttempt(ctx, types.CreateTaskAttemptParams{
		TaskID:       n.TaskID,
		TaskNodeID:   n.ID,
		AttemptNo:    attemptNo,
		ExecutorKind: s.executorKind(n.TypeKey),
		ExecutorID:   "",
		Status:       status,
		Input:        ensureJSON(input),
		Output:       ensureJSON(output),
		Error:        ensureJSON(errorJSON),
		CompletedAt:  &completedAt,
	})
	if err != nil {
		return fmt.Errorf("write attempt %d for node %s: %w", attemptNo, n.ID, err)
	}
	return nil
}

func (s *WorkflowRunService) publishReadyNodes(ctx context.Context, projectID uuid.UUID, taskID int32, nodeIDs []string) {
	if len(nodeIDs) == 0 {
		return
	}
	for _, id := range nodeIDs {
		s.svc.publishToProject(ctx, projectID, types.EventNodeReady, map[string]any{
			"task_id":    taskID,
			"node_id":    id,
			"project_id": projectID.String(),
		})
	}
}

// activationModeFor derives a node's inbound activation mode: flow.end fires
// on any activated port (the completed/failed pair), flow.join follows its
// configured mode, everything else requires all inbound edges (plan §5.2).
func activationModeFor(typeKey string, config json.RawMessage) string {
	switch typeKey {
	case "flow.end":
		return types.ActivationModeAny
	case "flow.join":
		var cfg struct {
			Mode string `json:"mode"`
		}
		_ = json.Unmarshal(config, &cfg)
		if cfg.Mode == types.ActivationModeAny {
			return types.ActivationModeAny
		}
	}
	return types.ActivationModeAll
}

// maxAttemptsFromConfig reads config.retry.max_attempts; nodes without a
// retry policy run once.
func maxAttemptsFromConfig(config json.RawMessage) int {
	var cfg struct {
		Retry struct {
			MaxAttempts float64 `json:"max_attempts"`
		} `json:"retry"`
	}
	_ = json.Unmarshal(config, &cfg)
	if cfg.Retry.MaxAttempts < 1 {
		return 1
	}
	return int(cfg.Retry.MaxAttempts)
}

// backoffSeconds reads config.retry.backoff_seconds.
func backoffSeconds(config json.RawMessage) time.Duration {
	var cfg struct {
		Retry struct {
			BackoffSeconds float64 `json:"backoff_seconds"`
		} `json:"retry"`
	}
	_ = json.Unmarshal(config, &cfg)
	return time.Duration(cfg.Retry.BackoffSeconds * float64(time.Second))
}

// configArrayField extracts a JSON array field from node config; absent
// fields degrade to an empty array.
func configArrayField(config json.RawMessage, field string) json.RawMessage {
	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(config, &cfg); err != nil {
		return json.RawMessage("[]")
	}
	raw, ok := cfg[field]
	if !ok || len(raw) == 0 {
		return json.RawMessage("[]")
	}
	return raw
}

// resolveInputMap evaluates config.input_map against the run context (§3.5);
// nodes without an input_map resolve to {}.
func resolveInputMap(config json.RawMessage, resolve func(string) (any, bool)) json.RawMessage {
	var cfg struct {
		InputMap map[string]string `json:"input_map"`
	}
	if err := json.Unmarshal(config, &cfg); err != nil || len(cfg.InputMap) == 0 {
		return json.RawMessage("{}")
	}
	input := make(map[string]any, len(cfg.InputMap))
	for key, value := range cfg.InputMap {
		input[key] = workflow.RenderText(value, resolve)
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return json.RawMessage("{}")
	}
	return encoded
}

// decodeRunContext decodes the stored context and guarantees the four roots.
func decodeRunContext(raw json.RawMessage) map[string]any {
	root := map[string]any{
		"trigger": map[string]any{},
		"task":    map[string]any{},
		"vars":    map[string]any{},
		"nodes":   map[string]any{},
	}
	if len(raw) > 0 {
		var decoded map[string]any
		if err := json.Unmarshal(raw, &decoded); err == nil && decoded != nil {
			for key, value := range decoded {
				root[key] = value
			}
		}
	}
	return root
}

func decodeJSONValue(raw json.RawMessage) any {
	var value any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &value)
	}
	return value
}

// terminalNodesRoot builds the context.nodes map keyed by template node id.
// When a node's trimmed name is non-empty and unique across the whole graph
// (counted over all nodes, including non-terminal ones), its entry is also
// reachable under that name — the alias points at the same map, so expressions
// can address upstream output as nodes.<名字>.output like n8n's node-name refs
// (the trim matches the frontend's name handling). A duplicated name never
// becomes an alias; the id key always stays usable.
func terminalNodesRoot(nodes []types.TaskNode) map[string]any {
	out := map[string]any{}
	for _, n := range nodes {
		if !workflow.NodeLifecycleStatus(n.Status).IsTerminal() {
			continue
		}
		out[n.TemplateNodeID] = map[string]any{"output": decodeJSONValue(n.Output), "status": n.Status}
	}
	nameCounts := make(map[string]int, len(nodes))
	for _, n := range nodes {
		if name := strings.TrimSpace(n.Name); name != "" {
			nameCounts[name]++
		}
	}
	for _, n := range nodes {
		name := strings.TrimSpace(n.Name)
		if name == "" || nameCounts[name] != 1 {
			continue
		}
		if entry, ok := out[n.TemplateNodeID]; ok {
			// A name colliding with an existing key (another node's id) never
			// overwrites it: the id-keyed entry must stay addressable.
			if _, exists := out[name]; !exists {
				out[name] = entry
			}
		}
	}
	return out
}

func hasActivatedOutbound(edges []types.TaskEdge, nodeID string) bool {
	for _, e := range edges {
		if e.SourceNodeID == nodeID && e.Resolution == workflow.EdgeActivated {
			return true
		}
	}
	return false
}

// mergeBranchIntoOutput folds the emitted branch into the node output object.
func mergeBranchIntoOutput(output json.RawMessage, branch string) (json.RawMessage, error) {
	if branch == "" {
		return ensureJSON(output), nil
	}
	var m map[string]any
	if len(output) > 0 {
		if err := json.Unmarshal(output, &m); err != nil {
			return nil, fmt.Errorf("decode output: %w", err)
		}
	}
	if m == nil {
		m = map[string]any{}
	}
	m["branch"] = branch
	encoded, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode output: %w", err)
	}
	return encoded, nil
}

func ensureJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage("{}")
	}
	return raw
}

// ---------------------------------------------------------------------------
// Agentd execution protocol (S4, plan §12.6)
// ---------------------------------------------------------------------------

// ErrNodeNotClaimable marks a claim against a node that is not a claimable
// ready agentd node: wrong state, already claimed, or server-executed kind.
var ErrNodeNotClaimable = errors.New("workflow node is not claimable")

// ErrExecutionSuperseded marks a receipt whose execution id does not match
// the node's in-flight attempt: unknown id, already closed, or replaced.
var ErrExecutionSuperseded = errors.New("execution superseded")

// ErrNodeNotFound marks a node id that does not exist or does not belong to
// the addressed task.
var ErrNodeNotFound = errors.New("workflow node not found")

// ClaimForAgent claims a ready agentd node for one agent instance (plan
// §12.6): ready → running under the node's optimistic version, plus one
// running attempt row whose id becomes the opaque execution id the daemon
// echoes in receipts. A concurrent claim loses the version check and fails
// with ErrNodeVersionConflict.
func (s *WorkflowRunService) ClaimForAgent(ctx context.Context, taskID int32, nodeID uuid.UUID, agentID uuid.UUID) (types.ExecutionUnit, error) {
	var unit types.ExecutionUnit
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("claim node %s: %w", nodeID, ErrNodeNotFound)
			}
			return err
		}
		if node.TaskID != taskID {
			return fmt.Errorf("claim node %s: %w", nodeID, ErrNodeNotFound)
		}
		if node.Status != types.TaskNodeStatusReady || s.executorKind(node.TypeKey) != "agentd" {
			return fmt.Errorf("claim node %s: %w", nodeID, ErrNodeNotClaimable)
		}
		// Defence in depth: a task cancelled between the discovery poll and
		// this claim must not hand out new executions.
		task, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return err
		}
		if task.Status != types.TaskStatusActive {
			return fmt.Errorf("claim node %s: task %d is %s: %w", nodeID, taskID, task.Status, ErrNodeNotClaimable)
		}
		node.Status = types.TaskNodeStatusRunning
		node.AttemptCount++
		// The reservation feeds the task-cancel interrupt push, which finds
		// the claiming agent by this column.
		reserved := agentID.String()
		node.ReservedForAgentID = &reserved
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		attempt, err := tx.CreateTaskAttempt(ctx, types.CreateTaskAttemptParams{
			TaskID:       taskID,
			TaskNodeID:   node.ID,
			AttemptNo:    node.AttemptCount,
			ExecutorKind: "agentd",
			ExecutorID:   agentID.String(),
			Status:       "running",
			Input:        ensureJSON(node.Input),
			Output:       json.RawMessage("{}"),
			Error:        json.RawMessage("{}"),
		})
		if err != nil {
			return err
		}
		unit = BuildExecutionUnit(node, attempt.ID)
		return nil
	})
	if err != nil {
		return types.ExecutionUnit{}, err
	}
	return unit, nil
}

// CompleteForAgent closes the in-flight execution identified by the opaque
// execution id as succeeded (plan §12.6): the summary lands on the node, the
// output on the attempt, then the graph advances. A receipt for an unknown,
// closed or replaced execution, or against a node no longer running, fails
// with ErrExecutionSuperseded / ErrNodeNotRunning.
func (s *WorkflowRunService) CompleteForAgent(ctx context.Context, taskID int32, nodeID uuid.UUID, executionID, summary string, output json.RawMessage) error {
	var (
		st        advanceState
		pubTaskID int32
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		attempt, err := validateRunningAttempt(ctx, tx, taskID, nodeID, executionID)
		if err != nil {
			return err
		}
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			return err
		}
		if node.Status != types.TaskNodeStatusRunning {
			return fmt.Errorf("complete node %s: %w", nodeID, ErrNodeNotRunning)
		}
		now := s.svc.Store.Clock.Now()
		merged, err := mergeBranchIntoOutput(output, "")
		if err != nil {
			return fmt.Errorf("complete node %s: %w", nodeID, err)
		}
		node.Status = types.TaskNodeStatusSucceeded
		node.Output = merged
		node.CompletedAt = &now
		if int(attempt.AttemptNo) > node.AttemptCount {
			node.AttemptCount = int(attempt.AttemptNo)
		}
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		if err := tx.UpdateTaskNodeSummary(ctx, nodeID, summary); err != nil {
			return err
		}
		if err := s.writeAttempt(ctx, tx, &node, int(attempt.AttemptNo), "succeeded", node.Input, merged, json.RawMessage("{}"), now); err != nil {
			return err
		}
		pubTaskID = node.TaskID
		st, err = s.advanceTx(ctx, tx, node.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishReadyNodes(ctx, st.projectID, pubTaskID, st.readyAgentd)
	return nil
}

// FailForAgent closes the in-flight execution identified by the opaque
// execution id as failed (plan §6.3, §9.3): inside the retry budget the node
// returns to waiting with a scheduled retry, after it the node fails and the
// advance activates the failed port's edges. The error code is stored
// verbatim on the node and the attempt.
func (s *WorkflowRunService) FailForAgent(ctx context.Context, taskID int32, nodeID uuid.UUID, executionID, code, message string) error {
	var (
		st        advanceState
		pubTaskID int32
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		attempt, err := validateRunningAttempt(ctx, tx, taskID, nodeID, executionID)
		if err != nil {
			return err
		}
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			return err
		}
		if node.Status != types.TaskNodeStatusRunning {
			return fmt.Errorf("fail node %s: %w", nodeID, ErrNodeNotRunning)
		}
		now := s.svc.Store.Clock.Now()
		errorJSON, err := json.Marshal(map[string]string{"code": code, "message": message})
		if err != nil {
			return fmt.Errorf("fail node %s: %w", nodeID, err)
		}
		if int(attempt.AttemptNo) > node.AttemptCount {
			node.AttemptCount = int(attempt.AttemptNo)
		}
		if int(attempt.AttemptNo) < node.MaxAttempts {
			backoff := backoffSeconds(node.Config)
			node.Status = types.TaskNodeStatusWaiting
			nextRetry := now.Add(backoff)
			node.NextRetryAt = &nextRetry
		} else {
			node.Status = types.TaskNodeStatusFailed
			node.Error = errorJSON
			node.CompletedAt = &now
		}
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		if err := s.writeAttempt(ctx, tx, &node, int(attempt.AttemptNo), "failed", node.Input, json.RawMessage("{}"), errorJSON, now); err != nil {
			return err
		}
		pubTaskID = node.TaskID
		st, err = s.advanceTx(ctx, tx, node.TaskID)
		return err
	})
	if err != nil {
		return err
	}
	s.publishReadyNodes(ctx, st.projectID, pubTaskID, st.readyAgentd)
	return nil
}

// MarkNodeManualIntervention moves a running node to manual_intervention on
// the daemon's manual receipt: the execution id must still be the node's
// in-flight attempt, and the transition is recorded with the operator.
func (s *WorkflowRunService) MarkNodeManualIntervention(ctx context.Context, taskID int32, nodeID uuid.UUID, executionID, operatorID, operatorType, comment string) error {
	return s.moveNodeToManualIntervention(ctx, taskID, nodeID, &executionID, operatorID, operatorType, comment, "manual")
}

// AckNodeInterrupt records the daemon's interrupt acknowledgement: the node
// lands in manual_intervention with an interrupt_ack transition. The
// acknowledgement may arrive after the task was cancelled (the node is
// cancelled by then); terminal nodes reject it as stale.
func (s *WorkflowRunService) AckNodeInterrupt(ctx context.Context, taskID int32, nodeID uuid.UUID, operatorID, operatorType, comment string) error {
	return s.moveNodeToManualIntervention(ctx, taskID, nodeID, nil, operatorID, operatorType, comment, "interrupt_ack")
}

// moveNodeToManualIntervention is the shared core of the manual receipt and
// the interrupt acknowledgement. executionID is validated when present.
func (s *WorkflowRunService) moveNodeToManualIntervention(ctx context.Context, taskID int32, nodeID uuid.UUID, executionID *string, operatorID, operatorType, comment, action string) error {
	return s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		if executionID != nil {
			if _, err := validateRunningAttempt(ctx, tx, taskID, nodeID, *executionID); err != nil {
				return err
			}
		}
		node, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("node %s: %w", nodeID, ErrNodeNotFound)
			}
			return err
		}
		if node.TaskID != taskID {
			return fmt.Errorf("node %s: %w", nodeID, ErrNodeNotFound)
		}
		interruptible := node.Status == types.TaskNodeStatusRunning ||
			(action == "interrupt_ack" && node.Status == types.TaskNodeStatusCancelled)
		if !interruptible {
			return fmt.Errorf("node %s: %w", nodeID, ErrNodeNotRunning)
		}
		from := node.Status
		node.Status = types.TaskNodeStatusManualIntervention
		if err := s.writeNodeRun(ctx, tx, &node); err != nil {
			return err
		}
		return tx.CreateNodeTransition(ctx, types.CreateNodeTransitionParams{
			TaskNodeID:   node.ID,
			FromStatus:   from,
			ToStatus:     node.Status,
			Action:       action,
			Comment:      &comment,
			OperatorID:   nullableOperatorID(operatorID),
			OperatorType: operatorType,
		})
	})
}

// ListAgentInProgressNodes serves the daemon recovery view (plan §12.6):
// every node with a running agentd attempt of this agent instance, keyed by
// its original execution id so resumed receipts match the in-flight attempt.
func (s *WorkflowRunService) ListAgentInProgressNodes(ctx context.Context, agentID uuid.UUID) ([]types.InProgressExecution, error) {
	rows, err := s.svc.Store.ListAgentInProgressAttempts(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent in progress nodes: %w", err)
	}
	out := make([]types.InProgressExecution, 0, len(rows))
	for _, row := range rows {
		unit := BuildExecutionUnit(row.Node, row.ExecutionID)
		out = append(out, types.InProgressExecution{ExecutionUnit: unit, ProjectID: row.ProjectID})
	}
	return out, nil
}

// validateRunningAttempt resolves a receipt's execution id to the node's
// in-flight attempt: the attempt must exist, belong to the addressed task and
// node, and still be running. Any mismatch is a superseded execution.
func validateRunningAttempt(ctx context.Context, tx *store.Tx, taskID int32, nodeID uuid.UUID, executionID string) (types.TaskNodeAttempt, error) {
	attemptUUID, err := uuid.Parse(executionID)
	if err != nil {
		return types.TaskNodeAttempt{}, fmt.Errorf("validate execution %q: %w", executionID, ErrExecutionSuperseded)
	}
	attempt, err := tx.GetTaskAttempt(ctx, attemptUUID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return types.TaskNodeAttempt{}, fmt.Errorf("validate execution %q: %w", executionID, ErrExecutionSuperseded)
		}
		return types.TaskNodeAttempt{}, err
	}
	if attempt.TaskID != taskID || attempt.TaskNodeID != nodeID.String() || attempt.Status != "running" {
		return types.TaskNodeAttempt{}, fmt.Errorf("validate execution %q: %w", executionID, ErrExecutionSuperseded)
	}
	return attempt, nil
}

// nullableOperatorID maps an empty operator id to a NULL transition operator.
func nullableOperatorID(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}
