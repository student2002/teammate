// workflow_schedule.go drives the trigger.schedule node type (plan §2.1):
// every full-graph save syncs one schedule row per schedule-triggered
// template, and the scheduler scan fires due templates through the run
// engine's Start. The fired run lands in the project pinned on the trigger
// config; multi-instance double-fires are arbitrated by the fire CAS.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// scheduleScanLimit bounds one due-schedule scan.
const scheduleScanLimit int32 = 50

// WorkflowScheduleService syncs and fires template schedules.
type WorkflowScheduleService struct {
	svc *Service
}

func NewWorkflowScheduleService(svc *Service) *WorkflowScheduleService {
	return &WorkflowScheduleService{svc: svc}
}

// syncScheduleFromGraph aligns the template's schedule row with its trigger
// node: schedule templates get next_run_at = now + interval (reset on every
// save so an interval or project change takes effect immediately), every
// other trigger kind removes the row.
func syncScheduleFromGraph(ctx context.Context, svc *Service, templateID uuid.UUID, nodes []scheduleNodeView) error {
	for _, n := range nodes {
		if !strings.HasPrefix(n.TypeKey, "trigger.") {
			continue
		}
		if n.TypeKey != "trigger.schedule" {
			// A template has exactly one trigger; a non-schedule trigger
			// removes any stale schedule row.
			return svc.Store.DeleteTemplateSchedule(ctx, templateID)
		}
		var cfg struct {
			IntervalMinutes float64 `json:"interval_minutes"`
			ProjectID       string  `json:"project_id"`
		}
		_ = json.Unmarshal(n.Config, &cfg)
		if cfg.IntervalMinutes <= 0 {
			return fmt.Errorf("sync schedule: template %s has a non-positive interval", templateID)
		}
		projectID, err := uuid.Parse(cfg.ProjectID)
		if err != nil {
			return fmt.Errorf("sync schedule: template %s has no valid project_id: %w", templateID, err)
		}
		next := time.Now().Add(time.Duration(cfg.IntervalMinutes) * time.Minute)
		_, err = svc.Store.UpsertTemplateSchedule(ctx, templateID, projectID, next)
		return err
	}
	// No trigger node (unreachable for a valid graph) — drop any stale row.
	return svc.Store.DeleteTemplateSchedule(ctx, templateID)
}

// scheduleNodeView is the slice of a template node the sync needs.
type scheduleNodeView struct {
	TypeKey string
	Config  json.RawMessage
}

// SyncScheduleFromGraph is the public hook the template services call after a
// full-graph write.
func (s *WorkflowService) SyncScheduleFromGraph(ctx context.Context, templateID uuid.UUID, nodes []types.WorkflowTemplateNode) error {
	views := make([]scheduleNodeView, 0, len(nodes))
	for _, n := range nodes {
		views = append(views, scheduleNodeView{TypeKey: n.TypeKey, Config: n.Config})
	}
	return syncScheduleFromGraph(ctx, s.svc, templateID, views)
}

// ProcessDueSchedules fires every due schedule. It runs on the scheduler scan
// ("workflow_schedule_triggers"); the fire CAS arbitrates multi-instance
// double-fires.
func (s *WorkflowScheduleService) ProcessDueSchedules(ctx context.Context) error {
	now := s.svc.Store.Clock.Now()
	due, err := s.svc.Store.ListDueTemplateSchedules(ctx, now, scheduleScanLimit)
	if err != nil {
		return err
	}
	runSvc := NewWorkflowRunService(s.svc)
	for _, item := range due {
		templateID, parseErr := uuid.Parse(item.TemplateID)
		if parseErr != nil {
			continue
		}
		interval := s.templateInterval(ctx, templateID)
		if interval <= 0 {
			slog.Error("schedule without a usable interval", "template_id", item.TemplateID)
			continue
		}
		projectID, parseErr := uuid.Parse(item.ProjectID)
		if parseErr != nil {
			slog.Error("schedule with an invalid project", "template_id", item.TemplateID)
			continue
		}
		fired, err := s.svc.Store.FireTemplateSchedule(ctx, templateID, interval, item.NextRunAt)
		if err != nil {
			return err
		}
		if !fired {
			continue
		}
		payload, _ := json.Marshal(map[string]any{
			"event":        "schedule",
			"triggered_at": now.UTC().Format(time.RFC3339),
		})
		if _, err := runSvc.Start(ctx, templateID, projectID, payload, item.TemplateName, uuid.Nil); err != nil {
			slog.Error("fire schedule run", "template_id", item.TemplateID, "err", err)
		}
	}
	return nil
}

// templateInterval reads interval_minutes from the template's trigger node.
func (s *WorkflowScheduleService) templateInterval(ctx context.Context, templateID uuid.UUID) time.Duration {
	nodes, err := s.svc.Store.ListTemplateNodes(ctx, templateID)
	if err != nil {
		return 0
	}
	for _, n := range nodes {
		if n.TypeKey != "trigger.schedule" {
			continue
		}
		var cfg struct {
			IntervalMinutes float64 `json:"interval_minutes"`
		}
		_ = json.Unmarshal(n.Config, &cfg)
		return time.Duration(cfg.IntervalMinutes) * time.Minute
	}
	return 0
}
