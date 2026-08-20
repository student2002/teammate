// workflow_trigger_test.go 覆盖工作流触发器数据访问的测试。
package store_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

func TestWorkflowTriggerTemplateFieldsRoundTrip(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()
	ws := createTestWorkspace(t, s)

	config := json.RawMessage(`{"project_id":"` + uuid.NewString() + `","interval_minutes":30}`)
	nextRunAt := time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)
	tpl, nodes, err := s.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID:     ws.ID,
		Name:            "scheduled workflow",
		Description:     strPtr("schedule trigger"),
		TriggerType:     "schedule",
		TriggerConfig:   config,
		TriggerEnabled:  true,
		NextRunAt:       &nextRunAt,
		LastTriggeredAt: nil,
	}, []types.CreateTemplateNodeParams{
		{
			Name:            "implement",
			SortOrder:       1,
			NodeType:        "standard",
			AssigneeType:    "any_agent",
			TimeoutMinutes:  60,
			MaxRejectCycles: 3,
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowTemplate: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(nodes))
	}

	got, err := s.GetWorkflowTemplate(ctx, uuid.MustParse(tpl.ID))
	if err != nil {
		t.Fatalf("GetWorkflowTemplate: %v", err)
	}
	if got.TriggerType != "schedule" {
		t.Fatalf("trigger type = %q, want %q", got.TriggerType, "schedule")
	}
	if !jsonEqual(t, got.TriggerConfig, config) {
		t.Fatalf("trigger config = %s, want %s", got.TriggerConfig, config)
	}
	if !got.TriggerEnabled {
		t.Fatal("expected trigger to be enabled")
	}
	if got.NextRunAt == nil || !got.NextRunAt.Equal(time.Date(2026, 6, 29, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("next_run_at = %#v, want scheduled time", got.NextRunAt)
	}
}

func jsonEqual(t *testing.T, got, want json.RawMessage) bool {
	t.Helper()
	var gotValue any
	var wantValue any
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("unmarshal got json: %v", err)
	}
	if err := json.Unmarshal(want, &wantValue); err != nil {
		t.Fatalf("unmarshal want json: %v", err)
	}
	gotBytes, _ := json.Marshal(gotValue)
	wantBytes, _ := json.Marshal(wantValue)
	return string(gotBytes) == string(wantBytes)
}

func TestWorkflowTriggerRunExternalKeyIsUniquePerTemplate(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()
	ws := createTestWorkspace(t, s)
	project := createTestProject(t, s, ws.ID)
	tpl, _ := createTestWorkflowTemplate(t, s, ws.ID, 1)

	params := types.CreateWorkflowTriggerRunParams{
		WorkspaceID:        ws.ID,
		ProjectID:          project.ID,
		WorkflowTemplateID: tpl.ID,
		TriggerType:        "github_issue",
		ExternalKey:        "github:owner/repo:issue:42",
		Status:             "started",
		Payload:            json.RawMessage(`{"number":42}`),
	}
	if _, err := s.CreateWorkflowTriggerRun(ctx, params); err != nil {
		t.Fatalf("CreateWorkflowTriggerRun first insert: %v", err)
	}
	if _, err := s.CreateWorkflowTriggerRun(ctx, params); err == nil {
		t.Fatal("expected duplicate external key insert to fail")
	}
}
