// workflow_call_test.go covers the sub-workflow mechanism (plan §9.5) at the
// service level: child run creation with parent linkage and call context
// roots, completed/failed children waking the parent through the matching
// ports, the depth limit, self-recursion rejection and the parent-cancel
// cascade onto active child runs.
package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// childTasks returns the task ids whose parent_task_id is parentID.
func (e *runTestEnv) childTasks(t *testing.T, parentID int32) []int32 {
	t.Helper()
	rows, err := e.db.Query(`SELECT id FROM tasks WHERE parent_task_id = $1 ORDER BY id`, parentID)
	if err != nil {
		t.Fatalf("query child tasks: %v", err)
	}
	defer rows.Close()
	var ids []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan child task id: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// callContext decodes a task's call_depth/call_chain context roots.
func (e *runTestEnv) callContext(t *testing.T, taskID int32) (int, []string) {
	t.Helper()
	raw := e.task(t, taskID).Context
	var ctxRoot struct {
		CallDepth int      `json:"call_depth"`
		CallChain []string `json:"call_chain"`
	}
	if err := json.Unmarshal(raw, &ctxRoot); err != nil {
		t.Fatalf("decode call context: %v", err)
	}
	return ctxRoot.CallDepth, ctxRoot.CallChain
}

// simpleEndTemplate persists trigger.manual → flow.end.
func (e *runTestEnv) simpleEndTemplate(t *testing.T, name string) string {
	t.Helper()
	triggerID, endID := uuid.NewString(), uuid.NewString()
	return e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{
			wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
			wfNode(endID, "flow.end", "完成", 1, `{}`),
		},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), triggerID, "success", endID, "completed"),
		})
}

// callFlowTemplate persists trigger.manual → workflow.call → flow.end with
// the success and failed ports routed to the matching end ports.
func (e *runTestEnv) callFlowTemplate(t *testing.T, targetTemplateID string) (string, string) {
	t.Helper()
	triggerID, callID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tplID := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{
			wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
			wfNode(callID, "workflow.call", "调用子流程", 1, fmt.Sprintf(`{"template_id":%q}`, targetTemplateID)),
			wfNode(endID, "flow.end", "完成", 2, `{}`),
		},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), triggerID, "success", callID, "input"),
			wfEdge(uuid.NewString(), callID, "success", endID, "completed"),
			wfEdge(uuid.NewString(), callID, "failed", endID, "failed"),
		})
	return tplID, callID
}

// TestWorkflowCallSubRunLifecycle verifies the completed-child path: the child
// run carries parent_task_id and the call context roots, the call node parks
// on wait_key "call:{childTaskID}" and the wake scan routes the success port.
func TestWorkflowCallSubRunLifecycle(t *testing.T) {
	e := setupRunTest(t)

	childTplID := e.simpleEndTemplate(t, "子流程")
	parentTplID, callID := e.callFlowTemplate(t, childTplID)

	parentID := e.start(t, parentTplID, json.RawMessage(`{}`))
	node := e.nodeByTemplate(t, parentID, callID)
	if node.Status != types.TaskNodeStatusReady {
		t.Fatalf("call node status = %s, want ready", node.Status)
	}

	e.runAsyncWorker(t)

	node = e.nodeByTemplate(t, parentID, callID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("call node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	children := e.childTasks(t, parentID)
	if len(children) != 1 {
		t.Fatalf("child runs = %v, want exactly one", children)
	}
	childID := children[0]

	var output struct {
		ChildTaskID int32  `json:"child_task_id"`
		ChildStatus string `json:"child_status"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode call output %s: %v", node.Output, err)
	}
	if output.ChildTaskID != childID || output.ChildStatus != types.TaskStatusCompleted {
		t.Fatalf("call output = %+v, want child %d completed", output, childID)
	}

	child := e.task(t, childID)
	if child.ParentTaskID == nil || *child.ParentTaskID != parentID {
		t.Fatalf("child parent_task_id = %v, want %d", child.ParentTaskID, parentID)
	}
	depth, chain := e.callContext(t, childID)
	if depth != 1 {
		t.Fatalf("child call_depth = %d, want 1", depth)
	}
	// The child inherits the parent's own template id on the chain: the guard
	// that rejects A→B→A at B (comprehensive review P1-1).
	if len(chain) != 2 || chain[0] != parentTplID || chain[1] != childTplID {
		t.Fatalf("child call_chain = %v, want [%s %s]", chain, parentTplID, childTplID)
	}
	if got := child.Status; got != types.TaskStatusCompleted {
		t.Fatalf("child status = %s, want completed", got)
	}
	if got := e.task(t, parentID).Status; got != types.TaskStatusCompleted {
		t.Fatalf("parent status = %s, want completed", got)
	}
}

// TestWorkflowCallChildFailureFailsParent verifies a failed child fails the
// call node and drives the parent through the failed port.
func TestWorkflowCallChildFailureFailsParent(t *testing.T) {
	e := setupRunTest(t)

	// The child calls the GitHub API without a project integration, so its
	// action.github node fails and takes the child run down with it.
	triggerID, actionID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	childTplID := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{
			wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
			wfNode(actionID, "action.github", "评论", 1, `{"operation":"add_comment","issue":"1","body":"x","retry":{"max_attempts":1,"backoff_seconds":0}}`),
			wfNode(endID, "flow.end", "完成", 2, `{}`),
		},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), triggerID, "success", actionID, "input"),
			wfEdge(uuid.NewString(), actionID, "success", endID, "completed"),
			wfEdge(uuid.NewString(), actionID, "failed", endID, "failed"),
		})
	parentTplID, callID := e.callFlowTemplate(t, childTplID)

	parentID := e.start(t, parentTplID, json.RawMessage(`{}`))
	e.runAsyncWorker(t)

	children := e.childTasks(t, parentID)
	if len(children) != 1 {
		t.Fatalf("child runs = %v, want exactly one", children)
	}
	childID := children[0]
	if got := e.task(t, childID).Status; got != types.TaskStatusActive {
		t.Fatalf("child status before github failure = %s, want active", got)
	}
	if node := e.nodeByTemplate(t, parentID, callID); node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("call node status = %s, want waiting", node.Status)
	}

	// The S5 worker fails the child's action.github node; the child task fails.
	actionWorker := service.NewWorkflowNodeActionService(e.svc)
	if err := actionWorker.ProcessGitHubActionNodes(context.Background()); err != nil {
		t.Fatalf("github worker: %v", err)
	}
	if got := e.task(t, childID).Status; got != types.TaskStatusFailed {
		t.Fatalf("child status after github failure = %s, want failed", got)
	}

	// The next async pass wakes the parent through the failed port.
	e.runAsyncWorker(t)

	node := e.nodeByTemplate(t, parentID, callID)
	if node.Status != types.TaskNodeStatusFailed {
		t.Fatalf("call node status = %s, want failed", node.Status)
	}
	var output struct {
		ChildStatus string `json:"child_status"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode call output %s: %v", node.Output, err)
	}
	if output.ChildStatus != types.TaskStatusFailed {
		t.Fatalf("call output child_status = %q, want failed", output.ChildStatus)
	}
	if code := nodeError(t, node)["code"]; code != "workflow_call_child_failed" {
		t.Fatalf("call error code = %q, want workflow_call_child_failed", code)
	}
	if got := e.task(t, parentID).Status; got != types.TaskStatusFailed {
		t.Fatalf("parent status = %s, want failed via the failed port", got)
	}
}

// TestWorkflowCallRejectsSelfRecursion verifies a template calling itself is
// rejected with workflow_call_invalid and no child run is created.
func TestWorkflowCallRejectsSelfRecursion(t *testing.T) {
	e := setupRunTest(t)

	placeholder := uuid.NewString()
	triggerID, callID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tplID := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{
			wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
			wfNode(callID, "workflow.call", "自调用", 1, fmt.Sprintf(`{"template_id":%q}`, placeholder)),
			wfNode(endID, "flow.end", "完成", 2, `{}`),
		},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), triggerID, "success", callID, "input"),
			wfEdge(uuid.NewString(), callID, "success", endID, "completed"),
			wfEdge(uuid.NewString(), callID, "failed", endID, "failed"),
		})

	parentID := e.start(t, tplID, json.RawMessage(`{}`))
	node := e.nodeByTemplate(t, parentID, callID)
	// Point the run node's config snapshot at the parent's own template.
	if _, err := e.db.Exec(`UPDATE task_nodes SET config = $2::jsonb WHERE id = $1`, node.ID,
		fmt.Sprintf(`{"template_id":%q}`, tplID)); err != nil {
		t.Fatalf("rewrite call config: %v", err)
	}

	e.runAsyncWorker(t)

	node = e.nodeByTemplate(t, parentID, callID)
	if node.Status != types.TaskNodeStatusFailed {
		t.Fatalf("call node status = %s, want failed", node.Status)
	}
	if code := nodeError(t, node)["code"]; code != "workflow_call_invalid" {
		t.Fatalf("error code = %q, want workflow_call_invalid", code)
	}
	if children := e.childTasks(t, parentID); len(children) != 0 {
		t.Fatalf("self-recursion created child runs %v, want none", children)
	}
	if got := e.task(t, parentID).Status; got != types.TaskStatusFailed {
		t.Fatalf("parent status = %s, want failed", got)
	}
}

// TestWorkflowCallDepthLimit verifies the depth budget: five nested calls
// instantiate, the sixth is rejected and the failure unwinds the whole chain.
func TestWorkflowCallDepthLimit(t *testing.T) {
	e := setupRunTest(t)

	// T0 → T1 → ... → T6: each template calls the next one.
	ids := make([]string, 7)
	ids[6] = e.simpleEndTemplate(t, "深层-6")
	for i := 5; i >= 0; i-- {
		ids[i], _ = e.callFlowTemplate(t, ids[i+1])
	}

	parentID := e.start(t, ids[0], json.RawMessage(`{}`))
	// Every pass executes the deepest ready call node and unwinds one wake
	// level; loop until the parent reaches a terminal state.
	for i := 0; i < 30; i++ {
		e.runAsyncWorker(t)
		if status := e.task(t, parentID).Status; status != types.TaskStatusActive {
			break
		}
	}

	if got := e.task(t, parentID).Status; got != types.TaskStatusFailed {
		t.Fatalf("parent status = %s, want failed after the depth rejection", got)
	}
	// Five nested children materialized, the sixth level never did.
	created := 0
	next := parentID
	for i := 0; i < 6; i++ {
		children := e.childTasks(t, next)
		if len(children) == 0 {
			break
		}
		created++
		next = children[0]
	}
	if created != 5 {
		t.Fatalf("nested child runs = %d, want exactly 5 (depth budget)", created)
	}
	depth, _ := e.callContext(t, next)
	if depth != 5 {
		t.Fatalf("deepest call_depth = %d, want 5", depth)
	}
}

// TestWorkflowCallParentCancelCascades verifies a cancelled parent explicitly
// cancels its still-active child runs recursively (plan §9.5).
func TestWorkflowCallParentCancelCascades(t *testing.T) {
	e := setupRunTest(t)

	// The grandchild parks on an agent.task that never completes, so the
	// child and grandchild stay active until the cascade.
	agentNode := func(id string) types.CreateTemplateNodeParams {
		return wfNode(id, "agent.task", "干活", 1,
			`{"instruction":"处理","assignee":{"mode":"any_agent"},"timeout_minutes":5,"retry":{"max_attempts":1,"backoff_seconds":0}}`)
	}
	gTrigger, gAgent, gEnd := uuid.NewString(), uuid.NewString(), uuid.NewString()
	grandchildTpl := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{wfNode(gTrigger, "trigger.manual", "开始", 0, `{}`), agentNode(gAgent), wfNode(gEnd, "flow.end", "完成", 2, `{}`)},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), gTrigger, "success", gAgent, "input"),
			wfEdge(uuid.NewString(), gAgent, "success", gEnd, "completed"),
			wfEdge(uuid.NewString(), gAgent, "failed", gEnd, "failed"),
		})
	childTpl, _ := e.callFlowTemplate(t, grandchildTpl)
	parentTpl, callID := e.callFlowTemplate(t, childTpl)

	parentID := e.start(t, parentTpl, json.RawMessage(`{}`))
	// Each pass materializes one nesting level: parent → child → grandchild.
	var grandchildren []int32
	for i := 0; i < 6; i++ {
		e.runAsyncWorker(t)
		current := e.childTasks(t, parentID)
		if len(current) == 1 {
			grandchildren = e.childTasks(t, current[0])
			if len(grandchildren) == 1 {
				break
			}
		}
	}
	children := e.childTasks(t, parentID)
	if len(children) != 1 {
		t.Fatalf("child runs = %v, want one", children)
	}
	childID := children[0]
	if len(grandchildren) != 1 {
		t.Fatalf("grandchild runs = %v, want one", grandchildren)
	}
	grandchildID := grandchildren[0]
	if got := e.task(t, grandchildID).Status; got != types.TaskStatusActive {
		t.Fatalf("grandchild status = %s, want active", got)
	}

	runSvc := service.NewWorkflowRunService(e.svc)
	if err := runSvc.CancelChildRuns(context.Background(), parentID); err != nil {
		t.Fatalf("cancel child runs: %v", err)
	}

	if got := e.task(t, childID).Status; got != types.TaskStatusCancelled {
		t.Fatalf("child status = %s, want cancelled", got)
	}
	if got := e.task(t, grandchildID).Status; got != types.TaskStatusCancelled {
		t.Fatalf("grandchild status = %s, want cancelled", got)
	}
	grandchildNodes, err := e.svc.Store.ListTaskNodes(context.Background(), grandchildID)
	if err != nil {
		t.Fatalf("list grandchild nodes: %v", err)
	}
	for _, n := range grandchildNodes {
		if n.TypeKey == "agent.task" && n.Status != types.TaskNodeStatusCancelled {
			t.Fatalf("grandchild agent node status = %s, want cancelled", n.Status)
		}
	}
	// The parent itself is untouched: its call node keeps waiting.
	if got := e.task(t, parentID).Status; got != types.TaskStatusActive {
		t.Fatalf("parent status = %s, want active", got)
	}
	if node := e.nodeByTemplate(t, parentID, callID); node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("parent call node status = %s, want waiting", node.Status)
	}
}

// TestWorkflowCallChildCancelFailsParent verifies the reverse cancel direction:
// a manually cancelled child run fails its parent's parked workflow.call node
// and drives the parent to a terminal state, instead of leaving it parked
// forever on a cancelled child (a cancelled child is not a call wake status).
func TestWorkflowCallChildCancelFailsParent(t *testing.T) {
	e := setupRunTest(t)

	// The child parks on an agent.task that never completes, so it stays
	// active until the manual cancel.
	gTrigger, gAgent, gEnd := uuid.NewString(), uuid.NewString(), uuid.NewString()
	childTpl := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{wfNode(gTrigger, "trigger.manual", "开始", 0, `{}`), wfNode(gAgent, "agent.task", "干活", 1,
			`{"instruction":"处理","assignee":{"mode":"any_agent"},"timeout_minutes":5,"retry":{"max_attempts":1,"backoff_seconds":0}}`), wfNode(gEnd, "flow.end", "完成", 2, `{}`)},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), gTrigger, "success", gAgent, "input"),
			wfEdge(uuid.NewString(), gAgent, "success", gEnd, "completed"),
			wfEdge(uuid.NewString(), gAgent, "failed", gEnd, "failed"),
		})
	parentTpl, callID := e.callFlowTemplate(t, childTpl)

	parentID := e.start(t, parentTpl, json.RawMessage(`{}`))
	var childID int32
	for i := 0; i < 6; i++ {
		e.runAsyncWorker(t)
		children := e.childTasks(t, parentID)
		if len(children) == 1 {
			childID = children[0]
			break
		}
	}
	if childID == 0 {
		t.Fatalf("child run did not materialize")
	}
	if got := e.task(t, childID).Status; got != types.TaskStatusActive {
		t.Fatalf("child status = %s, want active", got)
	}
	if node := e.nodeByTemplate(t, parentID, callID); node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("call node status before cancel = %s, want waiting", node.Status)
	}

	// Manual cancel of the child: the handler's cancel chain is a status
	// update (full params, as buildUpdateTaskParams does) followed by
	// CancelTaskRun.
	taskSvc := service.NewTaskService(e.svc)
	child := e.task(t, childID)
	description, constraints := child.Description, child.Constraints
	if _, err := taskSvc.Update(context.Background(), types.UpdateTaskParams{
		ID:          childID,
		Title:       child.Title,
		Description: &description,
		Constraints: &constraints,
		Status:      types.TaskStatusCancelled,
		Priority:    child.Priority,
		Labels:      child.Labels,
		DueDate:     child.DueDate,
	}); err != nil {
		t.Fatalf("cancel child task: %v", err)
	}
	runSvc := service.NewWorkflowRunService(e.svc)
	if err := runSvc.CancelTaskRun(context.Background(), childID); err != nil {
		t.Fatalf("cancel child run: %v", err)
	}

	node := e.nodeByTemplate(t, parentID, callID)
	if node.Status != types.TaskNodeStatusFailed {
		t.Fatalf("call node status = %s, want failed", node.Status)
	}
	if code := nodeError(t, node)["code"]; code != "workflow_call_child_cancelled" {
		t.Fatalf("call error code = %q, want workflow_call_child_cancelled", code)
	}
	var output struct {
		ChildStatus string `json:"child_status"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode call output %s: %v", node.Output, err)
	}
	if output.ChildStatus != types.TaskStatusCancelled {
		t.Fatalf("call output child_status = %q, want cancelled", output.ChildStatus)
	}
	if got := e.task(t, parentID).Status; got != types.TaskStatusFailed {
		t.Fatalf("parent status = %s, want failed via the failed port", got)
	}
}
