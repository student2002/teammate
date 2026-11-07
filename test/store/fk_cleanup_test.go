// fk_cleanup_test.go tests the explicit null-setting behavior of store delete transactions after 002_remove_fks
// (FK strategy: after foreign keys are removed, the application layer ensures integrity; see docs/数据存储设计.md and
// docs/实现与设计偏差记录.md #4).
package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// TestDeleteAgent_ClearsNodeRefs verifies that task_nodes.assignee_id is explicitly set to NULL when an Agent is deleted.
func TestDeleteAgent_ClearsNodeRefs(t *testing.T) {
	s, db := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	agent := createTestAgent(t, s, ws.ID)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	desc := "desc"
	task, createdNodes, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    proj.ID,
		Title:        "cleanup test task",
		Description:  &desc,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "agent",
		AuthorID:     agent.ID,
		WorkflowName: "test-flow",
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Point the node assignee to the Agent about to be deleted
	if _, err := db.Exec(
		`UPDATE task_nodes SET assignee_id = $1 WHERE id = $2`, agent.ID, createdNodes[0].ID); err != nil {
		t.Fatalf("set node assignee: %v", err)
	}

	if err := s.DeleteAgent(ctx, uuid.MustParse(agent.ID)); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	var assignee sql.NullString
	if err := db.QueryRow(`SELECT assignee_id FROM task_nodes WHERE id = $1`, createdNodes[0].ID).Scan(&assignee); err != nil {
		t.Fatalf("query node assignee: %v", err)
	}
	if assignee.Valid {
		t.Fatalf("expected task_nodes.assignee_id to be NULL, got %q", assignee.String)
	}

	// The task itself should still exist (nodes preserved, only references cleared)
	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes after DeleteAgent: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes to remain, got %d", len(nodes))
	}
}

// TestDeleteProject_ClearsMemorySourceTask verifies that memories.source_task_id is explicitly set to NULL when a project is deleted.
func TestDeleteProject_ClearsMemorySourceTask(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 1)

	desc := "desc"
	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:    proj.ID,
		Title:        "mem ref task",
		Description:  &desc,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "member",
		AuthorID:     uuid.New().String(),
		WorkflowName: "test-flow",
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	mem, err := s.CreateMemory(ctx, types.CreateMemoryParams{
		WorkspaceID:  ws.ID,
		SourceTaskID: &task.ID,
		Type:         "decision",
		Title:        "ref memory",
		Content:      "referencing task",
		Confidence:   0.5,
		Metadata:     json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("CreateMemory: %v", err)
	}

	if err := s.DeleteProject(ctx, uuid.MustParse(proj.ID)); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	mem2, err := s.GetMemory(ctx, uuid.MustParse(mem.ID))
	if err != nil {
		t.Fatalf("GetMemory after DeleteProject: %v", err)
	}
	if mem2.SourceTaskID != nil {
		t.Fatalf("expected memories.source_task_id to be NULL, got %d", *mem2.SourceTaskID)
	}
}

// TestDeleteMember_ClearsGitCredentialCreatedBy verifies that
// git_credentials.created_by is explicitly set to NULL when a member is deleted.
// Deleting a member must NOT delete the credential itself (only agent deletion does).
func TestDeleteMember_ClearsGitCredentialCreatedBy(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)
	agent := createTestAgent(t, s, ws.ID)
	proj := createTestProject(t, s, ws.ID)

	cred, err := s.CreateGitCredential(ctx, types.CreateGitCredentialParams{
		ProjectID:    proj.ID,
		AgentID:      agent.ID,
		Username:     "git",
		EncryptedPAT: "encrypted-pat",
		CreatedBy:    &member.ID,
	})
	if err != nil {
		t.Fatalf("CreateGitCredential: %v", err)
	}

	if err := s.DeleteMember(ctx, uuid.MustParse(member.ID)); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}

	cred2, err := s.GetGitCredential(ctx, uuid.MustParse(cred.ID))
	if err != nil {
		t.Fatalf("GetGitCredential after DeleteMember: %v", err)
	}
	if cred2.CreatedBy != nil {
		t.Fatalf("expected git_credentials.created_by to be NULL, got %q", *cred2.CreatedBy)
	}
}

// TestDeleteAgent_DeletesGitCredentials verifies that git_credentials rows
// bound to the deleted agent are removed with it (agent_id has no foreign key;
// the delete transaction cleans them up explicitly), while other agents'
// credentials are untouched.
func TestDeleteAgent_DeletesGitCredentials(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	agentA := createTestAgent(t, s, ws.ID)
	agentB := createTestAgent(t, s, ws.ID)
	proj := createTestProject(t, s, ws.ID)

	credA, err := s.CreateGitCredential(ctx, types.CreateGitCredentialParams{
		ProjectID:    proj.ID,
		AgentID:      agentA.ID,
		Username:     "git",
		EncryptedPAT: "encrypted-pat-a",
	})
	if err != nil {
		t.Fatalf("CreateGitCredential A: %v", err)
	}
	credB, err := s.CreateGitCredential(ctx, types.CreateGitCredentialParams{
		ProjectID:    proj.ID,
		AgentID:      agentB.ID,
		Username:     "git",
		EncryptedPAT: "encrypted-pat-b",
	})
	if err != nil {
		t.Fatalf("CreateGitCredential B: %v", err)
	}

	if err := s.DeleteAgent(ctx, uuid.MustParse(agentA.ID)); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}

	creds, err := s.GetGitCredentialsByProjectAndAgent(ctx, uuid.MustParse(proj.ID), uuid.MustParse(credA.AgentID))
	if err != nil {
		t.Fatalf("GetGitCredentialsByProjectAndAgent after DeleteAgent: %v", err)
	}
	if len(creds) != 0 {
		t.Fatalf("expected deleted agent's credentials to be gone, got %d", len(creds))
	}

	credB2, err := s.GetGitCredential(ctx, uuid.MustParse(credB.ID))
	if err != nil {
		t.Fatalf("GetGitCredential B after DeleteAgent: %v", err)
	}
	if credB2.AgentID != agentB.ID {
		t.Fatalf("expected surviving credential to stay bound to agent B, got %q", credB2.AgentID)
	}
}

// TestDeleteMember_ClearsInvitationsInvitedBy verifies that invitations.invited_by is explicitly set to NULL when a member is deleted.
func TestDeleteMember_ClearsInvitationsInvitedBy(t *testing.T) {
	s, db := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	var invitedBy sql.NullString
	if err := db.QueryRow(
		`INSERT INTO invitations (workspace_id, email, role, token_hash, invited_by, expires_at)
		 VALUES ($1, 'invite@example.com', 'member', 'tok-' || md5(random()::text), $2, now() + interval '1 day')
		 RETURNING invited_by`,
		ws.ID, member.ID).Scan(&invitedBy); err != nil {
		t.Fatalf("insert invitation: %v", err)
	}
	if !invitedBy.Valid || invitedBy.String != member.ID {
		t.Fatalf("expected invited_by = member ID, got %+v", invitedBy)
	}

	if err := s.DeleteMember(ctx, uuid.MustParse(member.ID)); err != nil {
		t.Fatalf("DeleteMember: %v", err)
	}

	var cleared sql.NullString
	if err := db.QueryRow(
		`SELECT invited_by FROM invitations WHERE workspace_id = $1 AND email = 'invite@example.com'`,
		ws.ID).Scan(&cleared); err != nil {
		t.Fatalf("query invitation after DeleteMember: %v", err)
	}
	if cleared.Valid {
		t.Fatalf("expected invitations.invited_by to be NULL, got %q", cleared.String)
	}
}

var _ = store.New // Keep import reference (consistent with setupTestStore in helpers)
