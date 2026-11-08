// workflow_schedule_test.go covers the trigger.schedule driver: the schedule
// row synced on template save, the due-fire through the run engine, and the
// CAS that keeps a schedule firing exactly once per interval.
package service_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// scheduleGraph builds a minimal valid schedule template pinned to projID.
func scheduleGraph(projID string) ([]types.CreateTemplateNodeParams, []types.CreateTemplateEdgeParams) {
	triggerID := uuid.NewString()
	endID := uuid.NewString()
	nodes := []types.CreateTemplateNodeParams{
		wfNode(triggerID, "trigger.schedule", "开始", 0, `{"interval_minutes":30,"project_id":"`+projID+`"}`),
		wfNode(endID, "flow.end", "完成", 1, `{}`),
	}
	edges := []types.CreateTemplateEdgeParams{
		wfEdge(uuid.NewString(), triggerID, "success", endID, "completed"),
	}
	return nodes, edges
}

func TestScheduleSyncOnTemplateSave(t *testing.T) {
	e := setupRunTest(t)
	wfSvc := service.NewWorkflowService(e.svc)
	nodes, edges := scheduleGraph(e.projID)
	created, err := wfSvc.Create(context.Background(), mustParse(t, e.wsID), service.TemplateGraphInput{
		Name: "定时模板-" + uuid.NewString()[:6], Enabled: true, Nodes: nodes, Edges: edges,
	})
	if err != nil {
		t.Fatalf("create schedule template: %v", err)
	}

	var nextRun time.Time
	var projectID string
	if err := e.db.QueryRow(`SELECT next_run_at, project_id FROM workflow_template_schedules WHERE template_id = $1`,
		created.Template.ID).Scan(&nextRun, &projectID); err != nil {
		t.Fatalf("load schedule row: %v", err)
	}
	if projectID != e.projID {
		t.Fatalf("schedule project_id = %s, want %s", projectID, e.projID)
	}
	if time.Until(nextRun) <= 0 || time.Until(nextRun) > 31*time.Minute {
		t.Fatalf("next_run_at = %v, want ~30 minutes ahead", nextRun)
	}

	// Switching the trigger kind removes the row.
	nodes[0].TypeKey = "trigger.manual"
	nodes[0].Config = json.RawMessage(`{}`)
	if _, err := wfSvc.Update(context.Background(), mustParse(t, created.Template.ID), service.TemplateGraphInput{
		Name: created.Template.Name, Enabled: true, Nodes: nodes, Edges: edges,
	}); err != nil {
		t.Fatalf("update to manual trigger: %v", err)
	}
	var count int
	if err := e.db.QueryRow(`SELECT count(*) FROM workflow_template_schedules WHERE template_id = $1`, created.Template.ID).Scan(&count); err != nil {
		t.Fatalf("count schedule rows: %v", err)
	}
	if count != 0 {
		t.Fatalf("schedule row survived the trigger switch, want removed")
	}
}

func TestScheduleDueFireStartsRunOnce(t *testing.T) {
	e := setupRunTest(t)
	wfSvc := service.NewWorkflowService(e.svc)
	nodes, edges := scheduleGraph(e.projID)
	created, err := wfSvc.Create(context.Background(), mustParse(t, e.wsID), service.TemplateGraphInput{
		Name: "定时触发-" + uuid.NewString()[:6], Enabled: true, Nodes: nodes, Edges: edges,
	})
	if err != nil {
		t.Fatalf("create schedule template: %v", err)
	}
	templateID := mustParse(t, created.Template.ID)

	// Due: the fire CAS expects next_run_at to equal the stored instant.
	if _, err := e.db.Exec(`UPDATE workflow_template_schedules SET next_run_at = now() - interval '1 minute' WHERE template_id = $1`, templateID); err != nil {
		t.Fatalf("backdate schedule: %v", err)
	}

	schedSvc := service.NewWorkflowScheduleService(e.svc)
	if err := schedSvc.ProcessDueSchedules(context.Background()); err != nil {
		t.Fatalf("process due schedules: %v", err)
	}

	tasks, err := e.svc.Store.ListAllTasks(context.Background(), mustParse(t, e.projID))
	if err != nil {
		t.Fatalf("list project tasks: %v", err)
	}
	fired := 0
	for _, task := range tasks {
		if task.WorkflowTemplateID != nil && *task.WorkflowTemplateID == created.Template.ID {
			fired++
		}
	}
	if fired != 1 {
		t.Fatalf("schedule fired %d runs, want exactly 1", fired)
	}

	// The next instant is one interval ahead: a second scan must not fire.
	if err := schedSvc.ProcessDueSchedules(context.Background()); err != nil {
		t.Fatalf("second scan: %v", err)
	}
	tasks, _ = e.svc.Store.ListAllTasks(context.Background(), mustParse(t, e.projID))
	fired = 0
	for _, task := range tasks {
		if task.WorkflowTemplateID != nil && *task.WorkflowTemplateID == created.Template.ID {
			fired++
		}
	}
	if fired != 1 {
		t.Fatalf("second scan fired again: %d runs, want 1", fired)
	}
}
