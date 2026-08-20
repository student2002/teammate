// mcp_test.go 覆盖 MCP 服务器设计点（见 技能与MCP设计.md §3）。
// 覆盖点：
//   - §3.1 type 字段自由字符串，约定取值 sse/http/streamable_http
//   - §3.2 env_vars 加密写入（AES-256-GCM，标记 teammate-mcp-env-v1）+ 读取脱敏（********）
//   - §3.4 HealthCheck：从 URL 提取 host:port → TCP 5s → connected/disconnected
package service_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	servercrypto "github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/test/testdb"
)

// setupMcpWorkspace 创建一个独立的工作区用于 MCP 测试，返回 svc 与 workspaceID。
func setupMcpWorkspace(t *testing.T) (*service.Service, string) {
	t.Helper()
	pgDB := svcConnectTestDB(t)
	t.Cleanup(func() { pgDB.Close() })
	svc := service.New(pgDB, nil, nil)
	ctx := context.Background()

	ws, err := svc.Store.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "mcp-test-" + uuid.New().String()[:8],
		Description: strPtr("mcp test"),
		IssuePrefix: "MT",
	})
	if err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() { _ = testdb.DeleteWorkspace(pgDB, ws.ID) })
	return svc, ws.ID
}

// createMcpViaService 通过 McpService.Create 创建一个带敏感 env_vars 的 MCP 服务器。
func createMcpViaService(t *testing.T, svc *service.Service, wsID, mcpType string) (types.McpServer, string) {
	t.Helper()
	secretValue := "super-secret-token-" + uuid.New().String()[:8]
	created, err := service.NewMcpService(svc).Create(context.Background(), types.CreateMcpServerParams{
		WorkspaceID: wsID,
		Name:        "MCP " + mcpType,
		URL:         "https://mcp.example.test/" + mcpType,
		Type:        strPtr(mcpType),
		AuthType:    "api_key",
		EnvVars:     json.RawMessage(`{"API_TOKEN":"` + secretValue + `","REGION":"us-east-1"}`),
	})
	if err != nil {
		t.Fatalf("create mcp (%s): %v", mcpType, err)
	}
	return created, secretValue
}

// TestMcpTypeAcceptsAllConventionalValues 验证 §3.1：type 为自由字符串，
// sse/http/streamable_http 三种约定取值均能创建并被持久化保留。
func TestMcpTypeAcceptsAllConventionalValues(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	for _, mcpType := range []string{"sse", "http", "streamable_http"} {
		created, _ := createMcpViaService(t, svc, wsID, mcpType)
		if created.Type != mcpType {
			t.Fatalf("type %q not persisted, got %q", mcpType, created.Type)
		}
		// 通过 Get 验证落库后取回的 type 一致
		got, err := service.NewMcpService(svc).Get(ctx, uuid.MustParse(created.ID))
		if err != nil {
			t.Fatalf("get mcp (%s): %v", mcpType, err)
		}
		if got.Type != mcpType {
			t.Fatalf("type %q not persisted on Get, got %q", mcpType, got.Type)
		}
	}
}

// TestMcpEnvVarsEncryptedAtRestAndMaskedOnRead 验证 §3.2：
// Create 时 env_vars 逐值 AES-256-GCM 加密（落库带 teammate-mcp-env-v1 标记、不含明文），
// Create/List 返回时脱敏为 ******** 只暴露 key 名。
func TestMcpEnvVarsEncryptedAtRestAndMaskedOnRead(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	created, secretValue := createMcpViaService(t, svc, wsID, "streamable_http")

	// Create 返回即应脱敏
	var maskedEnv map[string]interface{}
	if err := json.Unmarshal(created.EnvVars, &maskedEnv); err != nil {
		t.Fatalf("decode created env_vars: %v", err)
	}
	if maskedEnv["API_TOKEN"] != "********" || maskedEnv["REGION"] != "********" {
		t.Fatalf("expected masked env_vars on Create, got %#v", maskedEnv)
	}
	if len(maskedEnv) != 2 {
		t.Fatalf("expected 2 keys exposed (only key names), got %d", len(maskedEnv))
	}

	// 落库内容应加密：不含明文、带格式标记
	got, err := service.NewMcpService(svc).Get(ctx, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("get stored mcp: %v", err)
	}
	storedRaw := string(got.EnvVars)
	if strings.Contains(storedRaw, secretValue) {
		t.Fatalf("stored env_vars contains plaintext secret: %s", storedRaw)
	}
	if !strings.Contains(storedRaw, "teammate-mcp-env-v1") {
		t.Fatalf("stored env_vars missing encrypted envelope marker: %s", storedRaw)
	}

	// List 应整体脱敏
	listed, err := service.NewMcpService(svc).List(ctx, uuid.MustParse(wsID))
	if err != nil {
		t.Fatalf("list mcp: %v", err)
	}
	var found bool
	for _, s := range listed {
		if s.ID == created.ID {
			found = true
			raw := string(s.EnvVars)
			if strings.Contains(raw, secretValue) {
				t.Fatalf("list response leaked plaintext secret: %s", raw)
			}
			if !strings.Contains(raw, "********") {
				t.Fatalf("list response missing masked values: %s", raw)
			}
		}
	}
	if !found {
		t.Fatalf("created mcp not found in list")
	}
}

// TestMcpHealthCheckDisconnectedForUnreachable 验证 §3.4：
// HealthCheck 对不可达 URL 做 TCP 5s 探测 → 置 disconnected。
func TestMcpHealthCheckDisconnectedForUnreachable(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	// 使用一个几乎必然不可达的端口（端口 1 上的保留地址）
	created, err := service.NewMcpService(svc).Create(ctx, types.CreateMcpServerParams{
		WorkspaceID: wsID,
		Name:        "Unreachable MCP",
		URL:         "http://127.0.0.1:1/streamable_http",
		Type:        strPtr("streamable_http"),
		AuthType:    "none",
		EnvVars:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create mcp: %v", err)
	}

	updated, err := service.NewMcpService(svc).HealthCheck(ctx, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if updated.Status != "disconnected" {
		t.Fatalf("expected disconnected for unreachable URL, got %q", updated.Status)
	}
}

// TestMcpHealthCheckConnectedForReachable 验证 §3.4：
// HealthCheck 对可达的本地监听端口 TCP 探测成功 → 置 connected。
func TestMcpHealthCheckConnectedForReachable(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	// 起一个本地 TCP 监听作为"可达"目标
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	// 起一个最小 HTTP server 占住端口（HealthCheck 只做 TCP 拨号，不读 HTTP）
	ts := httptest.NewUnstartedServer(nil)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)

	created, err := service.NewMcpService(svc).Create(ctx, types.CreateMcpServerParams{
		WorkspaceID: wsID,
		Name:        "Reachable MCP",
		URL:         "http://" + ln.Addr().String() + "/streamable_http",
		Type:        strPtr("streamable_http"),
		AuthType:    "none",
		EnvVars:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("create mcp: %v", err)
	}

	updated, err := service.NewMcpService(svc).HealthCheck(ctx, uuid.MustParse(created.ID))
	if err != nil {
		t.Fatalf("health check: %v", err)
	}
	if updated.Status != "connected" {
		t.Fatalf("expected connected for reachable URL, got %q", updated.Status)
	}
}
