-- 搜索查询

-- name: SearchTasksByWorkspace :many
SELECT t.*
FROM tasks t
JOIN projects p ON p.id = t.project_id
WHERE p.workspace_id = $1
  AND (t.title ILIKE $2 OR t.description ILIKE $2)
ORDER BY t.created_at DESC;

-- name: SearchTasksByWorkspaceAndProject :many
SELECT t.*
FROM tasks t
JOIN projects p ON p.id = t.project_id
WHERE p.workspace_id = $1
  AND t.project_id = $2
  AND (t.title ILIKE $3 OR t.description ILIKE $3)
ORDER BY t.created_at DESC;

-- name: SearchAgentsByWorkspace :many
SELECT *
FROM agents
WHERE workspace_id = $1
  AND name ILIKE $2
ORDER BY created_at DESC;
