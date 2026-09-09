// workspace_test.go tests for workspace data access.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestSeedBuiltinTemplates verifies that a created workspace automatically includes 5 built-in templates, each with nodes.
func TestSeedBuiltinTemplates(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// Verify 5 built-in templates have been created
	templates, err := s.ListWorkflowTemplates(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListWorkflowTemplates: %v", err)
	}
	if len(templates) != 5 {
		t.Fatalf("expected 5 built-in templates, got %d", len(templates))
	}

	// Verify each template has nodes
	for _, tpl := range templates {
		nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
		if err != nil {
			t.Fatalf("ListTemplateNodes for %s: %v", tpl.Name, err)
		}
		if len(nodes) == 0 {
			t.Fatalf("template %s has no nodes", tpl.Name)
		}
	}
}

// TestGetWorkspaceOwner verifies fetching workspace owner functionality.
func TestGetWorkspaceOwner(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	// Set this member as owner
	_, err := s.UpdateMemberRole(ctx, types.UpdateMemberRoleParams{
		WorkspaceID: ws.ID,
		MemberID:    member.ID,
		Role:        "owner",
	})
	if err != nil {
		t.Fatalf("UpdateMemberRole: %v", err)
	}

	owner, err := s.GetWorkspaceOwner(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("GetWorkspaceOwner: %v", err)
	}
	if owner.WorkspaceRole != "owner" {
		t.Fatalf("expected role 'owner', got %s", owner.WorkspaceRole)
	}
}

// TestGetWorkspaceOwner_NoOwner verifies that an error is returned when a workspace has no owner.
func TestGetWorkspaceOwner_NoOwner(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws, err := s.CreateWorkspace(ctx, types.CreateWorkspaceParams{
		Name:        "no-owner-ws-" + uuid.New().String()[:8],
		IssuePrefix: "NOW",
	})
	if err != nil {
		t.Fatalf("CreateWorkspace: %v", err)
	}

	// Create a non-owner member
	nonOwner, err := s.CreateMember(ctx, types.CreateMemberParams{
		Name:  "member",
		Email: "member-" + uuid.New().String()[:8] + "@test.com",
	})
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	// Add member to workspace with role 'member' (not owner)
	_, err = s.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    nonOwner.ID,
		Role:        "member",
	})
	if err != nil {
		t.Fatalf("CreateWorkspaceMember: %v", err)
	}

	_, err = s.GetWorkspaceOwner(ctx, uuid.MustParse(ws.ID))
	if err == nil {
		t.Fatal("expected error when no owner exists")
	}
}
