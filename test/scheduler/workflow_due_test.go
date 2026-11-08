// workflow_due_test.go covers the scheduler scans of the workflow V2 run
// engine: retry-due nodes return to ready via ProcessRetryDueNodes and
// expired duration waits complete via ProcessWaitDueNodes; nodes that are not
// yet due stay untouched.
package scheduler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/scheduler"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// dueEnv bundles the service stack one due-scan test needs.
type dueEnv struct {
	svc  *service.Service
	db   *sql.DB
	wsID string
}

func setupDueTest(t *testing.T) *dueEnv {
	t.Helper()
	pgDB := connectTestDB(t)
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	ws, err := svc.Store.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "due-test-" + uuid.New().String()[:8],
		IssuePrefix: "DT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })

	if _, err := svc.Store.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "due-proj-" + uuid.New().String()[:8],
		Status:      "active",
	}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	return &dueEnv{svc: svc, db: pgDB, wsID: ws.ID}
}

// createAgentFlowTemplate persists trigger.manual → agent.task → flow.end.
func (e *dueEnv) createAgentFlowTemplate(t *testing.T, retryConfig string) (string, string, string) {
	t.Helper()
	triggerID := uuid.NewString()
	agentID := uuid.NewString()
	endID := uuid.NewString()
	node := func(id, typeKey, name string, order int32, config string) types.CreateTemplateNodeParams {
		return types.CreateTemplateNodeParams{ID: &id, Name: name, SortOrder: order, TypeKey: typeKey, Config: json.RawMessage(config), UIConfig: json.RawMessage(`{}`)}
	}
	edge := func(source, srcPort, target, tgtPort string) types.CreateTemplateEdgeParams {
		return types.CreateTemplateEdgeParams{SourceNodeID: source, SourcePort: srcPort, TargetNodeID: target, TargetPort: tgtPort, Config: json.RawMessage(`{}`)}
	}
	tpl, _, _, err := e.svc.Store.CreateWorkflowTemplate(context.Background(), types.CreateWorkflowTemplateParams{
		WorkspaceID: e.wsID,
		Name:        "due-flow-" + uuid.New().String()[:8],
		Version:     1,
		Enabled:     true,
	}, []types.CreateTemplateNodeParams{
		node(triggerID, "trigger.manual", "开始", 0, `{}`),
		node(agentID, "agent.task", "干活", 1, retryConfig),
		node(endID, "flow.end", "完成", 2, `{}`),
	}, []types.CreateTemplateEdgeParams{
		edge(triggerID, "success", agentID, "input"),
		edge(agentID, "success", endID, "completed"),
		edge(agentID, "failed", endID, "failed"),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	return tpl.ID, agentID, endID
}

func (e *dueEnv) startRun(t *testing.T, templateID string) int32 {
	t.Helper()
	projID, err := uuid.Parse(e.projects(t)[0])
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}
	runSvc := service.NewWorkflowRunService(e.svc)
	taskID, err := runSvc.Start(context.Background(), uuid.MustParse(templateID), projID, json.RawMessage(`{}`), "", uuid.New())
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	return taskID
}

func (e *dueEnv) projects(t *testing.T) []string {
	t.Helper()
	projs, err := e.svc.Store.ListProjects(context.Background(), uuid.MustParse(e.wsID))
	if err != nil {
		t.Fatalf("list projects: %v", err)
	}
	ids := make([]string, 0, len(projs))
	for _, p := range projs {
		ids = append(ids, p.ID)
	}
	return ids
}

func (e *dueEnv) nodeByTemplate(t *testing.T, taskID int32, templateNodeID string) types.TaskNode {
	t.Helper()
	nodes, err := e.svc.Store.ListTaskNodes(context.Background(), taskID)
	if err != nil {
		t.Fatalf("list nodes: %v", err)
	}
	for _, n := range nodes {
		if n.TemplateNodeID == templateNodeID {
			return n
		}
	}
	t.Fatalf("node %s not found in task %d", templateNodeID, taskID)
	return types.TaskNode{}
}

func (e *dueEnv) setRunning(t *testing.T, nodeID string) {
	t.Helper()
	if _, err := e.db.Exec("UPDATE task_nodes SET status = 'running', updated_at = now() WHERE id = $1", nodeID); err != nil {
		t.Fatalf("set running: %v", err)
	}
}

func (e *dueEnv) newScheduler() *scheduler.Scheduler {
	s := scheduler.NewScheduler(e.svc.Store, nil, nil)
	s.WorkflowRuns = service.NewWorkflowRunService(e.svc)
	return s
}

func TestSchedulerRetryDueWakesNode(t *testing.T) {
	e := setupDueTest(t)
	retryConfig := `{
		"instruction": "会失败的任务",
		"assignee": {"mode": "any_agent"},
		"timeout_minutes": 5,
		"retry": {"max_attempts": 2, "backoff_seconds": 0}
	}`
	tplID, agentID, _ := e.createAgentFlowTemplate(t, retryConfig)
	taskID := e.startRun(t, tplID)
	agent := e.nodeByTemplate(t, taskID, agentID)

	runSvc := service.NewWorkflowRunService(e.svc)
	nodeID := uuid.MustParse(agent.ID)
	e.setRunning(t, agent.ID)
	if err := runSvc.FailNode(context.Background(), nodeID, 1, service.NodeFailure{Code: "boom"}); err != nil {
		t.Fatalf("fail node: %v", err)
	}
	if node := e.nodeByTemplate(t, taskID, agentID); node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("agent status = %s, want waiting before scan", node.Status)
	}

	if err := e.newScheduler().ProcessRetryDueNodes(context.Background()); err != nil {
		t.Fatalf("process retry due: %v", err)
	}
	if node := e.nodeByTemplate(t, taskID, agentID); node.Status != types.TaskNodeStatusReady {
		t.Fatalf("agent status = %s, want ready after retry-due scan", node.Status)
	}
}

func TestSchedulerWaitDueCompletesWait(t *testing.T) {
	e := setupDueTest(t)
	triggerID := uuid.NewString()
	waitID := uuid.NewString()
	agentID := uuid.NewString()
	endID := uuid.NewString()
	node := func(id, typeKey, name string, order int32, config string) types.CreateTemplateNodeParams {
		return types.CreateTemplateNodeParams{ID: &id, Name: name, SortOrder: order, TypeKey: typeKey, Config: json.RawMessage(config), UIConfig: json.RawMessage(`{}`)}
	}
	edge := func(source, srcPort, target, tgtPort string) types.CreateTemplateEdgeParams {
		return types.CreateTemplateEdgeParams{SourceNodeID: source, SourcePort: srcPort, TargetNodeID: target, TargetPort: tgtPort, Config: json.RawMessage(`{}`)}
	}
	tpl, _, _, err := e.svc.Store.CreateWorkflowTemplate(context.Background(), types.CreateWorkflowTemplateParams{
		WorkspaceID: e.wsID,
		Name:        "wait-flow-" + uuid.New().String()[:8],
		Version:     1,
		Enabled:     true,
	}, []types.CreateTemplateNodeParams{
		node(triggerID, "trigger.manual", "开始", 0, `{}`),
		node(waitID, "flow.wait", "等待", 1, `{"mode":"duration","minutes":5}`),
		node(agentID, "agent.task", "干活", 2, fmt.Sprintf(`{
			"instruction": "等后的任务",
			"assignee": {"mode": "any_agent"},
			"timeout_minutes": 5,
			"retry": {"max_attempts": 1, "backoff_seconds": 0}
		}`)),
		node(endID, "flow.end", "完成", 3, `{}`),
	}, []types.CreateTemplateEdgeParams{
		edge(triggerID, "success", waitID, "input"),
		edge(waitID, "resumed", agentID, "input"),
		edge(waitID, "timeout", endID, "failed"),
		edge(agentID, "success", endID, "completed"),
		edge(agentID, "failed", endID, "failed"),
	})
	if err != nil {
		t.Fatalf("create template: %v", err)
	}
	taskID := e.startRun(t, tpl.ID)

	wait := e.nodeByTemplate(t, taskID, waitID)
	if wait.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("wait status = %s, want waiting", wait.Status)
	}

	// Not due yet: the scan must not touch the waiting node.
	if err := e.newScheduler().ProcessWaitDueNodes(context.Background()); err != nil {
		t.Fatalf("process wait due (early): %v", err)
	}
	if node := e.nodeByTemplate(t, taskID, waitID); node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("wait status = %s, want still waiting before expiry", node.Status)
	}

	// Expire the wait and scan again.
	if _, err := e.db.Exec("UPDATE task_nodes SET wait_expires_at = now() - interval '1 minute' WHERE id = $1", wait.ID); err != nil {
		t.Fatalf("expire wait: %v", err)
	}
	if err := e.newScheduler().ProcessWaitDueNodes(context.Background()); err != nil {
		t.Fatalf("process wait due: %v", err)
	}
	wait = e.nodeByTemplate(t, taskID, waitID)
	if wait.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("wait status = %s, want succeeded after scan", wait.Status)
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
