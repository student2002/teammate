-- Git credential queries (one credential per (project, member agent))

-- name: CreateGitCredential :one
INSERT INTO git_credentials (project_id, agent_id, username, encrypted_pat, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetGitCredential :one
SELECT * FROM git_credentials
WHERE id = $1;

-- name: ListGitCredentialsByProject :many
SELECT * FROM git_credentials
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: ListGitCredentialsByProjectAndAgent :many
SELECT * FROM git_credentials
WHERE project_id = $1 AND agent_id = $2;

-- name: UpdateGitCredential :one
UPDATE git_credentials
SET username = $2, encrypted_pat = $3, updated_at = now()
WHERE id = $1
RETURNING *;
