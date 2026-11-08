// Package e2e_test drives the S5 GitHub vertical loop end to end against a
// real database and a fake GitHub API server (plan §16-S5): webhook delivery
// → durable event → template trigger → data.set → condition routing →
// action.github execution → flow.wait(github_event) → review event → run
// completes.
package e2e_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/scheduler"
	"github.com/teammate/server/internal/server"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/test/testdb"
)

// TestMain prepares the test database and the encryption key.
func TestMain(m *testing.M) {
	if _, err := testdb.SetupTestDB(); err != nil {
		fmt.Fprintf(osStderr, "failed to setup test database: %v\n", err)
		osExit(1)
	}
	if err := crypto.SetEncryptionKey([]byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")); err != nil {
		fmt.Fprintf(osStderr, "failed to set encryption key: %v\n", err)
		osExit(1)
	}
	code := m.Run()
	osExit(code)
}

// githubFake records GitHub API calls and answers the endpoints the flow uses.
type githubFake struct {
	srv      *httptest.Server
	mu       chan []recordedCall
	failNext bool
}

type recordedCall struct {
	Method string
	Path   string
	Body   string
	Auth   string
}

// newGitHubFake starts the fake GitHub: GET repository succeeds, issue
// comment creation returns a comment URL.
func newGitHubFake(t *testing.T) *githubFake {
	t.Helper()
	fake := &githubFake{mu: make(chan []recordedCall, 1)}
	fake.mu <- nil
	fake.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := new(bytes.Buffer)
		_, _ = body.ReadFrom(r.Body)
		calls := <-fake.mu
		calls = append(calls, recordedCall{Method: r.Method, Path: r.URL.Path, Body: body.String(), Auth: r.Header.Get("Authorization")})
		fake.mu <- calls

		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/repos/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","default_branch":"main"}`))
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments"):
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/owner/repo/issues/42#issuecomment-1"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(fake.srv.Close)
	return fake
}

// calls snapshots the recorded calls.
func (f *githubFake) calls() []recordedCall {
	select {
	case calls := <-f.mu:
		f.mu <- calls
		return calls
	default:
		return nil
	}
}

// waitFor polls fn until it returns true or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// TestGitHubVerticalLoop is the S5 acceptance flow.
func TestGitHubVerticalLoop(t *testing.T) {
	ctx := context.Background()
	fake := newGitHubFake(t)

	// HTTP surface (webhook + CRUD) and the worker service share the DB.
	db, err := sql.Open("pgx", testdb.GetTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	router := server.NewRouter(server.RouterDeps{
		Config: server.Config{
			JWTSecret:      "e2e-jwt-secret",
			AllowedOrigins: "*",
			GitHubAPIURL:   fake.srv.URL,
		},
		DB:      db,
		Redis:   nil,
		Hub:     ws.NewHub(nil),
		Gateway: ws.NewGateway(nil),
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	client := srv.Client()

	workerSvc := service.New(db, nil, nil)
	workerSvc.GitHubAPIURL = fake.srv.URL
	eventWorker := service.NewWorkflowEventService(workerSvc)
	actionWorker := service.NewWorkflowNodeActionService(workerSvc)

	// -- workspace + project + integration -------------------------------
	token, wsID := e2eRegister(t, client, srv.URL)
	projID := e2eCreateProject(t, client, srv.URL, wsID, token)
	integrationID := e2eCreateIntegration(t, client, srv.URL, wsID, projID, token)
	webhookSecret := e2eDecryptWebhookSecret(t, db, integrationID)

	// -- template: trigger → data.set → condition → (true) action → wait →
	// end; (false / failed) end -------------------------------------------
	triggerID, datasetID, conditionID, actionID, waitID, endID :=
		uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	templateName := "Issue 自动修复闭环"
	graph := map[string]interface{}{
		"name": templateName, "description": "S5 端到端", "enabled": true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.github", "name": "触发", "sort_order": 0,
				"config": map[string]interface{}{"integration_id": integrationID, "event": "issues", "actions": []string{"opened"}}},
			{"id": datasetID, "type_key": "data.set", "name": "记录标题", "sort_order": 1,
				"config": map[string]interface{}{"name": "issue_title", "value": "{{ trigger.issue.title }}"}},
			{"id": conditionID, "type_key": "flow.condition", "name": "是否 bug", "sort_order": 2,
				"config": map[string]interface{}{"condition": map[string]interface{}{
					"all": []map[string]interface{}{{"path": "trigger.issue.labels", "op": "contains", "value": "bug"}},
				}}},
			{"id": actionID, "type_key": "action.github", "name": "回复 Issue", "sort_order": 3,
				"config": map[string]interface{}{
					"operation": "add_comment",
					"issue":     "{{ trigger.issue.number }}",
					"body":      "自动回复：{{ trigger.issue.title }}",
					"retry":     map[string]interface{}{"max_attempts": 2, "backoff_seconds": 0},
				}},
			{"id": waitID, "type_key": "flow.wait", "name": "等待 Review", "sort_order": 4,
				"config": map[string]interface{}{"mode": "github_event", "event": "pull_request_review", "integration_id": integrationID}},
			{"id": endID, "type_key": "flow.end", "name": "完成", "sort_order": 5, "config": map[string]interface{}{}},
		},
		"edges": []map[string]interface{}{
			{"source_node_id": triggerID, "source_port": "success", "target_node_id": datasetID, "target_port": "input"},
			{"source_node_id": datasetID, "source_port": "success", "target_node_id": conditionID, "target_port": "input"},
			{"source_node_id": conditionID, "source_port": "true", "target_node_id": actionID, "target_port": "input"},
			{"source_node_id": conditionID, "source_port": "false", "target_node_id": endID, "target_port": "completed"},
			{"source_node_id": actionID, "source_port": "success", "target_node_id": waitID, "target_port": "input"},
			{"source_node_id": actionID, "source_port": "failed", "target_node_id": endID, "target_port": "failed"},
			{"source_node_id": waitID, "source_port": "resumed", "target_node_id": endID, "target_port": "completed"},
		},
	}
	templateID := e2eCreateTemplate(t, client, srv.URL, wsID, token, graph)

	// -- webhook delivery: issues/opened with a correct HMAC ---------------
	issueBody := `{"action":"opened","issue":{"number":42,"title":"登录页提交后返回 500","body":"复现步骤","labels":[{"name":"bug"},{"name":"ui"}],"html_url":"https://github.com/owner/repo/issues/42"},"repository":{"id":1,"full_name":"owner/repo","default_branch":"main"},"sender":{"login":"octocat"}}`
	e2eDeliverWebhook(t, client, srv.URL, integrationID, webhookSecret, "issues", "d1000000-0000-4000-8000-000000000001", issueBody)

	// Drain the event worker until the template's run exists.
	var taskID int32
	waitFor(t, 10*time.Second, "triggered task", func() bool {
		if err := eventWorker.ProcessPendingEvents(ctx); err != nil {
			t.Logf("event worker: %v", err)
		}
		taskID = e2eTaskIDByTemplate(t, db, templateID)
		return taskID != 0
	})

	// Trigger node output = the normalized payload; data.set wrote the var.
	nodes := e2eGraphNodes(t, client, srv.URL, token, taskID)
	trigger := e2eNodeByTemplate(t, nodes, triggerID)
	if trigger["status"] != "succeeded" {
		t.Fatalf("trigger status = %v", trigger["status"])
	}
	triggerOutput, _ := trigger["output"].(map[string]interface{})
	if triggerOutput["event"] != "issues" || triggerOutput["action"] != "opened" {
		t.Fatalf("trigger output not the normalized payload: %v", triggerOutput)
	}
	dataset := e2eNodeByTemplate(t, nodes, datasetID)
	if dataset["status"] != "succeeded" {
		t.Fatalf("data.set status = %v", dataset["status"])
	}
	vars := e2eTaskVars(t, db, taskID)
	if vars["issue_title"] != "登录页提交后返回 500" {
		t.Fatalf("vars.issue_title = %v, want the rendered issue title", vars["issue_title"])
	}

	// Condition routed the true branch; the false edge resolved skipped.
	condition := e2eNodeByTemplate(t, nodes, conditionID)
	if condition["status"] != "succeeded" {
		t.Fatalf("condition status = %v", condition["status"])
	}
	if !e2eEdgeSkipped(t, client, srv.URL, token, taskID, conditionID, "false") {
		t.Fatalf("condition false edge should be skipped")
	}

	// action.github is ready; the node worker executes it against the fake.
	waitFor(t, 5*time.Second, "action.github ready", func() bool {
		return e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), actionID)["status"] == "ready"
	})
	waitFor(t, 10*time.Second, "action.github executed", func() bool {
		if err := actionWorker.ProcessGitHubActionNodes(ctx); err != nil {
			t.Logf("action worker: %v", err)
		}
		return e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), actionID)["status"] == "succeeded"
	})
	calls := fake.calls()
	var commentCall *recordedCall
	for i := range calls {
		if calls[i].Method == http.MethodPost && strings.Contains(calls[i].Path, "/issues/42/comments") {
			commentCall = &calls[i]
		}
	}
	if commentCall == nil {
		t.Fatalf("fake github received no comment call: %+v", calls)
	}
	if !strings.Contains(commentCall.Body, "自动回复：登录页提交后返回 500") {
		t.Fatalf("comment body not rendered from trigger: %s", commentCall.Body)
	}
	if !strings.HasPrefix(commentCall.Auth, "Bearer github_pat_e2e") {
		t.Fatalf("comment call auth = %q", commentCall.Auth)
	}

	// flow.wait(github_event) registered a waiter and is waiting.
	waitFor(t, 5*time.Second, "wait node waiting", func() bool {
		return e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), waitID)["status"] == "waiting"
	})
	var waiterCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM workflow_event_waiters WHERE task_id = $1 AND status = 'waiting'`, taskID).Scan(&waiterCount); err != nil {
		t.Fatalf("query waiters: %v", err)
	}
	if waiterCount != 1 {
		t.Fatalf("waiting waiters = %d, want 1", waiterCount)
	}

	// -- replay the same delivery: no second task --------------------------
	e2eDeliverWebhook(t, client, srv.URL, integrationID, webhookSecret, "issues", "d1000000-0000-4000-8000-000000000001", issueBody)
	if err := eventWorker.ProcessPendingEvents(ctx); err != nil {
		t.Fatalf("event worker replay: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if tasks := e2eTaskCountByTemplate(t, db, templateID); tasks != 1 {
		t.Fatalf("task count after replay = %d, want 1", tasks)
	}

	// -- review event wakes the waiter; the run completes ------------------
	reviewBody := `{"action":"submitted","review":{"state":"approved"},"pull_request":{"number":7,"title":"Fix","body":"","labels":[],"html_url":"https://github.com/owner/repo/pull/7"},"repository":{"id":1,"full_name":"owner/repo","default_branch":"main"},"sender":{"login":"rev"}}`
	e2eDeliverWebhook(t, client, srv.URL, integrationID, webhookSecret, "pull_request_review", "d1000000-0000-4000-8000-000000000002", reviewBody)
	waitFor(t, 10*time.Second, "task completed", func() bool {
		if err := eventWorker.ProcessPendingEvents(ctx); err != nil {
			t.Logf("event worker: %v", err)
		}
		return e2eTaskStatus(t, db, taskID) == "completed"
	})

	waitNode := e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), waitID)
	if waitNode["status"] != "succeeded" {
		t.Fatalf("wait node status = %v, want succeeded", waitNode["status"])
	}
	waitOutput, _ := waitNode["output"].(map[string]interface{})
	if waitOutput["outcome"] != "resumed" {
		t.Fatalf("wait output = %v, want outcome resumed", waitOutput)
	}
	finalNodes := e2eGraphNodes(t, client, srv.URL, token, taskID)
	if endNode := e2eNodeByTemplate(t, finalNodes, endID); endNode["status"] != "succeeded" {
		t.Fatalf("end node status = %v, want succeeded", endNode["status"])
	}
	if got := e2eTaskStatus(t, db, taskID); got != "completed" {
		t.Fatalf("task status = %s, want completed", got)
	}
}

// TestGitHubActionRetriesOnRateLimit verifies a 429 from GitHub sends the
// action.github node through the engine's retry path (FailNode → waiting →
// retry-due scan → ready) and the second attempt succeeds.
func TestGitHubActionRetriesOnRateLimit(t *testing.T) {
	ctx := context.Background()
	attempts := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments") {
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"message":"API rate limit exceeded"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"html_url":"https://github.com/owner/repo/issues/1#issuecomment-2"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer fake.Close()

	db, err := sql.Open("pgx", testdb.GetTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	router := server.NewRouter(server.RouterDeps{
		Config: server.Config{
			JWTSecret:      "e2e-jwt-secret",
			AllowedOrigins: "*",
			GitHubAPIURL:   fake.URL,
		},
		DB:      db,
		Redis:   nil,
		Hub:     ws.NewHub(nil),
		Gateway: ws.NewGateway(nil),
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	client := srv.Client()

	workerSvc := service.New(db, nil, nil)
	workerSvc.GitHubAPIURL = fake.URL
	actionWorker := service.NewWorkflowNodeActionService(workerSvc)

	token, wsID := e2eRegister(t, client, srv.URL)
	projID := e2eCreateProject(t, client, srv.URL, wsID, token)
	// The action executor resolves the project's github integration for the
	// PAT; the template itself needs no integration reference.
	e2eCreateIntegration(t, client, srv.URL, wsID, projID, token)

	triggerID, actionID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	graph := map[string]interface{}{
		"name": "限流重试", "description": "", "enabled": true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.manual", "name": "开始", "sort_order": 0, "config": map[string]interface{}{}},
			{"id": actionID, "type_key": "action.github", "name": "评论", "sort_order": 1,
				"config": map[string]interface{}{
					"operation": "add_comment",
					"issue":     "1",
					"body":      "重试",
					"retry":     map[string]interface{}{"max_attempts": 3, "backoff_seconds": 0},
				}},
			{"id": endID, "type_key": "flow.end", "name": "完成", "sort_order": 2, "config": map[string]interface{}{}},
		},
		"edges": []map[string]interface{}{
			{"source_node_id": triggerID, "source_port": "success", "target_node_id": actionID, "target_port": "input"},
			{"source_node_id": actionID, "source_port": "success", "target_node_id": endID, "target_port": "completed"},
			{"source_node_id": actionID, "source_port": "failed", "target_node_id": endID, "target_port": "failed"},
		},
	}
	templateID := e2eCreateTemplate(t, client, srv.URL, wsID, token, graph)

	// Manual run; the trigger completes inline and action.github is ready.
	status, raw := e2eDo(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/projects/%s/workflows/%s/runs", srv.URL, projID, templateID), token,
		map[string]interface{}{"input": map[string]interface{}{}})
	if status != http.StatusCreated {
		t.Fatalf("start run: expected 201, got %d, body: %s", status, raw)
	}
	var run struct {
		TaskID int32 `json:"task_id"`
	}
	_ = json.Unmarshal(raw, &run)
	taskID := run.TaskID

	waitFor(t, 5*time.Second, "action ready", func() bool {
		return e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), actionID)["status"] == "ready"
	})

	// First attempt hits the 429 → FailNode puts the node back to waiting.
	waitFor(t, 10*time.Second, "node waiting after 429", func() bool {
		if err := actionWorker.ProcessGitHubActionNodes(ctx); err != nil {
			t.Logf("action worker: %v", err)
		}
		return e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), actionID)["status"] == "waiting"
	})

	// The retry-due scan wakes the node; the second attempt succeeds.
	retryScheduler := scheduler.NewScheduler(workerSvc.Store, nil, nil)
	retryScheduler.WorkflowRuns = service.NewWorkflowRunService(workerSvc)
	if err := retryScheduler.ProcessRetryDueNodes(ctx); err != nil {
		t.Fatalf("retry due scan: %v", err)
	}
	waitFor(t, 10*time.Second, "node succeeded after retry", func() bool {
		if err := actionWorker.ProcessGitHubActionNodes(ctx); err != nil {
			t.Logf("action worker: %v", err)
		}
		return e2eNodeByTemplate(t, e2eGraphNodes(t, client, srv.URL, token, taskID), actionID)["status"] == "succeeded"
	})
	if attempts != 2 {
		t.Fatalf("github attempts = %d, want 2", attempts)
	}
	waitFor(t, 5*time.Second, "task completed", func() bool {
		return e2eTaskStatus(t, db, taskID) == "completed"
	})
}
