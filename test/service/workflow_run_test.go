// workflow_run_test.go covers the V2 run engine at the service level:
// Start instantiation, condition/switch/parallel/join routing with skipped
// cascades, agent.task ready parking with resolved input, FailNode retry and
// exhaustion, end-node outcome routing, concurrent CompleteNode, wait/human
// wake keys and template snapshot isolation.
package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// wfNode builds a template node parameter from a fixed id.
func wfNode(id, typeKey, name string, order int32, config string) types.CreateTemplateNodeParams {
	return types.CreateTemplateNodeParams{
		ID:        &id,
		Name:      name,
		SortOrder: order,
		TypeKey:   typeKey,
		Config:    json.RawMessage(config),
		UIConfig:  json.RawMessage(`{}`),
	}
}

// wfEdge builds a template edge parameter from fixed node ids.
func wfEdge(id, source, srcPort, target, tgtPort string) types.CreateTemplateEdgeParams {
	return types.CreateTemplateEdgeParams{
		ID:           &id,
		SourceNodeID: source,
		SourcePort:   srcPort,
		TargetNodeID: target,
		TargetPort:   tgtPort,
		Config:       json.RawMessage(`{}`),
	}
}

// runTestEnv is the shared environment of the run engine tests.
type runTestEnv struct {
	svc      *service.Service
	db       *sql.DB
	wsID     string
	projID   string
	memberID string
}

func setupRunTest(t *testing.T) *runTestEnv {
	t.Helper()
	pgDB := svcConnectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	ws, err := svc.Store.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "run-test-" + uuid.New().String()[:8],
		Description: strPtr("workflow run test"),
		IssuePrefix: "RT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })

	member, err := svc.Store.CreateMember(ctx, types.CreateMemberParams{
		Name:  "run-tester",
		Email: "run-" + uuid.New().String()[:8] + "@test.local",
	})
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteMember(pgDB, member.ID) })

	if _, err := svc.Store.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "admin",
	}); err != nil {
		t.Fatalf("create workspace member: %v", err)
	}

	proj, err := svc.Store.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "run-proj-" + uuid.New().String()[:8],
		Description: strPtr("run test"),
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	return &runTestEnv{svc: svc, db: pgDB, wsID: ws.ID, projID: proj.ID, memberID: member.ID}
}

// createRunTemplate persists a V2 template through the store (the graphs in
// these tests are hand-built and valid).
func (e *runTestEnv) createRunTemplate(t *testing.T, nodes []types.CreateTemplateNodeParams, edges []types.CreateTemplateEdgeParams) string {
	t.Helper()
	tpl, _, _, err := e.svc.Store.CreateWorkflowTemplate(context.Background(), types.CreateWorkflowTemplateParams{
		WorkspaceID: e.wsID,
		Name:        "run-flow-" + uuid.New().String()[:8],
		Description: strPtr("run engine test template"),
		Version:     1,
		Enabled:     true,
	}, nodes, edges)
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	return tpl.ID
}

func (e *runTestEnv) authorID(t *testing.T) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(e.memberID)
	if err != nil {
		t.Fatalf("parse member id: %v", err)
	}
	return id
}

func (e *runTestEnv) start(t *testing.T, templateID string, input json.RawMessage) int32 {
	t.Helper()
	templateUUID, err := uuid.Parse(templateID)
	if err != nil {
		t.Fatalf("parse template id: %v", err)
	}
	projectUUID, err := uuid.Parse(e.projID)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}
	runSvc := service.NewWorkflowRunService(e.svc)
	taskID, err := runSvc.Start(context.Background(), templateUUID, projectUUID, input, "", e.authorID(t))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return taskID
}

// nodeByTemplate returns the run node derived from one template node.
func (e *runTestEnv) nodeByTemplate(t *testing.T, taskID int32, templateNodeID string) types.TaskNode {
	t.Helper()
	nodes, err := e.svc.Store.ListTaskNodes(context.Background(), taskID)
	if err != nil {
		t.Fatalf("list task nodes: %v", err)
	}
	for _, n := range nodes {
		if n.TemplateNodeID == templateNodeID {
			return n
		}
	}
	t.Fatalf("node with template id %s not found in task %d", templateNodeID, taskID)
	return types.TaskNode{}
}

func (e *runTestEnv) edgeByEnds(t *testing.T, taskID int32, sourceTemplateID, targetTemplateID string, srcPort string) types.TaskEdge {
	t.Helper()
	sourceNode := e.nodeByTemplate(t, taskID, sourceTemplateID)
	targetNode := e.nodeByTemplate(t, taskID, targetTemplateID)
	edges, err := e.svc.Store.ListTaskEdges(context.Background(), taskID)
	if err != nil {
		t.Fatalf("list task edges: %v", err)
	}
	for _, edge := range edges {
		if edge.SourceNodeID == sourceNode.ID && edge.TargetNodeID == targetNode.ID && edge.SourcePort == srcPort {
			return edge
		}
	}
	t.Fatalf("edge %s->%s(%s) not found in task %d", sourceTemplateID, targetTemplateID, srcPort, taskID)
	return types.TaskEdge{}
}

func (e *runTestEnv) task(t *testing.T, taskID int32) types.Task {
	t.Helper()
	task, err := e.svc.Store.GetTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	return task
}

func (e *runTestEnv) contextVars(t *testing.T, taskID int32) map[string]any {
	t.Helper()
	raw := e.task(t, taskID).Context
	var ctxRoot struct {
		Vars map[string]any `json:"vars"`
	}
	if err := json.Unmarshal(raw, &ctxRoot); err != nil {
		t.Fatalf("decode task context: %v", err)
	}
	return ctxRoot.Vars
}

func (e *runTestEnv) attemptsOf(t *testing.T, taskID int32, nodeID string) []types.TaskNodeAttempt {
	t.Helper()
	attempts, err := e.svc.Store.GetTaskAttemptsByTask(context.Background(), taskID)
	if err != nil {
		t.Fatalf("get attempts: %v", err)
	}
	var out []types.TaskNodeAttempt
	for _, a := range attempts {
		if a.TaskNodeID == nodeID {
			out = append(out, a)
		}
	}
	return out
}

// setNodeRunning moves a node to running via direct SQL, simulating the S4
// claim transition the executor protocol will own.
func (e *runTestEnv) setNodeRunning(t *testing.T, nodeID string) {
	t.Helper()
	if _, err := e.db.Exec("UPDATE task_nodes SET status = 'running', updated_at = now() WHERE id = $1", nodeID); err != nil {
		t.Fatalf("set node running: %v", err)
	}
}

// manualAgentGraph builds a trigger.manual → agent.task → flow.end template
// whose node/edge ids are shared between the node list and the edge list,
// returning the ids by role.
func manualAgentGraph(agentConfig string) ([]types.CreateTemplateNodeParams, []types.CreateTemplateEdgeParams, string, string, string) {
	triggerID := uuid.NewString()
	agentID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(agentID, "agent.task", "干活", 1, agentConfig),
		wfNode(endID, "flow.end", "完成", 2, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", agentID, "input"),
		wfEdge(uuid.NewString(), agentID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), agentID, "failed", endID, "failed"),
	}
	return nodes, edges, triggerID, agentID, endID
}

const agentInputMapConfig = `{
	"instruction": "分析输入",
	"assignee": {"mode": "any_agent"},
	"input_map": {"title": "{{ trigger.title }}", "n": "{{ trigger.n }}"},
	"timeout_minutes": 5,
	"retry": {"max_attempts": 1, "backoff_seconds": 0}
}`

func TestWorkflowRunStartManual(t *testing.T) {
	e := setupRunTest(t)
	nodes, edges, triggerID, agentID, endID := manualAgentGraph(agentInputMapConfig)
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"title":"hello","n":1}`))

	task := e.task(t, taskID)
	if task.Status != types.TaskStatusActive {
		t.Fatalf("task status = %s, want active", task.Status)
	}
	if task.WorkflowVersion != 1 {
		t.Fatalf("workflow_version = %d, want 1", task.WorkflowVersion)
	}
	if task.Sequence < 1 {
		t.Fatalf("sequence = %d, want >= 1", task.Sequence)
	}
	var payload map[string]any
	if err := json.Unmarshal(task.TriggerPayload, &payload); err != nil {
		t.Fatalf("decode trigger payload: %v", err)
	}
	if payload["title"] != "hello" {
		t.Fatalf("trigger payload = %v", payload)
	}
	if task.AuthorType != "human" {
		t.Fatalf("author_type = %s, want human", task.AuthorType)
	}

	trigger := e.nodeByTemplate(t, taskID, triggerID)
	if trigger.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("trigger status = %s, want succeeded", trigger.Status)
	}
	if trigger.AttemptCount != 1 {
		t.Fatalf("trigger attempt_count = %d, want 1", trigger.AttemptCount)
	}
	var triggerOut map[string]any
	if err := json.Unmarshal(trigger.Output, &triggerOut); err != nil || triggerOut["title"] != "hello" {
		t.Fatalf("trigger output = %s", trigger.Output)
	}
	triggerAttempts := e.attemptsOf(t, taskID, trigger.ID)
	if len(triggerAttempts) != 1 || triggerAttempts[0].AttemptNo != 1 || triggerAttempts[0].Status != "succeeded" {
		t.Fatalf("trigger attempts = %+v", triggerAttempts)
	}

	agent := e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusReady {
		t.Fatalf("agent status = %s, want ready", agent.Status)
	}
	var agentInput map[string]any
	if err := json.Unmarshal(agent.Input, &agentInput); err != nil {
		t.Fatalf("decode agent input: %v", err)
	}
	if agentInput["title"] != "hello" {
		t.Fatalf("agent input = %v, want resolved input_map", agentInput)
	}
	// A pure placeholder keeps the JSON type: trigger.n is a number.
	if agentInput["n"] != float64(1) {
		t.Fatalf("agent input n = %#v, want 1", agentInput["n"])
	}

	end := e.nodeByTemplate(t, taskID, endID)
	if end.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("end status = %s, want waiting", end.Status)
	}

	if edge := e.edgeByEnds(t, taskID, triggerID, agentID, "success"); edge.Resolution != "activated" {
		t.Fatalf("trigger edge resolution = %s, want activated", edge.Resolution)
	}
	if edge := e.edgeByEnds(t, taskID, agentID, endID, "success"); edge.Resolution != "pending" {
		t.Fatalf("agent success edge resolution = %s, want pending", edge.Resolution)
	}
}

// conditionGraph builds trigger → data.set(target_branch) → condition →
// yes-agent/no-agent with both agents ending at flow.end.
func conditionGraph() ([]types.CreateTemplateNodeParams, []types.CreateTemplateEdgeParams, string, string, string, string, string, string) {
	triggerID := uuid.NewString()
	setID := uuid.NewString()
	condID := uuid.NewString()
	yesID := uuid.NewString()
	noID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(setID, "data.set", "记录分支", 1, `{"name":"target_branch","value":"{{ trigger.branch }}"}`),
		wfNode(condID, "flow.condition", "是否目标分支", 2, `{"condition":{"all":[{"path":"vars.target_branch","op":"eq","value":"feature"}]}}`),
		wfNode(yesID, "agent.task", "走真分支", 3, agentInputMapConfig),
		wfNode(noID, "agent.task", "走假分支", 4, agentInputMapConfig),
		wfNode(endID, "flow.end", "完成", 5, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", setID, "input"),
		wfEdge(uuid.NewString(), setID, "success", condID, "input"),
		wfEdge(uuid.NewString(), condID, "true", yesID, "input"),
		wfEdge(uuid.NewString(), condID, "false", noID, "input"),
		wfEdge(uuid.NewString(), yesID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), yesID, "failed", endID, "failed"),
		wfEdge(uuid.NewString(), noID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), noID, "failed", endID, "failed"),
	}
	return nodes, edges, triggerID, setID, condID, yesID, noID, endID
}

func TestWorkflowRunConditionRouting(t *testing.T) {
	e := setupRunTest(t)
	nodes, edges, _, setID, condID, yesID, noID, endID := conditionGraph()
	tplID := e.createRunTemplate(t, nodes, edges)

	// True route: vars written by data.set drive the condition.
	taskID := e.start(t, tplID, json.RawMessage(`{"branch":"feature"}`))
	if vars := e.contextVars(t, taskID); vars["target_branch"] != "feature" {
		t.Fatalf("vars = %v, want target_branch=feature", vars)
	}
	if node := e.nodeByTemplate(t, taskID, setID); node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("data.set status = %s, want succeeded", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID, condID); node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("condition status = %s, want succeeded", node.Status)
	}
	var condOut struct {
		Branch string `json:"branch"`
	}
	if err := json.Unmarshal(e.nodeByTemplate(t, taskID, condID).Output, &condOut); err != nil || condOut.Branch != "true" {
		t.Fatalf("condition output = %s", e.nodeByTemplate(t, taskID, condID).Output)
	}
	if node := e.nodeByTemplate(t, taskID, yesID); node.Status != types.TaskNodeStatusReady {
		t.Fatalf("yes-agent status = %s, want ready", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID, noID); node.Status != types.TaskNodeStatusSkipped {
		t.Fatalf("no-agent status = %s, want skipped", node.Status)
	}
	// Skipped cascade: the skipped agent's outbound edges resolve skipped.
	if edge := e.edgeByEnds(t, taskID, noID, endID, "success"); edge.Resolution != "skipped" {
		t.Fatalf("no-agent success edge = %s, want skipped", edge.Resolution)
	}
	if edge := e.edgeByEnds(t, taskID, condID, noID, "false"); edge.Resolution != "skipped" {
		t.Fatalf("condition false edge = %s, want skipped", edge.Resolution)
	}

	// False route on a fresh run of the same template.
	taskID2 := e.start(t, tplID, json.RawMessage(`{"branch":"main"}`))
	if node := e.nodeByTemplate(t, taskID2, yesID); node.Status != types.TaskNodeStatusSkipped {
		t.Fatalf("yes-agent status = %s, want skipped", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID2, noID); node.Status != types.TaskNodeStatusReady {
		t.Fatalf("no-agent status = %s, want ready", node.Status)
	}
}

func TestWorkflowRunSwitchRouting(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	switchID := uuid.NewString()
	aID := uuid.NewString()
	bID := uuid.NewString()
	defID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(switchID, "flow.switch", "按平台分发", 1, `{"rules":[{"branch":"a","condition":{"all":[{"path":"trigger.k","op":"eq","value":"a"}]}},{"branch":"b","condition":{"any":[{"path":"trigger.k","op":"eq","value":"b"}]}}]}`),
		wfNode(aID, "agent.task", "分支A", 2, agentInputMapConfig),
		wfNode(bID, "agent.task", "分支B", 3, agentInputMapConfig),
		wfNode(defID, "agent.task", "默认分支", 4, agentInputMapConfig),
		wfNode(endID, "flow.end", "完成", 5, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", switchID, "input"),
		wfEdge(uuid.NewString(), switchID, "a", aID, "input"),
		wfEdge(uuid.NewString(), switchID, "b", bID, "input"),
		wfEdge(uuid.NewString(), switchID, "default", defID, "input"),
		wfEdge(uuid.NewString(), aID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), aID, "failed", endID, "failed"),
		wfEdge(uuid.NewString(), bID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), bID, "failed", endID, "failed"),
		wfEdge(uuid.NewString(), defID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), defID, "failed", endID, "failed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"k":"b"}`))
	if node := e.nodeByTemplate(t, taskID, bID); node.Status != types.TaskNodeStatusReady {
		t.Fatalf("branch b status = %s, want ready", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID, aID); node.Status != types.TaskNodeStatusSkipped {
		t.Fatalf("branch a status = %s, want skipped", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID, defID); node.Status != types.TaskNodeStatusSkipped {
		t.Fatalf("default branch status = %s, want skipped", node.Status)
	}

	// Unmatched value routes to default.
	taskID2 := e.start(t, tplID, json.RawMessage(`{"k":"zzz"}`))
	if node := e.nodeByTemplate(t, taskID2, defID); node.Status != types.TaskNodeStatusReady {
		t.Fatalf("default branch status = %s, want ready", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID2, bID); node.Status != types.TaskNodeStatusSkipped {
		t.Fatalf("branch b status = %s, want skipped", node.Status)
	}
}

func TestWorkflowRunParallelJoinAll(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	parID := uuid.NewString()
	set1ID := uuid.NewString()
	set2ID := uuid.NewString()
	joinID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(parID, "flow.parallel", "并行", 1, `{"branches":["p1","p2"]}`),
		wfNode(set1ID, "data.set", "分支一", 2, `{"name":"one","value":1}`),
		wfNode(set2ID, "data.set", "分支二", 3, `{"name":"two","value":2}`),
		wfNode(joinID, "flow.join", "汇合", 4, `{"mode":"all"}`),
		wfNode(endID, "flow.end", "完成", 5, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", parID, "input"),
		wfEdge(uuid.NewString(), parID, "p1", set1ID, "input"),
		wfEdge(uuid.NewString(), parID, "p2", set2ID, "input"),
		wfEdge(uuid.NewString(), set1ID, "success", joinID, "input"),
		wfEdge(uuid.NewString(), set2ID, "success", joinID, "input"),
		wfEdge(uuid.NewString(), joinID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	task := e.task(t, taskID)
	if task.Status != types.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed (all-sync graph)", task.Status)
	}
	if string(task.Failure) != "{}" {
		t.Fatalf("task failure = %s, want {{}}", task.Failure)
	}
	for _, id := range []string{parID, set1ID, set2ID, joinID, endID} {
		if node := e.nodeByTemplate(t, taskID, id); node.Status != types.TaskNodeStatusSucceeded {
			t.Fatalf("node %s status = %s, want succeeded", id, node.Status)
		}
	}
	// Both parallel ports activated.
	if edge := e.edgeByEnds(t, taskID, parID, set1ID, "p1"); edge.Resolution != "activated" {
		t.Fatalf("p1 edge = %s, want activated", edge.Resolution)
	}
	if edge := e.edgeByEnds(t, taskID, parID, set2ID, "p2"); edge.Resolution != "activated" {
		t.Fatalf("p2 edge = %s, want activated", edge.Resolution)
	}
	// data.set wrote vars one/two.
	if vars := e.contextVars(t, taskID); vars["one"] != float64(1) || vars["two"] != float64(2) {
		t.Fatalf("vars = %v", vars)
	}
}

func TestWorkflowRunConditionJoinAny(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	condID := uuid.NewString()
	setID := uuid.NewString()
	joinID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(condID, "flow.condition", "是否继续", 1, `{"condition":{"all":[{"path":"trigger.go","op":"eq","value":true}]}}`),
		wfNode(setID, "data.set", "设置变量", 2, `{"name":"went","value":true}`),
		wfNode(joinID, "flow.join", "汇合", 3, `{"mode":"any"}`),
		wfNode(endID, "flow.end", "完成", 4, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", condID, "input"),
		wfEdge(uuid.NewString(), condID, "true", setID, "input"),
		wfEdge(uuid.NewString(), setID, "success", joinID, "input"),
		wfEdge(uuid.NewString(), condID, "false", joinID, "input"),
		wfEdge(uuid.NewString(), joinID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	// True run: the set node feeds the join.
	taskID := e.start(t, tplID, json.RawMessage(`{"go":true}`))
	if node := e.nodeByTemplate(t, taskID, setID); node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("set status = %s, want succeeded", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID, joinID); node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("join status = %s, want succeeded", node.Status)
	}
	if task := e.task(t, taskID); task.Status != types.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed", task.Status)
	}

	// False run: the direct false edge activates the join, the set branch is
	// skipped end to end.
	taskID2 := e.start(t, tplID, json.RawMessage(`{"go":false}`))
	if node := e.nodeByTemplate(t, taskID2, setID); node.Status != types.TaskNodeStatusSkipped {
		t.Fatalf("set status = %s, want skipped", node.Status)
	}
	if node := e.nodeByTemplate(t, taskID2, joinID); node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("join status = %s, want succeeded", node.Status)
	}
	if task := e.task(t, taskID2); task.Status != types.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed", task.Status)
	}
}

func TestWorkflowRunFailNodeRetryThenFailureEdge(t *testing.T) {
	e := setupRunTest(t)
	retryConfig := `{
		"instruction": "重试的任务",
		"assignee": {"mode": "any_agent"},
		"timeout_minutes": 5,
		"retry": {"max_attempts": 2, "backoff_seconds": 0}
	}`
	nodes, edges, _, agentID, endID := manualAgentGraph(retryConfig)
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	agent := e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusReady {
		t.Fatalf("agent status = %s, want ready", agent.Status)
	}

	runSvc := service.NewWorkflowRunService(e.svc)
	ctx := context.Background()
	nodeID := mustParse(t, agent.ID)

	// First failure schedules a retry.
	e.setNodeRunning(t, agent.ID)
	if err := runSvc.FailNode(ctx, nodeID, 1, service.NodeFailure{Code: "boom", Message: "first"}); err != nil {
		t.Fatalf("fail node 1: %v", err)
	}
	agent = e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("agent status = %s, want waiting (retry scheduled)", agent.Status)
	}
	if agent.NextRetryAt == nil {
		t.Fatalf("next_retry_at is nil, want scheduled")
	}
	if agent.AttemptCount != 1 {
		t.Fatalf("attempt_count = %d, want 1", agent.AttemptCount)
	}
	attempts := e.attemptsOf(t, taskID, agent.ID)
	if len(attempts) != 1 || attempts[0].Status != "failed" || attempts[0].AttemptNo != 1 {
		t.Fatalf("attempts = %+v", attempts)
	}
	if task := e.task(t, taskID); task.Status != types.TaskStatusActive {
		t.Fatalf("task status = %s, want active", task.Status)
	}

	// The retry-due scan wakes the node back to ready.
	if err := runSvc.WakeDueNode(ctx, nodeID); err != nil {
		t.Fatalf("wake due node: %v", err)
	}
	agent = e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusReady {
		t.Fatalf("agent status = %s, want ready after wake", agent.Status)
	}

	// Second failure exhausts the budget: node failed, failed edge activates,
	// flow.end fails the task.
	e.setNodeRunning(t, agent.ID)
	if err := runSvc.FailNode(ctx, nodeID, 2, service.NodeFailure{Code: "boom", Message: "second"}); err != nil {
		t.Fatalf("fail node 2: %v", err)
	}
	agent = e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusFailed {
		t.Fatalf("agent status = %s, want failed", agent.Status)
	}
	var nodeErr struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(agent.Error, &nodeErr); err != nil || nodeErr.Code != "boom" {
		t.Fatalf("node error = %s", agent.Error)
	}
	if edge := e.edgeByEnds(t, taskID, agentID, endID, "failed"); edge.Resolution != "activated" {
		t.Fatalf("failed edge = %s, want activated", edge.Resolution)
	}
	end := e.nodeByTemplate(t, taskID, endID)
	if end.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("end status = %s, want succeeded", end.Status)
	}
	task := e.task(t, taskID)
	if task.Status != types.TaskStatusFailed {
		t.Fatalf("task status = %s, want failed", task.Status)
	}
	var failure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(task.Failure, &failure); err != nil || failure.Code != "workflow_failed" {
		t.Fatalf("task failure = %s", task.Failure)
	}
	if got := e.attemptsOf(t, taskID, agent.ID); len(got) != 2 {
		t.Fatalf("attempt records = %d, want 2", len(got))
	}
}

func TestWorkflowRunRetryExhaustedWithoutFailureEdge(t *testing.T) {
	e := setupRunTest(t)
	// max_attempts 1 and no failed-port edge: exhaustion fails the task with
	// the node's own failure (§6.3).
	agentConfig := `{
		"instruction": "单次任务",
		"assignee": {"mode": "any_agent"},
		"timeout_minutes": 5,
		"retry": {"max_attempts": 1, "backoff_seconds": 0}
	}`
	triggerID := uuid.NewString()
	agentID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(agentID, "agent.task", "干活", 1, agentConfig),
		wfNode(endID, "flow.end", "完成", 2, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", agentID, "input"),
		wfEdge(uuid.NewString(), agentID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	agent := e.nodeByTemplate(t, taskID, agentID)
	e.setNodeRunning(t, agent.ID)

	runSvc := service.NewWorkflowRunService(e.svc)
	if err := runSvc.FailNode(context.Background(), mustParse(t, agent.ID), 1, service.NodeFailure{Code: "no_edge", Message: "died"}); err != nil {
		t.Fatalf("fail node: %v", err)
	}

	agent = e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusFailed {
		t.Fatalf("agent status = %s, want failed", agent.Status)
	}
	if agent.NextRetryAt != nil {
		t.Fatalf("next_retry_at set on exhausted node")
	}
	task := e.task(t, taskID)
	if task.Status != types.TaskStatusFailed {
		t.Fatalf("task status = %s, want failed", task.Status)
	}
	var failure struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(task.Failure, &failure); err != nil || failure.Code != "no_edge" {
		t.Fatalf("task failure = %s, want the node's failure code", task.Failure)
	}
}

func TestWorkflowRunConcurrentCompleteNode(t *testing.T) {
	e := setupRunTest(t)
	nodes, edges, _, agentID, _ := manualAgentGraph(agentInputMapConfig)
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"title":"并发"}`))
	agent := e.nodeByTemplate(t, taskID, agentID)
	e.setNodeRunning(t, agent.ID)

	runSvc := service.NewWorkflowRunService(e.svc)
	nodeID := mustParse(t, agent.ID)
	result := service.NodeResult{Output: json.RawMessage(`{"summary":"done"}`)}

	var mu sync.Mutex
	var successes, failures int
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := runSvc.CompleteNode(context.Background(), nodeID, 1, result)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failures++
			} else {
				successes++
			}
		}()
	}
	wg.Wait()

	if successes != 1 || failures != 1 {
		t.Fatalf("concurrent complete: successes=%d failures=%d, want 1/1", successes, failures)
	}
	agent = e.nodeByTemplate(t, taskID, agentID)
	if agent.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("agent status = %s, want succeeded", agent.Status)
	}
	attempts := e.attemptsOf(t, taskID, agent.ID)
	if len(attempts) != 1 || attempts[0].AttemptNo != 1 {
		t.Fatalf("attempt records = %+v, want exactly one attempt_no=1", attempts)
	}
	if task := e.task(t, taskID); task.Status != types.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed (advance ran after completion)", task.Status)
	}
}

func TestWorkflowRunWaitDurationWake(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	waitID := uuid.NewString()
	agentID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(waitID, "flow.wait", "等待5分钟", 1, `{"mode":"duration","minutes":5}`),
		wfNode(agentID, "agent.task", "干活", 2, agentInputMapConfig),
		wfNode(endID, "flow.end", "完成", 3, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", waitID, "input"),
		wfEdge(uuid.NewString(), waitID, "resumed", agentID, "input"),
		wfEdge(uuid.NewString(), waitID, "timeout", endID, "failed"),
		wfEdge(uuid.NewString(), agentID, "success", endID, "completed"),
		wfEdge(uuid.NewString(), agentID, "failed", endID, "failed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	wait := e.nodeByTemplate(t, taskID, waitID)
	if wait.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("wait status = %s, want waiting", wait.Status)
	}
	if wait.WaitKey != fmt.Sprintf("duration:%d:%s", taskID, wait.ID) {
		t.Fatalf("wait_key = %s", wait.WaitKey)
	}
	if wait.WaitExpiresAt == nil || wait.WaitExpiresAt.Before(time.Now().Add(4*time.Minute)) {
		t.Fatalf("wait_expires_at = %v, want ~5 minutes out", wait.WaitExpiresAt)
	}
	// The downstream agent is not activated before the wait resolves.
	if node := e.nodeByTemplate(t, taskID, agentID); node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("agent status = %s, want waiting", node.Status)
	}

	// Backdate the wake time, scan and wake.
	if _, err := e.db.Exec("UPDATE task_nodes SET wait_expires_at = now() - interval '1 minute' WHERE id = $1", wait.ID); err != nil {
		t.Fatalf("backdate wait: %v", err)
	}
	due, err := e.svc.Store.ListWaitDueNodes(context.Background(), time.Now(), 100)
	if err != nil {
		t.Fatalf("list wait due: %v", err)
	}
	found := false
	for _, n := range due {
		if n.ID == wait.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("wait node not in wait-due scan")
	}

	if err := service.NewWorkflowRunService(e.svc).WakeDueNode(context.Background(), mustParse(t, wait.ID)); err != nil {
		t.Fatalf("wake wait node: %v", err)
	}
	wait = e.nodeByTemplate(t, taskID, waitID)
	if wait.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("wait status = %s, want succeeded", wait.Status)
	}
	var out struct {
		Outcome string `json:"outcome"`
	}
	if err := json.Unmarshal(wait.Output, &out); err != nil || out.Outcome != "resumed" {
		t.Fatalf("wait output = %s, want resumed", wait.Output)
	}
	if node := e.nodeByTemplate(t, taskID, agentID); node.Status != types.TaskNodeStatusReady {
		t.Fatalf("agent status = %s, want ready", node.Status)
	}
}

func TestWorkflowRunHumanInputWaits(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	humanID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(humanID, "human.input", "人工确认", 1, `{"prompt":"继续吗？"}`),
		wfNode(endID, "flow.end", "完成", 2, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", humanID, "input"),
		wfEdge(uuid.NewString(), humanID, "submitted", endID, "completed"),
		wfEdge(uuid.NewString(), humanID, "cancelled", endID, "failed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	human := e.nodeByTemplate(t, taskID, humanID)
	if human.Status != types.TaskNodeStatusManualIntervention {
		t.Fatalf("human status = %s, want manual_intervention", human.Status)
	}
	if human.WaitKey != fmt.Sprintf("human:%d:%s", taskID, human.ID) {
		t.Fatalf("wait_key = %s", human.WaitKey)
	}
	if task := e.task(t, taskID); task.Status != types.TaskStatusActive {
		t.Fatalf("task status = %s, want active", task.Status)
	}
}

func TestWorkflowRunStartValidations(t *testing.T) {
	e := setupRunTest(t)
	nodes, edges, _, _, _ := manualAgentGraph(agentInputMapConfig)

	// Disabled template.
	tpl, _, _, err := e.svc.Store.CreateWorkflowTemplate(context.Background(), types.CreateWorkflowTemplateParams{
		WorkspaceID: e.wsID,
		Name:        "disabled-" + uuid.New().String()[:8],
		Version:     1,
		Enabled:     false,
	}, nodes, edges)
	if err != nil {
		t.Fatalf("create disabled template: %v", err)
	}
	runSvc := service.NewWorkflowRunService(e.svc)
	templateUUID := mustParse(t, tpl.ID)
	projectUUID := mustParse(t, e.projID)
	if _, err := runSvc.Start(context.Background(), templateUUID, projectUUID, json.RawMessage(`{}`), "", e.authorID(t)); err == nil {
		t.Fatalf("start on disabled template: expected error")
	}

	// Template from another workspace.
	otherWS, err := e.svc.Store.CreateWorkspace(context.Background(), types.CreateWorkspaceParams{
		Name:        "other-" + uuid.New().String()[:8],
		IssuePrefix: "OT",
	})
	if err != nil {
		t.Fatalf("create other workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(e.db, otherWS.ID) })
	otherProj, err := e.svc.Store.CreateProject(context.Background(), types.CreateProjectParams{
		WorkspaceID: otherWS.ID,
		Name:        "other-proj-" + uuid.New().String()[:8],
		Status:      "active",
	})
	if err != nil {
		t.Fatalf("create other project: %v", err)
	}
	enabledNodes, enabledEdges, _, _, _ := manualAgentGraph(agentInputMapConfig)
	tpl2 := e.createRunTemplate(t, enabledNodes, enabledEdges)
	if _, err := runSvc.Start(context.Background(), mustParse(t, tpl2), mustParse(t, otherProj.ID), json.RawMessage(`{}`), "", e.authorID(t)); err == nil {
		t.Fatalf("start with cross-workspace project: expected error")
	}
}

func TestWorkflowRunSnapshotIsolatedFromTemplateUpdate(t *testing.T) {
	e := setupRunTest(t)
	nodes, edges, _, agentID, _ := manualAgentGraph(agentInputMapConfig)
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	before := e.nodeByTemplate(t, taskID, agentID)

	// Rename the agent node in the template; the run snapshot keeps its copy.
	updated := make([]types.CreateTemplateNodeParams, len(nodes))
	for i, n := range nodes {
		if *n.ID == agentID {
			n.Name = "改名后的节点"
		}
		updated[i] = n
	}
	if _, _, _, err := e.svc.Store.UpdateTemplateGraph(context.Background(), mustParse(t, tplID), types.UpdateWorkflowTemplateParams{
		ID:      tplID,
		Name:    "renamed",
		Enabled: true,
	}, updated, edges); err != nil {
		t.Fatalf("update template: %v", err)
	}

	after := e.nodeByTemplate(t, taskID, agentID)
	if after.Name != before.Name {
		t.Fatalf("run node name changed with template update: %s -> %s", before.Name, after.Name)
	}
	if after.ID != before.ID {
		t.Fatalf("run node id changed: %s -> %s", before.ID, after.ID)
	}
}

func mustParse(t *testing.T, id string) uuid.UUID {
	t.Helper()
	parsed, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", id, err)
	}
	return parsed
}

// Regression tests for the context.nodes root refresh: expressions referencing
// nodes.<template_node_id>.output must resolve within the same advance pass
// (trigger→first node at Start, agent completion→downstream) and the resolved
// values must reach the agentd execution input.

func TestWorkflowRunNodesRootVisibleWithinStart(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	setterID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(setterID, "data.set", "记录标题", 1, `{"name":"title_copy","value":"{{ nodes.`+triggerID+`.output.title }}"}`),
		wfNode(endID, "flow.end", "完成", 2, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", setterID, "input"),
		wfEdge(uuid.NewString(), setterID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"title":"hello"}`))

	vars := e.contextVars(t, taskID)
	if vars["title_copy"] != "hello" {
		t.Fatalf("vars.title_copy = %v, want the resolved trigger output \"hello\"", vars["title_copy"])
	}
}

func TestWorkflowRunNodesRootVisibleAfterAgentCompletion(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	agentID := uuid.NewString()
	setterID := uuid.NewString()
	endID := uuid.NewString()
	agentCfg := `{"instruction":"do","assignee":{"mode":"any_agent","agent_id":null},"input_map":{},"timeout_minutes":60,"retry":{"max_attempts":1,"backoff_seconds":30},"readonly_dirs":[],"full_control_dirs":["."]}`
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(agentID, "agent.task", "修复", 1, agentCfg),
		wfNode(setterID, "data.set", "记录摘要", 2, `{"name":"summary_copy","value":"{{ nodes.`+agentID+`.output.summary }}"}`),
		wfNode(endID, "flow.end", "完成", 3, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", agentID, "input"),
		wfEdge(uuid.NewString(), agentID, "success", setterID, "input"),
		wfEdge(uuid.NewString(), setterID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{}`))
	agent := e.nodeByTemplate(t, taskID, agentID)
	e.setNodeRunning(t, agent.ID)

	runSvc := service.NewWorkflowRunService(e.svc)
	if err := runSvc.CompleteNode(context.Background(), mustParse(t, agent.ID), 1, service.NodeResult{Output: json.RawMessage(`{"summary":"done"}`)}); err != nil {
		t.Fatalf("complete agent node: %v", err)
	}

	vars := e.contextVars(t, taskID)
	if vars["summary_copy"] != "done" {
		t.Fatalf("vars.summary_copy = %v, want the resolved agent output \"done\"", vars["summary_copy"])
	}
}

// Regression: context.nodes also exposes name aliases. With graph-wide unique
// node names, expressions address upstream output as nodes.<名字>.output; the
// alias key is the trimmed name (matching the frontend), so a padded name
// still resolves.

func TestWorkflowRunNodesRootResolvesByUniqueName(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	setterID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", " 开始 ", 0, `{}`),
		wfNode(setterID, "data.set", "记录标题", 1, `{"name":"title_copy","value":"{{ nodes.开始.output.title }}"}`),
		wfNode(endID, "flow.end", "完成", 2, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", setterID, "input"),
		wfEdge(uuid.NewString(), setterID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"title":"hello"}`))

	vars := e.contextVars(t, taskID)
	if vars["title_copy"] != "hello" {
		t.Fatalf("vars.title_copy = %v, want the trigger output resolved via the node name alias", vars["title_copy"])
	}
}

// Duplicate node names must not create name aliases (the reference stays an
// unresolved literal), while the template-node-id path keeps resolving.

func TestWorkflowRunNodesRootNameAliasSkippedOnDuplicateNames(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	byIDID := uuid.NewString()
	byNameID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(byIDID, "data.set", "按ID引用", 1, `{"name":"id_copy","value":"{{ nodes.`+triggerID+`.output.title }}"}`),
		wfNode(byNameID, "data.set", "按名字引用", 2, `{"name":"name_copy","value":"{{ nodes.开始.output.title }}"}`),
		// "开始" appears twice after trimming (" 开始 " trims to "开始"): the
		// alias must not exist.
		wfNode(endID, "flow.end", " 开始 ", 3, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", byIDID, "input"),
		wfEdge(uuid.NewString(), byIDID, "success", byNameID, "input"),
		wfEdge(uuid.NewString(), byNameID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"title":"hello"}`))

	vars := e.contextVars(t, taskID)
	if vars["id_copy"] != "hello" {
		t.Fatalf("vars.id_copy = %v, want the trigger output resolved via the template node id", vars["id_copy"])
	}
	if vars["name_copy"] != "{{ nodes.开始.output.title }}" {
		t.Fatalf("vars.name_copy = %v, want the unresolved literal (duplicate names must not alias)", vars["name_copy"])
	}
}

func TestWorkflowRunAgentInputMapConsumesUpstreamOutput(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	agent1ID := uuid.NewString()
	agent2ID := uuid.NewString()
	endID := uuid.NewString()
	agentCfg := func(inputMap string) string {
		return `{"instruction":"do","assignee":{"mode":"any_agent","agent_id":null},"input_map":` + inputMap + `,"timeout_minutes":60,"retry":{"max_attempts":1,"backoff_seconds":30},"readonly_dirs":[],"full_control_dirs":["."]}`
	}
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(agent1ID, "agent.task", "第一步", 1, agentCfg(`{"up":"{{ trigger.issue.title }}"}`)),
		wfNode(agent2ID, "agent.task", "第二步", 2, agentCfg(`{"up":"{{ nodes.`+agent1ID+`.output.summary }}"}`)),
		wfNode(endID, "flow.end", "完成", 3, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", agent1ID, "input"),
		wfEdge(uuid.NewString(), agent1ID, "success", agent2ID, "input"),
		wfEdge(uuid.NewString(), agent2ID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)

	taskID := e.start(t, tplID, json.RawMessage(`{"issue":{"title":"T1"}}`))
	runSvc := service.NewWorkflowRunService(e.svc)
	agentUUID := uuid.New()

	agent1 := e.nodeByTemplate(t, taskID, agent1ID)
	if _, err := runSvc.ClaimForAgent(context.Background(), taskID, mustParse(t, agent1.ID), agentUUID); err != nil {
		t.Fatalf("claim agent1: %v", err)
	}
	if err := runSvc.CompleteNode(context.Background(), mustParse(t, agent1.ID), 1, service.NodeResult{Output: json.RawMessage(`{"summary":"done"}`)}); err != nil {
		t.Fatalf("complete agent1: %v", err)
	}

	agent2 := e.nodeByTemplate(t, taskID, agent2ID)
	unit, err := runSvc.ClaimForAgent(context.Background(), taskID, mustParse(t, agent2.ID), agentUUID)
	if err != nil {
		t.Fatalf("claim agent2: %v", err)
	}
	input := string(unit.Input)
	if !strings.Contains(input, "done") || strings.Contains(input, "{{") {
		t.Fatalf("agent2 input = %s, want the resolved upstream output \"done\"", input)
	}
}

// Regression tests for the S5 review P1 fixes: crashed-worker recovery
// (processing event requeue, server-executor lease reclaim) and the waiter
// lifecycle (late events cannot resurrect a terminal run).

func TestWorkflowEventProcessingRequeue(t *testing.T) {
	e := setupRunTest(t)
	integrationID := uuid.NewString()
	evSvc := service.NewWorkflowEventService(e.svc)

	// The integration row must exist: processEvent fails an event whose
	// integration is missing, and this test exercises the requeue, not that.
	if _, err := e.db.Exec(`INSERT INTO integrations (id, workspace_id, project_id, provider, name, status)
		VALUES ($1, $2, $3, 'github', 'requeue-test', 'connected')`, integrationID, e.wsID, e.projID); err != nil {
		t.Fatalf("seed integration: %v", err)
	}
	delivered, err := evSvc.RecordDelivery(context.Background(), types.WorkflowEvent{
		Provider:      "github",
		IntegrationID: integrationID,
		ExternalID:    "delivery-requeue-1",
		EventType:     "issues",
		Action:        "opened",
		Payload:       json.RawMessage(`{"event":"issues","action":"opened"}`),
	})
	if err != nil || !delivered {
		t.Fatalf("record delivery: delivered=%v err=%v", delivered, err)
	}

	// Claim the event (simulating a worker crash right after the claim).
	var eventID string
	if err := e.db.QueryRow(`SELECT id FROM workflow_events WHERE external_id = 'delivery-requeue-1' AND integration_id = $1`, integrationID).Scan(&eventID); err != nil {
		t.Fatalf("load event: %v", err)
	}
	if _, err := e.svc.Store.MarkWorkflowEventProcessing(context.Background(), mustParse(t, eventID)); err != nil {
		t.Fatalf("claim event: %v", err)
	}
	if _, err := e.db.Exec(`UPDATE workflow_events SET updated_at = now() - interval '20 minutes' WHERE id = $1`, eventID); err != nil {
		t.Fatalf("backdate lease: %v", err)
	}

	if err := evSvc.ProcessPendingEvents(context.Background()); err != nil {
		t.Fatalf("process events: %v", err)
	}

	var status string
	var attempts int
	if err := e.db.QueryRow(`SELECT status, attempts FROM workflow_events WHERE id = $1`, eventID).Scan(&status, &attempts); err != nil {
		t.Fatalf("reload event: %v", err)
	}
	if status != "completed" {
		t.Fatalf("event status = %s, want completed (requeued then processed)", status)
	}
	if attempts < 2 {
		t.Fatalf("attempts = %d, want >= 2 (initial claim + requeue processing)", attempts)
	}
}

func TestWorkflowEventProcessingBudgetExhausted(t *testing.T) {
	e := setupRunTest(t)
	evSvc := service.NewWorkflowEventService(e.svc)

	integrationID := uuid.NewString()
	if _, err := evSvc.RecordDelivery(context.Background(), types.WorkflowEvent{
		Provider:      "github",
		IntegrationID: integrationID,
		ExternalID:    "delivery-requeue-2",
		EventType:     "issues",
		Action:        "opened",
		Payload:       json.RawMessage(`{"event":"issues","action":"opened"}`),
	}); err != nil {
		t.Fatalf("record delivery: %v", err)
	}
	var eventID string
	if err := e.db.QueryRow(`SELECT id FROM workflow_events WHERE external_id = 'delivery-requeue-2' AND integration_id = $1`, integrationID).Scan(&eventID); err != nil {
		t.Fatalf("load event: %v", err)
	}
	if _, err := e.db.Exec(`UPDATE workflow_events SET status = 'processing', attempts = 3, updated_at = now() - interval '20 minutes' WHERE id = $1`, eventID); err != nil {
		t.Fatalf("simulate stuck event: %v", err)
	}

	if err := evSvc.ProcessPendingEvents(context.Background()); err != nil {
		t.Fatalf("process events: %v", err)
	}
	var status, errMsg string
	if err := e.db.QueryRow(`SELECT status, error FROM workflow_events WHERE id = $1`, eventID).Scan(&status, &errMsg); err != nil {
		t.Fatalf("reload event: %v", err)
	}
	if status != "failed" || errMsg != "processing lease expired" {
		t.Fatalf("event = %s/%q, want failed with processing lease expired", status, errMsg)
	}
}

func TestWorkflowNodeLeaseReclaimFailsStuckNode(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	httpID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(httpID, "action.http", "调用外部", 1, `{"method":"GET","url":"https://internal.invalid/ping","timeout_minutes":60}`),
		wfNode(endID, "flow.end", "完成", 2, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", httpID, "input"),
		wfEdge(uuid.NewString(), httpID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)
	taskID := e.start(t, tplID, json.RawMessage(`{}`))

	// Simulate the executor claiming the node and then dying.
	httpNode := e.nodeByTemplate(t, taskID, httpID)
	e.setNodeRunning(t, httpNode.ID)
	if _, err := e.db.Exec(`INSERT INTO task_node_attempts (task_id, task_node_id, attempt_no, executor_kind, executor_id, status, input, output, error)
		VALUES ($1, $2, 1, 'server', 'test-worker', 'running', '{}', '{}', '{}')`, taskID, httpNode.ID); err != nil {
		t.Fatalf("seed running attempt: %v", err)
	}
	if _, err := e.db.Exec(`UPDATE task_nodes SET updated_at = now() - interval '2 hours' WHERE id = $1`, httpNode.ID); err != nil {
		t.Fatalf("backdate lease: %v", err)
	}
	var seeded int
	if err := e.db.QueryRow(`SELECT count(*) FROM task_node_attempts WHERE task_node_id = $1 AND status = 'running'`, httpNode.ID).Scan(&seeded); err != nil {
		t.Fatalf("count seeded attempts: %v", err)
	}

	actionSvc := service.NewWorkflowNodeActionService(e.svc)
	if err := actionSvc.ReclaimLeasedNodes(context.Background()); err != nil {
		t.Fatalf("reclaim leased nodes: %v", err)
	}

	// max_attempts=1: the reclaim failure exhausts the budget, the failed-node
	// invariant fails the task (no failure edge is connected).
	gone := e.nodeByTemplate(t, taskID, httpID)
	if gone.Status != types.TaskNodeStatusFailed {
		t.Fatalf("node status = %s, want failed after lease reclaim", gone.Status)
	}
	if task := e.task(t, taskID); task.Status != types.TaskStatusFailed {
		t.Fatalf("task status = %s, want failed via failed-node invariant", task.Status)
	}
}

func TestWorkflowWaiterGuardAndTerminalCleanup(t *testing.T) {
	e := setupRunTest(t)
	triggerID := uuid.NewString()
	splitID := uuid.NewString()
	waitID := uuid.NewString()
	actID := uuid.NewString()
	endID := uuid.NewString()
	integrationID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
		wfNode(splitID, "flow.parallel", "并行", 1, `{"branches":["wait","act"]}`),
		wfNode(waitID, "flow.wait", "等待事件", 2, `{"mode":"github_event","event":"pull_request_review","integration_id":"`+integrationID+`"}`),
		wfNode(actID, "action.github", "评论", 3, `{"operation":"add_comment","issue":"{{ trigger.issue.number }}","body":"x"}`),
		wfNode(endID, "flow.end", "完成", 4, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", splitID, "input"),
		wfEdge(uuid.NewString(), splitID, "wait", waitID, "input"),
		wfEdge(uuid.NewString(), splitID, "act", actID, "input"),
		wfEdge(uuid.NewString(), waitID, "resumed", endID, "completed"),
		wfEdge(uuid.NewString(), actID, "success", endID, "completed"),
	}
	tplID := e.createRunTemplate(t, nodes, edges)
	taskID := e.start(t, tplID, json.RawMessage(`{"issue":{"number":7}}`))

	waitNode := e.nodeByTemplate(t, taskID, waitID)
	if waitNode.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("wait node status = %s, want waiting on the github_event branch", waitNode.Status)
	}
	var waiterCount int
	if err := e.db.QueryRow(`SELECT count(*) FROM workflow_event_waiters WHERE task_id = $1 AND status = 'waiting'`, taskID).Scan(&waiterCount); err != nil {
		t.Fatalf("count waiters: %v", err)
	}
	if waiterCount != 1 {
		t.Fatalf("live waiters = %d, want 1", waiterCount)
	}

	// The act branch fails fast (no integration in the project): the S5 worker
	// claims it, fails with integration_missing, and the failed-node invariant
	// drives the task terminal — which must resolve the live waiter.
	actionSvc := service.NewWorkflowNodeActionService(e.svc)
	if err := actionSvc.ProcessGitHubActionNodes(context.Background()); err != nil {
		t.Fatalf("process github actions: %v", err)
	}
	if task := e.task(t, taskID); task.Status != types.TaskStatusFailed {
		t.Fatalf("task status = %s, want failed via failed-node invariant", task.Status)
	}
	if err := e.db.QueryRow(`SELECT count(*) FROM workflow_event_waiters WHERE task_id = $1 AND status = 'waiting'`, taskID).Scan(&waiterCount); err != nil {
		t.Fatalf("recount waiters: %v", err)
	}
	if waiterCount != 0 {
		t.Fatalf("live waiters after terminal = %d, want 0 (terminal cleanup)", waiterCount)
	}

	// Guard: a late matching event must not resurrect the failed run.
	runSvc := service.NewWorkflowRunService(e.svc)
	woken, err := runSvc.WakeEventWaiter(context.Background(), mustParse(t, waitNode.ID), json.RawMessage(`{"event":"pull_request_review","action":"submitted"}`))
	if err != nil {
		t.Fatalf("wake waiter: %v", err)
	}
	if woken {
		t.Fatalf("waiter woke a failed task; want skipped")
	}
}
