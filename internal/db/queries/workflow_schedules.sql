-- Workflow schedule queries: per-template next-run state of the
-- trigger.schedule nodes. The row is synced on every template graph save.

-- name: UpsertTemplateSchedule :one
INSERT INTO workflow_template_schedules (template_id, project_id, next_run_at)
VALUES ($1, $2, $3)
ON CONFLICT (template_id) DO UPDATE
SET next_run_at = EXCLUDED.next_run_at, updated_at = now()
RETURNING *;

-- name: DeleteTemplateSchedule :exec
DELETE FROM workflow_template_schedules WHERE template_id = $1;

-- name: GetTemplateSchedule :one
SELECT * FROM workflow_template_schedules WHERE template_id = $1;

-- name: ListDueTemplateSchedules :many
-- Enabled templates whose schedule is due; the fire CAS below arbitrates
-- between instances.
SELECT s.template_id, s.project_id, s.next_run_at, t.name AS template_name
FROM workflow_template_schedules s
JOIN workflow_templates t ON t.id = s.template_id
WHERE t.enabled = true AND s.next_run_at <= $1
ORDER BY s.next_run_at
LIMIT $2;

-- name: FireTemplateSchedule :one
-- Advance next_run_at by the configured interval only from the expected due
-- instant; a row already fired by another instance matches nothing.
UPDATE workflow_template_schedules s
SET next_run_at = s.next_run_at + make_interval(mins => $2),
    last_fired_at = now(),
    updated_at = now()
FROM workflow_templates t
WHERE s.template_id = t.id AND s.template_id = $1 AND s.next_run_at = $3 AND t.enabled = true
RETURNING s.template_id;
