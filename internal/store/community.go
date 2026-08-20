// community.go 提供社区工作流的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CreateCommunityWorkflow 创建一个社区工作流。
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

// ListCommunityWorkflows 列出所有社区工作流。
func (s *Store) ListCommunityWorkflows(ctx context.Context) ([]types.CommunityWorkflow, error) {
	wfs, err := s.q.ListCommunityWorkflows(ctx)
	if err != nil {
		return nil, fmt.Errorf("list community workflows: %w", err)
	}
	return ToDomainCommunityWorkflowSlice(wfs)
}

// GetCommunityWorkflow 根据 ID 获取社区工作流。
func (s *Store) GetCommunityWorkflow(ctx context.Context, id uuid.UUID) (types.CommunityWorkflow, error) {
	wf, err := s.q.GetCommunityWorkflow(ctx, id)
	if err != nil {
		return types.CommunityWorkflow{}, fmt.Errorf("get community workflow: %w", err)
	}
	return ToDomainCommunityWorkflow(wf)
}
