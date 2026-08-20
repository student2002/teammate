// skill_mcp_config_test.go 覆盖技能与 MCP 配置接口的测试。
package handler_test

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSkillUpdatePartialKeepsExistingName(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/skills", token, map[string]interface{}{
			"name":            "OpenClaw 写作",
			"description":     "初始描述",
			"category":        "文档",
			"prompt_template": "初始模板",
		})
	if status != http.StatusCreated {
		t.Fatalf("create skill: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create skill: %v", err)
	}

	_, status, body = doRequestWithToken(t, client, http.MethodPut,
		ts.URL+"/api/workspaces/"+wsID+"/skills/"+created["id"].(string), token, map[string]interface{}{
			"description": "更新后的描述",
		})
	if status != http.StatusOK {
		t.Fatalf("update skill: expected 200, got %d, body: %s", status, body)
	}
	var updated map[string]interface{}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode updated skill: %v", err)
	}
	if updated["name"] != "OpenClaw 写作" {
		t.Fatalf("expected name to be kept, got %v", updated["name"])
	}
	if updated["description"] != "更新后的描述" {
		t.Fatalf("expected updated description, got %#v", updated["description"])
	}
}

func TestSkillUpdateRejectsExplicitEmptyName(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/skills", token, map[string]interface{}{
			"name":     "安全审计",
			"category": "安全",
		})
	if status != http.StatusCreated {
		t.Fatalf("create skill: expected 201, got %d, body: %s", status, body)
	}
	var created map[string]interface{}
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("decode create skill: %v", err)
	}

	_, status, _ = doRequestWithToken(t, client, http.MethodPut,
		ts.URL+"/api/workspaces/"+wsID+"/skills/"+created["id"].(string), token, map[string]interface{}{
			"name": "  ",
		})
	if status != http.StatusBadRequest {
		t.Fatalf("empty skill name update: expected 400, got %d", status)
	}
}

func TestMcpEnvVarsRejectsNull(t *testing.T) {
	ts, client := setupAgentTestRouter(t)
	token, wsID := registerTestUser(t, client, ts.URL)

	_, status, body := doRequestWithToken(t, client, http.MethodPost,
		ts.URL+"/api/workspaces/"+wsID+"/mcp-servers", token, map[string]interface{}{
			"name":      "Null Env MCP",
			"url":       "https://mcp.example.test/sse",
			"type":      "sse",
			"auth_type": "none",
			"env_vars":  nil,
		})
	if status != http.StatusBadRequest {
		t.Fatalf("create mcp with null env_vars: expected 400, got %d, body: %s", status, body)
	}
}
