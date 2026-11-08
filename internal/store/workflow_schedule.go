// workflow_schedule.go provides data access for the trigger.schedule state:
// one row per template carrying the next due instant (plan §2.1 trigger
// schedule, fired by the scheduler scan).
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
)

// TemplateSchedule is the schedule state of one workflow template.
type TemplateSchedule struct {
	TemplateID  string     `json:"template_id"`
	ProjectID   string     `json:"project_id"`
	NextRunAt   time.Time  `json:"next_run_at"`
	LastFiredAt *time.Time `json:"last_fired_at"`
}

// UpsertTemplateSchedule creates or resets the schedule row of a template
// (called on every full-graph save of a schedule-triggered template).
func (s *Store) UpsertTemplateSchedule(ctx context.Context, templateID, projectID uuid.UUID, nextRunAt time.Time) (TemplateSchedule, error) {
	row, err := s.q.UpsertTemplateSchedule(ctx, db.UpsertTemplateScheduleParams{
		TemplateID: templateID,
		ProjectID:  projectID,
		NextRunAt:  nextRunAt,
	})
	if err != nil {
		return TemplateSchedule{}, fmt.Errorf("upsert template schedule: %w", err)
	}
	return toDomainTemplateSchedule(row), nil
}

// DeleteTemplateSchedule removes the schedule row of a deleted template.
func (s *Store) DeleteTemplateSchedule(ctx context.Context, templateID uuid.UUID) error {
	if err := s.q.DeleteTemplateSchedule(ctx, templateID); err != nil {
		return fmt.Errorf("delete template schedule: %w", err)
	}
	return nil
}

// DueTemplateSchedule is one due schedule joined with its template name.
type DueTemplateSchedule struct {
	TemplateID   string    `json:"template_id"`
	ProjectID    string    `json:"project_id"`
	NextRunAt    time.Time `json:"next_run_at"`
	TemplateName string    `json:"template_name"`
}

// ListDueTemplateSchedules lists enabled templates whose schedule is due.
func (s *Store) ListDueTemplateSchedules(ctx context.Context, now time.Time, limit int32) ([]DueTemplateSchedule, error) {
	rows, err := s.q.ListDueTemplateSchedules(ctx, db.ListDueTemplateSchedulesParams{NextRunAt: now, Limit: limit})
	if err != nil {
		return nil, fmt.Errorf("list due template schedules: %w", err)
	}
	out := make([]DueTemplateSchedule, 0, len(rows))
	for _, row := range rows {
		out = append(out, DueTemplateSchedule{
			TemplateID:   row.TemplateID.String(),
			ProjectID:    row.ProjectID.String(),
			NextRunAt:    row.NextRunAt,
			TemplateName: row.TemplateName,
		})
	}
	return out, nil
}

// FireTemplateSchedule advances one due schedule from the expected instant;
// a schedule already fired by another instance matches nothing.
func (s *Store) FireTemplateSchedule(ctx context.Context, templateID uuid.UUID, interval time.Duration, expectedDue time.Time) (bool, error) {
	_, err := s.q.FireTemplateSchedule(ctx, db.FireTemplateScheduleParams{
		TemplateID: templateID,
		Mins:       int32(interval.Minutes()),
		NextRunAt:  expectedDue,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("fire template schedule: %w", err)
	}
	return true, nil
}

func toDomainTemplateSchedule(row db.WorkflowTemplateSchedule) TemplateSchedule {
	var lastFired *time.Time
	if row.LastFiredAt.Valid {
		t := row.LastFiredAt.Time
		lastFired = &t
	}
	return TemplateSchedule{
		TemplateID:  row.TemplateID.String(),
		ProjectID:   row.ProjectID.String(),
		NextRunAt:   row.NextRunAt,
		LastFiredAt: lastFired,
	}
}
