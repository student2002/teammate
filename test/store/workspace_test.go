// workspace_test.go tests for workspace data access.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestSeedBuiltinTemplates verifies that a created workspace automatically
// includes the V2 built-in template (trigger.manual → agent.task → flow.end)
// with its full node/edge graph.
func TestSeedBuiltinTemplates(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)

	// Verify the built-in template has been created
	templates, err := s.ListWorkflowTemplates(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListWorkflowTemplates: %v", err)
	}
	if len(templates) != 1 {
		t.Fatalf("expected 1 built-in template, got %d", len(templates))
	}

	// Verify the template graph: 3 nodes chained by 3 edges
	tpl := templates[0]
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes for %s: %v", tpl.Name, err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected 3 built-in template nodes, got %d", len(nodes))
	}
	edges, err := s.ListTemplateEdges(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateEdges for %s: %v", tpl.Name, err)
	}
	if len(edges) != 3 {
		t.Fatalf("expected 3 built-in template edges, got %d", len(edges))
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
