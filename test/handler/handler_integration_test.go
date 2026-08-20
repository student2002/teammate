// handler_integration_test.go 覆盖 handler 层集成流程的测试。
package handler_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	dbgen "github.com/teammate/server/internal/db/generated"
)

// ---------- 工作区 API ----------

func TestWorkspaceAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, _ := registerTestUser(t, client, srv.URL)

	// 2. Create workspace
	createBody := map[string]string{
		"name":         "test-ws-" + uuid.New().String()[:8],
		"description":  "integration test workspace",
		"issue_prefix": "TW",
	}
	resp, status, body := doRequestWithToken(t, client, http.MethodPost, srv.URL+"/api/workspaces", token, createBody)
	_ = resp
	if status != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d, body: %s", status, body)
	}

	var wsResult map[string]interface{}
	if err := json.Unmarshal(body, &wsResult); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	createdWsID := wsResult["id"].(string)
	t.Logf("Created workspace: %s", createdWsID)

	// 清理
	defer func() {
		doRequestWithToken(t, client, http.MethodDelete, srv.URL+"/api/workspaces/"+createdWsID, token, nil)
	}()

	// 3. List workspaces
	_, status, body = doRequestWithToken(t, client, http.MethodGet, srv.URL+"/api/workspaces", token, nil)
	if status != http.StatusOK {
		t.Fatalf("ListWorkspaces: expected 200, got %d", status)
	}
	var wsList []map[string]interface{}
	if err := json.Unmarshal(body, &wsList); err != nil {
		t.Fatalf("decode workspaces list: %v", err)
	}
	if len(wsList) == 0 {
		t.Fatal("ListWorkspaces: expected at least 1 workspace")
	}

	// 4. Get workspace
	_, status, body = doRequestWithToken(t, client, http.MethodGet, srv.URL+"/api/workspaces/"+createdWsID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("GetWorkspace: expected 200, got %d, body: %s", status, string(body))
	}
	var wsGet map[string]interface{}
	if err := json.Unmarshal(body, &wsGet); err != nil {
		t.Fatalf("decode workspace: %v", err)
	}
	if wsGet["name"] != createBody["name"] {
		t.Fatalf("expected name %q, got %q", createBody["name"], wsGet["name"])
	}

	// 5. Update workspace
	updateBody := map[string]string{
		"name":        "updated-ws",
		"description": "updated description",
	}
	_, status, body = doRequestWithToken(t, client, http.MethodPut, srv.URL+"/api/workspaces/"+createdWsID, token, updateBody)
	if status != http.StatusOK {
		t.Fatalf("UpdateWorkspace: expected 200, got %d, body: %s", status, body)
	}
	var wsUpdated map[string]interface{}
	if err := json.Unmarshal(body, &wsUpdated); err != nil {
		t.Fatalf("decode updated workspace: %v", err)
	}
	if wsUpdated["name"] != "updated-ws" {
		t.Fatalf("expected name 'updated-ws', got %q", wsUpdated["name"])
	}

	// 6. Delete workspace
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, srv.URL+"/api/workspaces/"+createdWsID, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace: expected 204, got %d", status)
	}
}

// ---------- 工作流 API ----------

func TestWorkflowAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	baseURL := srv.URL + "/api/workspaces/" + wsID + "/workflows"

	// 1. 创建带节点的工作流模板
	createBody := map[string]interface{}{
		"name":        "test-workflow",
		"description": "integration test workflow",
		"nodes": []map[string]interface{}{
			{
				"name":            "code-node",
				"description":     "write code",
				"sort_order":      1,
				"node_type":       "standard",
				"assignee_type":   "any_agent",
				"timeout_minutes": 60,
			},
			{
				"name":            "review-node",
				"description":     "review code",
				"sort_order":      2,
				"node_type":       "review",
				"assignee_type":   "any_agent",
				"timeout_minutes": 30,
			},
		},
	}
	_, status, body := doRequestWithToken(t, client, http.MethodPost, baseURL, token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("CreateWorkflowTemplate: expected 201, got %d, body: %s", status, body)
	}

	var wfResult map[string]interface{}
	if err := json.Unmarshal(body, &wfResult); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	tplMap := wfResult["template"].(map[string]interface{})
	tplID := tplMap["id"].(string)
	t.Logf("Created workflow template: %s", tplID)

	// 2. List templates
	_, status, body = doRequestWithToken(t, client, http.MethodGet, baseURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("ListWorkflowTemplates: expected 200, got %d", status)
	}
	var tplList []map[string]interface{}
	if err := json.Unmarshal(body, &tplList); err != nil {
		t.Fatalf("decode templates list: %v", err)
	}
	if len(tplList) == 0 {
		t.Fatal("ListWorkflowTemplates: expected at least 1 template")
	}

	// 3. Delete template
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, baseURL+"/"+tplID, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DeleteWorkflowTemplate: expected 204, got %d", status)
	}
}

// ---------- 项目 API ----------

func TestProjectAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	baseURL := srv.URL + "/api/workspaces/" + wsID + "/projects"

	// 1. Create project
	projName := "proj-" + uuid.New().String()[:8]
	createBody := map[string]interface{}{
		"name":               projName,
		"description":        "integration test project",
		"status":             "planned",
		"repo_url":           "https://github.com/test/repo.git",
		"max_review_cycles":  3,
	}
	_, status, body := doRequestWithToken(t, client, http.MethodPost, baseURL, token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("CreateProject: expected 201, got %d, body: %s", status, body)
	}

	var projResult map[string]interface{}
	if err := json.Unmarshal(body, &projResult); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	projID := projResult["id"].(string)
	t.Logf("Created project: %s", projID)

	// 2. List projects
	_, status, body = doRequestWithToken(t, client, http.MethodGet, baseURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("ListProjects: expected 200, got %d", status)
	}
	var projList []map[string]interface{}
	if err := json.Unmarshal(body, &projList); err != nil {
		t.Fatalf("decode projects list: %v", err)
	}
	if len(projList) == 0 {
		t.Fatal("ListProjects: expected at least 1 project")
	}

	// 3. Get project
	_, status, body = doRequestWithToken(t, client, http.MethodGet, baseURL+"/"+projID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("GetProject: expected 200, got %d", status)
	}
	var projGet map[string]interface{}
	if err := json.Unmarshal(body, &projGet); err != nil {
		t.Fatalf("decode project: %v", err)
	}
	if projGet["name"] != projName {
		t.Fatalf("expected name %q, got %q", projName, projGet["name"])
	}

	// 4. Update project
	updateBody := map[string]interface{}{
		"name":        "updated-project",
		"description": "updated description",
		"status":      "active",
	}
	_, status, body = doRequestWithToken(t, client, http.MethodPut, baseURL+"/"+projID, token, updateBody)
	if status != http.StatusOK {
		t.Fatalf("UpdateProject: expected 200, got %d, body: %s", status, body)
	}
	var projUpdated map[string]interface{}
	if err := json.Unmarshal(body, &projUpdated); err != nil {
		t.Fatalf("decode updated project: %v", err)
	}
	if projUpdated["name"] != "updated-project" {
		t.Fatalf("expected name 'updated-project', got %q", projUpdated["name"])
	}

	// 5. Delete project
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, baseURL+"/"+projID, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DeleteProject: expected 204, got %d", status)
	}
}

// ---------- 代理 API ----------

func TestAgentAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	baseURL := srv.URL + "/api/workspaces/" + wsID + "/agents"

	// 1. Create agent
	createBody := map[string]interface{}{
		"name":         "test-agent",
		"provider":     "claude",
		"instructions": "You are a test agent",
		"model":        "claude-3.5-sonnet",
		"extra_args":   []string{},
		"git_name":     "Test Agent",
		"git_email":    "agent@test.com",
	}
	_, status, body := doRequestWithToken(t, client, http.MethodPost, baseURL, token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("CreateAgent: expected 201, got %d, body: %s", status, body)
	}

	var agentResult map[string]interface{}
	if err := json.Unmarshal(body, &agentResult); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	agentID := agentResult["id"].(string)
	t.Logf("Created agent: %s", agentID)

	// 2. List agents
	_, status, body = doRequestWithToken(t, client, http.MethodGet, baseURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("ListAgents: expected 200, got %d", status)
	}
	var agentList []map[string]interface{}
	if err := json.Unmarshal(body, &agentList); err != nil {
		t.Fatalf("decode agents list: %v", err)
	}
	if len(agentList) == 0 {
		t.Fatal("ListAgents: expected at least 1 agent")
	}

	// 3. Get agent
	_, status, body = doRequestWithToken(t, client, http.MethodGet, baseURL+"/"+agentID, token, nil)
	if status != http.StatusOK {
		t.Fatalf("GetAgent: expected 200, got %d", status)
	}
	var agentGet map[string]interface{}
	if err := json.Unmarshal(body, &agentGet); err != nil {
		t.Fatalf("decode agent: %v", err)
	}
	if agentGet["name"] != "test-agent" {
		t.Fatalf("expected name 'test-agent', got %q", agentGet["name"])
	}

	// 4. Update agent
	updateBody := map[string]interface{}{
		"instructions": "Updated instructions",
		"model":        "claude-4",
		"status":       "online",
		"extra_args":   []string{"--verbose"},
	}
	_, status, body = doRequestWithToken(t, client, http.MethodPut, baseURL+"/"+agentID, token, updateBody)
	if status != http.StatusOK {
		t.Fatalf("UpdateAgent: expected 200, got %d, body: %s", status, body)
	}
	var agentUpdated map[string]interface{}
	if err := json.Unmarshal(body, &agentUpdated); err != nil {
		t.Fatalf("decode updated agent: %v", err)
	}
	if agentUpdated["status"] != "online" {
		t.Fatalf("expected status 'online', got %q", agentUpdated["status"])
	}

	// 5. Delete agent
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, baseURL+"/"+agentID, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DeleteAgent: expected 204, got %d", status)
	}
}

// ---------- 任务 API ----------

func TestTaskAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// 创建项目
	projID := createProject(t, client, srv.URL, wsID, token)

	// 创建带节点的工作流模板
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)

	// 设置
	updateBody := map[string]interface{}{
		"name":                "test-project",
		"description":         "integration test project",
		"status":              "active",
		"default_workflow_id": tplID,
		"max_review_cycles":   3,
	}
	_, _, _ = doRequestWithToken(t, client, http.MethodPut,
		srv.URL+"/api/workspaces/"+wsID+"/projects/"+projID, token, updateBody)

	taskBaseURL := srv.URL + "/api/projects/" + projID + "/tasks"

	// 1. 创建任务（应自动创建 task_nodes）
	authorID := uuid.New().String()
	createBody := map[string]interface{}{
		"title":                "Test task",
		"description":          "Integration test task",
		"type":                 "task",
		"priority":             "high",
		"author_type":          "agent",
		"author_id":            authorID,
		"workflow_template_id": tplID,
	}
	_, status, body := doRequestWithToken(t, client, http.MethodPost, taskBaseURL, token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d, body: %s", status, body)
	}

	var taskResult map[string]interface{}
	if err := json.Unmarshal(body, &taskResult); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	taskMap := taskResult["task"].(map[string]interface{})
	taskID := int32(taskMap["id"].(float64))
	t.Logf("Created task: %d", taskID)

	// 验证 task_nodes 已自动创建
	nodesArr, ok := taskResult["nodes"].([]interface{})
	if !ok || len(nodesArr) == 0 {
		t.Fatal("CreateTask: expected task_nodes to be auto-created, got none")
	}
	t.Logf("Auto-created %d task nodes", len(nodesArr))

	// 2. 列出任务（传入状态筛选器，避免空字符串导致 SQL 枚举转换错误）
	_, status, body = doRequestWithToken(t, client, http.MethodGet, taskBaseURL+"?status=active", token, nil)
	if status != http.StatusOK {
		t.Fatalf("ListTasks: expected 200, got %d", status)
	}
	var taskList []map[string]interface{}
	if err := json.Unmarshal(body, &taskList); err != nil {
		t.Fatalf("decode tasks list: %v", err)
	}
	if len(taskList) == 0 {
		t.Fatal("ListTasks: expected at least 1 task")
	}

	// 3. Get task
	_, status, body = doRequestWithToken(t, client, http.MethodGet, fmt.Sprintf("%s/%d", taskBaseURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("GetTask: expected 200, got %d, body: %s", status, body)
	}
	var taskGet map[string]interface{}
	if err := json.Unmarshal(body, &taskGet); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	if taskGet["title"] != "Test task" {
		t.Fatalf("expected title 'Test task', got %q", taskGet["title"])
	}

	// 4. 列出任务节点（task 作用域端点）
	_, status, body = doRequestWithToken(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("ListTaskNodes: expected 200, got %d", status)
	}
	var taskNodes []map[string]interface{}
	if err := json.Unmarshal(body, &taskNodes); err != nil {
		t.Fatalf("decode task nodes: %v", err)
	}
	if len(taskNodes) == 0 {
		t.Fatal("ListTaskNodes: expected at least 1 node")
	}

	// 清理：删除任务
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, fmt.Sprintf("%s/%d", taskBaseURL, taskID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DeleteTask: expected 204, got %d", status)
	}
}

// ---------- 节点操作 API ----------

func TestNodeOperationsAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// 完整设置：项目、工作流、任务
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)

	// 设置
	updateBody := map[string]interface{}{
		"name":                "test-project",
		"description":         "integration test project",
		"status":              "active",
		"default_workflow_id": tplID,
		"max_review_cycles":   3,
	}
	_, _, _ = doRequestWithToken(t, client, http.MethodPut,
		srv.URL+"/api/workspaces/"+wsID+"/projects/"+projID, token, updateBody)

	// 创建代理用于认领
	agentID, agentAPIToken := createAgent(t, client, srv.URL, wsID, token)
	// 将 Agent 添加到项目（认领节点所需）
	q := dbQueries(t, db)
	_, err := q.CreateProjectMember(context.Background(), dbgen.CreateProjectMemberParams{
		ProjectID:  uuid.MustParse(projID),
		MemberType: "agent",
		AgentID:    uuid.NullUUID{UUID: uuid.MustParse(agentID), Valid: true},
		Role:       "member",
	})
	if err != nil {
		t.Fatalf("addAgentToProject: %v", err)
	}
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// 创建任务
	taskBaseURL := srv.URL + "/api/projects/" + projID + "/tasks"
	authorID := uuid.New().String()
	createTaskBody := map[string]interface{}{
		"title":                "Node ops task",
		"description":          "Test node operations",
		"type":                 "task",
		"priority":             "medium",
		"author_type":          "agent",
		"author_id":            authorID,
		"workflow_template_id": tplID,
	}
	_, status, body := doRequestWithToken(t, client, http.MethodPost, taskBaseURL, token, createTaskBody)
	if status != http.StatusCreated {
		t.Fatalf("CreateTask: expected 201, got %d, body: %s", status, body)
	}

	var taskResult map[string]interface{}
	if err := json.Unmarshal(body, &taskResult); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	taskMap := taskResult["task"].(map[string]interface{})
	taskID := int32(taskMap["id"].(float64))
	nodesArr := taskResult["nodes"].([]interface{})
	if len(nodesArr) == 0 {
		t.Fatal("expected task nodes to be created")
	}

	// 获取第一个待处理节点
	firstNode := nodesArr[0].(map[string]interface{})
	nodeID := firstNode["id"].(string)
	t.Logf("Task %d, first node: %s", taskID, nodeID)

	nodeBaseURL := fmt.Sprintf("%s/api/tasks/%d/nodes", srv.URL, taskID)

	// 1. Claim node
	claimBody := map[string]interface{}{
		"agent_id": agentID,
	}
	_, status, body = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/claim", agentAPIToken, claimBody)
	if status != http.StatusOK {
		t.Fatalf("ClaimNode: expected 200, got %d, body: %s", status, body)
	}
	var claimedNode map[string]interface{}
	if err := json.Unmarshal(body, &claimedNode); err != nil {
		t.Fatalf("decode claimed node: %v", err)
	}
	if claimedNode["status"] != "in_progress" {
		t.Fatalf("expected status 'in_progress', got %q", claimedNode["status"])
	}
	t.Logf("Node claimed successfully, status: %v", claimedNode["status"])

	// 2. 同一 Agent 重复认领具有幂等性（返回 200）
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/claim", agentAPIToken, claimBody)
	if status != http.StatusOK {
		t.Fatalf("Same-agent double-claim: expected 200 (idempotent), got %d", status)
	}
	t.Log("Same-agent double-claim is idempotent (200)")

	// 3. Approve node
	approveBody := map[string]interface{}{
		"operator_id":   agentID,
		"operator_type": "agent",
		"comment":       "Looks good",
	}
	_, status, body = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/approve", agentAPIToken, approveBody)
	if status != http.StatusOK {
		t.Fatalf("ApproveNode: expected 200, got %d, body: %s", status, body)
	}
	var approvedNode map[string]interface{}
	if err := json.Unmarshal(body, &approvedNode); err != nil {
		t.Fatalf("decode approved node: %v", err)
	}
	if approvedNode["status"] != "completed" {
		t.Fatalf("expected status 'completed', got %q", approvedNode["status"])
	}
	t.Logf("Node approved, status: %v", approvedNode["status"])

	// 4. 测试乐观锁——尝试认领已完成的节点应失败
	_, status, _ = doRequestWithAPIKey(t, client, http.MethodPost, nodeBaseURL+"/"+nodeID+"/claim", agentAPIToken, claimBody)
	if status != http.StatusConflict {
		t.Fatalf("Claim completed node: expected 409, got %d", status)
	}
	t.Log("Claim on completed node correctly rejected with 409 (optimistic lock via status guard)")

	// 清理：删除任务
	_, _, _ = doRequestWithToken(t, client, http.MethodDelete, fmt.Sprintf("%s/%d", taskBaseURL, taskID), token, nil)
}

// ---------- 运行时 API ----------

func TestRuntimeAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// 创建代理
	agentID, agentAPIToken := createAgent(t, client, srv.URL, wsID, token)

	runtimeBaseURL := srv.URL + "/api/workspaces/" + wsID + "/runtimes"

	// 1. 注册 runtime（Agent 注册自己的 runtime）
	daemonID := fmt.Sprintf("daemon-%s", uuid.New().String()[:8])
	registerBody := map[string]interface{}{
		"agent_id":           agentID,
		"daemon_id":          daemonID,
		"provider":           "claude",
		"version":            "1.0.0",
		"status":             "online",
		"session_token_hash": "hash123",
		"public_key":         "pubkey123",
	}
	_, status, body := doRequestWithAPIKey(t, client, http.MethodPost, runtimeBaseURL, agentAPIToken, registerBody)
	if status != http.StatusCreated {
		t.Fatalf("RegisterRuntime: expected 201, got %d, body: %s", status, body)
	}

	var rtResult map[string]interface{}
	if err := json.Unmarshal(body, &rtResult); err != nil {
		t.Fatalf("decode runtime: %v", err)
	}
	rtID := rtResult["id"].(string)
	t.Logf("Registered runtime: %s", rtID)

	// 2. Heartbeat
	_, status, body = doRequestWithToken(t, client, http.MethodPost, runtimeBaseURL+"/"+rtID+"/heartbeat", token, nil)
	if status != http.StatusOK {
		t.Fatalf("Heartbeat: expected 200, got %d, body: %s", status, body)
	}
	var hbResult map[string]interface{}
	if err := json.Unmarshal(body, &hbResult); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	if hbResult["status"] != "online" {
		t.Fatalf("expected status 'online', got %q", hbResult["status"])
	}
	t.Log("Heartbeat successful")
}
