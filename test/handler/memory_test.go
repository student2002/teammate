// memory_test.go tests the memory API.
package handler_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func setupMemoryRouter(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()

	router, db, _ := setupTestRouter(t)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, db
}

func createMemoryViaAPI(t *testing.T, client *http.Client, baseURL, token, workspaceID, memType, title, content string, tags []string) map[string]interface{} {
	t.Helper()

	body := map[string]interface{}{
		"workspace_id": workspaceID,
		"type":         memType,
		"title":        title,
		"content":      content,
		"tags":         tags,
		"confidence":   0.8,
		"verified":     false,
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, baseURL+"/api/memories", token, body)
	if status != http.StatusCreated {
		t.Fatalf("createMemoryViaAPI: expected 201, got %d, body: %s", status, respBody)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode memory: %v", err)
	}
	return result
}

// TestCreateMemory verifies the basic flow of creating a memory via the API.
func TestCreateMemory(t *testing.T) {
	ts, _ := setupMemoryRouter(t)
	defer ts.Close()

	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	mem := createMemoryViaAPI(t, client, ts.URL, token, wsID, "decision", "Test Decision", "We decided to use PostgreSQL", []string{"database", "decision"})

	if mem["title"] != "Test Decision" {
		t.Errorf("expected title 'Test Decision', got %v", mem["title"])
	}
	if mem["type"] != "decision" {
		t.Errorf("expected type 'decision', got %v", mem["type"])
	}
	if mem["id"] == nil {
		t.Error("expected non-nil id")
	}
}

// TestListMemoriesByWorkspace verifies listing all memories by workspace.
func TestListMemoriesByWorkspace(t *testing.T) {
	ts, _ := setupMemoryRouter(t)
	defer ts.Close()

	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	// Create two memories
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "insight", "Insight 1", "Content 1", []string{})
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "convention", "Convention 1", "Content 2", []string{})

	// List all memories in the workspace
	url := fmt.Sprintf("%s/api/memories?workspace_id=%s", ts.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode memories: %v", err)
	}

	if len(result) != 2 {
		t.Errorf("expected 2 memories, got %d", len(result))
	}
}

// TestDeleteMemory verifies the delete memory operation and post-deletion verification.
func TestDeleteMemory(t *testing.T) {
	ts, _ := setupMemoryRouter(t)
	defer ts.Close()

	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	mem := createMemoryViaAPI(t, client, ts.URL, token, wsID, "insight", "To Delete", "Will be deleted", []string{})
	memID := mem["id"].(string)

	// Delete
	_, status, _ := doRequestWithToken(t, client, http.MethodDelete, fmt.Sprintf("%s/api/memories/%s", ts.URL, memID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// Verify it has been deleted by listing
	url := fmt.Sprintf("%s/api/memories?workspace_id=%s", ts.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d", status)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode memories: %v", err)
	}

	for _, m := range result {
		if m["id"] == memID {
			t.Error("memory should have been deleted")
		}
	}
}

func TestSearchMemoriesText(t *testing.T) {
	ts, _ := setupMemoryRouter(t)
	defer ts.Close()

	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	// Create memories with different titles
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "architecture", "Redis Cache Strategy", "Use Redis for caching with TTL", []string{"redis", "cache"})
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "decision", "Database Choice", "We chose PostgreSQL over MySQL", []string{"database"})
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "convention", "Code Style Guide", "Use tabs not spaces", []string{"style"})

	// Search for "Redis"
	url := fmt.Sprintf("%s/api/memories/search?q=Redis&workspace_id=%s", ts.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode search results: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 result for 'Redis', got %d", len(result))
	}

	if len(result) > 0 && result[0]["title"] != "Redis Cache Strategy" {
		t.Errorf("expected 'Redis Cache Strategy', got %v", result[0]["title"])
	}

	// Search for "PostgreSQL" (in content)
	url = fmt.Sprintf("%s/api/memories/search?q=PostgreSQL&workspace_id=%s", ts.URL, wsID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode search results: %v", err)
	}

	if len(result) != 1 {
		t.Errorf("expected 1 result for 'PostgreSQL', got %d", len(result))
	}

	// Search content matching multiple entries
	url = fmt.Sprintf("%s/api/memories/search?q=use&workspace_id=%s", ts.URL, wsID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode search results: %v", err)
	}

	// "use" appears in "Use Redis for caching" and "Use tabs not spaces"
	if len(result) < 2 {
		t.Errorf("expected at least 2 results for 'use', got %d", len(result))
	}
}
