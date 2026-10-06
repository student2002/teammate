// agent_test.go tests for Agent data access.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
)


func TestDeleteAgent_CascadeCleanup(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	agent := createTestAgent(t, s, ws.ID)
	addAgentToProject(t, s, proj.ID, agent.ID)

	// Bind the agent to a reporting daemon (daemon-first model)
	daemon, _, err := s.CreateDaemon(ctx, uuid.MustParse(ws.ID), "cascade-test-daemon")
	if err != nil {
		t.Fatalf("CreateDaemon: %v", err)
	}
	if err := s.RegisterDaemonAgent(ctx, uuid.MustParse(ws.ID), daemon.ID, uuid.MustParse(agent.ID)); err != nil {
		t.Fatalf("RegisterDaemonAgent: %v", err)
	}

	err = s.DeleteAgent(ctx, uuid.MustParse(agent.ID))
	if err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	// Verify the agent has been deleted
	_, err = s.GetAgent(ctx, uuid.MustParse(agent.ID))
	if err == nil {
		t.Fatal("expected error retrieving deleted agent")
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
	agent := createTestAgent(t, s, ws.ID)
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
