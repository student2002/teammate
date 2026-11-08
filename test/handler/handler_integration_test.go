// handler_integration_test.go tests handler layer integration flows.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// ---------- Workspace API ----------

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

	// Cleanup
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

// ---------- Workflow API ----------

func TestWorkflowAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	baseURL := srv.URL + "/api/workspaces/" + wsID + "/workflows"

	// 1. Create a workflow template with its full graph (V2 contract sample)
	createBody := workflowContractGraph()
	_, status, body := doRequestWithToken(t, client, http.MethodPost, baseURL, token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("CreateWorkflowTemplate: expected 201, got %d, body: %s", status, body)
	}

	var wfResult map[string]interface{}
	if err := json.Unmarshal(body, &wfResult); err != nil {
		t.Fatalf("decode workflow: %v", err)
	}
	tplMap, ok := wfResult["template"].(map[string]interface{})
	if !ok {
		t.Fatalf("CreateWorkflowTemplate: expected template in response, got %s", body)
	}
	tplID := tplMap["id"].(string)
	if nodes, _ := wfResult["nodes"].([]interface{}); len(nodes) != 7 {
		t.Errorf("CreateWorkflowTemplate: expected 7 nodes, got %d", len(nodes))
	}
	if edges, _ := wfResult["edges"].([]interface{}); len(edges) != 11 {
		t.Errorf("CreateWorkflowTemplate: expected 11 edges, got %d", len(edges))
	}
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

// ---------- Project API ----------

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

// ---------- Agent API ----------

func TestAgentAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	baseURL := srv.URL + "/api/workspaces/" + wsID + "/agents"

	// 1. Create agent (daemon-group path; tm_ credential minted by the helper)
	agentID, _ := createAgent(t, client, srv.URL, wsID, token)
	t.Logf("Created agent: %s", agentID)

	// 2. List agents
	_, status, body := doRequestWithToken(t, client, http.MethodGet, baseURL, token, nil)
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
	if agentGet["name"] == "" {
		t.Fatal("expected agent name in GET response")
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

// ---------- Task API ----------

func TestTaskAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// Create project
	projID := createProject(t, client, srv.URL, wsID, token)

	// Create a workflow template with the V2 contract graph
	tplID, _ := createV2WorkflowTemplate(t, client, srv.URL, wsID, token)

	// Setup
	updateBody := map[string]interface{}{
		"name":              "test-project",
		"description":       "integration test project",
		"status":            "active",
		"max_review_cycles": 3,
	}
	_, _, _ = doRequestWithToken(t, client, http.MethodPut,
		srv.URL+"/api/workspaces/"+wsID+"/projects/"+projID, token, updateBody)

	taskBaseURL := srv.URL + "/api/projects/" + projID + "/tasks"

	// 1. Create a task main record (V2: node instantiation is the engine's job;
	// the author identity is derived from the auth claims, not the body)
	createBody := map[string]interface{}{
		"title":                "Test task",
		"description":          "Integration test task",
		"type":                 "task",
		"priority":             "high",
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

	// 2. List tasks (pass status filter to avoid empty string causing SQL enum conversion error)
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

	// Cleanup: delete task
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, fmt.Sprintf("%s/%d", taskBaseURL, taskID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("DeleteTask: expected 204, got %d", status)
	}
}

// ---------- Daemon API ----------

func TestDaemonAPI(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)

	// Create agent
	agentID, _ := createAgent(t, client, srv.URL, wsID, token)

	// 1. Bind the agent to a fresh daemon (web creates daemon, machine registers machine info)
	daemonID, daemonToken := createDaemonForAgent(t, client, srv.URL, wsID, token, agentID, db)
	t.Logf("Daemon registered: %s", daemonID)

	// 2. Heartbeat
	_, status, body := doRequestWithAPIKey(t, client, http.MethodPost,
		srv.URL+"/api/daemons/heartbeat", daemonToken, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("Heartbeat: expected 200, got %d, body: %s", status, body)
	}
	var hbResult map[string]interface{}
	if err := json.Unmarshal(body, &hbResult); err != nil {
		t.Fatalf("decode heartbeat: %v", err)
	}
	t.Logf("Heartbeat successful, desired agents: %v", hbResult["desired_agents"])

	// 3. Deregister (graceful shutdown)
	_, status, body = doRequestWithAPIKey(t, client, http.MethodPost,
		srv.URL+"/api/daemons/deregister", daemonToken, map[string]interface{}{})
	if status != http.StatusOK {
		t.Fatalf("Deregister: expected 200, got %d, body: %s", status, body)
	}
	t.Log("Deregister successful")
}
