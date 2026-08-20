-- Agent 权限

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
-- 检查 Agent 是否拥有对某个资源的特定权限。
-- 匹配以下两种情况之一：精确匹配（resource_type + resource_id）或通配符匹配（resource_type = '*' 且 resource_id IS NULL）。
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
