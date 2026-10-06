-- Template statistics queries

-- name: GetTemplateStats :one
-- Aggregated per template id (tasks carry the id at creation), so renaming a
-- template never drifts its statistics. reject_rate is the share of tasks
-- that had at least one node rejection.
SELECT
    COUNT(*) AS usage_count,
    COALESCE(AVG(EXTRACT(EPOCH FROM (t.updated_at - t.created_at))), 0) AS avg_completion_seconds,
    COALESCE(
        SUM(CASE WHEN EXISTS (
            SELECT 1 FROM task_nodes tn WHERE tn.task_id = t.id AND tn.reject_count > 0
        ) THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0),
        0
    ) AS reject_rate
FROM tasks t
WHERE t.workflow_template_id = $1;
