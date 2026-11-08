// workflow_test.go tests for workflow template data access.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TestCreateWorkflowTemplate_WithNodes tests creating nodes and edges alongside
// workflow template creation.
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

	// Verify nodes and edges can be queried
	nodes, err := s.ListTemplateNodes(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 3 {
		t.Fatalf("expected ListTemplateNodes to return 3 nodes, got %d", len(nodes))
	}
	edges, err := s.ListTemplateEdges(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("ListTemplateEdges: %v", err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected ListTemplateEdges to return 2 edges, got %d", len(edges))
	}
}

// TestUpdateTemplateGraph_ReplaceAll tests that updating a workflow template
// replaces its whole graph atomically and bumps the template version.
func TestUpdateTemplateGraph_ReplaceAll(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 3)
	tplID := uuid.MustParse(tpl.ID)

	// Replace with a 2-node linear graph
	node1ID := uuid.NewString()
	node2ID := uuid.NewString()
	newNodes := []types.CreateTemplateNodeParams{
		{ID: &node1ID, Name: "new-node-1", SortOrder: 1, TypeKey: "trigger.manual", Config: []byte(`{}`), UIConfig: []byte(`{}`)},
		{ID: &node2ID, Name: "new-node-2", SortOrder: 2, TypeKey: "flow.end", Config: []byte(`{}`), UIConfig: []byte(`{}`)},
	}
	newEdges := []types.CreateTemplateEdgeParams{
		{SourceNodeID: node1ID, SourcePort: "success", TargetNodeID: node2ID, TargetPort: "completed"},
	}

	updatedTpl, updatedNodes, updatedEdges, err := s.UpdateTemplateGraph(ctx, tplID, types.UpdateWorkflowTemplateParams{
		ID:          tpl.ID,
		Name:        tpl.Name,
		Description: &tpl.Description,
		Enabled:     true,
	}, newNodes, newEdges)
	if err != nil {
		t.Fatalf("UpdateTemplateGraph: %v", err)
	}

	if updatedTpl.Version != tpl.Version+1 {
		t.Fatalf("expected version %d after graph update, got %d", tpl.Version+1, updatedTpl.Version)
	}
	if len(updatedNodes) != 2 {
		t.Fatalf("expected 2 nodes after update, got %d", len(updatedNodes))
	}
	if len(updatedEdges) != 1 {
		t.Fatalf("expected 1 edge after update, got %d", len(updatedEdges))
	}

	// Verify the graph has been fully replaced
	nodes, err := s.ListTemplateNodes(ctx, tplID)
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes after update, got %d", len(nodes))
	}
	if nodes[0].Name != "new-node-1" || nodes[1].Name != "new-node-2" {
		t.Fatalf("unexpected node names: %s, %s", nodes[0].Name, nodes[1].Name)
	}

	edges, err := s.ListTemplateEdges(ctx, tplID)
	if err != nil {
		t.Fatalf("ListTemplateEdges: %v", err)
	}
	if len(edges) != 1 || edges[0].SourceNodeID != node1ID || edges[0].TargetNodeID != node2ID {
		t.Fatalf("unexpected edges after update: %+v", edges)
	}
}

// TestDeleteWorkflowTemplate_Cascade tests cascading deletion of associated
// nodes and edges when deleting a workflow template.
func TestDeleteWorkflowTemplate_Cascade(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 3)
	tplID := uuid.MustParse(tpl.ID)

	err := s.DeleteWorkflowTemplate(ctx, tplID)
	if err != nil {
		t.Fatalf("DeleteWorkflowTemplate: %v", err)
	}

	// Verify template has been deleted
	_, err = s.GetWorkflowTemplate(ctx, tplID)
	if err == nil {
		t.Fatal("expected error when fetching deleted template")
	}

	// Verify associated nodes and edges have been deleted
	nodes, err := s.ListTemplateNodes(ctx, tplID)
	if err != nil {
		t.Fatalf("ListTemplateNodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("expected 0 nodes after deletion, got %d", len(nodes))
	}
	edges, err := s.ListTemplateEdges(ctx, tplID)
	if err != nil {
		t.Fatalf("ListTemplateEdges: %v", err)
	}
	if len(edges) != 0 {
		t.Fatalf("expected 0 edges after deletion, got %d", len(edges))
	}
}
