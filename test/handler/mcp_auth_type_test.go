// mcp_auth_type_test.go verifies auth_type handling against the mcp_auth_type
// DB enum ('none', 'api_key', 'oauth'): omission defaults to none, values the
// database cannot store are rejected with 400 before reaching the DB, and the
// legal 'oauth' value is accepted. These scenarios mirror real clients — the
// CLI omits --auth-type entirely when unset.
package handler_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
)

func TestMcpCreateAuthTypeOmittedDefaultsToNone(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	// No auth_type in the body at all — the CLI path when --auth-type is unset.
	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", ts.URL, wsID), token, map[string]interface{}{
			"name": "mcp-no-auth-" + wsID[:8],
			"url":  "https://mcp.example.test/sse",
			"type": "sse",
		})
	if status != http.StatusCreated {
		t.Fatalf("create without auth_type: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created mcp: %v", err)
	}
	if created["auth_type"] != "none" {
		t.Fatalf("auth_type default = %v, want none", created["auth_type"])
	}
}

func TestMcpCreateTypeOmittedDefaultsToSse(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	// No type in the body — the CLI path when --type is unset; the server must
	// apply the same default the web form preselects instead of storing "".
	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", ts.URL, wsID), token, map[string]interface{}{
			"name": "mcp-no-type",
			"url":  "https://mcp.example.test/sse",
		})
	if status != http.StatusCreated {
		t.Fatalf("create without type: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created mcp: %v", err)
	}
	if created["type"] != "sse" {
		t.Fatalf("type default = %v, want sse", created["type"])
	}
}

func TestMcpCreateAuthTypeValidationMatchesEnum(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)
	url := fmt.Sprintf("%s/api/workspaces/%s/mcp-servers", ts.URL, wsID)

	// A value the old whitelist accepted but the DB enum rejects must be 400.
	_, status, body := doRequestWithToken(t, client, http.MethodPost, url, token, map[string]interface{}{
		"name": "mcp-bearer", "url": "https://mcp.example.test/sse", "type": "sse", "auth_type": "bearer",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("create auth_type=bearer: expected 400, got %d, body: %s", status, body)
	}

	// A legal enum member the old whitelist wrongly rejected must be 201.
	_, status, body = doRequestWithToken(t, client, http.MethodPost, url, token, map[string]interface{}{
		"name": "mcp-oauth", "url": "https://mcp.example.test/sse", "type": "sse", "auth_type": "oauth",
	})
	if status != http.StatusCreated {
		t.Fatalf("create auth_type=oauth: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode created mcp: %v", err)
	}
	id := created["id"].(string)

	// Update: explicit empty auth_type is 400; switching to another enum member works.
	updateURL := fmt.Sprintf("%s/%s", url, id)
	_, status, _ = doRequestWithToken(t, client, http.MethodPut, updateURL, token, map[string]interface{}{
		"auth_type": "",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("update auth_type empty: expected 400, got %d", status)
	}
	_, status, body = doRequestWithToken(t, client, http.MethodPut, updateURL, token, map[string]interface{}{
		"auth_type": "api_key",
	})
	if status != http.StatusOK {
		t.Fatalf("update auth_type=api_key: expected 200, got %d, body: %s", status, body)
	}
	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode updated mcp: %v", err)
	}
	if updated["auth_type"] != "api_key" {
		t.Fatalf("updated auth_type = %v, want api_key", updated["auth_type"])
	}
}
