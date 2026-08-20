-- 审计日志

-- name: CreateAuditLog :one
INSERT INTO audit_logs (workspace_id, actor_type, actor_id, action, resource_type, resource_id, details, ip_address, user_agent, request_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: ListAuditLogs :many
SELECT * FROM audit_logs
WHERE workspace_id = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: ListAuditLogsByActor :many
SELECT * FROM audit_logs
WHERE workspace_id = $1 AND actor_type = $2 AND actor_id = $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;
