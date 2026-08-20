-- 工作流触发器查询

-- name: CreateWorkflowTriggerRun :one
INSERT INTO workflow_trigger_runs (
  workspace_id, project_id, workflow_template_id, trigger_type, external_key, status, payload
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: MarkWorkflowTriggerRunCompleted :one
UPDATE workflow_trigger_runs
SET status = 'completed', task_id = $2, error = ''
WHERE id = $1
RETURNING *;

-- name: MarkWorkflowTriggerRunFailed :one
UPDATE workflow_trigger_runs
SET status = 'failed', error = $2
WHERE id = $1
RETURNING *;

-- name: GetWorkflowTriggerRunByExternalKey :one
SELECT * FROM workflow_trigger_runs
WHERE workflow_template_id = $1 AND external_key = $2;

-- name: ListDueScheduledWorkflowTemplates :many
SELECT * FROM workflow_templates
WHERE trigger_type = 'schedule'
  AND trigger_enabled = true
  AND next_run_at IS NOT NULL
  AND next_run_at <= $1
ORDER BY next_run_at ASC, created_at ASC
LIMIT $2;

-- name: ListGithubIssueWorkflowTemplatesByRepo :many
SELECT * FROM workflow_templates
WHERE trigger_type = 'github_issue'
  AND trigger_enabled = true
  AND lower(trigger_config->>'repo_owner') = lower($1)
  AND lower(trigger_config->>'repo_name') = lower($2);

-- name: UpdateWorkflowTemplateTriggerSchedule :one
UPDATE workflow_templates
SET next_run_at = $2,
    last_triggered_at = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;
