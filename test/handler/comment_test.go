// comment_test.go 覆盖评论接口的测试。
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

// setupCommentTestRouter 使用主 setupTestRouter 创建测试服务器。
func setupCommentTestRouter(t *testing.T) (*httptest.Server, *sql.DB) {
	t.Helper()

	router, db, _ := setupTestRouter(t)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, db
}

// setupCommentTestRouterWithClock 创建使用自定义时钟的测试服务器。
func setupCommentTestRouterWithClock(t *testing.T, c clock.Clock) *httptest.Server {
	t.Helper()

	router, _ := setupTestRouterWithClock(t, c)

	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts
}

// TestCommentCreateAndList 验证评论的创建和列表查询功能。
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

	// 创建评论
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

	// 列出评论
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

// TestCommentContentLengthValidation 验证评论内容长度限制（超过 10000 字符被拒绝）。
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

	// 创建内容超过 10000 个字符的评论
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

	// 创建一条有效评论
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

// TestCommentReplyThread 验证评论的回复功能（父子评论关系）。
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

	// 创建父评论
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

	// 创建回复
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

	// 列出评论——应有 2 条
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
