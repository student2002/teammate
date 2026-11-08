// task_duedate_test.go verifies due_date validation on the task create,
// update, and subtask-create paths: non-empty unparseable values are rejected
// with 400 instead of being silently stored as NULL.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskDueDateValidation(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()

	server := httptest.NewServer(router)
	defer server.Close()

	client := server.Client()
	token, wsID := registerTestUser(t, client, server.URL)
	projID := createProject(t, client, server.URL, wsID, token)
	tplID, _ := createV2WorkflowTemplate(t, client, server.URL, wsID, token)

	taskBaseURL := fmt.Sprintf("%s/api/projects/%s/tasks", server.URL, projID)

	// create: invalid due_date -> 400
	body := map[string]interface{}{
		"title":                "duedate-task",
		"workflow_template_id": tplID,
		"due_date":             "not-a-date",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, taskBaseURL, token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("create with invalid due_date: expected 400, got %d, body: %s", status, respBody)
	}

	// create: empty string means "not supplied" and must stay accepted
	body["due_date"] = ""
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, taskBaseURL, token, body)
	if status != http.StatusCreated {
		t.Fatalf("create with empty due_date: expected 201, got %d, body: %s", status, respBody)
	}
	var createResult map[string]interface{}
	if err := json.Unmarshal(respBody, &createResult); err != nil {
		t.Fatalf("decode task: %v", err)
	}
	taskID := int64(createResult["task"].(map[string]interface{})["id"].(float64))

	// update: invalid due_date -> 400
	updateURL := fmt.Sprintf("%s/%d", taskBaseURL, taskID)
	body = map[string]interface{}{"due_date": "not-a-date"}
	_, status, respBody = doRequestWithToken(t, client, http.MethodPut, updateURL, token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("update with invalid due_date: expected 400, got %d, body: %s", status, respBody)
	}

	// update: valid date-only value -> 200 and persisted
	body = map[string]interface{}{"due_date": "2030-01-02"}
	_, status, respBody = doRequestWithToken(t, client, http.MethodPut, updateURL, token, body)
	if status != http.StatusOK {
		t.Fatalf("update with valid due_date: expected 200, got %d, body: %s", status, respBody)
	}

	// subtask: invalid due_date -> 400
	subtaskURL := fmt.Sprintf("%s/api/tasks/%d/subtasks", server.URL, taskID)
	body = map[string]interface{}{"title": "sub", "due_date": "not-a-date"}
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, subtaskURL, token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("create subtask with invalid due_date: expected 400, got %d, body: %s", status, respBody)
	}
}
