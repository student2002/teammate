-- Execution session queries

-- name: CreateExecutionSession :one
INSERT INTO execution_sessions (daemon_id, agent_id, task_node_id, attempt, status, workdir, branch, base_commit, claude_session_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: GetExecutionSession :one
SELECT * FROM execution_sessions WHERE id = $1;

-- name: GetActiveSessionByAgentAndWorkdir :one
SELECT * FROM execution_sessions
WHERE agent_id = $1 AND workdir = $2 AND status = 'completed' AND claude_session_id != ''
ORDER BY completed_at DESC
LIMIT 1;

-- name: UpdateExecutionSession :one
UPDATE execution_sessions
SET status = $2, head_commit = $3, completed_at = $4, interrupted_at = $5, claude_session_id = $6
WHERE id = $1
RETURNING *;

-- name: UpdateSessionClaudeID :one
UPDATE execution_sessions
SET claude_session_id = $2
WHERE id = $1
RETURNING *;

-- name: CompleteExecutionSession :one
UPDATE execution_sessions
SET status = 'completed', head_commit = $2, completed_at = now()
WHERE id = $1
RETURNING *;

-- name: InterruptExecutionSession :one
UPDATE execution_sessions
SET status = 'interrupted', interrupted_at = now()
WHERE id = $1
RETURNING *;

-- name: GetSessionsByTaskNode :many
SELECT * FROM execution_sessions
WHERE task_node_id = $1
ORDER BY started_at DESC;

-- name: GetLatestCompletedSessionByAgent :one
SELECT * FROM execution_sessions
WHERE agent_id = $1 AND status = 'completed' AND claude_session_id != ''
ORDER BY completed_at DESC
LIMIT 1;

-- name: ListAgentExecutions :many
SELECT es.id, es.task_node_id, tn.name AS node_name,
       t.id AS task_id, t.title AS task_title,
       es.status, es.branch, es.started_at, es.completed_at
FROM execution_sessions es
LEFT JOIN task_nodes tn ON tn.id = es.task_node_id
LEFT JOIN tasks t ON t.id = tn.task_id
WHERE es.agent_id = $1
ORDER BY es.started_at DESC
LIMIT $2;
