// project_defaults_test.go verifies server-owned update/defaults semantics on
// project endpoints: sparse updates never clear untouched fields, member role
// and member_type are defaulted/validated by the server, and agent bindings
// come back enabled when the payload omits the flag. Payloads mirror real
// clients — the CLI sends only the flags it was given.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestProjectUpdateSparseKeepsFields(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	projID := createProject(t, client, ts.URL, wsID, token)
	updateURL := fmt.Sprintf("%s/api/workspaces/%s/projects/%s", ts.URL, wsID, projID)

	// CLI `project update --description x` sends ONLY the description field.
	_, status, body := doRequestWithToken(t, client, http.MethodPut, updateURL, token, map[string]interface{}{
		"description": "sparse-update-desc",
	})
	if status != http.StatusOK {
		t.Fatalf("sparse update: expected 200, got %d, body: %s", status, body)
	}
	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode updated project: %v", err)
	}
	if updated["description"] != "sparse-update-desc" {
		t.Fatalf("description = %v, want sparse-update-desc", updated["description"])
	}
	if updated["repo_url"] != "https://github.com/test/repo.git" {
		t.Fatalf("sparse update cleared repo_url: %v", updated["repo_url"])
	}
	if updated["status"] != "active" {
		t.Fatalf("sparse update changed status: %v", updated["status"])
	}
	if name, _ := updated["name"].(string); name == "" {
		t.Fatal("sparse update cleared name")
	}

	// explicit empty name / repo_url and invalid status are rejected
	for _, payload := range []map[string]interface{}{
		{"name": ""},
		{"repo_url": ""},
		{"status": "bogus"},
	} {
		_, status, body = doRequestWithToken(t, client, http.MethodPut, updateURL, token, payload)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid update %v: expected 400, got %d, body: %s", payload, status, body)
		}
	}
}

func TestProjectMemberRoleAndTypeServerOwned(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	projID := createProject(t, client, ts.URL, wsID, token)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)
	memberURL := fmt.Sprintf("%s/api/workspaces/%s/projects/%s/members", ts.URL, wsID, projID)

	// CLI bind without --role: the server must apply the column default
	_, status, body := doRequestWithToken(t, client, http.MethodPost, memberURL, token, map[string]interface{}{
		"member_type": "agent",
		"agent_id":    agentID,
	})
	if status != http.StatusCreated {
		t.Fatalf("add member without role: expected 201, got %d, body: %s", status, body)
	}
	var member map[string]interface{}
	if err := json.Unmarshal(body, &member); err != nil {
		t.Fatalf("decode member: %v", err)
	}
	if member["role"] != "developer" {
		t.Fatalf("default role = %v, want developer", member["role"])
	}

	// invalid role and invalid/missing member_type are rejected up front
	for _, payload := range []map[string]interface{}{
		{"member_type": "agent", "agent_id": agentID, "role": "boss"},
		{"agent_id": agentID},
		{"member_type": "robot"},
	} {
		_, status, body = doRequestWithToken(t, client, http.MethodPost, memberURL, token, payload)
		if status != http.StatusBadRequest {
			t.Fatalf("invalid member payload %v: expected 400, got %d, body: %s", payload, status, body)
		}
	}
}

func TestAgentBindingEnabledServerOwned(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	agentID, _ := createAgent(t, client, ts.URL, wsID, token)

	// create a skill, then bind with the CLI payload (skill_id only)
	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/skills", ts.URL, wsID), token, map[string]interface{}{
			"name": "bind-default-skill",
		})
	if status != http.StatusCreated {
		t.Fatalf("create skill: expected 201, got %d, body: %s", status, body)
	}
	var skill map[string]interface{}
	if err := json.Unmarshal(body, &skill); err != nil {
		t.Fatalf("decode skill: %v", err)
	}

	bindURL := fmt.Sprintf("%s/api/workspaces/%s/agents/%s/skills", ts.URL, wsID, agentID)
	_, status, body = doRequestWithToken(t, client, http.MethodPost, bindURL, token, map[string]interface{}{
		"skill_id": skill["id"],
	})
	if status != http.StatusCreated {
		t.Fatalf("bind skill without enabled: expected 201, got %d, body: %s", status, body)
	}
	var binding map[string]interface{}
	if err := json.Unmarshal(body, &binding); err != nil {
		t.Fatalf("decode binding: %v", err)
	}
	if binding["enabled"] != true {
		t.Fatalf("binding enabled = %v, want true (bind action implies enable)", binding["enabled"])
	}

	// explicit false still binds disabled (upsert toggles)
	_, status, body = doRequestWithToken(t, client, http.MethodPost, bindURL, token, map[string]interface{}{
		"skill_id": skill["id"],
		"enabled":  false,
	})
	if status != http.StatusCreated {
		t.Fatalf("rebind with enabled=false: expected 201, got %d, body: %s", status, body)
	}
	binding = nil
	if err := json.Unmarshal(body, &binding); err != nil {
		t.Fatalf("decode rebound: %v", err)
	}
	if binding["enabled"] != false {
		t.Fatalf("rebound enabled = %v, want false", binding["enabled"])
	}
}
