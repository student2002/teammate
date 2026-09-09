// mcp_test.go covers MCP server design points (see skills_and_mcp_design.md §3).
// Coverage points:
//   - §3.1 type field is a free-form string, conventional values: sse/http/streamable_http
//   - §3.2 env_vars encrypted at rest (AES-256-GCM, tagged teammate-mcp-env-v1) + masked on read (********)
//   - §3.4 HealthCheck: extract host:port from URL → TCP 5s probe → connected/disconnected
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

// setupMcpWorkspace creates an isolated workspace for MCP tests, returns svc and workspaceID.
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

// createMcpViaService creates an MCP server with sensitive env_vars via McpService.Create.
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

// TestMcpTypeAcceptsAllConventionalValues verifies §3.1: type is a free-form string,
// all three conventional values sse/http/streamable_http can be created and persisted.
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
		// Verify that the type retrieved via Get matches the persisted value
		got, err := service.NewMcpService(svc).Get(ctx, uuid.MustParse(created.ID))
		if err != nil {
			t.Fatalf("get mcp (%s): %v", mcpType, err)
		}
		if got.Type != mcpType {
			t.Fatalf("type %q not persisted on Get, got %q", mcpType, got.Type)
		}
	}
}

// TestMcpEnvVarsEncryptedAtRestAndMaskedOnRead verifies §3.2:
// On Create, env_vars are encrypted value-by-value with AES-256-GCM (stored with teammate-mcp-env-v1 tag, no plaintext),
// On Create/List return, values are masked as ******** exposing only key names.
func TestMcpEnvVarsEncryptedAtRestAndMaskedOnRead(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	created, secretValue := createMcpViaService(t, svc, wsID, "streamable_http")

	// Create response should already be masked
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

	// Stored content should be encrypted: no plaintext, with format marker
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

	// List should be fully masked
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

// TestMcpHealthCheckDisconnectedForUnreachable verifies §3.4:
// HealthCheck performs TCP 5s probe on unreachable URL → sets disconnected.
func TestMcpHealthCheckDisconnectedForUnreachable(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	// Use an almost certainly unreachable port (reserved address on port 1)
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

// TestMcpHealthCheckConnectedForReachable verifies §3.4:
// HealthCheck successfully probes a reachable local listening port → sets connected.
func TestMcpHealthCheckConnectedForReachable(t *testing.T) {
	if err := servercrypto.SetEncryptionKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("set encryption key: %v", err)
	}
	svc, wsID := setupMcpWorkspace(t)
	ctx := context.Background()

	// Start a local TCP listener as a "reachable" target
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	// Start a minimal HTTP server to occupy the port (HealthCheck only does TCP dial, not HTTP)
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
