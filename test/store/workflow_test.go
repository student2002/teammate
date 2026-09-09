// workflow_test.go tests for workflow template data access.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestCreateWorkflowTemplate_WithNodes tests creating nodes alongside workflow template creation.
func TestCreateWorkflowTemplate_WithNodes(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, createdNodes := createTestWorkflowTemplate(t, s, ws.ID, 3)

	if tpl.ID == "" {
		t.Fatal("expected non-empty template ID")
	}
	if len(createdNodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(createdNodes))
	}

	// Verify nodes can be queried
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected ListTemplateNodes to return 3 nodes, got %d", len(nodes))
	}
}

// TestUpdateWorkflowTemplateWithNodes_ReplaceAll tests replacing all nodes when updating a workflow template.
func TestUpdateWorkflowTemplateWithNodes_ReplaceAll(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 3)

	// Update with 2 new nodes
	newNodes := []types.CreateTemplateNodeParams{
		{
			Name:            "new-node-1",
			SortOrder:       1,
			NodeType:        "standard",
			AssigneeType:    "any_agent",
			TimeoutMinutes:  30,
			MaxRejectCycles: 2,
		},
		{
			Name:            "new-node-2",
			SortOrder:       2,
			NodeType:        "review",
			AssigneeType:    "any_agent",
			TimeoutMinutes:  20,
			MaxRejectCycles: 2,
		},
	}

	_, updatedNodes, err := s.UpdateWorkflowTemplateWithNodes(ctx, types.UpdateWorkflowTemplateParams{
		ID:          tpl.ID,
		Name:        tpl.Name,
		Description: &tpl.Description,
	}, newNodes)
	if err != nil {
		t.Fatalf("UpdateWorkflowTemplateWithNodes: %v", err)
	}

	if len(updatedNodes) != 2 {
		t.Fatalf("expected 2 nodes after update, got %d", len(updatedNodes))
	}

	// Verify old nodes have been removed
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes after update, got %d", len(nodes))
	}

	// Verify new node names
	if nodes[0].Name != "new-node-1" || nodes[1].Name != "new-node-2" {
		t.Fatalf("unexpected node names: %s, %s", nodes[0].Name, nodes[1].Name)
	}
}

// TestDeleteWorkflowTemplate_Cascade tests cascading deletion of associated nodes when deleting a workflow template.
func TestDeleteWorkflowTemplate_Cascade(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 3)

	err := s.DeleteWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("DeleteWorkflowTemplate: %v", err)
	}

	// Verify template has been deleted
	_, err = s.GetWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err == nil {
		t.Fatal("expected error when fetching deleted template")
	}

	// Verify associated nodes have been deleted
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes after deletion, got %d", len(nodes))
	}
}
