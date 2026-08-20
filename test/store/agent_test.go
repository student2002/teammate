// agent_test.go 覆盖 Agent 数据访问的测试。
package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

func TestCreateAgent_WithToken(t *testing.T) {
	s, _ := setupTestStore(t)

	ws := createTestWorkspace(t, s)
	agent, apiToken := createTestAgent(t, s, ws.ID)

	if agent.ID == "" {
		t.Fatal("expected non-nil agent ID")
	}
	if !strings.HasPrefix(apiToken, "tm_") {
		t.Fatalf("expected token prefix 'tm_', got %s", apiToken)
	}
}

func TestDeleteAgent_CascadeCleanup(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent, _ := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)

	// 为agent创建运行时
	ver := "1.0.0"
	_, err := s.CreateRuntime(ctx, types.CreateRuntimeParams{
		AgentID:  agent.ID,
		Provider: "claude",
		Version:  &ver,
		Status:   "online",
	})
	if err != nil {
		t.Fatalf("CreateRuntime: %v", err)
	}

	err = s.DeleteAgent(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	// 验证agent已被删除
	_, err = s.GetAgent(ctx, uuid.MustParse(agent.ID))
	if err == nil {
		t.Fatal("expected error retrieving deleted agent")
	}
}

func TestRotateAgentToken(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	agent, oldToken := createTestAgent(t, s, ws.ID)

	newToken, err := s.RotateAgentToken(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("RotateAgentToken: %v", err)
	}
	if newToken == "" {
		t.Fatal("expected non-empty new token")
	}
	if newToken == oldToken {
		t.Fatal("expected different token after rotation")
	}

	// 旧token应失效
	_, err = s.ExchangeAPITokenForSession(ctx, oldToken)
	if err == nil {
		t.Fatal("expected old token to be invalid after rotation")
	}

	// 新token应有效
	_, err = s.ExchangeAPITokenForSession(ctx, newToken)
	if err != nil {
		t.Fatalf("new token should be valid: %v", err)
	}
}

func TestValidateAgentStatusTransition(t *testing.T) {
	tests := []struct {
		name   string
		from   string
		to     string
		expect bool
	}{
		{"offline to online", "offline", "online", true},
		{"online to busy", "online", "busy", true},
		{"online to offline", "online", "offline", true},
		{"online to paused", "online", "paused", true},
		{"busy to online", "busy", "online", true},
		{"busy to paused", "busy", "paused", true},
		{"paused to online", "paused", "online", true},
		{"paused to offline", "paused", "offline", true},
		{"offline to busy", "offline", "busy", false},
		{"busy to offline", "busy", "offline", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := store.ValidateAgentStatusTransition(tt.from, tt.to)
			if got != tt.expect {
				t.Fatalf("ValidateAgentStatusTransition(%s, %s) = %v, want %v", tt.from, tt.to, got, tt.expect)
			}
		})
	}
}

func TestGrantDefaultPermissions(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	agent, _ := createTestAgent(t, s, ws.ID)
	member := createTestMember(t, s, ws.ID)

	err := s.GrantDefaultPermissions(ctx, uuid.MustParse(agent.ID), uuid.MustParse(member.ID))
	if err != nil {
		t.Fatalf("GrantDefaultPermissions: %v", err)
	}

	listed, err := s.ListAgentPermissions(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("ListAgentPermissions: %v", err)
	}
	if len(listed) != 4 {
		t.Fatalf("expected 4 permissions, got %d", len(listed))
	}
}
