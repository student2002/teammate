-- Custom theme queries (runtime-uploaded themes, deployment-shared)

-- name: CreateCustomTheme :one
INSERT INTO custom_themes (id, name_zh, name_en, icon, vars, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: ListCustomThemes :many
SELECT * FROM custom_themes
ORDER BY created_at ASC;

-- name: DeleteCustomTheme :execrows
DELETE FROM custom_themes
WHERE id = $1;
