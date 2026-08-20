-- 工作流模板查询

-- name: CreateWorkflowTemplate :one
INSERT INTO workflow_templates (
  workspace_id, name, description, is_builtin,
  trigger_type, trigger_config, trigger_enabled, next_run_at, last_triggered_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetWorkflowTemplate :one
SELECT * FROM workflow_templates
WHERE id = $1;

-- name: ListWorkflowTemplates :many
SELECT * FROM workflow_templates
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateWorkflowTemplate :one
UPDATE workflow_templates
SET name = $2,
    description = $3,
    trigger_type = $4,
    trigger_config = $5,
    trigger_enabled = $6,
    next_run_at = $7,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowTemplate :exec
DELETE FROM workflow_templates
WHERE id = $1;

-- name: CreateTemplateNode :one
INSERT INTO workflow_template_nodes (template_id, name, description, sort_order, node_type, assignee_type, assignee_id, timeout_minutes, readonly_dirs, full_control_dirs, artifact, max_reject_cycles, depends_on)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
RETURNING *;

-- name: GetTemplateNode :one
SELECT * FROM workflow_template_nodes
WHERE id = $1;

-- name: ListTemplateNodes :many
SELECT * FROM workflow_template_nodes
WHERE template_id = $1
ORDER BY sort_order;

-- name: UpdateTemplateNode :one
UPDATE workflow_template_nodes
SET name = $2, description = $3, sort_order = $4, node_type = $5, assignee_type = $6, assignee_id = $7, timeout_minutes = $8, readonly_dirs = $9, full_control_dirs = $10, artifact = $11, max_reject_cycles = $12, depends_on = $13
WHERE id = $1
RETURNING *;

-- name: DeleteTemplateNode :exec
DELETE FROM workflow_template_nodes
WHERE id = $1;

-- name: DeleteTemplateNodesByTemplate :exec
DELETE FROM workflow_template_nodes
WHERE template_id = $1;

-- name: GetMaxRejectCyclesByNodeID :one
SELECT max_reject_cycles FROM workflow_template_nodes WHERE id = $1;
