-- 记忆查询

-- name: CreateMemory :one
INSERT INTO memories (workspace_id, source_task_id, type, title, content, tags, confidence, verified, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetMemory :one
SELECT * FROM memories WHERE id = $1;

-- name: ListMemoriesByWorkspace :many
SELECT * FROM memories
WHERE workspace_id = $1
  AND stale = false
  AND (sqlc.narg('verified')::boolean IS NULL OR verified = sqlc.narg('verified')::boolean)
  AND (sqlc.narg('min_confidence')::real IS NULL OR confidence >= sqlc.narg('min_confidence')::real)
ORDER BY created_at DESC
LIMIT sqlc.narg('limit')::integer;

-- name: DeleteMemory :exec
DELETE FROM memories WHERE id = $1;

-- name: SearchMemories :many
SELECT id, workspace_id, source_task_id, type, title, content, tags, embedding, confidence, verified, metadata, created_at, updated_at
FROM memories
WHERE workspace_id = $1
  AND stale = false
  AND (title ILIKE $2 OR content ILIKE $2)
ORDER BY created_at DESC
LIMIT 100;

-- name: MarkMemoriesStaleByTask :exec
UPDATE memories SET stale = true WHERE source_task_id = $1 AND stale = false;
