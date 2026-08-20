-- Agent 查询

-- name: CreateAgent :one
INSERT INTO agents (workspace_id, name, provider, instructions, model, status, custom_env, extra_args, git_name, git_email)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING *;

-- name: GetAgent :one
SELECT * FROM agents
WHERE id = $1;

-- name: ListAgents :many
SELECT * FROM agents
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: UpdateAgent :one
UPDATE agents
SET instructions = COALESCE(sqlc.narg('instructions'), instructions),
    model = COALESCE(sqlc.narg('model'), model),
    status = COALESCE(sqlc.narg('status'), status),
    custom_env = COALESCE(sqlc.narg('custom_env'), custom_env),
    extra_args = COALESCE(sqlc.narg('extra_args'), extra_args),
    git_name = COALESCE(sqlc.narg('git_name'), git_name),
    git_email = COALESCE(sqlc.narg('git_email'), git_email),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteAgent :exec
DELETE FROM agents
WHERE id = $1;

-- name: UpdateAgentStatus :one
UPDATE agents
SET status = $2, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateSkill :one
INSERT INTO skills (workspace_id, name, description, category, prompt_template)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetSkill :one
SELECT * FROM skills
WHERE id = $1;

-- name: ListSkills :many
SELECT * FROM skills
WHERE workspace_id = $1
ORDER BY name;

-- name: DeleteSkill :exec
DELETE FROM skills
WHERE id = $1;

-- name: CreateMcpServer :one
INSERT INTO mcp_servers (workspace_id, name, url, type, auth_type, env_vars, status)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetMcpServer :one
SELECT * FROM mcp_servers
WHERE id = $1;

-- name: ListMcpServers :many
SELECT * FROM mcp_servers
WHERE workspace_id = $1
ORDER BY name;

-- name: UpdateMcpServerStatus :one
UPDATE mcp_servers
SET status = $2
WHERE id = $1
RETURNING *;

-- name: DeleteMcpServer :exec
DELETE FROM mcp_servers
WHERE id = $1;

-- name: UpdateMcpServer :one
UPDATE mcp_servers
SET name = $2, url = $3, type = $4, auth_type = $5, env_vars = $6, status = $7
WHERE id = $1
RETURNING *;

-- name: UpdateSkill :one
UPDATE skills
SET name = $2, description = $3, category = $4, prompt_template = $5
WHERE id = $1
RETURNING *;

-- name: AddAgentSkill :one
INSERT INTO agent_skills (agent_id, skill_id, enabled)
VALUES ($1, $2, $3)
ON CONFLICT (agent_id, skill_id) DO UPDATE
SET enabled = EXCLUDED.enabled
RETURNING *;

-- name: RemoveAgentSkill :exec
DELETE FROM agent_skills
WHERE agent_id = $1 AND skill_id = $2;

-- name: ListAgentSkills :many
SELECT s.*, as_rel.enabled, as_rel.created_at AS assigned_at
FROM agent_skills as_rel
JOIN skills s ON s.id = as_rel.skill_id
WHERE as_rel.agent_id = $1;

-- name: AddAgentMcpServer :one
INSERT INTO agent_mcp_servers (agent_id, mcp_server_id, enabled)
VALUES ($1, $2, $3)
ON CONFLICT (agent_id, mcp_server_id) DO UPDATE
SET enabled = EXCLUDED.enabled
RETURNING *;

-- name: RemoveAgentMcpServer :exec
DELETE FROM agent_mcp_servers
WHERE agent_id = $1 AND mcp_server_id = $2;

-- name: ListAgentMcpServers :many
SELECT ms.*, amr.enabled, amr.created_at AS assigned_at
FROM agent_mcp_servers amr
JOIN mcp_servers ms ON ms.id = amr.mcp_server_id
WHERE amr.agent_id = $1;
