// v4_features_test.go 覆盖 v4 新特性的测试。
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teammate/server/internal/clock"
)

// TestProjectList_AgentSeesOnlyMemberProjects 验证代理调用项目列表 API 时只能看到其所属的项目，而非工作区中的所有项目。
func TestProjectList_AgentSeesOnlyMemberProjects(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	// 创建用户和工作区
	token, wsID := registerTestUser(t, client, srv.URL)

	// 创建两个项目
	proj1ID := createProject(t, client, srv.URL, wsID, token)
	proj2ID := createProject(t, client, srv.URL, wsID, token)

	// 创建代理
	agentID, apiToken := createAgent(t, client, srv.URL, wsID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// Agent 列出项目——初始应看不到任何项目（不是任何项目的成员）
	url := fmt.Sprintf("%s/api/workspaces/%s/projects", srv.URL, wsID)
	_, status, respBody := doRequestWithAPIKey(t, client, http.MethodGet, url, apiToken, nil)
	if status != http.StatusOK {
		t.Fatalf("agent list projects: expected 200, got %d, body: %s", status, respBody)
	}

	var projects []map[string]interface{}
	if err := json.Unmarshal(respBody, &projects); err != nil {
		t.Fatalf("unmarshal projects: %v", err)
	}
	if len(projects) != 0 {
		t.Errorf("agent should see 0 projects (not a member of any), got %d", len(projects))
	}

	// 仅将 Agent 添加为 proj1 的成员（直接通过数据库）
	addAgentToProject(t, q, proj1ID, agentID)

	// Agent 再次列出项目——应只看到 proj1
	_, status, respBody = doRequestWithAPIKey(t, client, http.MethodGet, url, apiToken, nil)
	if status != http.StatusOK {
		t.Fatalf("agent list projects after membership: expected 200, got %d, body: %s", status, respBody)
	}

	if err := json.Unmarshal(respBody, &projects); err != nil {
		t.Fatalf("unmarshal projects: %v", err)
	}
	if len(projects) != 1 {
		t.Errorf("agent should see 1 project (member of proj1 only), got %d", len(projects))
	}
	if len(projects) > 0 && projects[0]["id"] != proj1ID {
		t.Errorf("agent should see proj1=%s, got %v", proj1ID, projects[0]["id"])
	}

	// 验证 proj2 ID 不在列表中
	for _, p := range projects {
		if p["id"] == proj2ID {
			t.Error("agent should NOT see proj2 (not a member)")
		}
	}

	// 人工用户列出项目——应至少看到我们创建的两个项目
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("human list projects: expected 200, got %d, body: %s", status, respBody)
	}

	if err := json.Unmarshal(respBody, &projects); err != nil {
		t.Fatalf("unmarshal projects: %v", err)
	}

	// 验证创建的两个项目都在列表中
	foundProj1, foundProj2 := false, false
	for _, p := range projects {
		if p["id"] == proj1ID {
			foundProj1 = true
		}
		if p["id"] == proj2ID {
			foundProj2 = true
		}
	}
	if !foundProj1 || !foundProj2 {
		t.Errorf("human should see both proj1 and proj2, found proj1=%v proj2=%v", foundProj1, foundProj2)
	}
}

// TestReservationWindow_ClaimWithinWindow 验证代理可以在预留窗口内重新认领节点（reservation_expires_at 尚未过期）。测试 V4 延续窗口功能。
func TestReservationWindow_ClaimWithinWindow(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)
	projID := createProject(t, client, srv.URL, wsID, token)
	tplID := createWorkflowTemplate2Nodes(t, client, srv.URL, wsID, token)
	setProjectDefaultWorkflow(t, client, srv.URL, wsID, projID, tplID, token)

	agentID, apiToken := createAgent(t, client, srv.URL, wsID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)
	addAgentToProject(t, q, projID, agentID)

	// 创建任务
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 node")
	}
	nodeID := nodes[0]["id"].(string)

	// Agent 认领第一个节点
	claimed := claimNode(t, client, srv.URL, taskID, nodeID, agentID, apiToken)
	if claimed["status"] != "in_progress" {
		t.Fatalf("expected in_progress after claim, got %v", claimed["status"])
	}

	// 同一 Agent 重新认领（在保留窗口内）——应成功
	// 这模拟了 Agent 守护进程重新连接并重新认领
	reclaimed := claimNode(t, client, srv.URL, taskID, nodeID, agentID, apiToken)
	if reclaimed["status"] != "in_progress" {
		t.Fatalf("expected in_progress after re-claim within window, got %v", reclaimed["status"])
	}
}

// TestPermissionChangedEvent_GrantAndList 验证向代理授予权限后代理可以访问工作区资源。
func TestPermissionChangedEvent_GrantAndList(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)
	agentID, apiToken := createAgent(t, client, srv.URL, wsID, token)

	// 授予权限——应成功
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:execute", token)

	// 验证 Agent 可以使用该 Token 列出项目
	url := fmt.Sprintf("%s/api/workspaces/%s/projects", srv.URL, wsID)
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodGet, url, apiToken, nil)
	if status != http.StatusOK {
		t.Errorf("agent with task:execute should be able to list projects, got %d", status)
	}
}

// newFakeClock 返回始终返回固定时间的 clock.Clock。
func newFakeClock() clock.Clock {
	return clock.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
}
