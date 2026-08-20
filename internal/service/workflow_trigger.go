// workflow_trigger.go 实现工作流触发器运行记录的业务逻辑。
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

type WorkflowTriggerService struct {
	svc *Service
}

type GitHubIssueEvent struct {
	Owner      string
	Repo       string
	Number     int
	Title      string
	Body       string
	URL        string
	Author     string
	Labels     []string
	Action     string
	RawPayload json.RawMessage
}

type workflowTriggerConfig struct {
	ProjectID       string `json:"project_id"`
	IntervalMinutes int    `json:"interval_minutes"`
	Title           string `json:"title"`
	Description     string `json:"description"`
	RepoOwner       string `json:"repo_owner"`
	RepoName        string `json:"repo_name"`
	Secret          string `json:"secret"`
}

func NewWorkflowTriggerService(svc *Service) *WorkflowTriggerService {
	return &WorkflowTriggerService{svc: svc}
}

func (s *WorkflowTriggerService) ProcessDueSchedules(ctx context.Context, now time.Time) error {
	templates, err := s.svc.Store.ListDueScheduledWorkflowTemplates(ctx, types.ListDueScheduledWorkflowTemplatesParams{
		NextRunAt: &now,
		Limit:     100,
	})
	if err != nil {
		return fmt.Errorf("list due scheduled workflow templates: %w", err)
	}

	for _, template := range templates {
		if err := s.processScheduleTemplate(ctx, template, now); err != nil {
			return err
		}
	}
	return nil
}

func (s *WorkflowTriggerService) HandleGitHubIssue(ctx context.Context, event GitHubIssueEvent) (*CreateTaskResult, error) {
	if event.Action != "opened" {
		return nil, nil
	}

	templates, err := s.svc.Store.ListGithubIssueWorkflowTemplatesByRepo(ctx, event.Owner, event.Repo)
	if err != nil {
		return nil, fmt.Errorf("list github issue workflow templates: %w", err)
	}

	var result *CreateTaskResult
	for _, template := range templates {
		created, err := s.processGitHubIssueTemplate(ctx, template, event)
		if err != nil {
			return nil, err
		}
		if created != nil && result == nil {
			result = created
		}
	}
	return result, nil
}

func (s *WorkflowTriggerService) GitHubSecretsForRepo(ctx context.Context, owner, repo string) ([]string, error) {
	templates, err := s.svc.Store.ListGithubIssueWorkflowTemplatesByRepo(ctx, owner, repo)
	if err != nil {
		return nil, fmt.Errorf("list github issue workflow templates for secrets: %w", err)
	}
	secrets := make([]string, 0, len(templates))
	for _, template := range templates {
		cfg, err := parseWorkflowTriggerConfig(template)
		if err != nil {
			return nil, err
		}
		if cfg.Secret != "" {
			secrets = append(secrets, cfg.Secret)
		}
	}
	return secrets, nil
}

func (s *WorkflowTriggerService) processScheduleTemplate(ctx context.Context, template types.WorkflowTemplate, now time.Time) error {
	cfg, err := parseWorkflowTriggerConfig(template)
	if err != nil {
		return err
	}
	if cfg.IntervalMinutes <= 0 {
		return fmt.Errorf("schedule trigger %s interval_minutes must be greater than zero", template.ID)
	}

	projectID, err := uuid.Parse(cfg.ProjectID)
	if err != nil {
		return fmt.Errorf("parse schedule trigger project_id: %w", err)
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"scheduled_at": now.Format(time.RFC3339),
		"template_id":  template.ID,
	})
	var nextRunUnix int64
	if template.NextRunAt != nil {
		nextRunUnix = template.NextRunAt.Unix()
	}
	externalKey := fmt.Sprintf("schedule:%s:%d", template.ID, nextRunUnix)
	title := firstNonEmpty(cfg.Title, template.Name)
	description := cfg.Description

	_, err = s.createTaskFromTrigger(ctx, template, projectID, externalKey, "schedule", payload, title, description, []string{"schedule"})
	if err != nil {
		return err
	}

	nextRunAt := now.Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
	if _, err := s.svc.Store.UpdateWorkflowTemplateTriggerSchedule(ctx, types.UpdateWorkflowTemplateTriggerScheduleParams{
		ID:              template.ID,
		NextRunAt:       &nextRunAt,
		LastTriggeredAt: &now,
	}); err != nil {
		return fmt.Errorf("update workflow trigger schedule: %w", err)
	}
	return nil
}

func (s *WorkflowTriggerService) processGitHubIssueTemplate(ctx context.Context, template types.WorkflowTemplate, event GitHubIssueEvent) (*CreateTaskResult, error) {
	cfg, err := parseWorkflowTriggerConfig(template)
	if err != nil {
		return nil, err
	}
	projectID, err := uuid.Parse(cfg.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("parse github trigger project_id: %w", err)
	}

	payload := event.RawPayload
	if len(payload) == 0 {
		payload, _ = json.Marshal(event)
	}
	externalKey := fmt.Sprintf("github:%s/%s:issue:%d", strings.ToLower(event.Owner), strings.ToLower(event.Repo), event.Number)
	description := formatGitHubIssueDescription(event)
	labels := append([]string{"github", "issue"}, event.Labels...)

	return s.createTaskFromTrigger(ctx, template, projectID, externalKey, "github_issue", payload, event.Title, description, labels)
}

func (s *WorkflowTriggerService) createTaskFromTrigger(
	ctx context.Context,
	template types.WorkflowTemplate,
	projectID uuid.UUID,
	externalKey string,
	triggerType string,
	payload json.RawMessage,
	title string,
	description string,
	labels []string,
) (*CreateTaskResult, error) {
	templateID, err := uuid.Parse(template.ID)
	if err != nil {
		return nil, fmt.Errorf("parse template id: %w", err)
	}

	if _, err := s.svc.Store.GetWorkflowTriggerRunByExternalKey(ctx, templateID, externalKey); err == nil {
		return nil, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("check workflow trigger run duplicate: %w", err)
	}

	project, err := s.svc.Store.GetProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get trigger project: %w", err)
	}
	if project.WorkspaceID != template.WorkspaceID {
		return nil, fmt.Errorf("trigger project workspace mismatch")
	}

	run, err := s.svc.Store.CreateWorkflowTriggerRun(ctx, types.CreateWorkflowTriggerRunParams{
		WorkspaceID:        template.WorkspaceID,
		ProjectID:          projectID.String(),
		WorkflowTemplateID: template.ID,
		TriggerType:        triggerType,
		ExternalKey:        externalKey,
		Status:             "started",
		Payload:            payload,
	})
	if err != nil {
		return nil, fmt.Errorf("create workflow trigger run: %w", err)
	}

	var descPtr *string
	if description != "" {
		descPtr = &description
	}
	result, err := NewTaskService(s.svc).Create(ctx, projectID, types.CreateTaskParams{
		ProjectID:    projectID.String(),
		WorkflowName: template.Name,
		Title:        title,
		Description:  descPtr,
		Type:         "task",
		Priority:     "medium",
		Status:       "active",
		AuthorType:   "system",
		AuthorID:     template.ID,
		Labels:       labels,
		Sequence:     0,
	}, templateID)
	if err != nil {
		_, _ = s.svc.Store.MarkWorkflowTriggerRunFailed(ctx, types.MarkWorkflowTriggerRunFailedParams{
			ID:    run.ID,
			Error: err.Error(),
		})
		return nil, fmt.Errorf("create task from workflow trigger: %w", err)
	}

	taskID := result.Task.ID
	if _, err := s.svc.Store.MarkWorkflowTriggerRunCompleted(ctx, types.MarkWorkflowTriggerRunCompletedParams{
		ID:     run.ID,
		TaskID: &taskID,
	}); err != nil {
		return nil, fmt.Errorf("mark workflow trigger run completed: %w", err)
	}
	return result, nil
}

func parseWorkflowTriggerConfig(template types.WorkflowTemplate) (workflowTriggerConfig, error) {
	var cfg workflowTriggerConfig
	if len(template.TriggerConfig) == 0 {
		return cfg, nil
	}
	if err := json.Unmarshal(template.TriggerConfig, &cfg); err != nil {
		return cfg, fmt.Errorf("parse workflow trigger config for template %s: %w", template.ID, err)
	}
	return cfg, nil
}

func formatGitHubIssueDescription(event GitHubIssueEvent) string {
	parts := []string{
		fmt.Sprintf("GitHub Issue: %s", event.URL),
		fmt.Sprintf("Author: %s", event.Author),
	}
	if len(event.Labels) > 0 {
		parts = append(parts, "Labels: "+strings.Join(event.Labels, ", "))
	}
	if event.Body != "" {
		parts = append(parts, "", event.Body)
	}
	return strings.Join(parts, "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "工作流触发任务"
}
