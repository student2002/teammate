// audit_test.go 覆盖审计日志数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestAuditLogCRUD 验证审计日志的创建和列表查询功能。
func TestAuditLogCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	log, err := s.CreateAuditLog(ctx, types.CreateAuditLogParams{
		WorkspaceID:  ws.ID,
		Action:       "workspace.create",
		ActorType:    "member",
		ActorID:      uuid.New().String(),
		ResourceType: "workspace",
		ResourceID:   ws.ID,
	})
	if err != nil {
		t.Fatalf("CreateAuditLog: %v", err)
	}
	if log.Action != "workspace.create" {
		t.Fatalf("expected action 'workspace.create', got %s", log.Action)
	}

	logs, err := s.ListAuditLogs(ctx, uuid.MustParse(ws.ID), 10, 0)
	if err != nil {
		t.Fatalf("ListAuditLogs: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 audit log, got %d", len(logs))
	}
}
