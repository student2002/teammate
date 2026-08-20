// workflow_trigger.go 提供工作流触发器运行记录的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateWorkflowTriggerRun 创建一条工作流触发器运行记录。
func (s *Store) CreateWorkflowTriggerRun(ctx context.Context, params types.CreateWorkflowTriggerRunParams) (types.WorkflowTriggerRun, error) {
	dbParams, err := FromDomainCreateWorkflowTriggerRunParams(params)
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("convert create workflow trigger run params: %w", err)
	}
	run, err := s.q.CreateWorkflowTriggerRun(ctx, dbParams)
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("create workflow trigger run: %w", err)
	}
	return ToDomainWorkflowTriggerRun(run)
}

// GetWorkflowTriggerRunByExternalKey 根据模板 ID 和外部去重键查询运行记录。
func (s *Store) GetWorkflowTriggerRunByExternalKey(ctx context.Context, templateID uuid.UUID, externalKey string) (types.WorkflowTriggerRun, error) {
	run, err := s.q.GetWorkflowTriggerRunByExternalKey(ctx, db.GetWorkflowTriggerRunByExternalKeyParams{
		WorkflowTemplateID: templateID,
		ExternalKey:        externalKey,
	})
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("get workflow trigger run by external key: %w", err)
	}
	return ToDomainWorkflowTriggerRun(run)
}

// MarkWorkflowTriggerRunCompleted 将触发器运行记录标记为已完成。
func (s *Store) MarkWorkflowTriggerRunCompleted(ctx context.Context, params types.MarkWorkflowTriggerRunCompletedParams) (types.WorkflowTriggerRun, error) {
	dbParams, err := FromDomainMarkWorkflowTriggerRunCompletedParams(params)
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("convert mark workflow trigger run completed params: %w", err)
	}
	run, err := s.q.MarkWorkflowTriggerRunCompleted(ctx, dbParams)
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("mark workflow trigger run completed: %w", err)
	}
	return ToDomainWorkflowTriggerRun(run)
}

// MarkWorkflowTriggerRunFailed 将触发器运行记录标记为已失败。
func (s *Store) MarkWorkflowTriggerRunFailed(ctx context.Context, params types.MarkWorkflowTriggerRunFailedParams) (types.WorkflowTriggerRun, error) {
	dbParams, err := FromDomainMarkWorkflowTriggerRunFailedParams(params)
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("convert mark workflow trigger run failed params: %w", err)
	}
	run, err := s.q.MarkWorkflowTriggerRunFailed(ctx, dbParams)
	if err != nil {
		return types.WorkflowTriggerRun{}, fmt.Errorf("mark workflow trigger run failed: %w", err)
	}
	return ToDomainWorkflowTriggerRun(run)
}

// ListDueScheduledWorkflowTemplates 查询所有到期的调度触发器模板。
func (s *Store) ListDueScheduledWorkflowTemplates(ctx context.Context, params types.ListDueScheduledWorkflowTemplatesParams) ([]types.WorkflowTemplate, error) {
	dbParams, err := FromDomainListDueScheduledWorkflowTemplatesParams(params)
	if err != nil {
		return nil, fmt.Errorf("convert list due scheduled workflow templates params: %w", err)
	}
	templates, err := s.q.ListDueScheduledWorkflowTemplates(ctx, dbParams)
	if err != nil {
		return nil, fmt.Errorf("list due scheduled workflow templates: %w", err)
	}
	return ToDomainWorkflowTemplateSlice(templates)
}

// ListGithubIssueWorkflowTemplatesByRepo 查询指定仓库的 GitHub Issue 触发器模板。
func (s *Store) ListGithubIssueWorkflowTemplatesByRepo(ctx context.Context, owner, repo string) ([]types.WorkflowTemplate, error) {
	templates, err := s.q.ListGithubIssueWorkflowTemplatesByRepo(ctx, db.ListGithubIssueWorkflowTemplatesByRepoParams{
		Lower:   owner,
		Lower_2: repo,
	})
	if err != nil {
		return nil, fmt.Errorf("list github issue workflow templates by repo: %w", err)
	}
	return ToDomainWorkflowTemplateSlice(templates)
}

// UpdateWorkflowTemplateTriggerSchedule 更新模板的触发调度时间。
func (s *Store) UpdateWorkflowTemplateTriggerSchedule(ctx context.Context, params types.UpdateWorkflowTemplateTriggerScheduleParams) (types.WorkflowTemplate, error) {
	dbParams, err := FromDomainUpdateWorkflowTemplateTriggerScheduleParams(params)
	if err != nil {
		return types.WorkflowTemplate{}, fmt.Errorf("convert update workflow template trigger schedule params: %w", err)
	}
	template, err := s.q.UpdateWorkflowTemplateTriggerSchedule(ctx, dbParams)
	if err != nil {
		return types.WorkflowTemplate{}, fmt.Errorf("update workflow template trigger schedule: %w", err)
	}
	return ToDomainWorkflowTemplate(template)
}
