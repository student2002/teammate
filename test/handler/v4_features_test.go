// v4_features_test.go covers tests for v4 new features.
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

// TestProjectList_AgentSeesOnlyMemberProjects verifies that an agent calling the project list API can only see projects it belongs to, not all projects in the workspace.
func TestProjectList_AgentSeesOnlyMemberProjects(t *testing.T) {
	router, db, q := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	// Create user and workspace
	token, wsID := registerTestUser(t, client, srv.URL)

	// Create two projects
	proj1ID := createProject(t, client, srv.URL, wsID, token)
	proj2ID := createProject(t, client, srv.URL, wsID, token)

	// Create agent
	agentID, apiToken := createAgent(t, client, srv.URL, wsID, token)
	grantAgentAllTaskPermissions(t, client, srv.URL, wsID, agentID, token)

	// Agent lists projects - should initially see no projects (not a member of any)
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

	// Add Agent only as a member of proj1 (directly via database)
	addAgentToProject(t, q, proj1ID, agentID)

	// Agent lists projects again - should only see proj1
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

	// Verify proj2 ID is not in the list
	for _, p := range projects {
		if p["id"] == proj2ID {
			t.Error("agent should NOT see proj2 (not a member)")
		}
	}

	// Human user lists projects - should see at least the two projects we created
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("human list projects: expected 200, got %d, body: %s", status, respBody)
	}

	if err := json.Unmarshal(respBody, &projects); err != nil {
		t.Fatalf("unmarshal projects: %v", err)
	}

	// Verify both created projects are in the list
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

// TestReservationWindow_ClaimWithinWindow verifies that an agent can re-claim a node within the reservation window (reservation_expires_at has not expired). Tests the V4 continuation window feature.
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

	// Create task
	taskID, nodes := createTask(t, client, srv.URL, projID, tplID, token)
	if len(nodes) == 0 {
		t.Fatal("expected at least 1 node")
	}
	nodeID := nodes[0]["id"].(string)

	// Agent claims the first node
	claimed := claimNode(t, client, srv.URL, taskID, nodeID, agentID, apiToken)
	if claimed["status"] != "in_progress" {
		t.Fatalf("expected in_progress after claim, got %v", claimed["status"])
	}

	// Same Agent re-claims (within reservation window) - should succeed
	// This simulates an agent daemon reconnecting and re-claiming
	reclaimed := claimNode(t, client, srv.URL, taskID, nodeID, agentID, apiToken)
	if reclaimed["status"] != "in_progress" {
		t.Fatalf("expected in_progress after re-claim within window, got %v", reclaimed["status"])
	}
}

// TestPermissionChangedEvent_GrantAndList verifies that after granting permissions to an agent, it can access workspace resources.
func TestPermissionChangedEvent_GrantAndList(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()

	token, wsID := registerTestUser(t, client, srv.URL)
	agentID, apiToken := createAgent(t, client, srv.URL, wsID, token)

	// Grant permission - should succeed
	grantAgentPermission(t, client, srv.URL, wsID, agentID, "task:execute", token)

	// Verify Agent can list projects using the token
	url := fmt.Sprintf("%s/api/workspaces/%s/projects", srv.URL, wsID)
	_, status, _ := doRequestWithAPIKey(t, client, http.MethodGet, url, apiToken, nil)
	if status != http.StatusOK {
		t.Errorf("agent with task:execute should be able to list projects, got %d", status)
	}
}

// newFakeClock returns a clock.Clock that always returns a fixed time.
func newFakeClock() clock.Clock {
	return clock.NewFakeClock(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC))
}
