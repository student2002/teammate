// workflow.go provides data access operations for workflow templates.
//
// A Workflow Template defines the ordered node steps for task execution,
// e.g.: requirements analysis → technical design → coding → review → deployment.
//
// A template contains metadata and a list of template nodes; each template node defines the
// name, type, assignee type, timeout, etc. for each step.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateWorkflowTemplate creates a workflow template and its template nodes within a transaction.
//
// Note: the nodes parameter uses db.CreateTemplateNodeParams (within the transaction, n.TemplateID = template.ID must be assigned).
// If this is changed to types.CreateTemplateNodeParams in the future, type conversion must be done inside the loop.
//
// Returns:
//   - types.WorkflowTemplate: the created template record
//   - []types.WorkflowTemplateNode: list of created template nodes
//   - error: error if creation fails
func (s *Store) CreateWorkflowTemplate(ctx context.Context, params types.CreateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams) (types.WorkflowTemplate, []types.WorkflowTemplateNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	dbParams, err := FromDomainCreateWorkflowTemplateParams(params)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("convert create workflow template params: %w", err)
	}
	dbParams = normalizeCreateWorkflowTemplateParams(dbParams)
	template, err := qtx.CreateWorkflowTemplate(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("create workflow template: %w", err)
	}

	createdNodes := make([]db.WorkflowTemplateNode, 0, len(nodes))
	for _, n := range nodes {
		dbNode, err := FromDomainCreateTemplateNodeParams(n)
		if err != nil {
			return types.WorkflowTemplate{}, nil, fmt.Errorf("convert create template node params: %w", err)
		}
		dbNode.TemplateID = template.ID
		node, err := qtx.CreateTemplateNode(ctx, dbNode)
		if err != nil {
			return types.WorkflowTemplate{}, nil, fmt.Errorf("create template node: %w", err)
		}
		createdNodes = append(createdNodes, node)
	}

	if err := tx.Commit(); err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("commit tx: %w", err)
	}

	domainTpl, err := ToDomainWorkflowTemplate(template)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("convert template to domain: %w", err)
	}
	domainNodes, err := ToDomainWorkflowTemplateNodeSlice(createdNodes)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("convert template nodes to domain: %w", err)
	}
	return domainTpl, domainNodes, nil
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

// UpdateWorkflowTemplate updates the metadata of a workflow template.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters
//
// Returns:
//   - types.WorkflowTemplate: the updated template record
//   - error: error if the update fails
func (s *Store) UpdateWorkflowTemplate(ctx context.Context, params types.UpdateWorkflowTemplateParams) (types.WorkflowTemplate, error) {
	dbParams, err := FromDomainUpdateWorkflowTemplateParams(params)
	if err != nil {
		return types.WorkflowTemplate{}, fmt.Errorf("convert update workflow template params: %w", err)
	}
	dbParams, err = normalizeUpdateWorkflowTemplateParams(ctx, s, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, err
	}
	tpl, err := s.q.UpdateWorkflowTemplate(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, fmt.Errorf("update workflow template: %w", err)
	}
	return ToDomainWorkflowTemplate(tpl)
}

// UpdateWorkflowTemplateWithNodes updates a workflow template and replaces all nodes within a transaction.
//
// Note: the nodes parameter uses types.CreateTemplateNodeParams; within the transaction it is converted to db and then assigned TemplateID.
func (s *Store) UpdateWorkflowTemplateWithNodes(ctx context.Context, params types.UpdateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams) (types.WorkflowTemplate, []types.WorkflowTemplateNode, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	dbParams, err := FromDomainUpdateWorkflowTemplateParams(params)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("convert update workflow template params: %w", err)
	}
	dbParams, err = normalizeUpdateWorkflowTemplateParams(ctx, s, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, nil, err
	}
	// Update template metadata
	template, err := qtx.UpdateWorkflowTemplate(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("update workflow template: %w", err)
	}

	// Delete all old nodes (no foreign key constraint prevents this operation)
	if err := qtx.DeleteTemplateNodesByTemplate(ctx, dbParams.ID); err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("delete old template nodes: %w", err)
	}

	// Create new nodes
	createdNodes := make([]db.WorkflowTemplateNode, 0, len(nodes))
	for _, n := range nodes {
		dbNode, err := FromDomainCreateTemplateNodeParams(n)
		if err != nil {
			return types.WorkflowTemplate{}, nil, fmt.Errorf("convert create template node params: %w", err)
		}
		dbNode.TemplateID = template.ID
		node, err := qtx.CreateTemplateNode(ctx, dbNode)
		if err != nil {
			return types.WorkflowTemplate{}, nil, fmt.Errorf("create template node: %w", err)
		}
		createdNodes = append(createdNodes, node)
	}

	if err := tx.Commit(); err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("commit tx: %w", err)
	}

	domainTpl, err := ToDomainWorkflowTemplate(template)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("convert template to domain: %w", err)
	}
	domainNodes, err := ToDomainWorkflowTemplateNodeSlice(createdNodes)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("convert template nodes to domain: %w", err)
	}
	return domainTpl, domainNodes, nil
}

func normalizeCreateWorkflowTemplateParams(params db.CreateWorkflowTemplateParams) db.CreateWorkflowTemplateParams {
	if params.TriggerType == "" {
		params.TriggerType = db.WorkflowTriggerTypeManual
	}
	if len(params.TriggerConfig) == 0 {
		params.TriggerConfig = json.RawMessage(`{}`)
	}
	if !params.TriggerEnabled && params.TriggerType == db.WorkflowTriggerTypeManual {
		params.TriggerEnabled = true
	}
	return params
}

func normalizeUpdateWorkflowTemplateParams(ctx context.Context, s *Store, params db.UpdateWorkflowTemplateParams) (db.UpdateWorkflowTemplateParams, error) {
	if params.TriggerType != "" {
		if len(params.TriggerConfig) == 0 {
			params.TriggerConfig = json.RawMessage(`{}`)
		}
		return params, nil
	}

	existing, err := s.GetWorkflowTemplate(ctx, params.ID)
	if err != nil {
		return params, fmt.Errorf("get workflow template for trigger defaults: %w", err)
	}
	params.TriggerType = db.WorkflowTriggerType(existing.TriggerType)
	params.TriggerConfig = existing.TriggerConfig
	params.TriggerEnabled = existing.TriggerEnabled
	params.NextRunAt = ptrToNullTime(existing.NextRunAt)
	if params.NextRunAt.Valid && params.NextRunAt.Time.IsZero() {
		params.NextRunAt = sql.NullTime{}
	}
	return params, nil
}

// DeleteWorkflowTemplate deletes a workflow template and all its nodes within a transaction.
func (s *Store) DeleteWorkflowTemplate(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	// After 002_remove_fks, projects.default_workflow_id no longer has a foreign key; it must be explicitly set to NULL
	if _, err := tx.ExecContext(ctx,
		`UPDATE projects SET default_workflow_id = NULL WHERE default_workflow_id = $1`, id); err != nil {
		return fmt.Errorf("clear projects.default_workflow_id: %w", err)
	}

	if err := qtx.DeleteTemplateNodesByTemplate(ctx, id); err != nil {
		return fmt.Errorf("delete template nodes: %w", err)
	}

	if err := qtx.DeleteWorkflowTemplate(ctx, id); err != nil {
		return fmt.Errorf("delete workflow template: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
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
