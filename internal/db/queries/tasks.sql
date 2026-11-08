-- Task queries (V2: task_nodes are run snapshots keyed by template_node_id,
-- activation is driven by task_edges resolution — the engine owns node lifecycles)

-- name: CreateTask :one
INSERT INTO tasks (
    project_id, title, description, constraints,
    type, priority, status, author_type, author_id, due_date, labels, sequence, workflow_name, workflow_template_id
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11, $12, $13, sqlc.narg('workflow_template_id')::uuid
) RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks
WHERE id = $1;

-- name: ListTasks :many
-- Without an explicit status the view is "active work": finished tasks from
-- before today are hidden. An explicit status filter returns every matching
-- task regardless of age.
SELECT * FROM tasks
WHERE project_id = $1
  AND (sqlc.narg('status')::task_status IS NULL OR status = sqlc.narg('status'))
  AND (
    sqlc.narg('status')::task_status IS NOT NULL
    OR NOT (
      status IN ('completed', 'cancelled')
      AND updated_at < CURRENT_DATE
    )
  )
ORDER BY created_at DESC;

-- name: ListAllTasks :many
SELECT * FROM tasks
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: UpdateTask :one
UPDATE tasks
SET title = $2,
    description = $3,
    priority = $4,
    labels = $5,
    due_date = $6,
    constraints = $7,
    status = $8,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteTask :exec
DELETE FROM tasks
WHERE id = $1;

-- name: CountTasksByProject :one
SELECT count(*) FROM tasks
WHERE project_id = $1;

-- name: CreateTaskNode :one
INSERT INTO task_nodes (
    task_id, template_node_id, name, sort_order, type_key, config,
    status, activation_mode, max_attempts, readonly_dirs, full_control_dirs
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
) RETURNING *;

-- name: GetTaskNode :one
SELECT * FROM task_nodes
WHERE id = $1;

-- name: ListTaskNodesByTask :many
SELECT * FROM task_nodes
WHERE task_id = $1
ORDER BY sort_order;

-- name: ListTaskNodesByProject :many
SELECT tn.* FROM task_nodes tn
WHERE tn.task_id IN (SELECT id FROM tasks WHERE project_id = $1)
ORDER BY tn.sort_order;

-- name: ListTaskNodesByStatus :many
SELECT * FROM task_nodes
WHERE status = $1
ORDER BY updated_at;

-- name: ListTaskEdgesByTask :many
SELECT * FROM task_edges
WHERE task_id = $1
ORDER BY created_at;

-- name: CreateNodeTransition :one
INSERT INTO node_transitions (
    task_node_id, from_status, to_status, action, target_node_id,
    comment, operator_id, operator_type
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8
) RETURNING *;

-- name: ListNodeTransitions :many
SELECT * FROM node_transitions
WHERE task_node_id = $1
ORDER BY created_at;

-- name: CreateComment :one
INSERT INTO comments (
    task_id, node_id, source_node_id, parent_id, author_type, author_id, content, comment_type, metadata, mentions
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: ListComments :many
SELECT * FROM comments
WHERE task_id = $1
ORDER BY created_at ASC;

-- name: ListTaskLevelComments :many
SELECT * FROM comments
WHERE task_id = $1 AND node_id IS NULL
ORDER BY created_at ASC;

-- name: ListNodeComments :many
SELECT * FROM comments
WHERE task_id = $1 AND node_id = $2
ORDER BY created_at ASC;

-- name: ListExecutionContextComments :many
SELECT * FROM comments
WHERE task_id = $1
  AND (node_id IS NULL OR node_id = $2 OR sqlc.arg('mention_id')::uuid = ANY(mentions))
ORDER BY created_at ASC;

-- name: DeleteComment :exec
DELETE FROM comments
WHERE id = $1;

-- name: GetComment :one
SELECT * FROM comments WHERE id = $1;

-- name: UpdateComment :one
UPDATE comments SET content = $2, mentions = $3, edited_at = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateTokenUsage :one
INSERT INTO token_usage (
    task_node_id, agent_id, input_tokens, output_tokens, total_tokens, cost_estimate
) VALUES (
    $1, $2, $3, $4, $5, $6
) RETURNING *;

-- name: IsAgentProjectMember :one
SELECT EXISTS(
  SELECT 1 FROM project_members
  WHERE project_id = $1 AND agent_id = $2 AND member_type = 'agent'
);

-- name: UpdateTaskStatus :one
UPDATE tasks SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: UpdateTaskGitBranch :exec
UPDATE tasks SET git_branch = $2, updated_at = now() WHERE id = $1;

-- name: GetTokenUsageByTask :one
SELECT
    COALESCE(SUM(tu.input_tokens), 0) AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0) AS output_tokens,
    COALESCE(SUM(tu.total_tokens), 0) AS total_tokens,
    COALESCE(SUM(tu.cost_estimate), 0) AS cost_estimate
FROM token_usage tu
JOIN task_nodes tn ON tu.task_node_id = tn.id
WHERE tn.task_id = $1;

-- name: GetCompletedTasksOlderThan :many
SELECT DISTINCT t.id, t.project_id, p.workspace_id
FROM tasks t
JOIN projects p ON t.project_id = p.id
WHERE t.status IN ('completed', 'cancelled')
  AND t.updated_at < $1
  AND t.id > $2
ORDER BY t.id
LIMIT $3;

-- name: GetTokenUsageByAgent :one
SELECT COALESCE(SUM(tu.input_tokens), 0)  AS input_tokens,
       COALESCE(SUM(tu.output_tokens), 0) AS output_tokens,
       COALESCE(SUM(tu.total_tokens), 0)  AS total_tokens
FROM token_usage tu
WHERE tu.agent_id = $1;

-- name: GetTokenUsageByAgents :many
SELECT tu.agent_id,
       COALESCE(SUM(tu.input_tokens), 0)  AS input_tokens,
       COALESCE(SUM(tu.output_tokens), 0) AS output_tokens,
       COALESCE(SUM(tu.total_tokens), 0)  AS total_tokens
FROM token_usage tu
WHERE tu.agent_id = ANY($1::uuid[])
GROUP BY tu.agent_id;

-- name: GetTokenUsageByTaskNodes :many
SELECT tu.task_node_id,
       COALESCE(SUM(tu.input_tokens), 0)  AS input_tokens,
       COALESCE(SUM(tu.output_tokens), 0) AS output_tokens,
       COALESCE(SUM(tu.total_tokens), 0)  AS total_tokens
FROM token_usage tu
WHERE tu.task_node_id = ANY($1::uuid[])
GROUP BY tu.task_node_id;

-- name: ListTasksPaginated :many
-- Paginated query for tasks within a given project (supports status filtering and search); does not filter out historical tasks, for use by the historical-tasks page.
SELECT * FROM tasks
WHERE project_id = $1
  AND (sqlc.narg('status')::task_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('search_query')::text IS NULL OR title ILIKE '%' || sqlc.narg('search_query') || '%' OR description ILIKE '%' || sqlc.narg('search_query') || '%')
ORDER BY updated_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountTasksByStatus :one
-- Counts tasks under a given project and status (supports search), used for pagination calculation on the historical-tasks page.
SELECT count(*) FROM tasks
WHERE project_id = $1
  AND (sqlc.narg('status')::task_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('search_query')::text IS NULL OR title ILIKE '%' || sqlc.narg('search_query') || '%' OR description ILIKE '%' || sqlc.narg('search_query') || '%');
