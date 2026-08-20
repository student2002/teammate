// workflow.go 提供工作流模板的数据访问操作。
//
// 工作流模板（Workflow Template）定义了任务执行的有序节点步骤，
// 如：需求分析 → 技术设计 → 编码实现 → 审查 → 部署。
//
// 模板包含元数据和模板节点列表，模板节点定义了每个步骤的
// 名称、类型、分配者类型、超时时间等。
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

// CreateWorkflowTemplate 在事务中创建工作流模板及其模板节点。
//
// 注意：入参 nodes 用 db.CreateTemplateNodeParams（事务内部需 n.TemplateID = template.ID 赋值）。
// 未来若改用 types.CreateTemplateNodeParams，需在循环内做类型转换。
//
// 返回：
//   - types.WorkflowTemplate: 创建的模板记录
//   - []types.WorkflowTemplateNode: 创建的模板节点列表
//   - error: 创建失败时返回错误
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

// GetWorkflowTemplate 根据 ID 查询单个工作流模板记录。
func (s *Store) GetWorkflowTemplate(ctx context.Context, id uuid.UUID) (types.WorkflowTemplate, error) {
	tpl, err := s.q.GetWorkflowTemplate(ctx, id)
	if err != nil {
		return types.WorkflowTemplate{}, fmt.Errorf("get workflow template: %w", err)
	}
	return ToDomainWorkflowTemplate(tpl)
}

// ListWorkflowTemplates 查询指定工作区内的所有工作流模板。
func (s *Store) ListWorkflowTemplates(ctx context.Context, workspaceID uuid.UUID) ([]types.WorkflowTemplate, error) {
	templates, err := s.q.ListWorkflowTemplates(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workflow templates: %w", err)
	}
	return ToDomainWorkflowTemplateSlice(templates)
}

// UpdateWorkflowTemplate 更新工作流模板的元数据。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数
//
// 返回：
//   - types.WorkflowTemplate: 更新后的模板记录
//   - error: 更新失败时返回错误
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

// UpdateWorkflowTemplateWithNodes 在事务中更新工作流模板并替换所有节点。
//
// 注意：入参 nodes 用 types.CreateTemplateNodeParams，事务内部转换为 db 后赋值 TemplateID。
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
	// 更新模板元数据
	template, err := qtx.UpdateWorkflowTemplate(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("update workflow template: %w", err)
	}

	// 删除所有旧节点（没有外键约束阻止此操作）
	if err := qtx.DeleteTemplateNodesByTemplate(ctx, dbParams.ID); err != nil {
		return types.WorkflowTemplate{}, nil, fmt.Errorf("delete old template nodes: %w", err)
	}

	// 创建新节点
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

// DeleteWorkflowTemplate 在事务中删除工作流模板及其所有节点。
func (s *Store) DeleteWorkflowTemplate(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	// 002_remove_fks 后 projects.default_workflow_id 不再有外键，须显式置空
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

// ListTemplateNodes 查询指定模板的所有模板节点。
func (s *Store) ListTemplateNodes(ctx context.Context, templateID uuid.UUID) ([]types.WorkflowTemplateNode, error) {
	nodes, err := s.q.ListTemplateNodes(ctx, templateID)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}
	return ToDomainWorkflowTemplateNodeSlice(nodes)
}
