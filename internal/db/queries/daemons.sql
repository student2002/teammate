-- daemons.sql — agentd 实例(机器)的持久化查询。
-- 一个 daemon 对应一台工作机上的 agentd 进程;agent 实例由 web 在 daemon 分组创建,
-- desired_agents 以全集声明式下发(agents.daemon_id),agentd 据此对账增/改/删,
-- 心跳上报其运行状态。

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

-- name: CreateDaemonAgent :one
-- Web 端在守护进程分组下创建的实例:待下发,经 desired_agents 全集下发后由心跳上报上线;
-- persona_key 为跨空间记忆标识(显式填写或服务端生成,创建后不可变,service 层保证非空);
-- git 身份与身份指令由操作员创建时显式提供
INSERT INTO agents (workspace_id, name, provider, instructions, status, daemon_id, origin, persona_key, git_name, git_email)
VALUES ($1, $2, $3, $4, 'pending', $5, 'server', $6, $7, $8)
RETURNING *;

-- name: ListAgentsByDaemon :many
-- desired_agents 全集:该 daemon 名下全部实例(与状态无关,声明式对账的期望集)
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
-- 心跳 busy 名单中缺席的机器侧实例置离线;pending(待下发)实例不受影响
UPDATE agents SET status = 'offline', updated_at = now()
WHERE daemon_id = $1 AND status = 'online'
  AND NOT (name = ANY($2::text[]));

-- name: SetDaemonAgentStatusByName :exec
UPDATE agents SET status = $3, updated_at = now()
WHERE daemon_id = $1 AND name = $2;

-- name: SetDaemonOffline :execrows
UPDATE daemons SET status = 'offline', updated_at = now() WHERE id = $1;
