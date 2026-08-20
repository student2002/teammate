-- 社区工作流查询

-- name: CreateCommunityWorkflow :one
INSERT INTO community_workflows (
    name, description, author, version, workflow_definition,
    required_skills, required_mcp_servers, recommended_agent_instructions,
    downloads, is_official
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10
) RETURNING *;

-- name: ListCommunityWorkflows :many
SELECT * FROM community_workflows
ORDER BY downloads DESC, created_at DESC;

-- name: GetCommunityWorkflow :one
SELECT * FROM community_workflows
WHERE id = $1;
