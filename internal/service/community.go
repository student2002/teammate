// community.go 提供社区工作流的业务逻辑。
// 社区工作流是由社区贡献的可复用工作流模板，用户可以导入到自己的工作区。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CommunityService 提供社区工作流相关的业务逻辑。
type CommunityService struct {
	svc *Service
}

func NewCommunityService(svc *Service) *CommunityService {
	return &CommunityService{svc: svc}
}

// Create 创建一个新的社区工作流。
func (s *CommunityService) Create(ctx context.Context, params types.CreateCommunityWorkflowParams) (types.CommunityWorkflow, error) {
	return s.svc.Store.CreateCommunityWorkflow(ctx, params)
}

// List 列出所有社区工作流。
func (s *CommunityService) List(ctx context.Context) ([]types.CommunityWorkflow, error) {
	return s.svc.Store.ListCommunityWorkflows(ctx)
}

// Get 根据 ID 获取社区工作流。
func (s *CommunityService) Get(ctx context.Context, id uuid.UUID) (types.CommunityWorkflow, error) {
	return s.svc.Store.GetCommunityWorkflow(ctx, id)
}

// ImportWorkflowResult 保存导入社区工作流操作的结果。
type ImportWorkflowResult struct {
	Template       types.WorkflowTemplate  `json:"template"`
	SourceWorkflow types.CommunityWorkflow `json:"source_workflow"`
}

// ImportWorkflow 将社区工作流导入到指定工作区，创建工作流模板。
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
