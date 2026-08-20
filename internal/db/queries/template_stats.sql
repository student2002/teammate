-- 模板统计查询

-- name: GetTemplateStats :one
SELECT
    COUNT(*) AS usage_count,
    COALESCE(AVG(EXTRACT(EPOCH FROM (t.updated_at - t.created_at))), 0) AS avg_completion_seconds,
    COALESCE(
        SUM(CASE WHEN t.status = 'cancelled' THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0),
        0
    ) AS reject_rate
FROM tasks t
WHERE t.workflow_name = $1;
