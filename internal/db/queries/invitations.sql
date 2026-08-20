-- 邀请

-- name: CreateInvitation :one
INSERT INTO invitations (workspace_id, email, role, token_hash, invited_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetInvitationByToken :one
SELECT * FROM invitations
WHERE token_hash = $1 AND accepted_at IS NULL AND expires_at > NOW();

-- name: ListInvitations :many
SELECT * FROM invitations
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: AcceptInvitation :one
UPDATE invitations
SET accepted_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteInvitation :exec
DELETE FROM invitations
WHERE id = $1;

-- name: DeleteExpiredInvitations :exec
DELETE FROM invitations
WHERE expires_at < NOW() AND accepted_at IS NULL;
