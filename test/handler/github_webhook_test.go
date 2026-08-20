// github_webhook_test.go 覆盖 GitHub Webhook 端点的测试。
package handler_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// strPtr 返回 s 的指针，用于构造领域参数中的 *string 字段。
func strPtr(s string) *string { return &s }

func TestGitHubWebhookSignatureAndIssueCreation(t *testing.T) {
	router, pgDB, _ := setupTestRouter(t)
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()
	ws, project, _ := createGitHubWebhookTemplate(t, svc, "webhook-secret")

	t.Cleanup(func() {
		_ = testdb.DeleteWorkspace(pgDB, ws.ID)
	})

	payload := githubIssuePayload("opened", "acme", "rocket", 42, "修复登录失败")

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issues")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing signature status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-Hub-Signature-256", signGitHubPayload(payload, "webhook-secret"))
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("valid signature status = %d, want %d body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}

	tasks, err := svc.Store.ListAllTasks(ctx, uuid.MustParse(project.ID))
	if err != nil {
		t.Fatalf("ListAllTasks: %v", err)
	}
	var matching int
	for _, task := range tasks {
		if task.Title == "修复登录失败" {
			matching++
		}
	}
	if matching != 1 {
		t.Fatalf("expected one task created by webhook, got %d", matching)
	}
}

func createGitHubWebhookTemplate(t *testing.T, svc *service.Service, secret string) (types.Workspace, types.Project, types.WorkflowTemplate) {
	t.Helper()
	ctx := context.Background()
	ws, err := svc.Store.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "webhook-ws-" + uuid.New().String()[:8],
		Description: strPtr("webhook ws"),
		IssuePrefix: "GHW",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	project, err := svc.Store.CreateProject(ctx, types.CreateProjectParams{
		WorkspaceID: ws.ID,
		Name:        "webhook-project-" + uuid.New().String()[:8],
		Description: strPtr("webhook project"),
		Status:      types.ProjectStatusActive,
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	config := json.RawMessage(fmt.Sprintf(`{"project_id":"%s","repo_owner":"acme","repo_name":"rocket","secret":"%s"}`, project.ID, secret))
	tpl, _, err := svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID:    ws.ID,
		Name:           "webhook-flow-" + uuid.New().String()[:8],
		Description:    strPtr("webhook flow"),
		TriggerType:    "github_issue",
		TriggerConfig:  config,
		TriggerEnabled: true,
	}, []types.CreateTemplateNodeParams{
		{Name: "处理 Issue", SortOrder: 1, NodeType: types.NodeTypeStandard, AssigneeType: types.AssigneeTypeAnyAgent, TimeoutMinutes: 60, MaxRejectCycles: 3},
	})
	if err != nil {
		t.Fatalf("create workflow template: %v", err)
	}
	return ws, project, tpl
}

func githubIssuePayload(action, owner, repo string, number int, title string) []byte {
	body := map[string]interface{}{
		"action": action,
		"repository": map[string]interface{}{
			"name": repo,
			"owner": map[string]interface{}{
				"login": owner,
			},
			"full_name": owner + "/" + repo,
		},
		"issue": map[string]interface{}{
			"number":   number,
			"title":    title,
			"body":     "用户无法登录",
			"html_url": fmt.Sprintf("https://github.com/%s/%s/issues/%d", owner, repo, number),
			"user": map[string]interface{}{
				"login": "octocat",
			},
			"labels": []map[string]interface{}{
				{"name": "bug"},
			},
		},
	}
	payload, _ := json.Marshal(body)
	return payload
}

func signGitHubPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
