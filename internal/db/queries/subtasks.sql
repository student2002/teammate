-- 子任务查询

-- name: CreateSubtask :one
INSERT INTO tasks (
    project_id, title, description, constraints,
    type, priority, status, author_type, author_id, due_date, labels, sequence, workflow_name, parent_task_id
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: ListSubtasks :many
SELECT * FROM tasks
WHERE parent_task_id = $1
ORDER BY created_at ASC;
