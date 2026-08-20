-- 审查查询

-- name: GetReviewQueue :many
SELECT
    t.id AS task_id,
    t.title AS task_title,
    tn.id AS node_id,
    tn.name AS node_name,
    tn.status AS node_status,
    tn.assignee_type,
    tn.assignee_id,
    a.name AS agent_name,
    tn.created_at,
    tn.updated_at
FROM task_nodes tn
JOIN tasks t ON t.id = tn.task_id
LEFT JOIN agents a ON a.id = tn.assignee_id
WHERE t.project_id = $1
  AND tn.node_type = 'review'
  AND tn.status IN ('pending', 'in_progress')
ORDER BY tn.created_at ASC;

-- name: GetReviewNodeReviewer :one
SELECT assignee_id FROM task_nodes
WHERE id = $1 AND task_id = $2 AND node_type = 'review';

-- name: GetReviewNodeAuthor :one
SELECT tn.assignee_id
FROM task_nodes tn
WHERE tn.task_id = $1
  AND tn.sort_order = (
    -- 排序在前的最近节点（兼容 0 或 1 起始编号）
    SELECT MAX(sub2.sort_order)
    FROM task_nodes sub2
    JOIN task_nodes cur ON cur.id = $2
    WHERE sub2.task_id = cur.task_id AND sub2.sort_order < cur.sort_order
  );
