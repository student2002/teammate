// api_ext_test.go covers tests for extended API endpoints.
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

func TestReviewerConfig(t *testing.T) {
	router, db, _ := setupExtTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)

	// Prepare
	projID := createProject(t, client, server.URL, wsID, token)
	agentID, _ := createAgent(t, client, server.URL, wsID, token)

	// Add reviewer
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

	// List reviewers
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

	// Delete reviewer
	deleteReviewerURL := fmt.Sprintf("%s/api/workspaces/%s/projects/%s/reviewers/%s", server.URL, wsID, projID, reviewerID)
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, deleteReviewerURL, token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete reviewer: expected 204, got %d", status)
	}

	// Verify deletion
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

	// Create community workflow
	communityURL := fmt.Sprintf("%s/api/community/workflows", server.URL)
	workflowDef := map[string]interface{}{
		"nodes": []map[string]interface{}{
			{"id": "a1000000-0000-4000-8000-000000000001", "name": "开始", "sort_order": 1, "type_key": "trigger.manual"},
			{"id": "a1000000-0000-4000-8000-000000000002", "name": "完成", "sort_order": 2, "type_key": "flow.end"},
		},
		"edges": []map[string]interface{}{
			{"source_node_id": "a1000000-0000-4000-8000-000000000001", "source_port": "success", "target_node_id": "a1000000-0000-4000-8000-000000000002", "target_port": "completed"},
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

	// List community workflows
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

	// Import community workflow into workspace
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

	// The imported template must carry the nodes parsed from workflow_definition.
	// The V2 list response wraps each template as {template, nodes, edges}.
	template := importResult["template"].(map[string]interface{})
	listURL := fmt.Sprintf("%s/api/workspaces/%s/workflows", server.URL, wsID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, listURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list workflow templates: expected 200, got %d, body: %s", status, respBody)
	}
	var tplList []map[string]interface{}
	if err := json.Unmarshal(respBody, &tplList); err != nil {
		t.Fatalf("decode workflow templates: %v", err)
	}
	imported := false
	for _, item := range tplList {
		tpl, _ := item["template"].(map[string]interface{})
		if tpl != nil && tpl["id"] == template["id"] {
			imported = true
			nodes, _ := item["nodes"].([]interface{})
			if len(nodes) != 2 {
				t.Errorf("imported template node count = %d, want 2 (parsed from workflow_definition)", len(nodes))
			}
			break
		}
	}
	if !imported {
		t.Error("imported template not found in workspace template list")
	}
}

// TestCommunityWorkflowImportRejectsNodelessDefinition verifies that importing
// a community workflow without parsable nodes fails instead of producing an
// empty template shell.
func TestCommunityWorkflowImportRejectsNodelessDefinition(t *testing.T) {
	router, db, _ := setupExtTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)

	communityURL := fmt.Sprintf("%s/api/community/workflows", server.URL)
	body := map[string]interface{}{
		"name":                "nodeless-flow-" + uuid.New().String()[:8],
		"author":              "test-author",
		"version":             "1.0.0",
		"workflow_definition": map[string]interface{}{},
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, communityURL, token, body)
	if status != http.StatusCreated {
		t.Fatalf("create community workflow: expected 201, got %d, body: %s", status, respBody)
	}
	var cwResult map[string]interface{}
	if err := json.Unmarshal(respBody, &cwResult); err != nil {
		t.Fatalf("decode community workflow: %v", err)
	}

	importURL := fmt.Sprintf("%s/api/community/workflows/%s/import", server.URL, cwResult["id"])
	importBody := map[string]interface{}{"workspace_id": wsID}
	_, status, _ = doRequestWithToken(t, client, http.MethodPost, importURL, token, importBody)
	if status != http.StatusBadRequest {
		t.Errorf("import nodeless workflow: expected 400, got %d", status)
	}
}

// TestCommunityWorkflowImportNotFound verifies that importing a non-existent
// community workflow id maps to 404 instead of an internal error.
func TestCommunityWorkflowImportNotFound(t *testing.T) {
	router, db, _ := setupExtTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)

	importURL := fmt.Sprintf("%s/api/community/workflows/%s/import", server.URL, uuid.New())
	importBody := map[string]interface{}{"workspace_id": wsID}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, importURL, token, importBody)
	if status != http.StatusNotFound {
		t.Errorf("import missing community workflow: expected 404, got %d, body: %s", status, respBody)
	}
}
