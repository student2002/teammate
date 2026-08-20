-- 运行时查询

-- name: CreateRuntime :one
INSERT INTO runtimes (agent_id, daemon_id, provider, version, status, session_token_hash, session_expires_at, public_key)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetRuntime :one
SELECT * FROM runtimes WHERE id = $1;

-- name: GetRuntimeByAgent :one
SELECT * FROM runtimes WHERE agent_id = $1 AND status = 'online' ORDER BY last_heartbeat DESC LIMIT 1;

-- name: ListRuntimes :many
SELECT * FROM runtimes ORDER BY created_at DESC;

-- name: ListRuntimesByWorkspace :many
SELECT r.* FROM runtimes r
JOIN agents a ON r.agent_id = a.id
WHERE a.workspace_id = $1
ORDER BY r.created_at DESC;

-- name: UpdateRuntimeHeartbeat :one
UPDATE runtimes SET last_heartbeat = $2, status = 'online', updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateRuntimeStatus :one
UPDATE runtimes SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteRuntime :exec
DELETE FROM runtimes WHERE id = $1;

-- name: MarkStaleRuntimes :many
UPDATE runtimes SET status = 'offline', updated_at = now()
WHERE status = 'online' AND last_heartbeat < $1
RETURNING *;

-- name: UpdateOfflineAgents :many
UPDATE agents SET status = 'offline', updated_at = now()
WHERE id IN (
  SELECT DISTINCT a.id FROM agents a
  JOIN runtimes r ON r.agent_id = a.id
  WHERE a.status = 'online' AND r.status = 'offline'
) AND id NOT IN (
  SELECT DISTINCT r.agent_id FROM runtimes r WHERE r.status = 'online'
)
RETURNING *;

-- name: ListRuntimeIDsByAgent :many
SELECT id FROM runtimes WHERE agent_id = $1 ORDER BY created_at DESC;
