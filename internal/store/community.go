// community.go provides data access operations for community workflows.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CreateCommunityWorkflow creates a community workflow.
func (s *Store) CreateCommunityWorkflow(ctx context.Context, params types.CreateCommunityWorkflowParams) (types.CommunityWorkflow, error) {
	dbParams, err := FromDomainCreateCommunityWorkflowParams(params)
	if err != nil {
		return types.CommunityWorkflow{}, fmt.Errorf("convert create community workflow params: %w", err)
	}
	wf, err := s.q.CreateCommunityWorkflow(ctx, dbParams)
	if err != nil {
		return types.CommunityWorkflow{}, fmt.Errorf("create community workflow: %w", err)
	}
	return ToDomainCommunityWorkflow(wf)
}

// ListCommunityWorkflows lists all community workflows.
func (s *Store) ListCommunityWorkflows(ctx context.Context) ([]types.CommunityWorkflow, error) {
	wfs, err := s.q.ListCommunityWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list community workflows: %w", err)
	}
	return ToDomainCommunityWorkflowSlice(wfs)
}

// GetCommunityWorkflow gets a community workflow by ID.
func (s *Store) GetCommunityWorkflow(ctx context.Context, id uuid.UUID) (types.CommunityWorkflow, error) {
	wf, err := s.q.GetCommunityWorkflow(ctx, id)
	if err != nil {
		return types.CommunityWorkflow{}, fmt.Errorf("get community workflow: %w", err)
	}
	return ToDomainCommunityWorkflow(wf)
}
