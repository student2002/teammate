-- 任务查询

-- name: CreateTask :one
INSERT INTO tasks (
    project_id, title, description, constraints,
    type, priority, status, author_type, author_id, due_date, labels, sequence, workflow_name
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11, $12, $13
) RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks
WHERE id = $1;

-- name: ListTasks :many
SELECT * FROM tasks
WHERE project_id = $1
  AND (sqlc.narg('status')::task_status IS NULL OR status = sqlc.narg('status'))
  AND NOT (
    status IN ('completed', 'cancelled')
    AND updated_at < CURRENT_DATE
  )
ORDER BY created_at DESC;

-- name: ListAllTasks :many
SELECT * FROM tasks
WHERE project_id = $1
  AND NOT (
    status IN ('completed', 'cancelled')
    AND updated_at < CURRENT_DATE
  )
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
    task_id, name, description, sort_order,
    node_type, status, assignee_type, assignee_id, reserved_for_agent_id,
    max_reject_cycles, timeout_minutes, readonly_dirs, full_control_dirs, depends_on
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: GetTaskNode :one
SELECT * FROM task_nodes
WHERE id = $1;

-- name: ListTaskNodes :many
SELECT * FROM task_nodes
WHERE task_id = $1
ORDER BY sort_order;

-- name: UpdateTaskNodeStatus :one
UPDATE task_nodes
SET status = $2,
    assignee_type = $3,
    assignee_id = $4,
    reserved_for_agent_id = $5,
    reject_count = $6,
    version = version + 1,
    completed_at = $7,
    completed_by = $8,
    reservation_expires_at = $9,
    updated_at = now()
WHERE id = $1 AND version = $10 AND status = $11
RETURNING *;

-- name: ClaimTaskNode :one
UPDATE task_nodes
SET status = 'in_progress',
    assignee_type = 'specific_agent',
    assignee_id = $2,
    reserved_for_agent_id = $2,
    version = version + 1,
    updated_at = now()
WHERE task_nodes.id = $1
  AND status = 'pending'
  AND (reserved_for_agent_id IS NULL OR reserved_for_agent_id = $2)
  AND task_nodes.version = $3
  AND (
    -- 线性前驱检查：首节点（任务内最小 sort_order，兼容 0 或 1 起始编号）或排序在前的最近节点已完成
    (SELECT sort_order FROM task_nodes WHERE id = $1) =
      (SELECT MIN(sort_order) FROM task_nodes WHERE task_id = (SELECT task_id FROM task_nodes WHERE id = $1))
    OR EXISTS (
      SELECT 1
      FROM task_nodes prev
      JOIN task_nodes cur ON cur.id = $1
      WHERE prev.task_id = cur.task_id
        AND prev.sort_order = (
          SELECT MAX(p2.sort_order) FROM task_nodes p2
          WHERE p2.task_id = cur.task_id AND p2.sort_order < cur.sort_order
        )
        AND prev.status = 'completed'
    )
  )
  AND (
    -- DAG 依赖检查：所有 depends_on 节点必须已完成
    depends_on = '{}'
    OR NOT EXISTS (
      SELECT 1 FROM unnest(depends_on) AS dep_id
      WHERE NOT EXISTS (
        SELECT 1 FROM task_nodes dep
        WHERE dep.id = dep_id AND dep.status = 'completed'
      )
    )
  )
RETURNING *;

-- name: ClaimTaskNodeByHuman :one
UPDATE task_nodes
SET status = 'in_progress',
    assignee_type = 'human',
    assignee_id = $2,
    version = version + 1,
    updated_at = now()
WHERE task_nodes.id = $1
  AND status = 'pending'
  AND assignee_type = 'human'
  AND task_nodes.version = $3
  AND (
    -- 线性前驱检查：首节点（任务内最小 sort_order，兼容 0 或 1 起始编号）或排序在前的最近节点已完成
    (SELECT sort_order FROM task_nodes WHERE id = $1) =
      (SELECT MIN(sort_order) FROM task_nodes WHERE task_id = (SELECT task_id FROM task_nodes WHERE id = $1))
    OR EXISTS (
      SELECT 1
      FROM task_nodes prev
      JOIN task_nodes cur ON cur.id = $1
      WHERE prev.task_id = cur.task_id
        AND prev.sort_order = (
          SELECT MAX(p2.sort_order) FROM task_nodes p2
          WHERE p2.task_id = cur.task_id AND p2.sort_order < cur.sort_order
        )
        AND prev.status = 'completed'
    )
  )
  AND (
    depends_on = '{}'
    OR NOT EXISTS (
      SELECT 1 FROM unnest(depends_on) AS dep_id
      WHERE NOT EXISTS (
        SELECT 1 FROM task_nodes dep WHERE dep.id = dep_id AND dep.status = 'completed'
      )
    )
  )
RETURNING *;

-- name: ReclaimTaskNode :one
UPDATE task_nodes
SET version = version + 1,
    updated_at = now()
WHERE task_nodes.id = $1
  AND status = 'in_progress'
  AND reserved_for_agent_id = $2
  AND version = $3
RETURNING *;

-- name: GetTimedOutNodes :many
SELECT tn.*
FROM task_nodes tn
WHERE tn.status = 'in_progress'
  AND tn.node_type != 'manual'
  AND tn.assignee_type != 'human'
  AND tn.timeout_minutes > 0
  AND ($1 - tn.updated_at) > (tn.timeout_minutes * interval '1 minute');

-- name: DeleteTaskNodes :exec
DELETE FROM task_nodes
WHERE task_id = $1;

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

-- name: GetPrevStandardNodeAssignee :one
SELECT tn.assignee_id FROM task_nodes tn
WHERE tn.task_id = $1
  AND tn.sort_order < (SELECT sort_order FROM task_nodes sub WHERE sub.id = $2)
  AND tn.node_type = 'standard'
  AND tn.assignee_id IS NOT NULL
ORDER BY tn.sort_order DESC LIMIT 1;

-- name: IsAgentProjectMember :one
SELECT EXISTS(
  SELECT 1 FROM project_members
  WHERE project_id = $1 AND agent_id = $2 AND member_type = 'agent'
);

-- name: GetNextTaskNode :one
SELECT * FROM task_nodes tn
WHERE tn.task_id = $1 AND tn.sort_order > (SELECT sub.sort_order FROM task_nodes sub WHERE sub.id = $2)
ORDER BY tn.sort_order ASC LIMIT 1;

-- name: GetPrevTaskNode :one
SELECT * FROM task_nodes tn
WHERE tn.task_id = $1 AND tn.sort_order < (SELECT sub.sort_order FROM task_nodes sub WHERE sub.id = $2)
ORDER BY tn.sort_order DESC LIMIT 1;

-- name: UpdateTaskStatus :one
UPDATE tasks SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

-- name: UpdateTaskGitBranch :exec
UPDATE tasks SET git_branch = $2, updated_at = now() WHERE id = $1;

-- name: IncrementRejectCount :one
UPDATE task_nodes SET reject_count = reject_count + 1, version = version + 1, updated_at = now()
WHERE id = $1 RETURNING *;

-- name: ResetRejectCount :one
UPDATE task_nodes SET reject_count = 0, version = version + 1, status = 'in_progress', updated_at = now()
WHERE id = $1 AND version = $2 RETURNING *;

-- name: GetTaskNodeBySortOrder :one
SELECT * FROM task_nodes WHERE task_id = $1 AND sort_order = $2;

-- name: GetTaskNodeCount :one
SELECT COUNT(*) FROM task_nodes WHERE task_id = $1;

-- name: GetCompletedNodeCount :one
SELECT COUNT(*) FROM task_nodes WHERE task_id = $1 AND status = 'completed';

-- name: GetTokenUsageByTask :one
SELECT
    COALESCE(SUM(tu.input_tokens), 0) AS input_tokens,
    COALESCE(SUM(tu.output_tokens), 0) AS output_tokens,
    COALESCE(SUM(tu.total_tokens), 0) AS total_tokens,
    COALESCE(SUM(tu.cost_estimate), 0) AS cost_estimate
FROM token_usage tu
JOIN task_nodes tn ON tu.task_node_id = tn.id
WHERE tn.task_id = $1;

-- name: ClearExpiredReservations :many
UPDATE task_nodes SET reserved_for_agent_id = NULL, reservation_expires_at = NULL, updated_at = now()
WHERE reserved_for_agent_id IS NOT NULL
  AND status = 'pending'
  AND reservation_expires_at IS NOT NULL
  AND reservation_expires_at < $1
RETURNING *;

-- name: UpdateNodeSummary :one
UPDATE task_nodes SET summary = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ReleaseClaimTimeoutNodes :many
UPDATE task_nodes SET
  status = 'manual_intervention',
  assignee_type = 'human',
  version = version + 1,
  updated_at = now()
WHERE status = 'in_progress'
  AND node_type != 'manual'
  AND assignee_type != 'human'
  AND updated_at < $1
RETURNING *;

-- name: GetCompletedTasksOlderThan :many
SELECT DISTINCT t.id, t.project_id, p.workspace_id
FROM tasks t
JOIN projects p ON t.project_id = p.id
WHERE t.status IN ('completed', 'cancelled')
  AND t.updated_at < $1
  AND t.id > $2
ORDER BY t.id
LIMIT $3;

-- name: GetReadyNodes :many
SELECT * FROM task_nodes
WHERE task_nodes.task_id = $1
  AND task_nodes.status = 'pending'
  AND (
    task_nodes.depends_on = '{}'
    OR NOT EXISTS (
      SELECT 1 FROM unnest(task_nodes.depends_on) AS dep_id
      WHERE NOT EXISTS (
        SELECT 1 FROM task_nodes dep
        WHERE dep.id = dep_id AND dep.status = 'completed'
      )
    )
  )
ORDER BY task_nodes.sort_order;

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

-- name: GetInProgressNodesByAgent :many
-- 查询指定 Agent 在指定工作区中认领但未完成（in_progress）的节点，
-- 用于 Agent 重启后恢复未完成的执行。
SELECT tn.*, t.project_id FROM task_nodes tn
JOIN tasks t ON tn.task_id = t.id
JOIN projects p ON t.project_id = p.id
WHERE tn.status = 'in_progress'
  AND tn.assignee_id = $1
  AND p.workspace_id = $2
ORDER BY tn.sort_order;

-- name: ListTasksPaginated :many
-- 分页查询指定项目内的任务（支持状态过滤和搜索），不过滤历史任务，供历史任务页面使用。
SELECT * FROM tasks
WHERE project_id = $1
  AND (sqlc.narg('status')::task_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('search_query')::text IS NULL OR title ILIKE '%' || sqlc.narg('search_query') || '%' OR description ILIKE '%' || sqlc.narg('search_query') || '%')
ORDER BY updated_at DESC
LIMIT sqlc.arg('limit') OFFSET sqlc.arg('offset');

-- name: CountTasksByStatus :one
-- 统计指定项目和状态下的任务数量（支持搜索），用于历史任务页面分页计算。
SELECT count(*) FROM tasks
WHERE project_id = $1
  AND (sqlc.narg('status')::task_status IS NULL OR status = sqlc.narg('status'))
  AND (sqlc.narg('search_query')::text IS NULL OR title ILIKE '%' || sqlc.narg('search_query') || '%' OR description ILIKE '%' || sqlc.narg('search_query') || '%');
