// node_exec_test.go covers the agentd node execution protocol (S4, plan
// §12.6): discovery over board + node list, claim with its execution-unit
// response, complete/fail receipts against the execution id, manual and
// interrupt-ack transitions, and the in-progress recovery view.
package handler_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/service"
)

// nodeExecTemplate builds a V2 template exercising the full S4 chain:
// trigger → agent.task → action.command → data.set → condition → end. The
// data.set/condition pair proves that a completion receipt really advances
// the graph. It returns the submitted node ids by role.
func nodeExecTemplate(name string) (map[string]interface{}, map[string]string) {
	ids := map[string]string{
		"trigger":   uuid.NewString(),
		"agent":     uuid.NewString(),
		"command":   uuid.NewString(),
		"dataset":   uuid.NewString(),
		"condition": uuid.NewString(),
		"end":       uuid.NewString(),
	}
	edge := func(source, srcPort, target, tgtPort string) map[string]interface{} {
		return map[string]interface{}{
			"source_node_id": ids[source],
			"source_port":    srcPort,
			"target_node_id": ids[target],
			"target_port":    tgtPort,
		}
	}
	body := map[string]interface{}{
		"name":        name,
		"description": "node exec protocol test",
		"enabled":     true,
		"nodes": []map[string]interface{}{
			{"id": ids["trigger"], "type_key": "trigger.manual", "name": "开始", "sort_order": 0, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{"x": 0, "y": 0}},
			{"id": ids["agent"], "type_key": "agent.task", "name": "干活", "sort_order": 1, "config": map[string]interface{}{
				"instruction":       "分析并修复",
				"assignee":          map[string]interface{}{"mode": "any_agent"},
				"input_map":         map[string]interface{}{"title": "{{ trigger.title }}"},
				"timeout_minutes":   45,
				"retry":             map[string]interface{}{"max_attempts": 2, "backoff_seconds": 0},
				"readonly_dirs":     []string{"docs"},
				"full_control_dirs": []string{"src"},
			}, "ui_config": map[string]interface{}{"x": 100, "y": 0}},
			{"id": ids["command"], "type_key": "action.command", "name": "跑命令", "sort_order": 2, "config": map[string]interface{}{
				"command":         map[string]interface{}{"program": "go", "args": []string{"version"}, "workdir": ".", "env": map[string]string{"FOO": "bar"}},
				"timeout_minutes": 10,
				"retry":           map[string]interface{}{"max_attempts": 2, "backoff_seconds": 0},
			}, "ui_config": map[string]interface{}{"x": 200, "y": 0}},
			{"id": ids["dataset"], "type_key": "data.set", "name": "记结果", "sort_order": 3, "config": map[string]interface{}{
				"name":  "fix_result",
				"value": fmt.Sprintf("{{ nodes.%s.output.done }}", ids["agent"]),
			}, "ui_config": map[string]interface{}{"x": 300, "y": 0}},
			{"id": ids["condition"], "type_key": "flow.condition", "name": "判断", "sort_order": 4, "config": map[string]interface{}{
				"condition": map[string]interface{}{
					"all": []map[string]interface{}{
						{"path": "vars.fix_result", "op": "eq", "value": "yes"},
					},
				},
			}, "ui_config": map[string]interface{}{"x": 400, "y": 0}},
			{"id": ids["end"], "type_key": "flow.end", "name": "完成", "sort_order": 5, "config": map[string]interface{}{}, "ui_config": map[string]interface{}{"x": 500, "y": 0}},
		},
		"edges": []map[string]interface{}{
			edge("trigger", "success", "agent", "input"),
			edge("agent", "success", "command", "input"),
			edge("agent", "failed", "end", "failed"),
			edge("command", "success", "dataset", "input"),
			edge("command", "failed", "end", "failed"),
			edge("dataset", "success", "condition", "input"),
			edge("dataset", "failed", "end", "failed"),
			edge("condition", "true", "end", "completed"),
			edge("condition", "false", "end", "failed"),
		},
	}
	return body, ids
}

// executionTicket mirrors the wire contract of the claim response (the
// agentd TaskNode decoder shape) so the test asserts field names, not the
// server struct.
type executionTicket struct {
	ID              string          `json:"id"`
	TaskID          int32           `json:"task_id"`
	Name            string          `json:"name"`
	Status          string          `json:"status"`
	ExecutionID     string          `json:"execution_id"`
	ExecutionKind   string          `json:"execution_kind"`
	Instruction     string          `json:"instruction"`
	Input           json.RawMessage `json:"input"`
	Command         *struct {
		Program string            `json:"program"`
		Args    []string          `json:"args"`
		Workdir string            `json:"workdir"`
		Env     map[string]string `json:"env"`
	} `json:"command"`
	TimeoutMinutes  int32           `json:"timeout_minutes"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
}

// graphNodeView is the run-view node subset the assertions read.
type graphNodeView struct {
	ID           string          `json:"id"`
	TypeKey      string          `json:"type_key"`
	Status       string          `json:"status"`
	Summary      string          `json:"summary"`
	Output       json.RawMessage `json:"output"`
	Error        json.RawMessage `json:"error"`
	AttemptCount int             `json:"attempt_count"`
	NextRetryAt  *time.Time      `json:"next_retry_at"`
	Attempts     []struct {
		ID           string          `json:"id"`
		AttemptNo    int32           `json:"attempt_no"`
		ExecutorKind string          `json:"executor_kind"`
		ExecutorID   string          `json:"executor_id"`
		Status       string          `json:"status"`
		Input        json.RawMessage `json:"input"`
		Output       json.RawMessage `json:"output"`
		Error        json.RawMessage `json:"error"`
	} `json:"attempts"`
}

type graphView struct {
	Task struct {
		ID     int32  `json:"id"`
		Status string `json:"status"`
	} `json:"task"`
	Nodes []graphNodeView `json:"nodes"`
}

// fetchGraph loads the run view of one task.
func fetchGraph(t *testing.T, client *http.Client, baseURL, token string, taskID int32) graphView {
	t.Helper()
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/graph", baseURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("get graph: expected 200, got %d, body: %s", status, respBody)
	}
	var graph graphView
	if err := json.Unmarshal(respBody, &graph); err != nil {
		t.Fatalf("decode graph: %v", err)
	}
	return graph
}

func (g graphView) nodeByTypeKey(typeKey string) graphNodeView {
	for _, n := range g.Nodes {
		if n.TypeKey == typeKey {
			return n
		}
	}
	return graphNodeView{}
}

// startNodeExecRun creates the S4 template, starts one run and returns the
// task id plus the node id map.
func startNodeExecRun(t *testing.T, client *http.Client, baseURL, token, wsID, projID, name string) (int32, map[string]string) {
	t.Helper()
	body, ids := nodeExecTemplate(name)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, workflowsURL(baseURL, wsID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body: %s", status, respBody)
	}
	var tpl struct {
		Template map[string]interface{} `json:"template"`
	}
	if err := json.Unmarshal(respBody, &tpl); err != nil {
		t.Fatalf("decode template: %v", err)
	}
	tplID, _ := tpl.Template["id"].(string)

	runURL := fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", baseURL, projID, tplID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, runURL, token, map[string]interface{}{
		"input": map[string]interface{}{"title": "来自测试"},
	})
	if status != http.StatusCreated {
		t.Fatalf("start run: expected 201, got %d, body: %s", status, respBody)
	}
	var run struct {
		TaskID int32 `json:"task_id"`
	}
	if err := json.Unmarshal(respBody, &run); err != nil {
		t.Fatalf("decode run: %v", err)
	}
	// Run nodes are fresh instances; resolve their ids by type key so the
	// protocol calls address task nodes, not template nodes.
	graph := fetchGraph(t, client, baseURL, token, run.TaskID)
	runIDs := make(map[string]string, len(ids))
	for role, typeKey := range map[string]string{
		"trigger": "trigger.manual", "agent": "agent.task", "command": "action.command",
		"dataset": "data.set", "condition": "flow.condition", "end": "flow.end",
	} {
		node := graph.nodeByTypeKey(typeKey)
		if node.ID == "" {
			t.Fatalf("run graph has no %s node", typeKey)
		}
		runIDs[role] = node.ID
	}
	return run.TaskID, runIDs
}

// claimURL builds a node action URL under the agentd protocol.
func nodeActionURL(baseURL string, taskID int32, nodeID, action string) string {
	return fmt.Sprintf("%s/api/tasks/%d/nodes/%s/%s", baseURL, taskID, nodeID, action)
}

// setupNodeExecEnv boots the test server and keeps the db handle for direct
// scheduler-entry calls (WakeDueNode) in retry tests.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// TestNodeClaimCompleteAdvances covers the happy path of the S4 protocol:
// discovery (nodes list + board pending), claim of the agent node with the
// exact execution-unit response, duplicate-claim 409, non-member agent 403,
// completion receipt advancing data.set → condition → end, then the command
// node claim with its command spec.
func TestNodeClaimCompleteAdvances(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)
	taskID, ids := startNodeExecRun(t, client, srv.URL, token, wsID, projID, "s4-claim-complete")

	agentID, tdKey := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbQueries(t, openTestDB(t)), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// Discovery: the nodes list is a bare array in execution-unit shape and
	// the unclaimed agent node carries an empty execution id.
	_, status, respBody := doRequestWithAgentKey(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID), tdKey, agentID, nil)
	if status != http.StatusOK {
		t.Fatalf("list nodes: expected 200, got %d, body: %s", status, respBody)
	}
	var units []executionTicket
	if err := json.Unmarshal(respBody, &units); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	var readyTicket *executionTicket
	for i := range units {
		if units[i].Status == "ready" && units[i].ExecutionKind == "agent" {
			readyTicket = &units[i]
		}
	}
	if readyTicket == nil || readyTicket.Status != "ready" {
		t.Fatalf("agent node missing or not ready in nodes list: %s", respBody)
	}
	if readyTicket.ExecutionID != "" {
		t.Fatalf("unclaimed node execution_id = %q, want empty", readyTicket.ExecutionID)
	}
	if readyTicket.ExecutionKind != "agent" {
		t.Fatalf("unclaimed agent node execution_kind = %q, want agent", readyTicket.ExecutionKind)
	}

	// Discovery: the board pending column contains the task.
	board := fetchBoard(t, client, srv.URL, tdKey, agentID, projID)
	if !boardHasTask(board, "pending", taskID) {
		t.Fatalf("board pending column misses task %d", taskID)
	}

	// Non-member agent is rejected before claiming.
	otherID, otherKey := createAgent(t, client, srv.URL, wsID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, otherID, token)
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "claim"), otherKey, otherID, map[string]interface{}{"agent_id": otherID})
	if status != http.StatusForbidden {
		t.Fatalf("non-member claim: expected 403, got %d, body: %s", status, respBody)
	}

	// Claim the agent node.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
	if status != http.StatusOK {
		t.Fatalf("claim: expected 200, got %d, body: %s", status, respBody)
	}
	var ticket executionTicket
	if err := json.Unmarshal(respBody, &ticket); err != nil {
		t.Fatalf("decode claim response: %v", err)
	}
	if ticket.ID != ids["agent"] || ticket.TaskID != taskID || ticket.Name != "干活" {
		t.Fatalf("claim identity fields = %+v", ticket)
	}
	if ticket.Status != "running" {
		t.Fatalf("claim status = %q, want running", ticket.Status)
	}
	if ticket.ExecutionID == "" {
		t.Fatalf("claim execution_id is empty")
	}
	if ticket.ExecutionKind != "agent" {
		t.Fatalf("claim execution_kind = %q, want agent", ticket.ExecutionKind)
	}
	if ticket.Instruction != "分析并修复" {
		t.Fatalf("instruction = %q", ticket.Instruction)
	}
	var input map[string]interface{}
	if err := json.Unmarshal(ticket.Input, &input); err != nil {
		t.Fatalf("decode input: %v", err)
	}
	if input["title"] != "来自测试" {
		t.Fatalf("input = %v, want resolved input_map title", input)
	}
	if ticket.TimeoutMinutes != 45 {
		t.Fatalf("timeout_minutes = %d, want 45", ticket.TimeoutMinutes)
	}
	if string(ticket.ReadonlyDirs) != `["docs"]` || string(ticket.FullControlDirs) != `["src"]` {
		t.Fatalf("dirs = %s / %s", ticket.ReadonlyDirs, ticket.FullControlDirs)
	}
	if ticket.Command != nil {
		t.Fatalf("agent ticket carries command %+v, want null", ticket.Command)
	}

	// Duplicate claim loses: the node is no longer ready.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
	if status != http.StatusConflict {
		t.Fatalf("duplicate claim: expected 409, got %d, body: %s", status, respBody)
	}

	// Claiming a non-agentd node (already executed end node) is a conflict.
	_, status, _ = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["end"], "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
	if status != http.StatusConflict {
		t.Fatalf("non-agentd claim: expected 409, got %d", status)
	}

	// The node is running with a running agentd attempt owned by the agent.
	graph := fetchGraph(t, client, srv.URL, token, taskID)
	agentNode := graph.nodeByTypeKey("agent.task")
	if agentNode.Status != "running" {
		t.Fatalf("agent node status = %q, want running", agentNode.Status)
	}
	if len(agentNode.Attempts) != 1 || agentNode.Attempts[0].Status != "running" ||
		agentNode.Attempts[0].ExecutorKind != "agentd" || agentNode.Attempts[0].ExecutorID != agentID ||
		agentNode.Attempts[0].ID != ticket.ExecutionID {
		t.Fatalf("agent attempts = %+v, want one running agentd attempt with the claim execution_id", agentNode.Attempts)
	}

	// A forged execution id cannot complete the node.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "complete"), tdKey, agentID, map[string]interface{}{
		"execution_id": uuid.NewString(),
		"summary":      "伪造",
		"output":       map[string]interface{}{"done": "no"},
	})
	if status != http.StatusConflict {
		t.Fatalf("forged execution: expected 409, got %d, body: %s", status, respBody)
	}

	// Completion receipt: summary/output land, and the graph advances.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "complete"), tdKey, agentID, map[string]interface{}{
		"execution_id": ticket.ExecutionID,
		"summary":      "修好了",
		"output":       map[string]interface{}{"done": "yes"},
	})
	if status != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d, body: %s", status, respBody)
	}

	graph = fetchGraph(t, client, srv.URL, token, taskID)
	agentNode = graph.nodeByTypeKey("agent.task")
	if agentNode.Status != "succeeded" || agentNode.Summary != "修好了" {
		t.Fatalf("agent node after complete = %+v", agentNode)
	}
	var output map[string]interface{}
	if err := json.Unmarshal(agentNode.Output, &output); err != nil || output["done"] != "yes" {
		t.Fatalf("agent node output = %s", agentNode.Output)
	}
	if len(agentNode.Attempts) != 1 || agentNode.Attempts[0].Status != "succeeded" || string(agentNode.Attempts[0].Output) == "" {
		t.Fatalf("agent attempt after complete = %+v", agentNode.Attempts)
	}
	// The advance moved the graph: the command node is ready, downstream
	// server nodes stay pending until the command finishes.
	if cmd := graph.nodeByTypeKey("action.command"); cmd.Status != "ready" {
		t.Fatalf("command node status = %q, want ready after agent completion", cmd.Status)
	}

	// Claim the command node: command execution unit with the spec.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["command"], "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
	if status != http.StatusOK {
		t.Fatalf("claim command: expected 200, got %d, body: %s", status, respBody)
	}
	var commandTicket executionTicket
	if err := json.Unmarshal(respBody, &commandTicket); err != nil {
		t.Fatalf("decode command claim: %v", err)
	}
	if commandTicket.ExecutionKind != "command" || commandTicket.Instruction != "" || commandTicket.Command == nil {
		t.Fatalf("command ticket = %+v", commandTicket)
	}
	if commandTicket.Command.Program != "go" || len(commandTicket.Command.Args) != 1 || commandTicket.Command.Args[0] != "version" ||
		commandTicket.Command.Workdir != "." || commandTicket.Command.Env["FOO"] != "bar" {
		t.Fatalf("command spec = %+v", commandTicket.Command)
	}

	// Completing the command runs data.set → condition → end and finishes the task.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["command"], "complete"), tdKey, agentID, map[string]interface{}{
		"execution_id": commandTicket.ExecutionID,
		"summary":      "命令通过",
		"output":       map[string]interface{}{"exit_code": 0},
	})
	if status != http.StatusOK {
		t.Fatalf("complete command: expected 200, got %d, body: %s", status, respBody)
	}
	graph = fetchGraph(t, client, srv.URL, token, taskID)
	if graph.Task.Status != "completed" {
		t.Fatalf("task status = %q, want completed", graph.Task.Status)
	}
	for _, typeKey := range []string{"action.command", "data.set", "flow.condition", "flow.end"} {
		if n := graph.nodeByTypeKey(typeKey); n.Status != "succeeded" {
			t.Fatalf("%s status = %q, want succeeded", typeKey, n.Status)
		}
	}

	// A replayed receipt for the same execution id is superseded.
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["command"], "complete"), tdKey, agentID, map[string]interface{}{
		"execution_id": commandTicket.ExecutionID,
		"summary":      "重放",
		"output":       map[string]interface{}{},
	})
	if status != http.StatusConflict {
		t.Fatalf("replayed complete: expected 409, got %d, body: %s", status, respBody)
	}
}

// TestNodeFailRetryExhausts covers the failure receipt: inside the retry
// budget the node returns to waiting and can be claimed again with attempt 2;
// exhausting the budget fails the node and routes the task through the failed
// port to the end node.
func TestNodeFailRetryExhausts(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)
	taskID, ids := startNodeExecRun(t, client, srv.URL, token, wsID, projID, "s4-fail-retry")

	agentID, tdKey := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbQueries(t, openTestDB(t)), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	claim := func() string {
		_, status, respBody := doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
		if status != http.StatusOK {
			t.Fatalf("claim: expected 200, got %d, body: %s", status, respBody)
		}
		var ticket executionTicket
		if err := json.Unmarshal(respBody, &ticket); err != nil {
			t.Fatalf("decode claim: %v", err)
		}
		return ticket.ExecutionID
	}
	fail := func(executionID, code string) {
		_, status, respBody := doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "fail"), tdKey, agentID, map[string]interface{}{
			"execution_id":  executionID,
			"error_code":    code,
			"error_message": "boom",
		})
		if status != http.StatusOK {
			t.Fatalf("fail: expected 200, got %d, body: %s", status, respBody)
		}
	}

	// First failure stays inside the retry budget: the node waits for its
	// scheduled retry and the attempt records the verbatim error code.
	first := claim()
	fail(first, "agent_execution_failed")
	graph := fetchGraph(t, client, srv.URL, token, taskID)
	agentNode := graph.nodeByTypeKey("agent.task")
	if agentNode.Status != "waiting" || agentNode.NextRetryAt == nil {
		t.Fatalf("agent node after first fail = %+v, want waiting with next_retry_at", agentNode)
	}
	if len(agentNode.Attempts) != 1 || agentNode.Attempts[0].Status != "failed" {
		t.Fatalf("agent attempts after first fail = %+v", agentNode.Attempts)
	}
	var attemptErr map[string]string
	if err := json.Unmarshal(agentNode.Attempts[0].Error, &attemptErr); err != nil || attemptErr["code"] != "agent_execution_failed" {
		t.Fatalf("attempt error = %s", agentNode.Attempts[0].Error)
	}
	if graph.Task.Status != "active" {
		t.Fatalf("task status = %q, want active while retrying", graph.Task.Status)
	}

	// The scheduler entry moves the due node back to ready; the second claim
	// runs under attempt 2.
	wakeDueNode(t, agentNode.ID)
	graph = fetchGraph(t, client, srv.URL, token, taskID)
	if n := graph.nodeByTypeKey("agent.task"); n.Status != "ready" {
		t.Fatalf("agent node after wake = %q, want ready", n.Status)
	}
	second := claim()
	graph = fetchGraph(t, client, srv.URL, token, taskID)
	if n := graph.nodeByTypeKey("agent.task"); n.AttemptCount != 2 || len(n.Attempts) != 2 {
		t.Fatalf("agent node after second claim = attempt_count %d attempts %d", n.AttemptCount, len(n.Attempts))
	}

	// Exhausting the budget fails the node and the task through the failed port.
	fail(second, "command_exit_nonzero")
	graph = fetchGraph(t, client, srv.URL, token, taskID)
	agentNode = graph.nodeByTypeKey("agent.task")
	if agentNode.Status != "failed" {
		t.Fatalf("agent node after exhaust = %q, want failed", agentNode.Status)
	}
	var nodeErr map[string]string
	if err := json.Unmarshal(agentNode.Error, &nodeErr); err != nil || nodeErr["code"] != "command_exit_nonzero" {
		t.Fatalf("node error = %s, want verbatim command_exit_nonzero", agentNode.Error)
	}
	if last := agentNode.Attempts[len(agentNode.Attempts)-1]; last.AttemptNo != 2 || last.Status != "failed" {
		t.Fatalf("last attempt = %+v, want failed attempt 2", last)
	}
	if graph.Task.Status != "failed" {
		t.Fatalf("task status = %q, want failed", graph.Task.Status)
	}
	if n := graph.nodeByTypeKey("flow.end"); n.Status != "succeeded" {
		t.Fatalf("end node status = %q, want succeeded via failed port", n.Status)
	}

	// Receipts for the closed execution are stale.
	_, status, respBody := doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "fail"), tdKey, agentID, map[string]interface{}{
		"execution_id": second,
		"error_code":   "command_exit_nonzero",
	})
	if status != http.StatusConflict {
		t.Fatalf("stale fail: expected 409, got %d, body: %s", status, respBody)
	}
}

// TestNodeManualAndInterruptAck covers both manual-landing endpoints: the
// manual receipt requires the in-flight execution id (stale ones get 409),
// and the interrupt acknowledgement records an interrupt_ack transition.
func TestNodeManualAndInterruptAck(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)

	agentID, tdKey := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbQueries(t, openTestDB(t)), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	claimAgent := func(taskID int32, agentNodeID string) string {
		_, status, respBody := doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, agentNodeID, "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
		if status != http.StatusOK {
			t.Fatalf("claim: expected 200, got %d, body: %s", status, respBody)
		}
		var ticket executionTicket
		if err := json.Unmarshal(respBody, &ticket); err != nil {
			t.Fatalf("decode claim: %v", err)
		}
		return ticket.ExecutionID
	}

	// Manual receipt moves the running node to manual_intervention.
	taskID, ids := startNodeExecRun(t, client, srv.URL, token, wsID, projID, "s4-manual")
	executionID := claimAgent(taskID, ids["agent"])

	_, status, respBody := doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "manual"), tdKey, agentID, map[string]interface{}{
		"operator_id":   agentID,
		"operator_type": "agent",
		"execution_id":  uuid.NewString(),
		"comment":       "需要人工",
	})
	if status != http.StatusConflict {
		t.Fatalf("stale manual: expected 409, got %d, body: %s", status, respBody)
	}

	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "manual"), tdKey, agentID, map[string]interface{}{
		"operator_id":   agentID,
		"operator_type": "agent",
		"execution_id":  executionID,
		"comment":       "需要人工",
	})
	if status != http.StatusOK {
		t.Fatalf("manual: expected 200, got %d, body: %s", status, respBody)
	}
	graph := fetchGraph(t, client, srv.URL, token, taskID)
	if n := graph.nodeByTypeKey("agent.task"); n.Status != "manual_intervention" {
		t.Fatalf("agent node after manual = %q, want manual_intervention", n.Status)
	}
	assertTransition(t, ids["agent"], "manual", "running", "manual_intervention", agentID)

	// Interrupt ack lands the node in manual_intervention with its own action.
	taskID2, ids2 := startNodeExecRun(t, client, srv.URL, token, wsID, projID, "s4-interrupt-ack")
	claimAgent(taskID2, ids2["agent"])
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID2, ids2["agent"], "interrupt-ack"), tdKey, agentID, map[string]interface{}{
		"comment": "interrupt acknowledged by agent",
	})
	if status != http.StatusOK {
		t.Fatalf("interrupt-ack: expected 200, got %d, body: %s", status, respBody)
	}
	graph = fetchGraph(t, client, srv.URL, token, taskID2)
	if n := graph.nodeByTypeKey("agent.task"); n.Status != "manual_intervention" {
		t.Fatalf("agent node after interrupt-ack = %q, want manual_intervention", n.Status)
	}
	assertTransition(t, ids2["agent"], "interrupt_ack", "running", "manual_intervention", agentID)
}

// TestNodeInProgressRecovery covers the recovery view: a claimed node is
// listed with its original execution id and project, scoped to the owning
// agent only.
func TestNodeInProgressRecovery(t *testing.T) {
	srv, client, token, wsID := setupWorkflowGraphServer(t)
	projID := createProject(t, client, srv.URL, wsID, token)
	taskID, ids := startNodeExecRun(t, client, srv.URL, token, wsID, projID, "s4-in-progress")

	agentID, tdKey := createAgent(t, client, srv.URL, wsID, token)
	addAgentToProject(t, dbQueries(t, openTestDB(t)), projID, agentID)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	_, status, respBody := doRequestWithAgentKey(t, client, http.MethodPost, nodeActionURL(srv.URL, taskID, ids["agent"], "claim"), tdKey, agentID, map[string]interface{}{"agent_id": agentID})
	if status != http.StatusOK {
		t.Fatalf("claim: expected 200, got %d, body: %s", status, respBody)
	}
	var ticket executionTicket
	if err := json.Unmarshal(respBody, &ticket); err != nil {
		t.Fatalf("decode claim: %v", err)
	}

	inProgressURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/in-progress-nodes", srv.URL, wsID, agentID)
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodGet, inProgressURL, tdKey, agentID, nil)
	if status != http.StatusOK {
		t.Fatalf("in-progress: expected 200, got %d, body: %s", status, respBody)
	}
	var list []struct {
		executionTicket
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(respBody, &list); err != nil {
		t.Fatalf("decode in-progress: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("in-progress list = %d entries, want 1: %s", len(list), respBody)
	}
	entry := list[0]
	if entry.ID != ids["agent"] || entry.ExecutionID != ticket.ExecutionID || entry.Status != "running" || entry.ProjectID != projID {
		t.Fatalf("in-progress entry = %+v", entry)
	}
	if entry.ExecutionKind != "agent" {
		t.Fatalf("in-progress execution_kind = %q", entry.ExecutionKind)
	}

	// Another agent sees an empty list and cannot read the first agent's list.
	otherID, otherKey := createAgent(t, client, srv.URL, wsID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, otherID, token)
	_, status, respBody = doRequestWithAgentKey(t, client, http.MethodGet, fmt.Sprintf("%s/api/workspaces/%s/agents/%s/in-progress-nodes", srv.URL, wsID, otherID), otherKey, otherID, nil)
	if status != http.StatusOK {
		t.Fatalf("other in-progress: expected 200, got %d", status)
	}
	var otherList []map[string]interface{}
	if err := json.Unmarshal(respBody, &otherList); err != nil || len(otherList) != 0 {
		t.Fatalf("other agent in-progress = %s, want empty array", respBody)
	}
	_, status, _ = doRequestWithAgentKey(t, client, http.MethodGet, inProgressURL, otherKey, otherID, nil)
	if status != http.StatusForbidden {
		t.Fatalf("cross-agent in-progress: expected 403, got %d", status)
	}
}

// --- test-only helpers for the node exec suite ---

type boardView struct {
	Columns []struct {
		Key   string `json:"key"`
		Tasks []struct {
			ID int32 `json:"id"`
		} `json:"tasks"`
	} `json:"columns"`
}

// fetchBoard loads the project board as the agent (agentd discovery path).
func fetchBoard(t *testing.T, client *http.Client, baseURL, tdKey, agentID, projID string) boardView {
	t.Helper()
	_, status, respBody := doRequestWithAgentKey(t, client, http.MethodGet, fmt.Sprintf("%s/api/projects/%s/board", baseURL, projID), tdKey, agentID, nil)
	if status != http.StatusOK {
		t.Fatalf("board: expected 200, got %d, body: %s", status, respBody)
	}
	var board boardView
	if err := json.Unmarshal(respBody, &board); err != nil {
		t.Fatalf("decode board: %v", err)
	}
	return board
}

func boardHasTask(board boardView, column string, taskID int32) bool {
	for _, col := range board.Columns {
		if col.Key != column {
			continue
		}
		for _, task := range col.Tasks {
			if task.ID == taskID {
				return true
			}
		}
	}
	return false
}

// wakeDueNode drives the scheduler's wake entry for one due node (the test
// router does not run the background scheduler).
func wakeDueNode(t *testing.T, nodeID string) {
	t.Helper()
	db := openTestDB(t)
	runSvc := service.NewWorkflowRunService(service.New(db, nil, nil))
	if err := runSvc.WakeDueNode(context.Background(), uuid.MustParse(nodeID)); err != nil {
		t.Fatalf("wake due node: %v", err)
	}
}

// assertTransition checks one node transition-history record.
func assertTransition(t *testing.T, nodeID, action, from, to, operatorID string) {
	t.Helper()
	q := dbQueries(t, openTestDB(t))
	transitions, err := q.ListNodeTransitions(context.Background(), uuid.MustParse(nodeID))
	if err != nil {
		t.Fatalf("list transitions: %v", err)
	}
	for _, tr := range transitions {
		if string(tr.Action) != action {
			continue
		}
		if string(tr.FromStatus) != from || string(tr.ToStatus) != to {
			t.Fatalf("transition %s = %s→%s, want %s→%s", action, tr.FromStatus, tr.ToStatus, from, to)
		}
		if !tr.OperatorID.Valid || tr.OperatorID.UUID.String() != operatorID {
			t.Fatalf("transition %s operator = %v, want %s", action, tr.OperatorID, operatorID)
		}
		return
	}
	t.Fatalf("transition %s not recorded for node %s (found %d transitions)", action, nodeID, len(transitions))
}
