// helpers_test.go provides the e2e helpers: HTTP bootstrap (register/project/
// integration/template), webhook delivery with HMAC and the DB/graph
// assertions.
package e2e_test

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/integration/github"
	"github.com/teammate/server/test/testdb"
)

// osStderr/osExit are indirections for the TestMain plumbing.
var (
	osStderr = os.Stderr
	osExit   = os.Exit
)

// e2eDo performs one JSON request and returns the status and body.
func e2eDo(t *testing.T, client *http.Client, method, url, token string, body interface{}) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	return resp.StatusCode, raw
}

// e2eRegister registers a user and returns the token and workspace id.
func e2eRegister(t *testing.T, client *http.Client, baseURL string) (string, string) {
	t.Helper()
	body := map[string]string{
		"name":     "E2E User",
		"email":    "e2e-" + uuid.New().String()[:8] + "@test.com",
		"password": "Test123456",
	}
	status, raw := e2eDo(t, client, http.MethodPost, baseURL+"/api/auth/register", "", body)
	if status != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d, body: %s", status, raw)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(raw, &result)
	token, _ := result["token"].(string)
	wsID, _ := result["workspace_id"].(string)
	if token == "" || wsID == "" {
		t.Fatalf("register response missing token/workspace_id: %s", raw)
	}
	// Cleanup removes the workspace (cascades to projects/tasks).
	memberID := e2eMemberIDFromJWT(t, token)
	t.Cleanup(func() {
		if db, err := sql.Open("pgx", testdbDSN()); err == nil {
			defer db.Close()
			_, _ = db.Exec(`DELETE FROM workspaces WHERE id = $1`, wsID)
			_, _ = db.Exec(`DELETE FROM members WHERE id = $1`, memberID)
		}
	})
	return token, wsID
}

// e2eMemberIDFromJWT decodes the JWT payload's user_id claim.
func e2eMemberIDFromJWT(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT")
	}
	decoded, err := base64RawURL(parts[1])
	if err != nil {
		t.Fatalf("decode JWT: %v", err)
	}
	var claims map[string]interface{}
	_ = json.Unmarshal(decoded, &claims)
	id, _ := claims["user_id"].(string)
	return id
}

// e2eCreateProject creates a project and returns its id.
func e2eCreateProject(t *testing.T, client *http.Client, baseURL, wsID, token string) string {
	t.Helper()
	body := map[string]interface{}{
		"name": "e2e-proj-" + uuid.New().String()[:8], "status": "active",
		"repo_url": "https://github.com/owner/repo.git",
	}
	status, raw := e2eDo(t, client, http.MethodPost, fmt.Sprintf("%s/api/workspaces/%s/projects", baseURL, wsID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create project: expected 201, got %d, body: %s", status, raw)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(raw, &result)
	id, _ := result["id"].(string)
	if id == "" {
		t.Fatalf("project id missing: %s", raw)
	}
	return id
}

// e2eCreateIntegration creates the project's GitHub integration.
func e2eCreateIntegration(t *testing.T, client *http.Client, baseURL, wsID, projID, token string) string {
	t.Helper()
	body := map[string]interface{}{
		"name": "e2e-repo", "repo_owner": "owner", "repo_name": "repo",
		"pat":            "github_pat_e2e0000abcd1234",
		"webhook_secret": "e2e-hook-secret-" + uuid.New().String()[:8],
	}
	status, raw := e2eDo(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/projects/%s/integrations/github", baseURL, wsID, projID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("create integration: expected 201, got %d, body: %s", status, raw)
	}
	var result map[string]interface{}
	_ = json.Unmarshal(raw, &result)
	id, _ := result["id"].(string)
	if id == "" {
		t.Fatalf("integration id missing: %s", raw)
	}
	return id
}

// e2eDecryptWebhookSecret reads the integration's decrypted webhook secret
// from the database (the API never returns it).
func e2eDecryptWebhookSecret(t *testing.T, db *sql.DB, integrationID string) string {
	t.Helper()
	var encrypted string
	if err := db.QueryRow(`SELECT encrypted_webhook_secret FROM integrations WHERE id = $1`, integrationID).Scan(&encrypted); err != nil {
		t.Fatalf("query webhook secret: %v", err)
	}
	secret, err := crypto.DecryptString(encrypted)
	if err != nil {
		t.Fatalf("decrypt webhook secret: %v", err)
	}
	return secret
}

// e2eCreateTemplate posts the full graph and returns the template id.
func e2eCreateTemplate(t *testing.T, client *http.Client, baseURL, wsID, token string, graph map[string]interface{}) string {
	t.Helper()
	status, raw := e2eDo(t, client, http.MethodPost, fmt.Sprintf("%s/api/workspaces/%s/workflows", baseURL, wsID), token, graph)
	if status != http.StatusCreated {
		t.Fatalf("create template: expected 201, got %d, body: %s", status, raw)
	}
	var result struct {
		Template map[string]interface{} `json:"template"`
	}
	_ = json.Unmarshal(raw, &result)
	id, _ := result.Template["id"].(string)
	if id == "" {
		t.Fatalf("template id missing: %s", raw)
	}
	return id
}

// e2eDeliverWebhook posts one signed GitHub delivery.
func e2eDeliverWebhook(t *testing.T, client *http.Client, baseURL, integrationID, secret, event, delivery, body string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("%s/api/webhooks/github/%s", baseURL, integrationID), strings.NewReader(body))
	if err != nil {
		t.Fatalf("build webhook request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-Hub-Signature-256", github.SignWebhookBody(secret, []byte(body)))
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("deliver webhook: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("deliver webhook: expected 202, got %d, body: %s", resp.StatusCode, raw)
	}
}

// e2eTaskIDByTemplate returns the id of the template's run (0 when absent).
func e2eTaskIDByTemplate(t *testing.T, db *sql.DB, templateID string) int32 {
	t.Helper()
	var id int32
	err := db.QueryRow(`SELECT id FROM tasks WHERE workflow_template_id = $1 ORDER BY id`, templateID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0
	}
	if err != nil {
		t.Fatalf("query task: %v", err)
	}
	return id
}

// e2eTaskCountByTemplate counts the template's runs.
func e2eTaskCountByTemplate(t *testing.T, db *sql.DB, templateID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE workflow_template_id = $1`, templateID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	return count
}

// e2eTaskStatus reads one task's status.
func e2eTaskStatus(t *testing.T, db *sql.DB, taskID int32) string {
	t.Helper()
	var status string
	if err := db.QueryRow(`SELECT status FROM tasks WHERE id = $1`, taskID).Scan(&status); err != nil {
		t.Fatalf("query task status: %v", err)
	}
	return status
}

// e2eTaskVars reads the vars root of the run context.
func e2eTaskVars(t *testing.T, db *sql.DB, taskID int32) map[string]interface{} {
	t.Helper()
	var contextRaw []byte
	if err := db.QueryRow(`SELECT context FROM tasks WHERE id = $1`, taskID).Scan(&contextRaw); err != nil {
		t.Fatalf("query task context: %v", err)
	}
	var ctx map[string]interface{}
	_ = json.Unmarshal(contextRaw, &ctx)
	vars, _ := ctx["vars"].(map[string]interface{})
	if vars == nil {
		return map[string]interface{}{}
	}
	return vars
}

// e2eGraphNodes loads the run graph and returns the node list.
func e2eGraphNodes(t *testing.T, client *http.Client, baseURL, token string, taskID int32) []map[string]interface{} {
	t.Helper()
	status, raw := e2eDo(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/graph", baseURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("load graph: expected 200, got %d, body: %s", status, raw)
	}
	var graph struct {
		Nodes []map[string]interface{} `json:"nodes"`
	}
	_ = json.Unmarshal(raw, &graph)
	return graph.Nodes
}

// e2eNodeByTemplate finds one run node by its template node id.
func e2eNodeByTemplate(t *testing.T, nodes []map[string]interface{}, templateNodeID string) map[string]interface{} {
	t.Helper()
	for _, node := range nodes {
		if node["template_node_id"] == templateNodeID {
			return node
		}
	}
	t.Fatalf("node %s not found among %d nodes", templateNodeID, len(nodes))
	return nil
}

// e2eEdgeSkipped reports whether the outbound edge of one node on the given
// port resolved as skipped.
func e2eEdgeSkipped(t *testing.T, client *http.Client, baseURL, token string, taskID int32, sourceTemplateNodeID, sourcePort string) bool {
	t.Helper()
	status, raw := e2eDo(t, client, http.MethodGet, fmt.Sprintf("%s/api/tasks/%d/graph", baseURL, taskID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("load graph: expected 200, got %d, body: %s", status, raw)
	}
	var graph struct {
		Nodes []map[string]interface{} `json:"nodes"`
		Edges []map[string]interface{} `json:"edges"`
	}
	_ = json.Unmarshal(raw, &graph)
	var sourceNodeID string
	for _, node := range graph.Nodes {
		if node["template_node_id"] == sourceTemplateNodeID {
			sourceNodeID, _ = node["id"].(string)
		}
	}
	for _, edge := range graph.Edges {
		if edge["source_node_id"] == sourceNodeID && edge["source_port"] == sourcePort {
			return edge["resolution"] == "skipped"
		}
	}
	return false
}

// testdbDSN wraps the test DSN accessor.
func testdbDSN() string { return testdb.GetTestDSN() }

// base64RawURL decodes a raw (unpadded) URL-safe base64 segment.
func base64RawURL(segment string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(segment)
}
