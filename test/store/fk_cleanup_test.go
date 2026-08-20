// fk_cleanup_test.go 覆盖 002_remove_fks 后 store 删除事务的显式置空行为
// （FK 策略：外键移除后由应用层保证完整性，见 docs/数据存储设计.md 与
// docs/实现与设计偏差记录.md #4）。
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

// TestDeleteWorkflowTemplate_ClearsProjectDefaultWorkflow 验证删除模板时
// projects.default_workflow_id 被显式置空——否则外键（NO ACTION）会阻止删除。
func TestDeleteWorkflowTemplate_ClearsProjectDefaultWorkflow(t *testing.T) {
	s, db := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 1)
	proj := createTestProject(t, s, ws.ID)

	// 将项目默认模板指向将被删除的模板
	if _, err := db.Exec(
		`UPDATE projects SET default_workflow_id = $1 WHERE id = $2`, tpl.ID, proj.ID); err != nil {
		t.Fatalf("set default_workflow_id: %v", err)
	}

	if err := s.DeleteWorkflowTemplate(ctx, uuid.MustParse(tpl.ID)); err != nil {
		t.Fatalf("DeleteWorkflowTemplate with project reference should succeed after explicit clear: %v", err)
	}

	var dwf sql.NullString
	if err := db.QueryRow(`SELECT default_workflow_id FROM projects WHERE id = $1`, proj.ID).Scan(&dwf); err != nil {
		t.Fatalf("query project default_workflow_id: %v", err)
	}
	if dwf.Valid {
		t.Fatalf("expected projects.default_workflow_id to be NULL, got %q", dwf.String)
	}
}

// TestDeleteAgent_ClearsNodeRefs 验证删除 Agent 时 task_nodes.assignee_id 被显式置空。
func TestDeleteAgent_ClearsNodeRefs(t *testing.T) {
	s, db := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	agent, _ := createTestAgent(t, s, ws.ID)
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

	// 将节点 assignee 指向将被删除的 Agent
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

	// 任务本身仍应存在（节点保留、仅置空引用）
	nodes, err := s.ListTaskNodes(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListTaskNodes after DeleteAgent: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes to remain, got %d", len(nodes))
	}
}

// TestDeleteProject_ClearsMemorySourceTask 验证删除项目时 memories.source_task_id 被显式置空。
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

// TestDeleteMember_ClearsGitCredentialCreatedBy 验证删除成员时
// git_credentials.created_by 被显式置空。
func TestDeleteMember_ClearsGitCredentialCreatedBy(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)
	proj := createTestProject(t, s, ws.ID)

	cred, err := s.CreateGitCredential(ctx, types.CreateGitCredentialParams{
		ProjectID:    proj.ID,
		RepoURL:      "https://example.com/teammate/repo-" + uuid.New().String()[:8] + ".git",
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

// TestDeleteMember_ClearsInvitationsInvitedBy 验证删除成员时 invitations.invited_by 被显式置空。
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

var _ = store.New // 保持 import 引用（与 helpers 中 setupTestStore 一致）
