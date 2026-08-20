-- 工作区

-- name: CreateWorkspace :one
INSERT INTO workspaces (name, description, issue_prefix, is_default)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetWorkspace :one
SELECT * FROM workspaces
WHERE id = $1;

-- name: ListWorkspaces :many
SELECT * FROM workspaces
ORDER BY created_at DESC;

-- name: ListWorkspacesByMemberID :many
SELECT w.* FROM workspaces w
JOIN workspace_members wm ON wm.workspace_id = w.id
WHERE wm.member_id = $1
ORDER BY w.created_at DESC;

-- name: UpdateWorkspace :one
UPDATE workspaces
SET name = $2,
    description = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteWorkspace :exec
DELETE FROM workspaces
WHERE id = $1;

-- 成员

-- name: CreateMember :one
INSERT INTO members (name, email)
VALUES ($1, $2)
RETURNING *;

-- name: GetMember :one
SELECT * FROM members
WHERE id = $1;

-- name: GetMemberByEmail :one
SELECT * FROM members
WHERE email = $1;

-- name: UpdateMemberPasswordHash :exec
UPDATE members
SET password_hash = $2,
    updated_at = now()
WHERE id = $1;

-- name: DeleteMember :exec
DELETE FROM members
WHERE id = $1;

-- 工作区成员

-- name: CreateWorkspaceMember :one
INSERT INTO workspace_members (workspace_id, member_id, role)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetWorkspaceMember :one
SELECT * FROM workspace_members
WHERE workspace_id = $1 AND member_id = $2;

-- name: ListMembersByWorkspace :many
SELECT m.*, wm.role as workspace_role, wm.created_at as workspace_joined_at
FROM members m
JOIN workspace_members wm ON wm.member_id = m.id
WHERE wm.workspace_id = $1
ORDER BY wm.created_at;

-- name: UpdateMemberRole :one
UPDATE workspace_members
SET role = $2,
    updated_at = now()
WHERE workspace_id = $1 AND member_id = $3
RETURNING *;

-- name: DeleteWorkspaceMember :exec
DELETE FROM workspace_members
WHERE workspace_id = $1 AND member_id = $2;

-- name: GetWorkspaceMemberRole :one
SELECT role FROM workspace_members
WHERE workspace_id = $1 AND member_id = $2;

-- name: GetFirstWorkspaceForMember :one
SELECT * FROM workspace_members
WHERE member_id = $1
ORDER BY created_at ASC
LIMIT 1;
