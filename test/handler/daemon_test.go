// daemon_test.go covers daemon (agentd instance) management endpoints and the
// daemon authentication chain: td_ token issuance, rotation invalidation,
// session token exchange, and X-Agent-ID effective-agent access.
package handler_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// dbHandle wraps the test database for direct row manipulation.
type dbHandle struct{ db *sql.DB }

// setAgentDaemon marks an agent as reported by a daemon, mirroring the
// registration upsert that the machine-side API performs.
func (h *dbHandle) setAgentDaemon(agentID, daemonID string) error {
	_, err := h.db.Exec(`UPDATE agents SET daemon_id = $1 WHERE id = $2`, daemonID, agentID)
	return err
}

func setupDaemonTestServer(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	router, _, _ := setupTestRouter(t)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, ts.Client()
}

func setupDaemonTestServerWithDB(t *testing.T) (*httptest.Server, *http.Client, *dbHandle) {
	t.Helper()
	router, db, _ := setupTestRouter(t)
	ts := httptest.NewServer(router)
	t.Cleanup(ts.Close)
	return ts, ts.Client(), &dbHandle{db: db}
}

// daemonRequest performs a request authenticated with daemon credentials
// (X-API-Key) plus an optional X-Agent-ID on-behalf-of header.
func daemonRequest(t *testing.T, client *http.Client, method, url, daemonToken, agentID string, body interface{}) (int, []byte) {
	t.Helper()

	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", daemonToken)
	if agentID != "" {
		req.Header.Set("X-Agent-ID", agentID)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp.StatusCode, respBody
}

func createTestDaemon(t *testing.T, client *http.Client, baseURL, wsID, memberToken, name string) (daemonID, daemonToken string) {
	t.Helper()
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		baseURL+"/api/workspaces/"+wsID+"/daemons", memberToken, map[string]string{"name": name})
	if status != http.StatusCreated {
		t.Fatalf("create daemon: expected 201, got %d, body: %s", status, respBody)
	}
	var result struct {
		ID          string `json:"id"`
		DaemonToken string `json:"daemon_token"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		t.Fatalf("decode create daemon response: %v", err)
	}
	if !strings.HasPrefix(result.DaemonToken, "td_") {
		t.Fatalf("daemon token should have td_ prefix, got %s", result.DaemonToken)
	}
	return result.ID, result.DaemonToken
}

func TestDaemonLifecycle(t *testing.T) {
	ts, client := setupDaemonTestServer(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	daemonID, tdToken := createTestDaemon(t, client, ts.URL, wsID, memberToken, "dev-box")

	// list contains the daemon
	_, status, respBody := doRequestWithToken(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/daemons", memberToken, nil)
	if status != http.StatusOK {
		t.Fatalf("list daemons: expected 200, got %d, body: %s", status, respBody)
	}
	var list []map[string]interface{}
	if err := json.Unmarshal(respBody, &list); err != nil {
		t.Fatalf("decode daemon list: %v", err)
	}
	found := false
	for _, d := range list {
		if d["id"] == daemonID {
			found = true
			if _, ok := d["agents"]; !ok {
				t.Fatalf("daemon response should carry an agents array")
			}
		}
	}
	if !found {
		t.Fatalf("created daemon %s not found in list", daemonID)
	}

	// the daemon token authenticates (403 = authorized as daemon but member-route, not 401)
	code, _ := daemonRequest(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents", tdToken, "", nil)
	if code != http.StatusForbidden {
		t.Fatalf("daemon token should authenticate (403 on member route), got %d", code)
	}

	// rotate: new token returned once, old one invalidated immediately
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonID+"/rotate-token", memberToken, nil)
	if status != http.StatusOK {
		t.Fatalf("rotate token: expected 200, got %d, body: %s", status, respBody)
	}
	var rotated struct {
		DaemonToken string `json:"daemon_token"`
	}
	if err := json.Unmarshal(respBody, &rotated); err != nil {
		t.Fatalf("decode rotate response: %v", err)
	}
	if !strings.HasPrefix(rotated.DaemonToken, "td_") || rotated.DaemonToken == tdToken {
		t.Fatalf("rotation should return a new td_ token")
	}
	code, _ = daemonRequest(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents", tdToken, "", nil)
	if code != http.StatusUnauthorized {
		t.Fatalf("old daemon token should be invalidated (401), got %d", code)
	}
	code, _ = daemonRequest(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents", rotated.DaemonToken, "", nil)
	if code != http.StatusForbidden {
		t.Fatalf("rotated daemon token should authenticate (403 on member route), got %d", code)
	}

	// delete, then list no longer contains it and delete again yields 404
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonID, memberToken, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete daemon: expected 204, got %d", status)
	}
	_, status, _ = doRequestWithToken(t, client, http.MethodDelete,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonID, memberToken, nil)
	if status != http.StatusNotFound {
		t.Fatalf("delete daemon twice: expected 404, got %d", status)
	}
}

func TestDaemonAgentOnBehalfOf(t *testing.T) {
	ts, client, dbh := setupDaemonTestServerWithDB(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	daemonAID, tdA := createTestDaemon(t, client, ts.URL, wsID, memberToken, "box-a")
	_, tdB := createTestDaemon(t, client, ts.URL, wsID, memberToken, "box-b")

	// create an agent and mark it as reported by daemon A (registration lands in T2)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/agents", memberToken, map[string]string{
			"name": "claude-01", "provider": "claude",
			"git_name": "claude-01", "git_email": "claude-01@teammate.local",
		})
	if status != http.StatusCreated {
		t.Fatalf("create agent: expected 201, got %d, body: %s", status, respBody)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode agent response: %v", err)
	}
	agentID := created["id"].(string)
	if err := dbh.setAgentDaemon(agentID, daemonAID); err != nil {
		t.Fatalf("set agent daemon: %v", err)
	}

	agentURL := ts.URL + "/api/workspaces/" + wsID + "/agents/" + agentID

	// owner daemon acting for the agent resolves to the agent identity
	code, _ := daemonRequest(t, client, http.MethodGet, agentURL, tdA, agentID, nil)
	if code != http.StatusOK {
		t.Fatalf("owner daemon + X-Agent-ID should access the agent, got %d", code)
	}

	// a different daemon cannot act for this agent
	code, _ = daemonRequest(t, client, http.MethodGet, agentURL, tdB, agentID, nil)
	if code != http.StatusForbidden {
		t.Fatalf("non-owner daemon + X-Agent-ID should be 403, got %d", code)
	}

	// unknown agent id and malformed header are rejected
	code, _ = daemonRequest(t, client, http.MethodGet, agentURL, tdA, uuid.NewString(), nil)
	if code != http.StatusForbidden {
		t.Fatalf("unknown X-Agent-ID should be 403, got %d", code)
	}
	code, _ = daemonRequest(t, client, http.MethodGet, agentURL, tdA, "not-a-uuid", nil)
	if code != http.StatusForbidden {
		t.Fatalf("malformed X-Agent-ID should be 403, got %d", code)
	}
}

func TestDaemonSessionTokenExchange(t *testing.T) {
	ts, client := setupDaemonTestServer(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	_, tdToken := createTestDaemon(t, client, ts.URL, wsID, memberToken, "dev-box")

	_, status, respBody := doRequest(t, client, http.MethodPost,
		ts.URL+"/api/auth/token-exchange", map[string]string{"api_token": tdToken})
	if status != http.StatusOK {
		t.Fatalf("token exchange: expected 200, got %d, body: %s", status, respBody)
	}
	var exchanged map[string]interface{}
	if err := json.Unmarshal(respBody, &exchanged); err != nil {
		t.Fatalf("decode exchange response: %v", err)
	}
	sessionToken, _ := exchanged["session_token"].(string)
	if !strings.HasPrefix(sessionToken, "st_") {
		t.Fatalf("exchange should return an st_ session token, got %v", exchanged)
	}

	// the session token authenticates as the daemon principal
	code, _ := daemonRequest(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/agents", sessionToken, "", nil)
	if code != http.StatusForbidden {
		t.Fatalf("daemon session token should authenticate (403 on member route), got %d", code)
	}
}
