-- 项目查询

-- name: CreateProject :one
INSERT INTO projects (workspace_id, name, description, icon, status, repo_url, context)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects
WHERE workspace_id = $1
ORDER BY created_at DESC;

-- name: ListProjectsByAgentMembership :many
SELECT p.* FROM projects p
JOIN project_members pm ON pm.project_id = p.id AND pm.member_type = 'agent' AND pm.agent_id = $2
WHERE p.workspace_id = $1
ORDER BY p.created_at DESC;

-- name: UpdateProject :one
UPDATE projects
SET name = $2, description = $3, status = $4, repo_url = $5, context = $6,
    default_workflow_id = COALESCE(sqlc.narg('default_workflow_id'), default_workflow_id),
    max_review_cycles = COALESCE(sqlc.narg('max_review_cycles'), max_review_cycles),
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects
WHERE id = $1;

-- name: CreateProjectMember :one
INSERT INTO project_members (project_id, member_type, agent_id, member_id, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetProjectMember :one
SELECT * FROM project_members
WHERE id = $1;

-- name: ListProjectMembers :many
SELECT * FROM project_members
WHERE project_id = $1;

-- name: DeleteProjectMember :exec
DELETE FROM project_members
WHERE id = $1;

-- name: ListProjectAgents :many
SELECT * FROM project_members
WHERE project_id = $1 AND member_type = 'agent';

-- name: IsMemberProjectMember :one
SELECT EXISTS(
  SELECT 1 FROM project_members
  WHERE project_id = $1 AND member_id = $2 AND member_type = 'human'
);

-- name: GetProjectMemberRole :one
SELECT role FROM project_members
WHERE project_id = $1 AND member_id = $2 AND member_type = 'human';

-- name: CreateProjectReviewer :one
INSERT INTO project_reviewers (project_id, member_type, agent_id, member_id)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListProjectReviewers :many
SELECT * FROM project_reviewers
WHERE project_id = $1;

-- name: DeleteProjectReviewer :exec
DELETE FROM project_reviewers
WHERE id = $1;

-- name: GetProjectReviewerByID :one
SELECT * FROM project_reviewers WHERE id = $1;
