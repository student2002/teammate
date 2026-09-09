// workflow.go implements the business logic for workflow template management, including
// creation, querying, updating, and deletion of templates, as well as management of
// template nodes. A workflow template defines the ordered node structure of a task,
// e.g. [implement → self-test → review → deploy]. When creating a task, the actual
// workflow nodes are generated based on the template.
//
// Design notes:
//   - Nodes are sorted by sort_order to guarantee workflow execution order
//   - UpdateWithNodes deletes old nodes before creating new ones, ensuring atomic replacement of the node list
//   - When creating a task, TaskService generates the actual TaskNode records based on template nodes
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// WorkflowService provides the business logic for workflow template management.
type WorkflowService struct {
	svc *Service
}

func NewWorkflowService(svc *Service) *WorkflowService {
	return &WorkflowService{svc: svc}
}

// CreateWorkflowResult saves the result of the create-workflow-template operation.
type CreateWorkflowResult struct {
	Template types.WorkflowTemplate        // Workflow template info
	Nodes    []types.WorkflowTemplateNode  // Template node list (ordered)
}

// Create creates a workflow template and its nodes. Nodes are sorted by sort_order.
//
// The input nodes use types.CreateTemplateNodeParams; the Store layer internally
// converts them into database parameters.
func (s *WorkflowService) Create(ctx context.Context, params types.CreateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams) (*CreateWorkflowResult, error) {
	template, createdNodes, err := s.svc.Store.CreateWorkflowTemplate(ctx, params, nodes)
	if err != nil {
		return nil, fmt.Errorf("create workflow template: %w", err)
	}
	return &CreateWorkflowResult{Template: template, Nodes: createdNodes}, nil
}

// GetTemplate fetches a workflow template by ID (without nodes).
func (s *WorkflowService) GetTemplate(ctx context.Context, id uuid.UUID) (types.WorkflowTemplate, error) {
	return s.svc.Store.GetWorkflowTemplate(ctx, id)
}

// Get fetches a workflow template and its nodes by ID.
func (s *WorkflowService) Get(ctx context.Context, id uuid.UUID) (*CreateWorkflowResult, error) {
	template, err := s.svc.Store.GetWorkflowTemplate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get workflow template: %w", err)
	}
	nodes, err := s.svc.Store.ListTemplateNodes(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}
	return &CreateWorkflowResult{Template: template, Nodes: nodes}, nil
}

// List lists all workflow templates and their nodes for the given workspace.
func (s *WorkflowService) List(ctx context.Context, workspaceID uuid.UUID) ([]CreateWorkflowResult, error) {
	templates, err := s.svc.Store.ListWorkflowTemplates(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workflow templates: %w", err)
	}

	result := make([]CreateWorkflowResult, 0, len(templates))
	for _, tpl := range templates {
		tplID, _ := uuid.Parse(tpl.ID)
		nodes, _ := s.svc.Store.ListTemplateNodes(ctx, tplID)
		if nodes == nil {
			nodes = []types.WorkflowTemplateNode{}
		}
		result = append(result, CreateWorkflowResult{
			Template: tpl,
			Nodes:    nodes,
		})
	}
	return result, nil
}

// Update updates the basic info of a workflow template (name, description, etc.), without modifying nodes.
func (s *WorkflowService) Update(ctx context.Context, params types.UpdateWorkflowTemplateParams) (types.WorkflowTemplate, error) {
	return s.svc.Store.UpdateWorkflowTemplate(ctx, params)
}

// UpdateWithNodes updates a workflow template and replaces all of its nodes.
// Old nodes are deleted first, then new nodes are created, ensuring atomic replacement of the node list.
//
// The input nodes use types.CreateTemplateNodeParams; the Store layer internally
// converts them into database parameters.
func (s *WorkflowService) UpdateWithNodes(ctx context.Context, params types.UpdateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams) (*CreateWorkflowResult, error) {
	template, createdNodes, err := s.svc.Store.UpdateWorkflowTemplateWithNodes(ctx, params, nodes)
	if err != nil {
		return nil, err
	}
	return &CreateWorkflowResult{
		Template: template,
		Nodes:    createdNodes,
	}, nil
}

// Delete deletes a workflow template and all of its nodes.
func (s *WorkflowService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteWorkflowTemplate(ctx, id)
}
