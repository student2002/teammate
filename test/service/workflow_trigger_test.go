// workflow_trigger_test.go covers tests for workflow trigger business logic.
package service_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

func TestWorkflowTriggerService_ProcessDueSchedulesCreatesTask(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()
	now := time.Date(2026, 6, 29, 9, 30, 0, 0, time.UTC)

	config := json.RawMessage(`{"project_id":"` + env.projectID + `","interval_minutes":30,"title":"Daily Inspection","description":"Check project status"}`)
	dueAt := now.Add(-time.Minute)
	tpl, _, err := svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID:    env.workspaceID,
		Name:           "schedule-flow-" + uuid.New().String()[:8],
		Description:    strPtr("scheduled flow"),
		TriggerType:    "schedule",
		TriggerConfig:  config,
		TriggerEnabled: true,
		NextRunAt:      &dueAt,
	}, []types.CreateTemplateNodeParams{
		{Name: "Perform Inspection", SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
	})
	if err != nil {
		t.Fatalf("create scheduled template: %v", err)
	}

	if err := service.NewWorkflowTriggerService(svc).ProcessDueSchedules(ctx, now); err != nil {
		t.Fatalf("ProcessDueSchedules: %v", err)
	}

	tasks, err := svc.Store.ListAllTasks(ctx, uuid.MustParse(env.projectID))
	if err != nil {
		t.Fatalf("ListAllTasks: %v", err)
	}
	if !hasTaskTitle(tasks, "Daily Inspection") {
		t.Fatalf("expected task title Daily Inspection in tasks: %#v", tasks)
	}

	updated, err := svc.Store.GetWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("GetWorkflowTemplate: %v", err)
	}
	if updated.LastTriggeredAt == nil {
		t.Fatal("expected last_triggered_at to be set")
	}
	if updated.NextRunAt == nil || !updated.NextRunAt.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("next_run_at = %#v, want %s", updated.NextRunAt, now.Add(30*time.Minute))
	}
}

func TestWorkflowTriggerService_GitHubIssueCreatesTaskAndDedupes(t *testing.T) {
	svc, _, env := setupServiceTest(t)
	ctx := context.Background()

	config := json.RawMessage(`{"project_id":"` + env.projectID + `","repo_owner":"acme","repo_name":"rocket","secret":"webhook-secret"}`)
	_, _, err := svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID:    env.workspaceID,
		Name:           "github-flow-" + uuid.New().String()[:8],
		Description:    strPtr("github flow"),
		TriggerType:    "github_issue",
		TriggerConfig:  config,
		TriggerEnabled: true,
	}, []types.CreateTemplateNodeParams{
		{Name: "Handle Issue", SortOrder: 1, NodeType: "standard", AssigneeType: "any_agent", TimeoutMinutes: 60, MaxRejectCycles: 3},
	})
	if err != nil {
		t.Fatalf("create github template: %v", err)
	}

	triggerSvc := service.NewWorkflowTriggerService(svc)
	event := service.GitHubIssueEvent{
		Owner:      "acme",
		Repo:       "rocket",
		Number:     42,
		Title:      "Fix login failure",
		Body:       "User cannot log in",
		URL:        "https://github.com/acme/rocket/issues/42",
		Author:     "octocat",
		Labels:     []string{"bug", "login"},
		Action:     "opened",
		RawPayload: json.RawMessage(`{"action":"opened"}`),
	}

	if _, err := triggerSvc.HandleGitHubIssue(ctx, event); err != nil {
		t.Fatalf("HandleGitHubIssue first delivery: %v", err)
	}
	if _, err := triggerSvc.HandleGitHubIssue(ctx, event); err != nil {
		t.Fatalf("HandleGitHubIssue duplicate delivery: %v", err)
	}

	tasks, err := svc.Store.ListAllTasks(ctx, uuid.MustParse(env.projectID))
	if err != nil {
		t.Fatalf("ListAllTasks: %v", err)
	}
	var matching int
	for _, task := range tasks {
		if strings.Contains(task.Title, "Fix login failure") {
			matching++
			if !strings.Contains(task.Description, "https://github.com/acme/rocket/issues/42") {
				t.Fatalf("task description %q does not include issue URL", task.Description)
			}
		}
	}
	if matching != 1 {
		t.Fatalf("expected exactly one GitHub issue task, got %d", matching)
	}
}

func hasTaskTitle(tasks []types.Task, title string) bool {
	for _, task := range tasks {
		if task.Title == title {
			return true
		}
	}
	return false
}
