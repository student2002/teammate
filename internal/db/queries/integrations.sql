-- Integration queries (V2): project-level external integrations (first
-- version: github). Secrets stay in the encrypted columns; no query ever
-- returns them to the API layer unmasked.

-- name: CreateIntegration :one
INSERT INTO integrations (
  workspace_id, project_id, provider, name, config,
  encrypted_secret, pat_masked, encrypted_webhook_secret, status, created_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetIntegration :one
SELECT * FROM integrations WHERE id = $1;

-- name: ListIntegrationsByProject :many
SELECT * FROM integrations
WHERE project_id = $1 AND provider = $2
ORDER BY created_at DESC;

-- name: GetProjectIntegration :one
-- The project's first integration of one provider (used by action.github to
-- resolve the project's github credentials).
SELECT * FROM integrations
WHERE project_id = $1 AND provider = $2
ORDER BY created_at
LIMIT 1;

-- name: UpdateIntegration :one
UPDATE integrations
SET name = $2, config = $3, encrypted_secret = $4, pat_masked = $5,
    encrypted_webhook_secret = $6, status = $7, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateIntegrationStatus :one
UPDATE integrations
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteIntegration :exec
DELETE FROM integrations WHERE id = $1;

-- name: ListWorkspaceIntegrations :many
-- Every integration in the workspace with its project's display name; the
-- workflow editor's integration pickers list them across projects.
SELECT i.*, p.name AS project_name
FROM integrations i
JOIN projects p ON p.id = i.project_id
WHERE i.workspace_id = $1
ORDER BY p.name, i.created_at;
