-- 通知查询

-- name: ListManualInterventionNodes :many
SELECT tn.id, tn.task_id, tn.name, tn.status, tn.created_at, t.title AS task_title
FROM task_nodes tn
JOIN tasks t ON tn.task_id = t.id
JOIN projects p ON t.project_id = p.id
WHERE p.workspace_id = $1
  AND tn.status = 'manual_intervention'
ORDER BY tn.created_at DESC;

-- name: ListMentionComments :many
SELECT c.id, c.task_id, c.content, c.mentions, c.created_at, c.author_type, c.author_id, t.title AS task_title
FROM comments c
JOIN tasks t ON c.task_id = t.id
JOIN projects p ON t.project_id = p.id
WHERE p.workspace_id = $1
  AND $2::uuid = ANY(c.mentions)
ORDER BY c.created_at DESC;
