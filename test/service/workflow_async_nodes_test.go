// workflow_async_nodes_test.go covers the S6 async executors at the service
// level: action.http against an httptest fake (success JSON output and header
// whitelist, non-2xx retry through the engine, redirect refusal, scheme
// policy, 1 MiB truncation, timeout) and action.create_task (independent task
// row plus expression rendering).
package service_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/scheduler"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// runAsyncWorker executes one async-node worker pass (ready nodes + wake
// scan) on the test env.
func (e *runTestEnv) runAsyncWorker(t *testing.T) {
	t.Helper()
	if err := service.NewWorkflowAsyncNodeService(e.svc).ProcessWorkflowAsyncNodes(context.Background()); err != nil {
		t.Fatalf("async worker pass: %v", err)
	}
}

// wakeRetryDueNodes runs the scheduler retry-due scan so waiting nodes with a
// due next_retry_at return to ready.
func (e *runTestEnv) wakeRetryDueNodes(t *testing.T) {
	t.Helper()
	s := scheduler.NewScheduler(e.svc.Store, nil, nil)
	s.WorkflowRuns = service.NewWorkflowRunService(e.svc)
	if err := s.ProcessRetryDueNodes(context.Background()); err != nil {
		t.Fatalf("retry due scan: %v", err)
	}
}

// startAsyncFlow starts a run of trigger.manual → action node → flow.end with
// the given trigger payload and returns the task id and the action node.
func (e *runTestEnv) startAsyncFlow(t *testing.T, payload json.RawMessage, typeKey, config string) (int32, string) {
	t.Helper()
	triggerID, actionID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	tplID := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{
			wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
			wfNode(actionID, typeKey, "执行", 1, config),
			wfNode(endID, "flow.end", "完成", 2, `{}`),
		},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), triggerID, "success", actionID, "input"),
			wfEdge(uuid.NewString(), actionID, "success", endID, "completed"),
			wfEdge(uuid.NewString(), actionID, "failed", endID, "failed"),
		})
	taskID := e.start(t, tplID, payload)
	return taskID, actionID
}

// nodeError decodes a node's structured error.
func nodeError(t *testing.T, node types.TaskNode) map[string]string {
	t.Helper()
	var out map[string]string
	if err := json.Unmarshal(node.Error, &out); err != nil {
		t.Fatalf("decode node error %s: %v", node.Error, err)
	}
	return out
}

// TestAsyncHTTPNodeSuccess verifies the happy path: the header expression
// renders against the trigger payload, the JSON body auto-parses and only
// whitelisted response headers reach the node output.
func TestAsyncHTTPNodeSuccess(t *testing.T) {
	e := setupRunTest(t)
	// The httptest fake speaks plain http; the deployment opt-in is under test
	// separately in TestAsyncHTTPNodeSchemePolicy.
	e.svc.HTTPNodeAllowHTTP = true

	var gotTrace string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTrace = r.Header.Get("X-Trace")
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "req-1")
		w.Header().Set("X-Secret", "must-not-leak")
		_, _ = w.Write([]byte(`{"ok":true,"n":3}`))
	}))
	t.Cleanup(fake.Close)

	taskID, httpID := e.startAsyncFlow(t, json.RawMessage(`{"num":7}`), "action.http", fmt.Sprintf(
		`{"method":"GET","url":"%s/hook","headers":{"X-Trace":"t-{{ trigger.num }}"}}`, fake.URL))

	node := e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusReady {
		t.Fatalf("http node status = %s, want ready", node.Status)
	}
	e.runAsyncWorker(t)

	node = e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("http node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	var output struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    map[string]any    `json:"body"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode output %s: %v", node.Output, err)
	}
	if output.Status != 200 {
		t.Fatalf("output status = %d, want 200", output.Status)
	}
	if output.Body["ok"] != true || output.Body["n"] != float64(3) {
		t.Fatalf("output body not the parsed JSON: %v", output.Body)
	}
	if output.Headers["X-Request-Id"] != "req-1" {
		t.Fatalf("headers[X-Request-Id] = %q, want req-1", output.Headers["X-Request-Id"])
	}
	if _, whitelisted := output.Headers["Content-Type"]; !whitelisted {
		t.Fatalf("Content-Type missing from whitelisted headers: %v", output.Headers)
	}
	if _, leaked := output.Headers["X-Secret"]; leaked {
		t.Fatalf("non-whitelisted header leaked: %v", output.Headers)
	}
	if gotTrace != "t-7" {
		t.Fatalf("fake saw X-Trace = %q, want rendered t-7", gotTrace)
	}
	if got := e.task(t, taskID).Status; got != types.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed", got)
	}
}

// TestAsyncHTTPNodeNon2xxRetries verifies a 500 sends the node through the
// engine's retry path (FailNode → waiting → retry-due scan → ready) and the
// second attempt succeeds; the request body renders as JSON.
func TestAsyncHTTPNodeNon2xxRetries(t *testing.T) {
	e := setupRunTest(t)
	e.svc.HTTPNodeAllowHTTP = true

	var attempts atomic.Int32
	var gotBody string
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`boom`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(fake.Close)

	taskID, httpID := e.startAsyncFlow(t, json.RawMessage(`{"v":"v"}`), "action.http", fmt.Sprintf(
		`{"method":"POST","url":"%s/api","body":{"k":"{{ trigger.v }}"},"retry":{"max_attempts":2,"backoff_seconds":0}}`, fake.URL))

	e.runAsyncWorker(t)
	node := e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusWaiting {
		t.Fatalf("after 500: node status = %s, want waiting for retry", node.Status)
	}
	if attempts.Load() != 1 {
		t.Fatalf("attempts after first pass = %d, want 1", attempts.Load())
	}

	e.wakeRetryDueNodes(t)
	e.runAsyncWorker(t)

	node = e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("after retry: node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	if attempts.Load() != 2 {
		t.Fatalf("attempts = %d, want 2", attempts.Load())
	}
	if gotBody != `{"k":"v"}` {
		t.Fatalf("fake saw body %q, want the rendered JSON object", gotBody)
	}
	if got := e.task(t, taskID).Status; got != types.TaskStatusCompleted {
		t.Fatalf("task status = %s, want completed", got)
	}
}

// TestAsyncHTTPNodeRedirectRejected verifies the client refuses to follow a
// redirect: the node fails and the redirect target is never contacted.
func TestAsyncHTTPNodeRedirectRejected(t *testing.T) {
	e := setupRunTest(t)
	e.svc.HTTPNodeAllowHTTP = true

	hits := map[string]int{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits[r.URL.Path]++
		if r.URL.Path == "/start" {
			w.Header().Set("Location", "/elsewhere")
			w.WriteHeader(http.StatusFound)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(fake.Close)

	taskID, httpID := e.startAsyncFlow(t, json.RawMessage(`{}`), "action.http", fmt.Sprintf(
		`{"method":"GET","url":"%s/start"}`, fake.URL))

	e.runAsyncWorker(t)

	node := e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusFailed {
		t.Fatalf("node status = %s, want failed", node.Status)
	}
	if code := nodeError(t, node)["code"]; code != "http_redirect_disallowed" {
		t.Fatalf("error code = %q, want http_redirect_disallowed", code)
	}
	if hits["/start"] != 1 || hits["/elsewhere"] != 0 {
		t.Fatalf("redirect followed: hits = %v", hits)
	}
}

// TestAsyncHTTPNodeSchemePolicy verifies plain http:// URLs fail by default
// and succeed when the deployment opts in via the service flag.
func TestAsyncHTTPNodeSchemePolicy(t *testing.T) {
	e := setupRunTest(t)

	var requests atomic.Int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(fake.Close)
	config := fmt.Sprintf(`{"method":"GET","url":"%s/hook"}`, fake.URL)

	// Default deployment: http is rejected without contacting the target.
	taskID, httpID := e.startAsyncFlow(t, json.RawMessage(`{}`), "action.http", config)
	e.runAsyncWorker(t)
	node := e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusFailed {
		t.Fatalf("default: node status = %s, want failed", node.Status)
	}
	if code := nodeError(t, node)["code"]; code != "http_scheme_disallowed" {
		t.Fatalf("default: error code = %q, want http_scheme_disallowed", code)
	}
	if requests.Load() != 0 {
		t.Fatalf("default: the target was contacted %d times", requests.Load())
	}

	// Explicit opt-in: the same request goes through.
	taskID, httpID = e.startAsyncFlow(t, json.RawMessage(`{}`), "action.http", config)
	allowSvc := service.New(e.db, nil, nil)
	allowSvc.HTTPNodeAllowHTTP = true
	if err := service.NewWorkflowAsyncNodeService(allowSvc).ProcessWorkflowAsyncNodes(context.Background()); err != nil {
		t.Fatalf("allow-http worker pass: %v", err)
	}
	node = e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("allow-http: node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	if requests.Load() != 1 {
		t.Fatalf("allow-http: requests = %d, want 1", requests.Load())
	}
}

// TestAsyncHTTPNodeTruncatesLargeBody verifies responses beyond 1 MiB are
// truncated and the string body keeps the cap (JSON parse degrades to text).
func TestAsyncHTTPNodeTruncatesLargeBody(t *testing.T) {
	e := setupRunTest(t)
	e.svc.HTTPNodeAllowHTTP = true

	big := strings.Repeat("x", 2<<20)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(big))
	}))
	t.Cleanup(fake.Close)

	taskID, httpID := e.startAsyncFlow(t, json.RawMessage(`{}`), "action.http", fmt.Sprintf(
		`{"method":"GET","url":"%s/big"}`, fake.URL))

	e.runAsyncWorker(t)

	node := e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	var output struct {
		Status  int               `json:"status"`
		Headers map[string]string `json:"headers"`
		Body    any               `json:"body"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode output: %v", err)
	}
	body, ok := output.Body.(string)
	if !ok {
		t.Fatalf("body = %T, want the truncated string", output.Body)
	}
	if len(body) != 1<<20 {
		t.Fatalf("body length = %d, want the 1 MiB cap", len(body))
	}
	if output.Headers["Content-Type"] != "text/plain" {
		t.Fatalf("headers = %v, want Content-Type whitelisted", output.Headers)
	}
}

// TestAsyncHTTPNodeTimeout verifies the config timeout applies: the request
// against a slow endpoint fails with http_request_failed.
func TestAsyncHTTPNodeTimeout(t *testing.T) {
	e := setupRunTest(t)
	e.svc.HTTPNodeAllowHTTP = true

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(fake.Close)

	taskID, httpID := e.startAsyncFlow(t, json.RawMessage(`{}`), "action.http", fmt.Sprintf(
		`{"method":"GET","url":"%s/slow","timeout_minutes":0.03}`, fake.URL))

	e.runAsyncWorker(t)

	node := e.nodeByTemplate(t, taskID, httpID)
	if node.Status != types.TaskNodeStatusFailed {
		t.Fatalf("node status = %s, want failed", node.Status)
	}
	if code := nodeError(t, node)["code"]; code != "http_request_failed" {
		t.Fatalf("error code = %q, want http_request_failed", code)
	}
}

// TestAsyncCreateTaskNodeRendersVars verifies the create_task title and
// description render vars references written upstream by data.set: the
// spawned task carries the rendered text, never the raw "{{ vars.key }}".
func TestAsyncCreateTaskNodeRendersVars(t *testing.T) {
	e := setupRunTest(t)

	triggerID, setID, createID, endID := uuid.NewString(), uuid.NewString(), uuid.NewString(), uuid.NewString()
	tplID := e.createRunTemplate(t,
		[]types.CreateTemplateNodeParams{
			wfNode(triggerID, "trigger.manual", "开始", 0, `{}`),
			wfNode(setID, "data.set", "预置变量", 1, `{"name":"key","value":"登录页修复"}`),
			wfNode(createID, "action.create_task", "建任务", 2,
				`{"title":"处理:{{ vars.key }}","description":"来源:{{ vars.key }}"}`),
			wfNode(endID, "flow.end", "完成", 3, `{}`),
		},
		[]types.CreateTemplateEdgeParams{
			wfEdge(uuid.NewString(), triggerID, "success", setID, "input"),
			wfEdge(uuid.NewString(), setID, "success", createID, "input"),
			wfEdge(uuid.NewString(), createID, "success", endID, "completed"),
			wfEdge(uuid.NewString(), createID, "failed", endID, "failed"),
		})
	taskID := e.start(t, tplID, json.RawMessage(`{}`))

	if vars := e.contextVars(t, taskID); vars["key"] != "登录页修复" {
		t.Fatalf("vars = %v, want key=登录页修复", vars)
	}
	e.runAsyncWorker(t)

	node := e.nodeByTemplate(t, taskID, createID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("create_task node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	var output struct {
		TaskID    int32  `json:"task_id"`
		TaskTitle string `json:"task_title"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode output %s: %v", node.Output, err)
	}
	created := e.task(t, output.TaskID)
	if created.Title != "处理:登录页修复" || created.Description != "来源:登录页修复" {
		t.Fatalf("created title/description = %q/%q, want the vars-rendered text", created.Title, created.Description)
	}
	if strings.Contains(created.Title, "{{") || strings.Contains(created.Description, "{{") {
		t.Fatalf("unresolved placeholder leaked: %q / %q", created.Title, created.Description)
	}
}

// TestAsyncCreateTaskNode verifies the spawned task row (independent, type
// task, system author, no nodes, MAX+1 sequence) and the rendered fields.
func TestAsyncCreateTaskNode(t *testing.T) {
	e := setupRunTest(t)

	taskID, createID := e.startAsyncFlow(t, json.RawMessage(`{"title":"发布准备"}`), "action.create_task",
		`{"title":"跟进 {{ trigger.title }}","description":"来源：{{ trigger.title }}"}`)

	node := e.nodeByTemplate(t, taskID, createID)
	if node.Status != types.TaskNodeStatusReady {
		t.Fatalf("create_task node status = %s, want ready", node.Status)
	}
	e.runAsyncWorker(t)

	node = e.nodeByTemplate(t, taskID, createID)
	if node.Status != types.TaskNodeStatusSucceeded {
		t.Fatalf("node status = %s, want succeeded (error %s)", node.Status, node.Error)
	}
	var output struct {
		TaskID    int32  `json:"task_id"`
		TaskTitle string `json:"task_title"`
	}
	if err := json.Unmarshal(node.Output, &output); err != nil {
		t.Fatalf("decode output %s: %v", node.Output, err)
	}
	if output.TaskID == 0 || output.TaskTitle != "跟进 发布准备" {
		t.Fatalf("output = %+v, want the created task with the rendered title", output)
	}

	projectUUID, err := uuid.Parse(e.projID)
	if err != nil {
		t.Fatalf("parse project id: %v", err)
	}
	created, err := e.svc.Store.GetTask(context.Background(), output.TaskID)
	if err != nil {
		t.Fatalf("get created task: %v", err)
	}
	if created.ProjectID != e.projID {
		t.Fatalf("created task project = %s, want the same project %s", created.ProjectID, e.projID)
	}
	if created.Type != "task" || created.AuthorType != "system" || created.Status != types.TaskStatusActive {
		t.Fatalf("created task type/author/status = %s/%s/%s, want task/system/active", created.Type, created.AuthorType, created.Status)
	}
	if created.ParentTaskID != nil {
		t.Fatalf("created task parent = %v, want nil (independent task)", *created.ParentTaskID)
	}
	if created.Title != "跟进 发布准备" || created.Description != "来源：发布准备" {
		t.Fatalf("created task title/description = %q/%q, want the rendered values", created.Title, created.Description)
	}
	nodes, err := e.svc.Store.ListTaskNodes(context.Background(), created.ID)
	if err != nil {
		t.Fatalf("list created task nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("created task has %d nodes, want none", len(nodes))
	}
	var maxSequence int
	if err := e.db.QueryRow(`SELECT COALESCE(MAX(sequence), 0) FROM tasks WHERE project_id = $1`, projectUUID).Scan(&maxSequence); err != nil {
		t.Fatalf("query max sequence: %v", err)
	}
	if created.Sequence != maxSequence {
		t.Fatalf("created sequence = %d, want the project max %d", created.Sequence, maxSequence)
	}
	if got := e.task(t, taskID).Status; got != types.TaskStatusCompleted {
		t.Fatalf("run task status = %s, want completed", got)
	}
}
