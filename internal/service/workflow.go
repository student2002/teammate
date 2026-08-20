// workflow.go 实现工作流模板管理的业务逻辑，包括模板的创建、查询、更新、删除，
// 以及模板节点的管理。工作流模板定义了任务的有序节点结构，
// 例如：[实现 → 自测 → 审查 → 部署]。创建任务时根据模板生成实际的工作流节点。
//
// 设计说明：
//   - 节点按 sort_order 排序，保证工作流的执行顺序
//   - UpdateWithNodes 先删除旧节点再创建新节点，保证节点列表的原子性替换
//   - 创建任务时，TaskService 根据模板节点生成实际的 TaskNode 记录
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// WorkflowService 提供工作流模板管理相关的业务逻辑。
type WorkflowService struct {
	svc *Service
}

func NewWorkflowService(svc *Service) *WorkflowService {
	return &WorkflowService{svc: svc}
}

// CreateWorkflowResult 保存创建工作流模板操作的结果。
type CreateWorkflowResult struct {
	Template types.WorkflowTemplate        // 工作流模板信息
	Nodes    []types.WorkflowTemplateNode  // 模板节点列表（有序）
}

// Create 创建一个工作流模板及其节点。节点按 sort_order 排序。
//
// 入参 nodes 使用 types.CreateTemplateNodeParams，Store 层内部转换为数据库参数。
func (s *WorkflowService) Create(ctx context.Context, params types.CreateWorkflowTemplateParams, nodes []types.CreateTemplateNodeParams) (*CreateWorkflowResult, error) {
	template, createdNodes, err := s.svc.Store.CreateWorkflowTemplate(ctx, params, nodes)
	if err != nil {
		return nil, fmt.Errorf("create workflow template: %w", err)
	}
	return &CreateWorkflowResult{Template: template, Nodes: createdNodes}, nil
}

// GetTemplate 根据 ID 获取工作流模板（不包含节点）。
func (s *WorkflowService) GetTemplate(ctx context.Context, id uuid.UUID) (types.WorkflowTemplate, error) {
	return s.svc.Store.GetWorkflowTemplate(ctx, id)
}

// Get 根据 ID 获取工作流模板及其节点。
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

// List 列出指定工作区的所有工作流模板及其节点。
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

// Update 更新工作流模板的基本信息（名称、描述等），不修改节点。
func (s *WorkflowService) Update(ctx context.Context, params types.UpdateWorkflowTemplateParams) (types.WorkflowTemplate, error) {
	return s.svc.Store.UpdateWorkflowTemplate(ctx, params)
}

// UpdateWithNodes 更新工作流模板并替换其所有节点。
// 先删除旧节点，再创建新节点，保证节点列表的原子性替换。
//
// 入参 nodes 使用 types.CreateTemplateNodeParams，Store 层内部转换为数据库参数。
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

// Delete 删除一个工作流模板及其所有节点。
func (s *WorkflowService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteWorkflowTemplate(ctx, id)
}
