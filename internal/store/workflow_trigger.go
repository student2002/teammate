// workflow_trigger.go provides data access operations for workflow trigger run records.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateWorkflowTriggerRun creates a workflow trigger run record.
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

// GetWorkflowTriggerRunByExternalKey queries a run record by template ID and external deduplication key.
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

// MarkWorkflowTriggerRunCompleted marks a trigger run record as completed.
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

// MarkWorkflowTriggerRunFailed marks a trigger run record as failed.
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

// ListDueScheduledWorkflowTemplates queries all due scheduled-trigger templates.
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

// ListGithubIssueWorkflowTemplatesByRepo queries the GitHub Issue trigger templates for the specified repository.
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

// UpdateWorkflowTemplateTriggerSchedule updates the trigger schedule time of a template.
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
