// memory_test.go 覆盖记忆接口的测试。
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

// TestCreateMemory 验证通过 API 创建记忆的基本流程。
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

// TestListMemoriesByWorkspace 验证按工作区列出所有记忆。
func TestListMemoriesByWorkspace(t *testing.T) {
	ts, _ := setupMemoryRouter(t)
	defer ts.Close()

	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	// 创建两条记忆
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "insight", "Insight 1", "Content 1", []string{})
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "convention", "Convention 1", "Content 2", []string{})

	// 列出工作区的所有记忆
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

// TestDeleteMemory 验证删除记忆操作及删除后验证。
func TestDeleteMemory(t *testing.T) {
	ts, _ := setupMemoryRouter(t)
	defer ts.Close()

	client := ts.Client()
	token, wsID := registerTestUser(t, client, ts.URL)

	mem := createMemoryViaAPI(t, client, ts.URL, token, wsID, "insight", "To Delete", "Will be deleted", []string{})
	memID := mem["id"].(string)

	// 删除
	_, status, _ := doRequestWithToken(t, client, http.MethodDelete, fmt.Sprintf("%s/api/memories/%s", ts.URL, memID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", status)
	}

	// 通过列出验证它已被删除
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

	// 创建标题不同的记忆
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "architecture", "Redis Cache Strategy", "Use Redis for caching with TTL", []string{"redis", "cache"})
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "decision", "Database Choice", "We chose PostgreSQL over MySQL", []string{"database"})
	createMemoryViaAPI(t, client, ts.URL, token, wsID, "convention", "Code Style Guide", "Use tabs not spaces", []string{"style"})

	// 搜索 "Redis"
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

	// 搜索 "PostgreSQL"（在内容中）
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

	// 搜索匹配多个条目的内容
	url = fmt.Sprintf("%s/api/memories/search?q=use&workspace_id=%s", ts.URL, wsID)
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", status, respBody)
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode search results: %v", err)
	}

	// "use" 出现在 "Use Redis for caching" 和 "Use tabs not spaces" 中
	if len(result) < 2 {
		t.Errorf("expected at least 2 results for 'use', got %d", len(result))
	}
}
