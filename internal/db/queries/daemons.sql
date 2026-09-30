-- daemons.sql — agentd 实例(机器)的持久化查询。
-- 一个 daemon 对应一台工作机上的 agentd 进程;agent 实例由其注册时上报(agents.daemon_id)。

-- name: CreateDaemon :one
INSERT INTO daemons (workspace_id, name)
VALUES ($1, $2)
RETURNING *;

-- name: GetDaemon :one
SELECT * FROM daemons WHERE id = $1;

-- name: ListDaemonsByWorkspace :many
SELECT * FROM daemons WHERE workspace_id = $1 ORDER BY created_at DESC;

-- name: DeleteDaemon :execrows
DELETE FROM daemons WHERE id = $1;

-- name: DetachAgentsFromDaemon :execrows
-- 删除 daemon 时其上报的 agent 实例全部置离线,配置保留以便重新接入
UPDATE agents SET status = 'offline', updated_at = now() WHERE daemon_id = $1 AND status != 'offline';

-- name: UpdateDaemonRegistration :one
-- 注册时更新机器信息、工具检测快照与公钥,并置在线
UPDATE daemons
SET device_name = $2, version = $3, providers = $4,
    public_key = COALESCE(NULLIF($5, ''), public_key),
    status = 'online', last_heartbeat = now(), updated_at = now()
WHERE id = $1
RETURNING *;

-- name: TouchDaemonHeartbeat :execrows
UPDATE daemons SET last_heartbeat = now(), status = 'online', updated_at = now()
WHERE id = $1;

-- name: MarkStaleDaemons :many
-- 心跳超时的 daemon 置离线,返回受影响的 id 以联动其 agent 实例
UPDATE daemons SET status = 'offline', updated_at = now()
WHERE status = 'online'
  AND (last_heartbeat IS NULL OR last_heartbeat < now() - interval '100 seconds')
RETURNING id;

-- name: SetAgentsStatusByDaemon :execrows
UPDATE agents SET status = $2, updated_at = now()
WHERE daemon_id = $1 AND status != $2;

-- name: UpsertDaemonAgent :one
-- 注册上报的实例按 (daemon, name) upsert;冲突时保留 Git 身份/指令等人工配置
-- (Git 身份占位由调用方计算:同名 + @teammate.local,管理员可后续修正)
INSERT INTO agents (workspace_id, name, provider, status, daemon_id, origin, git_name, git_email)
VALUES ($1, $2, $3, 'online', $4, 'daemon', $5, $6)
ON CONFLICT (daemon_id, name) WHERE daemon_id IS NOT NULL
DO UPDATE SET provider = EXCLUDED.provider, status = 'online', updated_at = now()
RETURNING *;

-- name: AdoptUnmanagedAgentByDaemon :one
-- 同名且未纳管(daemon_id IS NULL)的既有 agent 由该 daemon 收编:
-- 保留人工配置,仅接管归属并置在线
UPDATE agents
SET daemon_id = $1, provider = $2, status = 'online', origin = 'daemon', updated_at = now()
WHERE workspace_id = $3 AND name = $4 AND daemon_id IS NULL
RETURNING *;

-- name: CreateDaemonAgent :one
-- Web 端在守护进程分组下创建的实例:待下发,agentd 收编(再次上报同名)后上线
INSERT INTO agents (workspace_id, name, provider, status, daemon_id, origin, git_name, git_email)
VALUES ($1, $2, $3, 'pending', $4, 'server', $5, $6)
RETURNING *;

-- name: ListPendingDaemonAgents :many
-- desired_agents:Web 创建、等待 agentd 收编的实例
SELECT * FROM agents WHERE daemon_id = $1 AND origin = 'server' AND status = 'pending';

-- name: ListAgentsByDaemon :many
SELECT * FROM agents WHERE daemon_id = $1 ORDER BY created_at;

-- name: CountDaemonAgents :one
SELECT COUNT(*)::int FROM agents WHERE daemon_id = $1;

-- name: GetOnlineDaemonIDByAgent :one
SELECT d.id FROM daemons d JOIN agents a ON a.daemon_id = d.id
WHERE a.id = $1 AND d.status = 'online';

-- name: GetDaemonIDByAgent :one
SELECT d.id FROM daemons d JOIN agents a ON a.daemon_id = d.id
WHERE a.id = $1;

-- name: OfflineUnreportedDaemonAgents :execrows
-- 上报中缺席的机器侧实例置离线;Web 下发(pending)实例不受影响
UPDATE agents SET status = 'offline', updated_at = now()
WHERE daemon_id = $1 AND origin = 'daemon' AND status = 'online'
  AND NOT (name = ANY($2::text[]));

-- name: SetDaemonAgentStatusByName :exec
UPDATE agents SET status = $3, updated_at = now()
WHERE daemon_id = $1 AND name = $2;

-- name: SetDaemonOffline :execrows
UPDATE daemons SET status = 'offline', updated_at = now() WHERE id = $1;
