-- Agent permissions

-- name: CreateAgentPermission :one
INSERT INTO agent_permissions (agent_id, permission, resource_type, resource_id, granted_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetAgentPermission :one
SELECT * FROM agent_permissions
WHERE id = $1;

-- name: ListAgentPermissions :many
SELECT * FROM agent_permissions
WHERE agent_id = $1
ORDER BY created_at;

-- name: DeleteAgentPermission :exec
DELETE FROM agent_permissions
WHERE id = $1;

-- name: HasAgentPermission :one
-- Checks whether an Agent has a specific permission on a resource.
-- Matches either of two cases: exact match (resource_type + resource_id) or wildcard match (resource_type = '*' AND resource_id IS NULL).
SELECT EXISTS(
    SELECT 1 FROM agent_permissions
    WHERE agent_id = $1 AND permission = $2
    AND (
        (resource_type = $3 AND (resource_id = $4 OR (resource_id IS NULL AND $4 IS NULL)))
        OR (resource_type = '*' AND resource_id IS NULL)
    )
);

-- name: HasAgentPermissionAny :one
SELECT EXISTS(
    SELECT 1 FROM agent_permissions
    WHERE agent_id = $1 AND permission = $2
);

-- name: DeleteAgentPermissionsByAgent :exec
DELETE FROM agent_permissions
WHERE agent_id = $1;
