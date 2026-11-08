// custom_themes_test.go covers the runtime theme installation API: upload/list/delete
// plus payload contract validation (id, icon, variable coverage, color whitelist).
package handler_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// validThemeVars returns a full THEME_VARS-compliant variable map.
func validThemeVars() map[string]string {
	return map[string]string{
		"--bg": "#09090b", "--surface": "#101012", "--surface-2": "#18181b", "--surface-3": "#27272a",
		"--line": "#27272a", "--line-strong": "#3f3f46",
		"--fg": "#ededed", "--fg-muted": "#a1a1aa", "--fg-subtle": "#71717a", "--fg-faint": "#52525b",
		"--primary": "#5e6ad2", "--primary-strong": "#7a83eb", "--primary-fg": "#a5abf4",
		"--accent": "#14b8a6", "--accent-strong": "#2dd4bf", "--accent-fg": "#5eead4",
		"--success": "#68cc58", "--success-strong": "#7ddb6e", "--success-fg": "#8ee087",
		"--warning": "#f2c94c", "--warning-strong": "#f6d668", "--warning-fg": "#f8dd7a",
		"--danger": "#eb5757", "--danger-strong": "#f07b7b", "--danger-fg": "#f49c9c",
		"--shadow": "rgba(0, 0, 0, 0.45)",
	}
}

func doTheme(t *testing.T, client *http.Client, method, url, token string, body map[string]interface{}) (int, string) {
	t.Helper()
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		payload = b
	}
	req, err := http.NewRequest(method, url, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.String()
}

func TestCustomThemeUploadListDelete(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	body := map[string]interface{}{
		"id": "graphite-test", "name_zh": "石墨", "name_en": "Graphite",
		"icon": "zap", "vars": validThemeVars(),
	}
	status, respBody := doTheme(t, client, http.MethodPost, fmt.Sprintf("%s/api/workspaces/%s/themes/custom", srv.URL, wsID), token, body)
	if status != http.StatusCreated {
		t.Fatalf("upload: expected 201, got %d: %s", status, respBody)
	}

	status, respBody = doTheme(t, client, http.MethodGet, fmt.Sprintf("%s/api/workspaces/%s/themes/custom", srv.URL, wsID), token, nil)
	if status != http.StatusOK {
		t.Fatalf("list: expected 200, got %d: %s", status, respBody)
	}
	if !bytes.Contains([]byte(respBody), []byte("graphite-test")) {
		t.Fatalf("list should contain the uploaded theme: %s", respBody)
	}

	// duplicate id → 409
	status, _ = doTheme(t, client, http.MethodPost, fmt.Sprintf("%s/api/workspaces/%s/themes/custom", srv.URL, wsID), token, body)
	if status != http.StatusConflict {
		t.Errorf("duplicate id: expected 409, got %d", status)
	}

	// delete → 204, then the theme is gone from the list
	status, _ = doTheme(t, client, http.MethodDelete, fmt.Sprintf("%s/api/workspaces/%s/themes/custom/graphite-test", srv.URL, wsID), token, nil)
	if status != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", status)
	}
	_, respBody = doTheme(t, client, http.MethodGet, fmt.Sprintf("%s/api/workspaces/%s/themes/custom", srv.URL, wsID), token, nil)
	if bytes.Contains([]byte(respBody), []byte("graphite-test")) {
		t.Errorf("deleted theme should be gone from list: %s", respBody)
	}

	// delete again → 404
	status, _ = doTheme(t, client, http.MethodDelete, fmt.Sprintf("%s/api/workspaces/%s/themes/custom/graphite-test", srv.URL, wsID), token, nil)
	if status != http.StatusNotFound {
		t.Errorf("delete missing: expected 404, got %d", status)
	}
}

func TestCustomThemePayloadValidation(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)

	valid := func() map[string]interface{} {
		return map[string]interface{}{
			"id": "valid-theme", "name_zh": "名", "name_en": "Name",
			"icon": "sparkles", "vars": validThemeVars(),
		}
	}

	cases := []struct {
		name   string
		mutate func(body map[string]interface{})
	}{
		{"内置 id 冲突", func(b map[string]interface{}) { b["id"] = "dark" }},
		{"id 非法字符", func(b map[string]interface{}) { b["id"] = "Bad_ID" }},
		{"缺少中文名", func(b map[string]interface{}) { b["name_zh"] = "" }},
		{"icon 非法", func(b map[string]interface{}) { b["icon"] = "rocket" }},
		{"颜色非 hex", func(b map[string]interface{}) {
			v := validThemeVars()
			v["--bg"] = "javascript:alert(1)"
			b["vars"] = v
		}},
		{"shadow 非 rgba", func(b map[string]interface{}) {
			v := validThemeVars()
			v["--shadow"] = "url(evil)"
			b["vars"] = v
		}},
		{"缺变量", func(b map[string]interface{}) {
			v := validThemeVars()
			delete(v, "--primary")
			b["vars"] = v
		}},
		{"多余变量键(注入面)", func(b map[string]interface{}) {
			v := validThemeVars()
			v["--a}*{display:none}/*"] = "#09090b"
			b["vars"] = v
		}},
		{"名称超长", func(b map[string]interface{}) {
			b["name_zh"] = string(make([]rune, 101))
		}},
		{"rgba 分量越界", func(b map[string]interface{}) {
			v := validThemeVars()
			v["--shadow"] = "rgba(999, 0, 0, 0.5)"
			b["vars"] = v
		}},
	}
	for _, tc := range cases {
		body := valid()
		tc.mutate(body)
		status, respBody := doTheme(t, client, http.MethodPost, fmt.Sprintf("%s/api/workspaces/%s/themes/custom", srv.URL, wsID), token, body)
		if status != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", tc.name, status, respBody)
		}
	}
}

func TestCustomThemePermissions(t *testing.T) {
	router, db, _ := setupTestRouter(t)
	defer db.Close()
	srv := httptest.NewServer(router)
	defer srv.Close()
	client := srv.Client()
	token, wsID := registerTestUser(t, client, srv.URL)
	base := fmt.Sprintf("%s/api/workspaces/%s/themes/custom", srv.URL, wsID)

	// custom_themes 是部署级共享表且测试库跨运行持久，固定 ID 会留下残留，先清掉再上传
	if _, err := db.Exec("DELETE FROM custom_themes WHERE id = 'perm-theme'"); err != nil {
		t.Fatal(err)
	}

	body := map[string]interface{}{
		"id": "perm-theme", "name_zh": "名", "name_en": "Name",
		"icon": "sparkles", "vars": validThemeVars(),
	}

	// agent 上传 → 403;agent 读列表 → 200(全员可读)
	_, agentToken := createAgent(t, client, srv.URL, wsID, token)
	resp, status, _ := doRequestWithAPIKey(t, client, http.MethodPost, base, agentToken, body)
	if status != http.StatusForbidden {
		t.Errorf("agent upload: expected 403, got %d", status)
	}
	resp, status, _ = doRequestWithAPIKey(t, client, http.MethodGet, base, agentToken, nil)
	if status != http.StatusOK {
		t.Errorf("agent list: expected 200, got %d", status)
	}
	_ = resp

	// viewer 上传/删除 → 403;恢复 owner 后 → 正常
	if _, err := db.Exec("UPDATE workspace_members SET role='viewer' WHERE workspace_id=$1", wsID); err != nil {
		t.Fatal(err)
	}
	status, respBody := doTheme(t, client, http.MethodPost, base, token, body)
	if status != http.StatusForbidden {
		t.Errorf("viewer upload: expected 403, got %d: %s", status, respBody)
	}
	if _, err := db.Exec("UPDATE workspace_members SET role='owner' WHERE workspace_id=$1", wsID); err != nil {
		t.Fatal(err)
	}
	status, _ = doTheme(t, client, http.MethodPost, base, token, body)
	if status != http.StatusCreated {
		t.Errorf("owner upload after restore: expected 201, got %d", status)
	}
	delURL := fmt.Sprintf("%s/perm-theme", base)
	resp, status, _ = doRequestWithAPIKey(t, client, http.MethodDelete, delURL, agentToken, nil)
	if status != http.StatusForbidden {
		t.Errorf("agent delete: expected 403, got %d", status)
	}
	_ = resp
}
