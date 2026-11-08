-- Template statistics queries

-- name: GetTemplateStats :one
-- Aggregated per template id (tasks carry the id at creation), so renaming a
-- template never drifts its statistics.
SELECT
    COUNT(*) AS usage_count,
    COALESCE(AVG(EXTRACT(EPOCH FROM (t.updated_at - t.created_at))), 0) AS avg_completion_seconds
FROM tasks t
WHERE t.workflow_template_id = $1;
