// api_ext_test.go 覆盖扩展 API 端点的测试。
package handler_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	dbgen "github.com/teammate/server/internal/db/generated"
)

func setupExtTestRouter(t *testing.T) (chi.Router, *sql.DB, *dbgen.Queries) {
	t.Helper()

	router, db, q := setupTestRouter(t)
	return router, db, q
}

func TestNotificationList(t *testing.T) {
	router, db, q := setupExtTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)

	// 准备
	tplID := createWorkflowTemplate2Nodes(t, client, server.URL, wsID, token)
	projID := createProject(t, client, server.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, server.URL, wsID, projID, tplID, token)

	// 创建代理并添加到项目
	agentID, agentAPIToken := createAgent(t, client, server.URL, wsID, token)
	addAgentToProject(t, q, projID, agentID)
	grantAgentAllTaskPermissions(t, client, server.URL, wsID, agentID, token)

	// 创建任务
	taskID, nodes := createTask(t, client, server.URL, projID, tplID, token)
	if len(nodes) < 1 {
		t.Fatalf("expected at least 1 node, got %d", len(nodes))
	}

	// 使用正确的业务流程：认领节点（pending→in_progress），然后手动干预（in_progress→manual_intervention）
	nodeID := nodes[0]["id"].(string)
	claimNode(t, client, server.URL, taskID, nodeID, agentID, agentAPIToken)

	// 通过 API 触发手动干预
	manualURL := fmt.Sprintf("%s/api/tasks/%d/nodes/%s/manual", server.URL, taskID, nodeID)
	body := map[string]interface{}{
		"comment": "needs human review",
	}
	_, status, _ := doRequestWithToken(t, client, http.MethodPost, manualURL, token, body)
	if status != http.StatusOK {
		t.Fatalf("manual intervention: expected 200, got %d", status)
	}

	// 获取通知
	notifURL := fmt.Sprintf("%s/api/workspaces/%s/notifications", server.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, notifURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list notifications: expected 200, got %d, body: %s", status, respBody)
	}

	var notifications []map[string]interface{}
	if err := json.Unmarshal(respBody, &notifications); err != nil {
		t.Fatalf("decode notifications: %v", err)
	}

	found := false
	for _, n := range notifications {
		if n["type"] == "manual_intervention" && int32(n["task_id"].(float64)) == taskID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find manual_intervention notification for task %d", taskID)
	}

	// 清理
	deleteTask(t, client, server.URL, projID, taskID, token)
}

func TestReviewerConfig(t *testing.T) {
	router, db, _ := setupExtTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)

	// 准备
	projID := createProject(t, client, server.URL, wsID, token)
	agentID, _ := createAgent(t, client, server.URL, wsID, token)

	// 添加审查者
	addReviewerURL := fmt.Sprintf("%s/api/workspaces/%s/projects/%s/reviewers", server.URL, wsID, projID)
	reviewerBody := map[string]interface{}{
		"member_type": "agent",
		"agent_id":    agentID,
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, addReviewerURL, token, reviewerBody)
	if status != http.StatusCreated {
		t.Fatalf("add reviewer: expected 201, got %d, body: %s", status, respBody)
	}

	var reviewerResult map[string]interface{}
	if err := json.Unmarshal(respBody, &reviewerResult); err != nil {
		t.Fatalf("decode reviewer: %v", err)
	}
	reviewerID := reviewerResult["id"].(string)

	// 列出审查者
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, addReviewerURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list reviewers: expected 200, got %d, body: %s", status, respBody)
	}

	var reviewers []map[string]interface{}
	if err := json.Unmarshal(respBody, &reviewers); err != nil {
		t.Fatalf("decode reviewers: %v", err)
	}
	if len(reviewers) < 1 {
		t.Errorf("expected at least 1 reviewer, got %d", len(reviewers))
	}

	// 删除审查者
	deleteReviewerURL := fmt.Sprintf("%s/api/workspaces/%s/projects/%s/reviewers/%s", server.URL, wsID, projID, reviewerID)
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, deleteReviewerURL, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete reviewer: expected 204, got %d", status)
	}

	// 验证 deletion
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, addReviewerURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list reviewers after delete: expected 200, got %d", status)
	}
	if err := json.Unmarshal(respBody, &reviewers); err != nil {
		t.Fatalf("decode reviewers: %v", err)
	}
	for _, r := range reviewers {
		if r["id"] == reviewerID {
			t.Error("reviewer should have been deleted")
		}
	}
}

func TestCommunityWorkflow(t *testing.T) {
	router, db, _ := setupExtTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)

	// 创建社区工作流
	communityURL := fmt.Sprintf("%s/api/community/workflows", server.URL)
	workflowDef := map[string]interface{}{
		"nodes": []map[string]interface{}{
			{"name": "code", "sort_order": 1},
			{"name": "review", "sort_order": 2},
		},
	}
	body := map[string]interface{}{
		"name":                 "community-flow-" + uuid.New().String()[:8],
		"description":          "A community workflow",
		"author":               "test-author",
		"version":              "1.0.0",
		"workflow_definition":  workflowDef,
		"required_skills":      []string{},
		"required_mcp_servers": []string{},
		"is_official":          false,
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, communityURL, token, body)
	if status != http.StatusCreated {
		t.Fatalf("create community workflow: expected 201, got %d, body: %s", status, respBody)
	}

	var cwResult map[string]interface{}
	if err := json.Unmarshal(respBody, &cwResult); err != nil {
		t.Fatalf("decode community workflow: %v", err)
	}
	cwID := cwResult["id"].(string)

	// 列出社区工作流
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, communityURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list community workflows: expected 200, got %d, body: %s", status, respBody)
	}

	var workflows []map[string]interface{}
	if err := json.Unmarshal(respBody, &workflows); err != nil {
		t.Fatalf("decode community workflows: %v", err)
	}

	found := false
	for _, w := range workflows {
		if w["id"] == cwID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected to find community workflow %s in list", cwID)
	}

	// 将社区工作流导入工作区
	importURL := fmt.Sprintf("%s/api/community/workflows/%s/import", server.URL, cwID)
	importBody := map[string]interface{}{
		"workspace_id": wsID,
	}
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, importURL, token, importBody)
	if status != http.StatusCreated {
		t.Fatalf("import community workflow: expected 201, got %d, body: %s", status, respBody)
	}

	var importResult map[string]interface{}
	if err := json.Unmarshal(respBody, &importResult); err != nil {
		t.Fatalf("decode import result: %v", err)
	}

	if _, ok := importResult["template"]; !ok {
		t.Error("expected template in import result")
	}
}
