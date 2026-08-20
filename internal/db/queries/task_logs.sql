-- 任务日志查询

-- name: CreateTaskLog :one
INSERT INTO task_logs (
    task_id, node_id, type, content, timestamp
) VALUES (
    $1, $2, $3, $4, $5
) RETURNING *;

-- name: ListTaskLogsByTask :many
SELECT * FROM task_logs
WHERE task_id = $1
ORDER BY timestamp ASC, id ASC;

-- name: ListTaskLogsByTaskNode :many
SELECT * FROM task_logs
WHERE task_id = $1 AND node_id = $2
ORDER BY timestamp ASC, id ASC;
