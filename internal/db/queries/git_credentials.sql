-- Git 凭据查询

-- name: CreateGitCredential :one
INSERT INTO git_credentials (project_id, repo_url, username, encrypted_pat, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetGitCredential :one
SELECT * FROM git_credentials
WHERE id = $1;

-- name: ListGitCredentialsByProject :many
SELECT * FROM git_credentials
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: UpdateGitCredential :one
UPDATE git_credentials
SET repo_url = $2, username = $3, encrypted_pat = $4, updated_at = now()
WHERE id = $1
RETURNING *;
