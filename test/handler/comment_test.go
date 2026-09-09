// comment_test.go tests the comment API.
package handler_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/clock"
)

// setupCommentTestRouter creates a test server using the main setupTestRouter.
func setupCommentTestRouter(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()

	router, db, _ := setupTestRouter(t)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, db
}

// setupCommentTestRouterWithClock creates a test server with a custom clock.
func setupCommentTestRouterWithClock(t *testing.T, c clock.Clock) *httptest.Server {
	t.Helper()

	router, _ := setupTestRouterWithClock(t, c)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts
}

// TestCommentCreateAndList verifies comment creation and listing functionality.
func TestCommentCreateAndList(t *testing.T) {
	ts, _ := setupCommentTestRouter(t)
	client := ts.Client()
	baseURL := ts.URL
	token, wsID := registerTestUser(t, client, baseURL)

	projID := createProject(t, client, baseURL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, baseURL, wsID, token)
	setProjectDefaultWorkflow(t, client, baseURL, wsID, projID, tplID, token)
	taskID, _ := createTask(t, client, baseURL, projID, tplID, token)
	defer deleteTask(t, client, baseURL, projID, taskID, token)

	// Create comment
	authorID := uuid.New()
	body := map[string]interface{}{
		"author_type": "human",
		"author_id":   authorID,
		"content":     "This is a test comment",
		"mentions":    []uuid.UUID{},
	}
	_, status, respBody := doRequestWithToken(t, client, "POST", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create comment: expected 201, got %d: %s", status, string(respBody))
	}

	var createResult map[string]interface{}
	json.Unmarshal(respBody, &createResult)

	commentID, ok := createResult["id"].(string)
	if !ok || commentID == "" {
		t.Fatalf("missing comment id in response: %v", createResult)
	}

	// List comments
	_, status, respBody = doRequestWithToken(t, client, "GET", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list comments: expected 200, got %d", status)
	}

	var comments []map[string]interface{}
	json.Unmarshal(respBody, &comments)

	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}

	t.Logf("Comment created and listed: id=%s", commentID)
}

// TestCommentContentLengthValidation verifies comment content length limit (exceeding 10000 characters is rejected).
func TestCommentContentLengthValidation(t *testing.T) {
	ts, _ := setupCommentTestRouter(t)
	client := ts.Client()
	baseURL := ts.URL
	token, wsID := registerTestUser(t, client, baseURL)

	projID := createProject(t, client, baseURL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, baseURL, wsID, token)
	setProjectDefaultWorkflow(t, client, baseURL, wsID, projID, tplID, token)
	taskID, _ := createTask(t, client, baseURL, projID, tplID, token)
	defer deleteTask(t, client, baseURL, projID, taskID, token)

	// Create a comment with content exceeding 10000 characters
	longContent := strings.Repeat("a", 10001)
	authorID := uuid.New()
	body := map[string]interface{}{
		"author_type": "human",
		"author_id":   authorID,
		"content":     longContent,
		"mentions":    []uuid.UUID{},
	}
	_, status, _ := doRequestWithToken(t, client, "POST", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, body)
	if status != http.StatusBadRequest {
		t.Fatalf("create comment with long content: expected 400, got %d", status)
	}

	// Create a valid comment
	body = map[string]interface{}{
		"author_type": "human",
		"author_id":   authorID,
		"content":     "Short comment",
		"mentions":    []uuid.UUID{},
	}
	_, status, respBody := doRequestWithToken(t, client, "POST", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create comment: expected 201, got %d", status)
	}

	var createResult map[string]interface{}
	json.Unmarshal(respBody, &createResult)

	t.Logf("Content length validation works correctly")
}

// TestCommentReplyThread verifies comment reply functionality (parent-child comment relationship).
func TestCommentReplyThread(t *testing.T) {
	ts, _ := setupCommentTestRouter(t)
	client := ts.Client()
	baseURL := ts.URL
	token, wsID := registerTestUser(t, client, baseURL)

	projID := createProject(t, client, baseURL, wsID, token)
	tplID := createWorkflowTemplate3Nodes(t, client, baseURL, wsID, token)
	setProjectDefaultWorkflow(t, client, baseURL, wsID, projID, tplID, token)
	taskID, _ := createTask(t, client, baseURL, projID, tplID, token)
	defer deleteTask(t, client, baseURL, projID, taskID, token)

	// Create parent comment
	humanAuthor := uuid.New()
	body := map[string]interface{}{
		"author_type": "human",
		"author_id":   humanAuthor,
		"content":     "Parent comment",
		"mentions":    []uuid.UUID{},
	}
	_, status, respBody := doRequestWithToken(t, client, "POST", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create parent comment: expected 201, got %d: %s", status, string(respBody))
	}

	var parentResult map[string]interface{}
	json.Unmarshal(respBody, &parentResult)
	parentID := parentResult["id"].(string)

	// Create reply
	agentAuthor := uuid.New()
	parentUUID, _ := uuid.Parse(parentID)
	replyBody := map[string]interface{}{
		"parent_id":   parentUUID,
		"author_type": "agent",
		"author_id":   agentAuthor,
		"content":     "Reply to parent",
		"mentions":    []uuid.UUID{},
	}
	_, status, respBody = doRequestWithToken(t, client, "POST", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, replyBody)
	if status != http.StatusCreated {
		t.Fatalf("create reply: expected 201, got %d: %s", status, string(respBody))
	}

	var replyResult map[string]interface{}
	json.Unmarshal(respBody, &replyResult)

	if replyResult["parent_id"] != parentID {
		t.Fatalf("expected parent_id=%s, got %v", parentID, replyResult["parent_id"])
	}

	// List comments — should have 2
	_, status, respBody = doRequestWithToken(t, client, "GET", fmt.Sprintf("%s/api/tasks/%d/comments", baseURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list comments: expected 200, got %d", status)
	}

	var comments []map[string]interface{}
	json.Unmarshal(respBody, &comments)

	if len(comments) != 2 {
		t.Fatalf("expected 2 comments, got %d", len(comments))
	}

	t.Logf("Comment reply thread works: parent=%s, reply has parent_id=%s", parentID, parentID)
}
