-- Workflow template queries (V2 event-driven graph: nodes by type_key, edges by ports)

-- name: CreateWorkflowTemplate :one
INSERT INTO workflow_templates (
  workspace_id, name, description, is_builtin, version, enabled
)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetWorkflowTemplate :one
SELECT * FROM workflow_templates
WHERE id = $1;

-- name: ListWorkflowTemplates :many
SELECT * FROM workflow_templates
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateWorkflowTemplateGraph :one
-- Full-graph save: bumps version in the same transaction as the metadata
-- write so in-flight task snapshots can pin the version they started from.
UPDATE workflow_templates
SET name = $2,
    description = $3,
    enabled = $4,
    version = version + 1,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkflowTemplate :exec
DELETE FROM workflow_templates
WHERE id = $1;

-- name: CreateTemplateNode :one
-- id comes from the caller so template node identity is stable across full
-- graph replacement (edges reference node ids).
INSERT INTO workflow_template_nodes (id, template_id, name, sort_order, type_key, config, ui_config)
VALUES (COALESCE(sqlc.narg('node_id')::uuid, gen_random_uuid()), $1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTemplateNode :one
SELECT * FROM workflow_template_nodes
WHERE id = $1;

-- name: ListTemplateNodes :many
SELECT * FROM workflow_template_nodes
WHERE template_id = $1
ORDER BY sort_order;

-- name: DeleteTemplateNodesByTemplate :exec
DELETE FROM workflow_template_nodes
WHERE template_id = $1;

-- name: CreateTemplateEdge :one
-- id comes from the caller so edge identity is stable across full graph replacement.
INSERT INTO workflow_template_edges (id, template_id, source_node_id, source_port, target_node_id, target_port, label, sort_order, config)
VALUES (COALESCE(sqlc.narg('edge_id')::uuid, gen_random_uuid()), $1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetTemplateEdge :one
SELECT * FROM workflow_template_edges
WHERE id = $1;

-- name: ListTemplateEdges :many
SELECT * FROM workflow_template_edges
WHERE template_id = $1
ORDER BY sort_order, created_at;

-- name: DeleteTemplateEdgesByTemplate :exec
DELETE FROM workflow_template_edges
WHERE template_id = $1;
