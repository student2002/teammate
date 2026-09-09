// community.go provides the business logic for community workflows.
// Community workflows are reusable workflow templates contributed by the community that users can import into their own workspaces.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CommunityService provides the business logic for community workflows.
type CommunityService struct {
	svc *Service
}

func NewCommunityService(svc *Service) *CommunityService {
	return &CommunityService{svc: svc}
}

// Create creates a new community workflow.
func (s *CommunityService) Create(ctx context.Context, params types.CreateCommunityWorkflowParams) (types.CommunityWorkflow, error) {
	return s.svc.Store.CreateCommunityWorkflow(ctx, params)
}

// List lists all community workflows.
func (s *CommunityService) List(ctx context.Context) ([]types.CommunityWorkflow, error) {
	return s.svc.Store.ListCommunityWorkflows(ctx)
}

// Get retrieves a community workflow by ID.
func (s *CommunityService) Get(ctx context.Context, id uuid.UUID) (types.CommunityWorkflow, error) {
	return s.svc.Store.GetCommunityWorkflow(ctx, id)
}

// ImportWorkflowResult holds the result of importing a community workflow.
type ImportWorkflowResult struct {
	Template       types.WorkflowTemplate  `json:"template"`
	SourceWorkflow types.CommunityWorkflow `json:"source_workflow"`
}

// ImportWorkflow imports a community workflow into the specified workspace, creating a workflow template.
func (s *CommunityService) ImportWorkflow(ctx context.Context, id uuid.UUID, workspaceID uuid.UUID) (*ImportWorkflowResult, error) {
	cw, err := s.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get community workflow: %w", err)
	}

	var descPtr *string
	if cw.Description != "" {
		d := cw.Description
		descPtr = &d
	}
	template, _, err := s.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: workspaceID.String(),
		Name:        cw.Name,
		Description: descPtr,
		IsBuiltin:   false,
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("create workflow template: %w", err)
	}

	return &ImportWorkflowResult{
		Template:       template,
		SourceWorkflow: cw,
	}, nil
}
