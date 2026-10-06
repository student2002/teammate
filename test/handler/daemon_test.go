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

// setAgentDaemon binds an agent to a daemon and marks it online (manual
// binding; machine flows never create or bind agent rows).
func (h *dbHandle) setAgentDaemon(agentID, daemonID string) error {
	_, err := h.db.Exec(`UPDATE agents SET daemon_id = $1, status = 'online' WHERE id = $2::uuid`, daemonID, agentID)
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

// registerBody is the machine-side register payload with a detected claude
// provider (no instances: registration is machine-info only).
func registerBody() map[string]interface{} {
	return map[string]interface{}{
		"device_name": "dev-box",
		"version":     "1.0.0",
		"providers":   []map[string]interface{}{{"provider": "claude", "version": "1.0", "installed": true}},
	}
}

// registerProviders registers the daemon's detected-tools snapshot.
func registerProviders(t *testing.T, client *http.Client, baseURL, tdToken string) {
	t.Helper()
	code, respBody := daemonRequest(t, client, http.MethodPost,
		baseURL+"/api/daemons/register", tdToken, "", registerBody())
	if code != http.StatusOK {
		t.Fatalf("register: expected 200, got %d, body: %s", code, respBody)
	}
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
	ts, client := setupDaemonTestServer(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	daemonAID, tdA := createTestDaemon(t, client, ts.URL, wsID, memberToken, "box-a")
	_, tdB := createTestDaemon(t, client, ts.URL, wsID, memberToken, "box-b")
	registerProviders(t, client, ts.URL, tdA)

	// create an instance in daemon A's group (web-created, delivered identity)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonAID+"/agents", memberToken,
		map[string]string{"name": "claude-01", "provider": "claude", "git_name": "Tester", "git_email": "t@t.local"})
	if status != http.StatusCreated {
		t.Fatalf("create daemon agent: expected 201, got %d, body: %s", status, respBody)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode agent response: %v", err)
	}
	agentID := created["id"].(string)

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

// TestDaemonRegisterResponseCarriesWorkspaceID asserts the register response
// echoes the workspace the daemon token is bound to, so agentd can adopt the
// authoritative workspace id per connection.
func TestDaemonRegisterResponseCarriesWorkspaceID(t *testing.T) {
	ts, client := setupDaemonTestServer(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	_, tdToken := createTestDaemon(t, client, ts.URL, wsID, memberToken, "dev-box")

	code, respBody := daemonRequest(t, client, http.MethodPost,
		ts.URL+"/api/daemons/register", tdToken, "", map[string]interface{}{
			"device_name": "dev-box",
			"version":     "1.0.0",
			"providers":   []map[string]interface{}{},
		})
	if code != http.StatusOK {
		t.Fatalf("register: expected 200, got %d, body: %s", code, respBody)
	}
	var registered map[string]interface{}
	if err := json.Unmarshal(respBody, &registered); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if got, _ := registered["workspace_id"].(string); got != wsID {
		t.Fatalf("register response workspace_id = %q, want %q", got, wsID)
	}
	if got, _ := registered["daemon_id"].(string); got == "" {
		t.Fatal("register response should carry daemon_id")
	}
}

// TestDaemonCrossWorkspaceAgentOnBehalfOfRejected asserts a daemon cannot act
// on behalf of an agent whose daemon_id matches but that lives in another
// workspace: the workspace binding is part of the effective-agent check.
func TestDaemonCrossWorkspaceAgentOnBehalfOfRejected(t *testing.T) {
	ts, client, dbh := setupDaemonTestServerWithDB(t)
	memberToken, wsA := registerTestUser(t, client, ts.URL)
	wsB := createWorkspace(t, client, ts.URL, memberToken)

	daemonAID, tdA := createTestDaemon(t, client, ts.URL, wsA, memberToken, "box-a")

	// Create an instance in workspace B's daemon group, then (as if by dirty
	// data) rebind it to the workspace-A daemon.
	daemonBID, tdB := createTestDaemon(t, client, ts.URL, wsB, memberToken, "box-b2")
	registerProviders(t, client, ts.URL, tdB)
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsB+"/daemons/"+daemonBID+"/agents", memberToken,
		map[string]string{"name": "claude-01", "provider": "claude", "git_name": "Tester", "git_email": "t@t.local"})
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

	agentURL := ts.URL + "/api/workspaces/" + wsB + "/agents/" + agentID
	code, _ := daemonRequest(t, client, http.MethodGet, agentURL, tdA, agentID, nil)
	if code != http.StatusForbidden {
		t.Fatalf("cross-workspace X-Agent-ID must be rejected, got %d", code)
	}
}

// TestDaemonGroupAgentCanActInWorkspace asserts instances created in the
// daemon group hold the default permission set from creation: without
// task:execute every effective-agent call (e.g. listing workspace projects)
// is rejected.
func TestDaemonGroupAgentCanActInWorkspace(t *testing.T) {
	ts, client := setupDaemonTestServer(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	daemonID, tdToken := createTestDaemon(t, client, ts.URL, wsID, memberToken, "dev-box")

	registerProviders(t, client, ts.URL, tdToken)

	// web creates the instance in the daemon group
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonID+"/agents", memberToken,
		map[string]string{"name": "claude-01", "provider": "claude", "git_name": "Tester", "git_email": "t@t.local"})
	if status != http.StatusCreated {
		t.Fatalf("create daemon agent: expected 201, got %d, body: %s", status, respBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode created agent: %v", err)
	}

	// the delivered identity comes from desired_agents
	code, respBody := daemonRequest(t, client, http.MethodPost,
		ts.URL+"/api/daemons/register", tdToken, "", registerBody())
	if code != http.StatusOK {
		t.Fatalf("re-register: expected 200, got %d, body: %s", code, respBody)
	}
	var registered struct {
		DesiredAgents []struct {
			AgentID string `json:"agent_id"`
			Name    string `json:"name"`
		} `json:"desired_agents"`
	}
	if err := json.Unmarshal(respBody, &registered); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if len(registered.DesiredAgents) != 1 || registered.DesiredAgents[0].AgentID != created.ID {
		t.Fatalf("expected delivered agent %s, got %+v", created.ID, registered.DesiredAgents)
	}

	// The delivered agent holds task:execute, so the effective-agent call
	// passes the permission check.
	code, respBody = daemonRequest(t, client, http.MethodGet,
		ts.URL+"/api/workspaces/"+wsID+"/projects", tdToken, registered.DesiredAgents[0].AgentID, nil)
	if code != http.StatusOK {
		t.Fatalf("list projects as delivered agent: expected 200, got %d, body: %s", code, respBody)
	}
}

// TestDaemonRegisterDoesNotCreateAgentRows asserts registration is
// machine-info only: no agent rows are created, and the response exposes
// desired_agents instead of an agents resolution map.
func TestDaemonRegisterDoesNotCreateAgentRows(t *testing.T) {
	ts, client, dbh := setupDaemonTestServerWithDB(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	_, tdToken := createTestDaemon(t, client, ts.URL, wsID, memberToken, "dev-box")

	countAgents := func() int {
		var n int
		if err := dbh.db.QueryRow(`SELECT COUNT(*) FROM agents WHERE workspace_id = $1::uuid`, wsID).Scan(&n); err != nil {
			t.Fatalf("count agents: %v", err)
		}
		return n
	}
	before := countAgents()

	code, respBody := daemonRequest(t, client, http.MethodPost,
		ts.URL+"/api/daemons/register", tdToken, "", registerBody())
	if code != http.StatusOK {
		t.Fatalf("register: expected 200, got %d, body: %s", code, respBody)
	}
	var registered map[string]interface{}
	if err := json.Unmarshal(respBody, &registered); err != nil {
		t.Fatalf("decode register response: %v", err)
	}
	if _, exists := registered["agents"]; exists {
		t.Fatal("register response must not carry an agents field")
	}
	if _, exists := registered["desired_agents"]; !exists {
		t.Fatal("register response should carry desired_agents")
	}
	if after := countAgents(); after != before {
		t.Fatalf("register must not create agent rows: before %d, after %d", before, after)
	}
}

// TestDaemonDesiredDeliveryAndHeartbeatStateMachine asserts the
// server-authoritative instance flow: a web-created instance is delivered via
// desired_agents with its agent_id, the heartbeat reporting its name brings it
// online, an absent name offlines it, and pending instances are unaffected by
// absence (incremental semantics: reported instances leave desired).
func TestDaemonDesiredDeliveryAndHeartbeatStateMachine(t *testing.T) {
	ts, client, dbh := setupDaemonTestServerWithDB(t)
	memberToken, wsID := registerTestUser(t, client, ts.URL)

	daemonID, tdToken := createTestDaemon(t, client, ts.URL, wsID, memberToken, "dev-box")
	registerProviders(t, client, ts.URL, tdToken)

	statusOf := func(agentID string) string {
		var status string
		if err := dbh.db.QueryRow(`SELECT status FROM agents WHERE id = $1::uuid`, agentID).Scan(&status); err != nil {
			t.Fatalf("query agent status: %v", err)
		}
		return status
	}
	heartbeat := func(entries ...map[string]interface{}) []map[string]interface{} {
		t.Helper()
		if entries == nil {
			entries = []map[string]interface{}{}
		}
		code, respBody := daemonRequest(t, client, http.MethodPost,
			ts.URL+"/api/daemons/heartbeat", tdToken, "", map[string]interface{}{"agents": entries})
		if code != http.StatusOK {
			t.Fatalf("heartbeat: expected 200, got %d, body: %s", code, respBody)
		}
		var resp struct {
			DesiredAgents []map[string]interface{} `json:"desired_agents"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			t.Fatalf("decode heartbeat response: %v", err)
		}
		return resp.DesiredAgents
	}
	desired := func() []map[string]interface{} {
		t.Helper()
		code, respBody := daemonRequest(t, client, http.MethodPost,
			ts.URL+"/api/daemons/register", tdToken, "", registerBody())
		if code != http.StatusOK {
			t.Fatalf("register: expected 200, got %d, body: %s", code, respBody)
		}
		var resp struct {
			DesiredAgents []map[string]interface{} `json:"desired_agents"`
		}
		if err := json.Unmarshal(respBody, &resp); err != nil {
			t.Fatalf("decode register response: %v", err)
		}
		return resp.DesiredAgents
	}

	// fresh daemon: nothing to deliver
	if d := desired(); len(d) != 0 {
		t.Fatalf("fresh daemon should have no desired agents, got %v", d)
	}

	// web creates an instance in the daemon group
	_, status, respBody := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonID+"/agents", memberToken,
		map[string]string{"name": "claude-01", "provider": "claude", "git_name": "Tester", "git_email": "t@t.local"})
	if status != http.StatusCreated {
		t.Fatalf("create daemon agent: expected 201, got %d, body: %s", status, respBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &created); err != nil {
		t.Fatalf("decode created agent: %v", err)
	}

	// the pending instance is delivered with its agent_id
	d := desired()
	if len(d) != 1 {
		t.Fatalf("expected 1 desired agent, got %v", d)
	}
	if got, _ := d[0]["agent_id"].(string); got != created.ID {
		t.Fatalf("desired agent_id = %q, want %q", got, created.ID)
	}

	// a busy report marks the instance busy; the full set still carries it
	if d := heartbeat(map[string]interface{}{"name": "claude-01", "busy": true}); len(d) != 1 {
		t.Fatalf("full set must still carry the reported instance, got %v", d)
	}
	if s := statusOf(created.ID); s != "busy" {
		t.Fatalf("agent status after busy report = %q, want busy", s)
	}

	// an idle report flips it online (still in the full set)
	if d := heartbeat(map[string]interface{}{"name": "claude-01", "busy": false}); len(d) != 1 {
		t.Fatalf("full set must still carry the online instance, got %v", d)
	}
	if s := statusOf(created.ID); s != "online" {
		t.Fatalf("agent status after idle report = %q, want online", s)
	}

	// a heartbeat missing the name offlines the instance (full set unchanged)
	if d := heartbeat(); len(d) != 1 {
		t.Fatalf("full set must be status-independent, got %v", d)
	}
	if s := statusOf(created.ID); s != "offline" {
		t.Fatalf("agent status after absence = %q, want offline", s)
	}

	// a second instance joins: the full set carries both from now on
	_, status, respBody = doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/daemons/"+daemonID+"/agents", memberToken,
		map[string]string{"name": "claude-02", "provider": "claude", "git_name": "Tester", "git_email": "t@t.local"})
	if status != http.StatusCreated {
		t.Fatalf("create second daemon agent: expected 201, got %d, body: %s", status, respBody)
	}
	var created2 struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(respBody, &created2); err != nil {
		t.Fatalf("decode created agent: %v", err)
	}
	d = heartbeat()
	if len(d) != 2 {
		t.Fatalf("full set must carry both instances, got %v", d)
	}
	gotIDs := map[string]bool{}
	for _, entry := range d {
		if id, _ := entry["agent_id"].(string); id != "" {
			gotIDs[id] = true
		}
	}
	if !gotIDs[created.ID] || !gotIDs[created2.ID] {
		t.Fatalf("full set must contain both %s and %s, got %v", created.ID, created2.ID, gotIDs)
	}
	if s := statusOf(created2.ID); s != "pending" {
		t.Fatalf("pending instance affected by absence: status = %q", s)
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
