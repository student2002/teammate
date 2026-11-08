// workflow.go provides data access operations for workflow templates.
//
// A Workflow Template is an event-driven graph: typed nodes (type_key) are
// connected by port-based edges. Create and update persist the whole graph
// inside one transaction; updates bump the template version atomically so
// in-flight task snapshots can pin the version they started from.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateWorkflowTemplate creates a workflow template with its full graph
// (nodes and edges) within a transaction.
//
// The nodes/edges parameters carry optional stable IDs; TemplateID is
// backfilled with the created template's ID inside the transaction.
func (s *Store) CreateWorkflowTemplate(ctx context.Context, params types.CreateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams, edges []types.CreateTemplateEdgeParams) (types.WorkflowTemplate, []types.WorkflowTemplateNode, []types.WorkflowTemplateEdge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	dbParams, err := FromDomainCreateWorkflowTemplateParams(params)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert create workflow template params: %w", err)
	}
	template, err := qtx.CreateWorkflowTemplate(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("create workflow template: %w", err)
	}

	createdNodes, err := insertTemplateNodes(ctx, qtx, template.ID, nodes)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, err
	}
	createdEdges, err := insertTemplateEdges(ctx, qtx, template.ID, edges)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	domainTpl, err := ToDomainWorkflowTemplate(template)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert template to domain: %w", err)
	}
	domainNodes, err := ToDomainWorkflowTemplateNodeSlice(createdNodes)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert template nodes to domain: %w", err)
	}
	domainEdges, err := ToDomainWorkflowTemplateEdgeSlice(createdEdges)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert template edges to domain: %w", err)
	}
	return domainTpl, domainNodes, domainEdges, nil
}

// UpdateTemplateGraph atomically replaces a template's whole graph: metadata
// write with version bump, then delete-and-reinsert of nodes and edges. Node
// and edge IDs come from the caller so references stay stable across saves.
func (s *Store) UpdateTemplateGraph(ctx context.Context, id uuid.UUID, params types.UpdateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams, edges []types.CreateTemplateEdgeParams) (types.WorkflowTemplate, []types.WorkflowTemplateNode, []types.WorkflowTemplateEdge, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	dbParams, err := FromDomainUpdateWorkflowTemplateParams(params)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert update workflow template params: %w", err)
	}
	template, err := qtx.UpdateWorkflowTemplateGraph(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("update workflow template graph: %w", err)
	}
	if err := qtx.DeleteTemplateEdgesByTemplate(ctx, id); err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("delete template edges: %w", err)
	}
	if err := qtx.DeleteTemplateNodesByTemplate(ctx, id); err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("delete template nodes: %w", err)
	}
	createdNodes, err := insertTemplateNodes(ctx, qtx, id, nodes)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, err
	}
	createdEdges, err := insertTemplateEdges(ctx, qtx, id, edges)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, err
	}

	if err := tx.Commit(); err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("commit tx: %w", err)
	}

	domainTpl, err := ToDomainWorkflowTemplate(template)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert template to domain: %w", err)
	}
	domainNodes, err := ToDomainWorkflowTemplateNodeSlice(createdNodes)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert template nodes to domain: %w", err)
	}
	domainEdges, err := ToDomainWorkflowTemplateEdgeSlice(createdEdges)
	if err != nil {
		return types.WorkflowTemplate{}, nil, nil, fmt.Errorf("convert template edges to domain: %w", err)
	}
	return domainTpl, domainNodes, domainEdges, nil
}

// insertTemplateNodes persists template nodes inside an open transaction,
// backfilling the template ID on every row.
func insertTemplateNodes(ctx context.Context, qtx *db.Queries, templateID uuid.UUID, nodes []types.CreateTemplateNodeParams) ([]db.WorkflowTemplateNode, error) {
	createdNodes := make([]db.WorkflowTemplateNode, 0, len(nodes))
	for _, n := range nodes {
		dbNode, err := FromDomainCreateTemplateNodeParams(n)
		if err != nil {
			return nil, fmt.Errorf("convert create template node params: %w", err)
		}
		dbNode.TemplateID = templateID
		node, err := qtx.CreateTemplateNode(ctx, dbNode)
		if err != nil {
			return nil, fmt.Errorf("create template node: %w", err)
		}
		createdNodes = append(createdNodes, node)
	}
	return createdNodes, nil
}

// insertTemplateEdges persists template edges inside an open transaction,
// backfilling the template ID on every row.
func insertTemplateEdges(ctx context.Context, qtx *db.Queries, templateID uuid.UUID, edges []types.CreateTemplateEdgeParams) ([]db.WorkflowTemplateEdge, error) {
	createdEdges := make([]db.WorkflowTemplateEdge, 0, len(edges))
	for _, e := range edges {
		dbEdge, err := FromDomainCreateTemplateEdgeParams(e)
		if err != nil {
			return nil, fmt.Errorf("convert create template edge params: %w", err)
		}
		dbEdge.TemplateID = templateID
		edge, err := qtx.CreateTemplateEdge(ctx, dbEdge)
		if err != nil {
			return nil, fmt.Errorf("create template edge: %w", err)
		}
		createdEdges = append(createdEdges, edge)
	}
	return createdEdges, nil
}

// GetWorkflowTemplate queries a single workflow template record by ID.
func (s *Store) GetWorkflowTemplate(ctx context.Context, id uuid.UUID) (types.WorkflowTemplate, error) {
	tpl, err := s.q.GetWorkflowTemplate(ctx, id)
	if err != nil {
		return types.WorkflowTemplate{}, fmt.Errorf("get workflow template: %w", err)
	}
	return ToDomainWorkflowTemplate(tpl)
}

// ListWorkflowTemplates queries all workflow templates within the specified workspace.
func (s *Store) ListWorkflowTemplates(ctx context.Context, workspaceID uuid.UUID) ([]types.WorkflowTemplate, error) {
	templates, err := s.q.ListWorkflowTemplates(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workflow templates: %w", err)
	}
	return ToDomainWorkflowTemplateSlice(templates)
}

// DeleteWorkflowTemplate deletes a workflow template; its nodes and edges are
// removed by the database cascade.
func (s *Store) DeleteWorkflowTemplate(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteWorkflowTemplate(ctx, id); err != nil {
		return fmt.Errorf("delete workflow template: %w", err)
	}
	return nil
}

// ListTemplateNodes queries all template nodes of the specified template.
func (s *Store) ListTemplateNodes(ctx context.Context, templateID uuid.UUID) ([]types.WorkflowTemplateNode, error) {
	nodes, err := s.q.ListTemplateNodes(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}
	return ToDomainWorkflowTemplateNodeSlice(nodes)
}

// ListTemplateEdges queries all template edges of the specified template.
func (s *Store) ListTemplateEdges(ctx context.Context, templateID uuid.UUID) ([]types.WorkflowTemplateEdge, error) {
	edges, err := s.q.ListTemplateEdges(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template edges: %w", err)
	}
	return ToDomainWorkflowTemplateEdgeSlice(edges)
}
