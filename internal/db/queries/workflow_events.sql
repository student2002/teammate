-- Workflow event queries (V2): durable external events (plan §7.5) and the
-- flow.wait(github_event) waiter registrations (plan §7.10). Deliveries are
-- deduplicated by the (provider, integration_id, external_id) unique index;
-- the insert below relies on ON CONFLICT DO NOTHING for idempotent 202s.

-- name: CreateWorkflowEvent :execrows
INSERT INTO workflow_events (
  provider, integration_id, external_id, event_type, action, payload
) VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, integration_id, external_id) DO NOTHING;

-- name: ListPendingWorkflowEvents :many
SELECT * FROM workflow_events
WHERE status = 'pending'
ORDER BY received_at
LIMIT $1;

-- name: MarkWorkflowEventProcessing :one
-- Claim one pending event for processing; a row already claimed by another
-- scan does not match and returns no row.
UPDATE workflow_events
SET status = 'processing', attempts = attempts + 1, updated_at = now()
WHERE id = $1 AND status = 'pending'
RETURNING *;

-- name: ListStuckProcessingEvents :many
-- Events whose processing crashed mid-flight (lease window passed): requeued
-- while the attempts budget lasts, failed with a timeout error beyond it.
SELECT * FROM workflow_events
WHERE status = 'processing'
  AND updated_at < now() - interval '10 minutes'
ORDER BY received_at
LIMIT $1;

-- name: RequeueWorkflowEvent :exec
UPDATE workflow_events
SET status = 'pending', updated_at = now()
WHERE id = $1 AND status = 'processing';

-- name: ListLeaseExpiredActionNodes :many
-- Server-executed nodes whose claim lease expired: the executor process died
-- before reaching a terminal state. A running workflow.call node is always a
-- crash residue (a live call parks the node in waiting on the child task).
SELECT * FROM task_nodes
WHERE status = 'running'
  AND type_key IN ('action.github', 'action.http', 'action.create_task', 'workflow.call')
  AND updated_at < now() - GREATEST(COALESCE((config->>'timeout_minutes')::numeric, 10), 10) * interval '1 minute'
ORDER BY updated_at
LIMIT $1;

-- name: GetRunningAttemptNoByNode :one
SELECT attempt_no FROM task_node_attempts
WHERE task_node_id = $1 AND status = 'running'
ORDER BY attempt_no DESC
LIMIT 1;

-- name: ResolveWaitersByTask :exec
-- A terminal task resolves its live event waiters so late matching events
-- cannot resurrect nodes on a cancelled/failed run.
UPDATE workflow_event_waiters
SET status = 'resolved', resolved_at = now()
WHERE task_id = $1 AND status = 'waiting';

-- name: CompleteWorkflowEvent :exec
UPDATE workflow_events
SET status = 'completed', processed_at = now(), error = '', updated_at = now()
WHERE id = $1;

-- name: FailWorkflowEvent :exec
UPDATE workflow_events
SET status = 'failed', processed_at = now(), error = $2, updated_at = now()
WHERE id = $1;

-- name: CreateEventWaiter :one
INSERT INTO workflow_event_waiters (
  task_id, task_node_id, integration_id, event_type, filter
) VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: DeleteWaitingWaitersByNode :exec
-- Re-activating a wait node replaces its stale waiting registrations so a
-- node never holds two live waiters for the same event.
DELETE FROM workflow_event_waiters
WHERE task_node_id = $1 AND status = 'waiting';

-- name: ListWaitingEventWaiters :many
SELECT * FROM workflow_event_waiters
WHERE integration_id = $1 AND event_type = $2 AND status = 'waiting'
  AND (expires_at IS NULL OR expires_at > $3)
ORDER BY created_at;

-- name: ResolveEventWaiter :exec
UPDATE workflow_event_waiters
SET status = 'resolved', resolved_at = now()
WHERE id = $1;

-- name: ListGitHubTriggerNodesByIntegration :many
-- Every trigger.github template node bound to one integration, with its
-- template identity and enabled flag; the event service filters by
-- event/actions and the integration delete check counts enabled templates.
SELECT n.id AS node_id, n.config, n.type_key,
       t.id AS template_id, t.name AS template_name, t.enabled
FROM workflow_template_nodes n
JOIN workflow_templates t ON t.id = n.template_id
WHERE n.type_key = 'trigger.github' AND n.config->>'integration_id' = $1::text;

-- name: ListReadyGitHubActionNodes :many
-- Ready action.github nodes awaiting execution by the scheduler worker.
SELECT * FROM task_nodes
WHERE status = 'ready' AND type_key = 'action.github'
ORDER BY updated_at
LIMIT $1;
