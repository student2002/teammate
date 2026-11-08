-- Workflow run engine queries (V2): task instantiation from templates, node
-- run-state transitions with optimistic locking, attempts and due-node scans.
-- All multi-row reads run inside a transaction that holds the task's
-- pg_advisory_xact_lock (AcquireTaskRunLock), which serializes concurrent
-- advances of the same task without row locks.

-- name: AcquireTaskRunLock :exec
SELECT pg_advisory_xact_lock($1::bigint);

-- name: GetProjectNextTaskSequence :one
SELECT COALESCE(MAX(sequence), 0) + 1 AS next_sequence FROM tasks
WHERE project_id = $1;

-- name: CreateTaskForRun :one
-- Run instantiation insert: type/priority/status and failure use the column
-- defaults ('task'/'medium'/'active'/'{}'); sequence is passed by the caller.
INSERT INTO tasks (
    project_id, workflow_name, workflow_template_id, workflow_version,
    trigger_payload, context, title, author_type, author_id, sequence
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
) RETURNING *;

-- name: UpdateTaskRunState :one
-- Engine-owned task fields: status (active/completed/failed/cancelled),
-- run context and failure detail. Versioned fields like context are written
-- under the task advisory lock, so no optimistic check is needed here.
UPDATE tasks
SET status = $2,
    context = $3,
    failure = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: InsertTaskNodeForRun :one
-- Copies one template node into the task graph. Trigger nodes pass their
-- terminal fields (succeeded status, output, attempt_count 1, completed_at);
-- waiting nodes pass the zero values ('{}', 0, NULL).
INSERT INTO task_nodes (
    task_id, template_node_id, name, sort_order, type_key, config,
    status, activation_mode, output, attempt_count, max_attempts,
    readonly_dirs, full_control_dirs, completed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
) RETURNING *;

-- name: InsertTaskEdgeForRun :one
-- Copies one template edge into the task graph with pending resolution.
INSERT INTO task_edges (
    task_id, source_node_id, source_port, target_node_id, target_port, label, config
) VALUES (
    $1, $2, $3, $4, $5, $6, $7
) RETURNING *;

-- name: UpdateTaskEdgeResolution :exec
-- Idempotent: only pending edges resolve, so a concurrent (serialized) writer
-- can never overwrite an existing resolution.
UPDATE task_edges
SET resolution = $2, resolved_at = now()
WHERE id = $1 AND resolution = 'pending';

-- name: UpdateTaskNodeRun :one
-- Optimistic full-state write of one run node; the WHERE clause pins the
-- version the caller loaded. No affected row means a concurrent state change.
UPDATE task_nodes
SET status = $2,
    input = $3,
    output = $4,
    error = $5,
    attempt_count = $6,
    max_attempts = $7,
    next_retry_at = $8,
    wait_key = $9,
    wait_expires_at = $10,
    completed_at = $11,
    reserved_for_agent_id = $12,
    version = version + 1,
    updated_at = now()
WHERE id = $1 AND version = $13
RETURNING version;

-- name: CancelNonTerminalRunNodes :many
-- A terminal task cancels its waiting/ready siblings (§6.2 cancelled: 上游终止).
UPDATE task_nodes
SET status = 'cancelled',
    updated_at = now()
WHERE task_id = $1 AND status IN ('waiting', 'ready')
RETURNING id;

-- name: CreateTaskAttempt :one
-- Upsert keyed by the (task_node_id, attempt_no) unique index: the claim-time
-- running row (agentd protocol) is updated in place, an absent row is created
-- directly in its terminal state (server-executed nodes).
INSERT INTO task_node_attempts (
    task_id, task_node_id, attempt_no, executor_kind, executor_id,
    status, input, output, error, completed_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10
)
ON CONFLICT (task_node_id, attempt_no) DO UPDATE
SET status = EXCLUDED.status,
    output = EXCLUDED.output,
    error = EXCLUDED.error,
    completed_at = EXCLUDED.completed_at
RETURNING *;

-- name: GetTaskAttemptsByTask :many
SELECT * FROM task_node_attempts
WHERE task_id = $1
ORDER BY task_node_id, attempt_no;

-- name: ListRetryDueNodes :many
-- Waiting nodes whose scheduled retry time has passed on active tasks.
SELECT tn.* FROM task_nodes tn
JOIN tasks t ON t.id = tn.task_id
WHERE tn.status = 'waiting'
  AND tn.next_retry_at IS NOT NULL AND tn.next_retry_at <= $1
  AND t.status = 'active'
ORDER BY tn.next_retry_at
LIMIT $2;

-- name: ListWaitDueNodes :many
-- Waiting duration-wait nodes whose wake time has passed on active tasks.
SELECT tn.* FROM task_nodes tn
JOIN tasks t ON t.id = tn.task_id
WHERE tn.status = 'waiting'
  AND tn.wait_key LIKE 'duration:%'
  AND tn.wait_expires_at IS NOT NULL AND tn.wait_expires_at <= $1
  AND t.status = 'active'
ORDER BY tn.wait_expires_at
LIMIT $2;

-- name: GetTaskAttempt :one
SELECT * FROM task_node_attempts
WHERE id = $1;

-- name: UpdateTaskNodeSummary :exec
-- Summary is written only by the agentd completion receipt; the engine's
-- full-state node write never touches it.
UPDATE task_nodes
SET summary = $2, updated_at = now()
WHERE id = $1;

-- name: ListReadyAsyncActionNodes :many
-- Ready server-executed async nodes (S6): HTTP requests, task creation and
-- sub-workflow calls awaiting the scheduler worker.
SELECT * FROM task_nodes
WHERE status = 'ready'
  AND type_key IN ('action.http', 'action.create_task', 'workflow.call')
ORDER BY updated_at
LIMIT $1;

-- name: CreateSpawnedTask :one
-- Plain task created by an action.create_task node: type 'task', default
-- active status, no run graph attached.
INSERT INTO tasks (
    project_id, title, description, type, author_type, author_id, sequence
) VALUES (
    $1, $2, $3, 'task', $4, $5, $6
)
RETURNING *;

-- name: SetTaskParent :exec
UPDATE tasks
SET parent_task_id = $2, updated_at = now()
WHERE id = $1;

-- name: GetTaskNodeByWaitKey :one
SELECT * FROM task_nodes
WHERE task_id = $1 AND wait_key = $2;

-- name: ListCallWaitNodes :many
-- Waiting sub-workflow call nodes (wait_key 'call:{childTaskID}') whose child
-- task reached a terminal state: the parent-wake scan's work list.
SELECT tn.* FROM task_nodes tn
JOIN tasks pt ON pt.id = tn.task_id
JOIN tasks ct ON ct.id = split_part(tn.wait_key, ':', 2)::int
WHERE tn.status = 'waiting'
  AND tn.wait_key LIKE 'call:%'
  AND pt.status = 'active'
  AND ct.status IN ('completed', 'failed')
ORDER BY tn.updated_at
LIMIT $1;

-- name: ListActiveChildTaskIDs :many
SELECT id FROM tasks
WHERE parent_task_id = $1 AND status = 'active'
ORDER BY id;

-- name: CancelRunTask :exec
UPDATE tasks
SET status = 'cancelled', updated_at = now()
WHERE id = $1 AND status = 'active';

-- name: ListAgentInProgressAttempts :many
-- Running agentd attempts of one agent instance with their node and project
-- context: the recovery view behind GET .../agents/{agentId}/in-progress-nodes.
SELECT sqlc.embed(tn), a.id AS execution_id, t.project_id
FROM task_node_attempts a
JOIN task_nodes tn ON tn.id = a.task_node_id
JOIN tasks t ON t.id = a.task_id
WHERE a.executor_kind = 'agentd'
  AND a.executor_id = $1
  AND a.status = 'running'
ORDER BY a.started_at;
