// integration_test.go covers the S5 GitHub integration HTTP surface:
// integration CRUD with secret masking and encrypted-at-rest columns, the
// connection test against a fake GitHub, the referenced-delete guard, and the
// public webhook entry (HMAC, idempotent deliveries, body limit, payload
// normalization).
package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/integration/github"
	"github.com/teammate/server/internal/server"
	"github.com/teammate/server/internal/server/ws"
	"github.com/teammate/server/test/testdb"
)

// setupIntegrationServer boots a router whose GitHub API URL points at the
// fake GitHub so POST .../test exercises the client end to end.
func setupIntegrationServer(t *testing.T, githubAPIURL string) (*httptest.Server, *http.Client, string, string) {
	t.Helper()
	db, err := sql.Open("pgx", getTestDSN())
	if err != nil {
		t.Fatalf("connect db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	router := server.NewRouter(server.RouterDeps{
		Config: server.Config{
			JWTSecret:      testJWTSecret,
			AllowedOrigins: "*",
			GitHubAPIURL:   githubAPIURL,
		},
		DB:      db,
		Redis:   nil,
		Hub:     ws.NewHub(nil),
		Gateway: ws.NewGateway(nil),
	})
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)
	return srv, client, token, wsID
}

// integrationURL builds the integration collection URL of one project.
func integrationURL(baseURL, wsID, projID string) string {
	return fmt.Sprintf("%s/api/workspaces/%s/projects/%s/integrations/github", baseURL, wsID, projID)
}

// integrationItemURL builds one integration's item URL.
func integrationItemURL(baseURL, wsID, projID, id string) string {
	return fmt.Sprintf("%s/%s", integrationURL(baseURL, wsID, projID), id)
}

// createIntegration posts an integration and returns its id.
func createIntegration(t *testing.T, client *http.Client, baseURL, wsID, projID, token string) string {
	t.Helper()
	body := map[string]interface{}{
		"name":           "主仓库",
		"repo_owner":     "owner",
		"repo_name":      "repo",
		"pat":            "github_pat_test0000abcd1234",
		"webhook_secret": "hook-secret-" + uuid.NewString()[:8],
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, integrationURL(baseURL, wsID, projID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create integration: expected 201, got %d, body: %s", status, respBody)
	}
	var view map[string]interface{}
	if err := json.Unmarshal(respBody, &view); err != nil {
		t.Fatalf("decode integration: %v", err)
	}
	id, _ := view["id"].(string)
	if id == "" {
		t.Fatalf("missing id: %s", respBody)
	}
	return id
}

// TestIntegrationCRUDSecretsMasked walks create → list → update → delete and
// asserts plaintext secrets never appear in any response while the encrypted
// columns are decryptable in the database.
func TestIntegrationCRUDSecretsMasked(t *testing.T) {
	pat := "github_pat_test0000abcd1234"
	secret := "hook-secret-plain-value"
	srv, client, token, wsID := setupIntegrationServer(t, "http://127.0.0.1:1")
	projID := createProject(t, client, srv.URL, wsID, token)

	body := map[string]interface{}{
		"name":           "主仓库",
		"repo_owner":     "owner",
		"repo_name":      "repo",
		"pat":            pat,
		"webhook_secret": secret,
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, integrationURL(srv.URL, wsID, projID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d, body: %s", status, respBody)
	}
	if strings.Contains(string(respBody), pat) || strings.Contains(string(respBody), secret) {
		t.Fatalf("response must not carry plaintext secrets: %s", respBody)
	}
	var view map[string]interface{}
	if err := json.Unmarshal(respBody, &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	id, _ := view["id"].(string)
	if masked, _ := view["pat_masked"].(string); masked == "" || strings.Contains(masked, pat) {
		t.Fatalf("pat_masked wrong: %q", masked)
	}
	if set, _ := view["webhook_secret_set"].(bool); !set {
		t.Fatalf("webhook_secret_set must be true")
	}
	if owner, _ := view["repo_owner"].(string); owner != "owner" {
		t.Fatalf("repo_owner = %v", view["repo_owner"])
	}

	// List is masked too.
	_, status, respBody = doRequestWithToken(t, client, http.MethodGet, integrationURL(srv.URL, wsID, projID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", status)
	}
	if strings.Contains(string(respBody), pat) || strings.Contains(string(respBody), secret) {
		t.Fatalf("list leaks secrets: %s", respBody)
	}

	// The stored columns are decryptable and not plaintext.
	db, err := sql.Open("pgx", testdb.GetTestDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var encryptedSecret, encryptedHook string
	if err := db.QueryRow(`SELECT encrypted_secret, encrypted_webhook_secret FROM integrations WHERE id = $1`, id).Scan(&encryptedSecret, &encryptedHook); err != nil {
		t.Fatalf("query integration: %v", err)
	}
	if encryptedSecret == pat || encryptedHook == secret {
		t.Fatal("database columns must not store plaintext")
	}
	decrypted, err := crypto.DecryptString(encryptedSecret)
	if err != nil || decrypted != pat {
		t.Fatalf("decrypt stored PAT: got %q, err %v", decrypted, err)
	}
	hookDecrypted, err := crypto.DecryptString(encryptedHook)
	if err != nil || hookDecrypted != secret {
		t.Fatalf("decrypt stored webhook secret: got %q, err %v", hookDecrypted, err)
	}

	// Update replaces the mutable fields; new secrets encrypt too.
	newBody := map[string]interface{}{
		"name":           "换仓",
		"repo_owner":     "owner2",
		"repo_name":      "repo2",
		"pat":            "github_pat_renewed9999zzzz",
		"webhook_secret": "hook-secret-updated",
	}
	_, status, respBody = doRequestWithToken(t, client, http.MethodPut, integrationItemURL(srv.URL, wsID, projID, id), token, newBody)
	if status != http.StatusOK {
		t.Fatalf("update: expected 200, got %d, body: %s", status, respBody)
	}
	if strings.Contains(string(respBody), "github_pat_renewed9999zzzz") || strings.Contains(string(respBody), "hook-secret-updated") {
		t.Fatalf("update leaks secrets: %s", respBody)
	}
	var updated map[string]interface{}
	_ = json.Unmarshal(respBody, &updated)
	if owner, _ := updated["repo_owner"].(string); owner != "owner2" {
		t.Fatalf("updated repo_owner = %v", updated["repo_owner"])
	}

	// Delete succeeds with no referencing templates.
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, integrationItemURL(srv.URL, wsID, projID, id), token, nil)
	if status != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d", status)
	}
}

// TestIntegrationTestConnection verifies POST .../test against a fake GitHub:
// 200 sets status=connected, 404 sets status=error and returns a detail that
// never contains the PAT.
func TestIntegrationTestConnection(t *testing.T) {
	var hits int
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("Authorization") != "Bearer github_pat_live0000abcd" {
			t.Errorf("fake github got wrong PAT header: %q", r.Header.Get("Authorization"))
		}
		if hits == 1 {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/repo","default_branch":"main"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer fake.Close()

	srv, client, token, wsID := setupIntegrationServer(t, fake.URL)
	projID := createProject(t, client, srv.URL, wsID, token)
	// The fake asserts the exact PAT of this integration (not the helper's).
	createBody := map[string]interface{}{
		"name":           "连接测试",
		"repo_owner":     "owner",
		"repo_name":      "repo",
		"pat":            "github_pat_live0000abcd",
		"webhook_secret": "hook-secret-live",
	}
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, integrationURL(srv.URL, wsID, projID), token, createBody)
	if status != http.StatusCreated {
		t.Fatalf("create integration: expected 201, got %d, body: %s", status, respBody)
	}
	var created map[string]interface{}
	_ = json.Unmarshal(respBody, &created)
	id, _ := created["id"].(string)
	testURL := integrationItemURL(srv.URL, wsID, projID, id) + "/test"

	// Success → connected.
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, testURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("test ok: expected 200, got %d, body: %s", status, respBody)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(respBody, &result)
	if result["status"] != "connected" {
		t.Fatalf("status = %v, want connected (body %s)", result["status"], respBody)
	}

	// Failure → error with a PAT-free detail.
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost, testURL, token, nil)
	if status != http.StatusOK {
		t.Fatalf("test fail: expected 200, got %d", status)
	}
	_ = json.Unmarshal(respBody, &result)
	if result["status"] != "error" {
		t.Fatalf("status = %v, want error", result["status"])
	}
	detail, _ := result["error"].(string)
	if detail == "" || strings.Contains(detail, "github_pat_live0000abcd") {
		t.Fatalf("error detail must be present and PAT-free: %q", detail)
	}
}

// TestIntegrationDeleteReferencedConflicts verifies the delete guard: an
// enabled template whose trigger.github node references the integration
// blocks deletion with 409; after disabling the template deletion succeeds.
func TestIntegrationDeleteReferencedConflicts(t *testing.T) {
	srv, client, token, wsID := setupIntegrationServer(t, "http://127.0.0.1:1")
	projID := createProject(t, client, srv.URL, wsID, token)
	integrationID := createIntegration(t, client, srv.URL, wsID, projID, token)

	triggerID := uuid.NewString()
	endID := uuid.NewString()
	graph := map[string]interface{}{
		"name": "引用集成", "description": "", "enabled": true,
		"nodes": []map[string]interface{}{
			{"id": triggerID, "type_key": "trigger.github", "name": "触发", "sort_order": 0,
				"config": map[string]interface{}{"integration_id": integrationID, "event": "issues", "actions": []string{"opened"}}},
			{"id": endID, "type_key": "flow.end", "name": "结束", "sort_order": 1, "config": map[string]interface{}{}},
		},
		"edges": []map[string]interface{}{
			{"source_node_id": triggerID, "source_port": "success", "target_node_id": endID, "target_port": "completed"},
		},
	}

	_, status, respBody := doRequestWithToken(t, client, http.MethodPost, workflowsURL(srv.URL, wsID), token, graph)
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body: %s", status, respBody)
	}
	var tpl struct {
		Template map[string]interface{} `json:"template"`
	}
	_ = json.Unmarshal(respBody, &tpl)
	templateID, _ := tpl.Template["id"].(string)

	// Delete while referenced → 409.
	_, status, respBody = doRequestWithToken(t, client, http.MethodDelete, integrationItemURL(srv.URL, wsID, projID, integrationID), token, nil)
	if status != http.StatusConflict {
		t.Fatalf("delete referenced: expected 409, got %d, body: %s", status, respBody)
	}

	// Disable the template → delete succeeds.
	putBody := graph
	putBody["enabled"] = false
	_, status, respBody = doRequestWithToken(t, client, http.MethodPut, fmt.Sprintf("%s/%s", workflowsURL(srv.URL, wsID), templateID), token, putBody)
	if status != http.StatusOK {
		t.Fatalf("disable template: expected 200, got %d, body: %s", status, respBody)
	}
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete, integrationItemURL(srv.URL, wsID, projID, integrationID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("delete after disable: expected 200, got %d", status)
	}
}

// TestWebhookSignatureAndIdempotency covers the webhook entry: correct HMAC
// stores one pending event, replaying the same delivery stays a single row,
// wrong/missing signatures are rejected, unknown integrations 404.
func TestWebhookSignatureAndIdempotency(t *testing.T) {
	srv, client, token, wsID := setupIntegrationServer(t, "http://127.0.0.1:1")
	projID := createProject(t, client, srv.URL, wsID, token)
	integrationID := createIntegration(t, client, srv.URL, wsID, projID, token)
	webhookURL := fmt.Sprintf("%s/api/webhooks/github/%s", srv.URL, integrationID)

	body := []byte(`{"action":"opened","issue":{"number":42,"title":"登录页 500","body":"x","labels":[{"name":"bug"}],"html_url":"https://github.com/owner/repo/issues/42"},"repository":{"id":1,"full_name":"owner/repo","default_branch":"main"},"sender":{"login":"octocat"}}`)

	post := func(sig string) (int, []byte) {
		req, err := http.NewRequest(http.MethodPost, webhookURL, bytes.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", "issues")
		req.Header.Set("X-GitHub-Delivery", "d1000000-0000-4000-8000-0000000000a1")
		if sig != "" {
			req.Header.Set("X-Hub-Signature-256", sig)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("post webhook: %v", err)
		}
		defer resp.Body.Close()
		buf := new(bytes.Buffer)
		_, _ = buf.ReadFrom(resp.Body)
		return resp.StatusCode, buf.Bytes()
	}

	// Unknown integration → 404.
	unknownURL := fmt.Sprintf("%s/api/webhooks/github/%s", srv.URL, uuid.NewString())
	req, _ := http.NewRequest(http.MethodPost, unknownURL, bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "d-unknown")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post unknown: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown integration: expected 404, got %d", resp.StatusCode)
	}

	// Missing signature → 401.
	if status, _ := post(""); status != http.StatusUnauthorized {
		t.Fatalf("missing signature: expected 401, got %d", status)
	}
	// Wrong signature → 401.
	if status, _ := post("sha256=deadbeef"); status != http.StatusUnauthorized {
		t.Fatalf("wrong signature: expected 401, got %d", status)
	}

	// Correct signature → 202 with one pending event row.
	signature := githubSign(dbSecret(t, integrationID), body)
	status, respBody := post(signature)
	if status != http.StatusAccepted {
		t.Fatalf("correct signature: expected 202, got %d, body: %s", status, respBody)
	}

	// Replay the same delivery → still 202, still one row.
	status, _ = post(signature)
	if status != http.StatusAccepted {
		t.Fatalf("replay: expected 202, got %d", status)
	}

	db, err := sql.Open("pgx", testdb.GetTestDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var count int
	var payloadRaw []byte
	if err := db.QueryRow(`SELECT COUNT(*), MIN(payload::text) FROM workflow_events WHERE integration_id = $1 AND external_id = $2`,
		integrationID, "d1000000-0000-4000-8000-0000000000a1").Scan(&count, &payloadRaw); err != nil {
		t.Fatalf("query events: %v", err)
	}
	if count != 1 {
		t.Fatalf("event rows = %d, want 1 (idempotent delivery)", count)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	for _, key := range []string{"event", "action", "delivery_id", "repository", "sender", "issue", "pull_request", "raw"} {
		if _, ok := payload[key]; !ok {
			t.Fatalf("stored payload missing key %q: %s", key, payloadRaw)
		}
	}
	if payload["event"] != "issues" || payload["action"] != "opened" {
		t.Fatalf("payload header fields wrong: %s", payloadRaw)
	}
	issue, _ := payload["issue"].(map[string]interface{})
	if issue == nil || issue["title"] != "登录页 500" {
		t.Fatalf("issue subset wrong: %v", payload["issue"])
	}
	labels, _ := issue["labels"].([]interface{})
	if len(labels) != 1 || labels[0] != "bug" {
		t.Fatalf("labels must be flattened names: %v", issue["labels"])
	}

	var statusCol string
	if err := db.QueryRow(`SELECT status FROM workflow_events WHERE integration_id = $1 AND external_id = $2`,
		integrationID, "d1000000-0000-4000-8000-0000000000a1").Scan(&statusCol); err != nil {
		t.Fatalf("query status: %v", err)
	}
	if statusCol != "pending" {
		t.Fatalf("event status = %q, want pending", statusCol)
	}
}

// TestWebhookBodyLimit verifies an oversized delivery is rejected with 413.
func TestWebhookBodyLimit(t *testing.T) {
	srv, client, token, wsID := setupIntegrationServer(t, "http://127.0.0.1:1")
	projID := createProject(t, client, srv.URL, wsID, token)
	integrationID := createIntegration(t, client, srv.URL, wsID, projID, token)

	big := make([]byte, 3<<20) // 3 MiB > 2 MiB webhook limit
	for i := range big {
		big[i] = 'a'
	}
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/webhooks/github/%s", srv.URL, integrationID), bytes.NewReader(big))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-GitHub-Delivery", "d-big")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("post big body: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: expected 413, got %d", resp.StatusCode)
	}
}

// TestWebhookPayloadShapesPerEvent verifies the normalized payload of PR and
// push deliveries (pull_request subset, null issue) lands in the event row.
func TestWebhookPayloadShapesPerEvent(t *testing.T) {
	srv, client, token, wsID := setupIntegrationServer(t, "http://127.0.0.1:1")
	projID := createProject(t, client, srv.URL, wsID, token)
	integrationID := createIntegration(t, client, srv.URL, wsID, projID, token)

	deliver := func(event, delivery, body string) {
		t.Helper()
		sig := githubSign(dbSecret(t, integrationID), []byte(body))
		req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/webhooks/github/%s", srv.URL, integrationID), strings.NewReader(body))
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-GitHub-Event", event)
		req.Header.Set("X-GitHub-Delivery", delivery)
		req.Header.Set("X-Hub-Signature-256", sig)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("deliver %s: %v", event, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			t.Fatalf("deliver %s: expected 202, got %d", event, resp.StatusCode)
		}
	}

	prBody := `{"action":"submitted","pull_request":{"number":7,"title":"Fix bug","body":"","labels":[],"html_url":"https://github.com/owner/repo/pull/7"},"repository":{"id":1,"full_name":"owner/repo","default_branch":"main"},"sender":{"login":"rev"}}`
	deliver("pull_request_review", "d1000000-0000-4000-8000-0000000000b1", prBody)

	pushBody := `{"ref":"refs/heads/main","repository":{"id":1,"full_name":"owner/repo","default_branch":"main"},"sender":{"login":"dev"}}`
	deliver("push", "d1000000-0000-4000-8000-0000000000b2", pushBody)

	db, err := sql.Open("pgx", testdb.GetTestDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var prPayload string
	if err := db.QueryRow(`SELECT payload::text FROM workflow_events WHERE integration_id=$1 AND external_id=$2`,
		integrationID, "d1000000-0000-4000-8000-0000000000b1").Scan(&prPayload); err != nil {
		t.Fatalf("query pr event: %v", err)
	}
	var pr map[string]interface{}
	_ = json.Unmarshal([]byte(prPayload), &pr)
	if pr["issue"] != nil {
		t.Fatalf("pr payload issue must be null: %s", prPayload)
	}
	prObj, _ := pr["pull_request"].(map[string]interface{})
	if prObj == nil || prObj["number"] != float64(7) {
		t.Fatalf("pr subset wrong: %v", pr["pull_request"])
	}

	var pushPayload string
	if err := db.QueryRow(`SELECT payload::text FROM workflow_events WHERE integration_id=$1 AND external_id=$2`,
		integrationID, "d1000000-0000-4000-8000-0000000000b2").Scan(&pushPayload); err != nil {
		t.Fatalf("query push event: %v", err)
	}
	var push map[string]interface{}
	_ = json.Unmarshal([]byte(pushPayload), &push)
	if push["issue"] != nil || push["pull_request"] != nil {
		t.Fatalf("push payload must have null issue/pull_request: %s", pushPayload)
	}
	if push["action"] != "" {
		t.Fatalf("push payload action must be empty: %s", pushPayload)
	}
}

// TestWorkspaceIntegrationsList verifies GET /api/workspaces/{ws}/integrations:
// after creating one github integration under one project the endpoint
// returns 200 with that single entry carrying project_name and a masked PAT
// (the mask never contains the plaintext).
func TestWorkspaceIntegrationsList(t *testing.T) {
	pat := "github_pat_test0000abcd1234"
	srv, client, token, wsID := setupIntegrationServer(t, "http://127.0.0.1:1")
	projID := createProject(t, client, srv.URL, wsID, token)
	createIntegration(t, client, srv.URL, wsID, projID, token)

	url := fmt.Sprintf("%s/api/workspaces/%s/integrations", srv.URL, wsID)
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet, url, token, nil)
	if status != http.StatusOK {
		t.Fatalf("list workspace integrations: expected 200, got %d, body: %s", status, respBody)
	}
	var view struct {
		Integrations []map[string]interface{} `json:"integrations"`
	}
	if err := json.Unmarshal(respBody, &view); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(view.Integrations) != 1 {
		t.Fatalf("integrations length = %d, want 1: %s", len(view.Integrations), respBody)
	}
	item := view.Integrations[0]
	projectName, _ := item["project_name"].(string)
	if projectName == "" {
		t.Fatalf("project_name missing: %s", respBody)
	}
	masked, _ := item["pat_masked"].(string)
	if masked == "" || strings.Contains(masked, pat) {
		t.Fatalf("pat_masked wrong: %q", masked)
	}
}

// dbSecret reads the decrypted webhook secret of one integration straight
// from the database (the API never returns it).
func dbSecret(t *testing.T, integrationID string) string {
	t.Helper()
	db, err := sql.Open("pgx", testdb.GetTestDSN())
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()
	var encrypted string
	if err := db.QueryRow(`SELECT encrypted_webhook_secret FROM integrations WHERE id = $1`, integrationID).Scan(&encrypted); err != nil {
		t.Fatalf("query secret: %v", err)
	}
	secret, err := crypto.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("decrypt secret: %v", err)
	}
	return secret
}

// githubSign signs a webhook body the way GitHub does (sha256=HMAC).
func githubSign(secret string, body []byte) string {
	return github.SignWebhookBody(secret, body)
}
