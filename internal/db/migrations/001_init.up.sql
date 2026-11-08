-- 001_init.sql
-- Teammate database initialization migration (workflow V2 target schema)
-- Schema changes rewrite this file directly and reset dev/acceptance databases (项目未上线,不保留旧结构)

-- ============================================================
-- 0. Extensions
-- ============================================================

CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================
-- 1. Enum types (must be defined before CREATE TABLE)
-- ============================================================

CREATE TYPE agent_provider AS ENUM ('claude', 'openclaw', 'opencode', 'atomcode', 'mimocode', 'copilot', 'hermes', 'gemini', 'pi', 'cursor', 'kimi', 'kiro');
CREATE TYPE agent_status AS ENUM ('online', 'offline', 'busy', 'paused', 'pending');
CREATE TYPE project_status AS ENUM ('planned', 'active', 'paused', 'completed', 'archived');
CREATE TYPE task_type AS ENUM ('story', 'bug', 'task');
CREATE TYPE task_priority AS ENUM ('urgent', 'high', 'medium', 'low');
-- 任务终态由 flow.end 的入边端口决定:completed / failed
CREATE TYPE task_status AS ENUM ('active', 'completed', 'failed', 'cancelled');
-- V2 节点状态机:waiting(等入边/定时/事件) → ready → running → succeeded/failed;skipped 为未激活分支
CREATE TYPE task_node_status AS ENUM ('waiting', 'ready', 'running', 'succeeded', 'failed', 'skipped', 'cancelled', 'manual_intervention');
-- 节点状态迁移动作,与 V2 状态机的迁移一一对应
CREATE TYPE transition_action AS ENUM ('ready', 'start', 'complete', 'fail', 'retry', 'skip', 'cancel', 'manual', 'timeout', 'interrupt_ack');
CREATE TYPE edge_resolution AS ENUM ('pending', 'activated', 'skipped');
CREATE TYPE attempt_status AS ENUM ('running', 'succeeded', 'failed', 'cancelled');
CREATE TYPE executor_kind AS ENUM ('server', 'agentd', 'human');
CREATE TYPE runtime_status AS ENUM ('online', 'offline', 'error');
CREATE TYPE daemon_status AS ENUM ('online', 'offline');
CREATE TYPE memory_type AS ENUM ('architecture', 'command', 'convention', 'decision', 'insight', 'environment');
CREATE TYPE token_type AS ENUM ('api', 'session', 'task', 'password_reset');
CREATE TYPE mcp_auth_type AS ENUM ('none', 'api_key', 'oauth');

-- ============================================================
-- 2. Sequences
-- ============================================================

CREATE SEQUENCE IF NOT EXISTS tasks_id_seq;

-- ============================================================
-- 3. Tables
-- ============================================================

-- 3.1 workspaces
CREATE TABLE workspaces (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         VARCHAR(200) NOT NULL,
    description  TEXT DEFAULT '',
    issue_prefix VARCHAR(10) NOT NULL DEFAULT 'MUL',
    is_default   BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 3.2 members (human)
CREATE TABLE members (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(200) NOT NULL,
    email         VARCHAR(300) NOT NULL UNIQUE,
    password_hash VARCHAR(200) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_members_email ON members(email);

-- 3.2b workspace_members — workspace-member association (many-to-many)
CREATE TABLE workspace_members (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id  UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    member_id     UUID NOT NULL REFERENCES members(id) ON DELETE CASCADE,
    role          VARCHAR(20) NOT NULL DEFAULT 'member',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, member_id)
);
CREATE INDEX idx_workspace_members_workspace ON workspace_members(workspace_id);
CREATE INDEX idx_workspace_members_member ON workspace_members(member_id);

-- 3.3 agents
CREATE TABLE agents (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name            VARCHAR(200) NOT NULL,
    provider        agent_provider NOT NULL,
    instructions    TEXT NOT NULL DEFAULT '',
    model           VARCHAR(100) DEFAULT '',
    status          agent_status NOT NULL DEFAULT 'offline',
    custom_env      JSONB DEFAULT '{}',
    extra_args      TEXT[] DEFAULT '{}',
    -- persona_key — the cross-workspace memory identity (记忆标识). Instances
    -- with the same persona_key on one agentd machine share one unified memory.
    -- Blank means "not set" (the web create path always fills it: explicit
    -- operator input or a generated unique value); blank rows are never
    -- delivered-materialized by agentd. Immutable after creation.
    persona_key     VARCHAR(64) NOT NULL DEFAULT '',
    git_name        VARCHAR(200) DEFAULT '',
    git_email       VARCHAR(300) DEFAULT '',
    daemon_id       UUID,
    origin          VARCHAR(10) NOT NULL DEFAULT 'daemon',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agents_workspace ON agents(workspace_id);
CREATE INDEX idx_agents_status ON agents(status);
-- Agent identity is (daemon, name); agents with no daemon are not constrained
CREATE UNIQUE INDEX idx_agents_daemon_name ON agents(daemon_id, name) WHERE daemon_id IS NOT NULL;

-- 3.4 workflow_templates — workflow templates
-- 触发配置全部进入唯一的 trigger 节点(version 在每次整图保存时 +1)
CREATE TABLE workflow_templates (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         VARCHAR(200) NOT NULL,
    description  TEXT DEFAULT '',
    is_builtin   BOOLEAN NOT NULL DEFAULT false,
    version      INTEGER NOT NULL DEFAULT 1,
    enabled      BOOLEAN NOT NULL DEFAULT true,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wf_templates_workspace ON workflow_templates(workspace_id);

-- 3.5 workflow_template_nodes — template node definitions
-- type_key 不用数据库 enum(节点目录由 Server registry 持有);ui_config 只存画布位置与折叠态
CREATE TABLE workflow_template_nodes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    sort_order  INTEGER NOT NULL,
    type_key    TEXT NOT NULL,
    config      JSONB NOT NULL DEFAULT '{}',
    ui_config   JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT workflow_template_nodes_template_sort_order_key UNIQUE (template_id, sort_order)
);
CREATE INDEX idx_wf_template_nodes_template ON workflow_template_nodes(template_id);

-- 3.5a workflow_template_schedules — 定时触发节点的到期状态
-- 模板保存时由 Service 同步:trigger.schedule 节点的 interval_minutes 决定
-- next_run_at;触发由 scheduler 周期扫描,CAS 推进防多实例重复触发。
-- 无外键:模板删除时应用层清理
CREATE TABLE workflow_template_schedules (
    template_id   UUID PRIMARY KEY,
    project_id    UUID NOT NULL,
    next_run_at   TIMESTAMPTZ NOT NULL,
    last_fired_at TIMESTAMPTZ,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wf_template_schedules_due ON workflow_template_schedules(next_run_at);

-- 3.5b workflow_template_edges — 模板连线:source 端口激活时唤醒 target
CREATE TABLE workflow_template_edges (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id    UUID NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    source_node_id UUID NOT NULL,
    source_port    TEXT NOT NULL,
    target_node_id UUID NOT NULL,
    target_port    TEXT NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    sort_order     INTEGER NOT NULL DEFAULT 0,
    config         JSONB NOT NULL DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- 同一 (source, source_port, target) 唯一:一个输出端口到同一目标只有一条边
    UNIQUE(template_id, source_node_id, source_port, target_node_id)
);
CREATE INDEX idx_wf_template_edges_template ON workflow_template_edges(template_id);

-- 3.6 projects
CREATE TABLE projects (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                  VARCHAR(200) NOT NULL,
    description           TEXT DEFAULT '',
    icon                  VARCHAR(50) DEFAULT '',
    status                project_status NOT NULL DEFAULT 'planned',
    repo_url              VARCHAR(500) DEFAULT '',
    context               TEXT DEFAULT '',
    max_review_cycles     INTEGER NOT NULL DEFAULT 5,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);
CREATE INDEX idx_projects_workspace ON projects(workspace_id);

-- 3.7 project_members — project members
CREATE TABLE project_members (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    member_type VARCHAR(10) NOT NULL,
    agent_id    UUID REFERENCES agents(id) ON DELETE CASCADE,
    member_id   UUID REFERENCES members(id) ON DELETE CASCADE,
    role        VARCHAR(20) NOT NULL DEFAULT 'developer',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, member_type, agent_id),
    UNIQUE(project_id, member_type, member_id)
);
CREATE INDEX idx_project_members_project ON project_members(project_id);
CREATE INDEX idx_project_members_agent ON project_members(agent_id);

-- 3.8 tasks (id is an auto-increment integer within the workspace)
-- workflow_version/source_event_id/trigger_payload 是实例化时的运行上下文快照;
-- context 承载 trigger/task/vars/nodes 四根,在推进过程中由引擎维护
CREATE TABLE tasks (
    id             INTEGER PRIMARY KEY DEFAULT nextval('tasks_id_seq'),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_name  TEXT NOT NULL DEFAULT '',
    workflow_template_id UUID,
    workflow_version     INTEGER NOT NULL DEFAULT 1,
    source_event_id      UUID,
    trigger_payload      JSONB NOT NULL DEFAULT '{}',
    context              JSONB NOT NULL DEFAULT '{}',
    failure              JSONB NOT NULL DEFAULT '{}',
    title          VARCHAR(500) NOT NULL,
    description    TEXT DEFAULT '',
    constraints    TEXT DEFAULT '',
    type           task_type NOT NULL DEFAULT 'task',
    priority       task_priority NOT NULL DEFAULT 'medium',
    status         task_status NOT NULL DEFAULT 'active',
    author_type    VARCHAR(10) NOT NULL DEFAULT 'human',
    author_id      UUID NOT NULL,
    due_date       TIMESTAMPTZ,
    labels         TEXT[] DEFAULT '{}',
    sequence       INTEGER NOT NULL,
    parent_task_id INTEGER REFERENCES tasks(id) ON DELETE CASCADE,
    git_branch     TEXT DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_tasks_project ON tasks(project_id);
CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_project_status ON tasks(project_id, status);
-- sequence 是同工程内的展示排序号;唯一约束把并发创建的 MAX+1 竞争变成可见错误
CREATE UNIQUE INDEX idx_tasks_project_sequence ON tasks(project_id, sequence);

-- 3.9 task_nodes — task node instances(模板节点的运行快照)
-- input 在进入 ready 前由 Server 解析表达式并快照;agentd 不解析工作流表达式
CREATE TABLE task_nodes (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id               INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    template_node_id      UUID NOT NULL,
    name                  VARCHAR(200) NOT NULL,
    sort_order            INTEGER NOT NULL,
    type_key              TEXT NOT NULL,
    config                JSONB NOT NULL DEFAULT '{}',
    status                task_node_status NOT NULL DEFAULT 'waiting',
    activation_mode       VARCHAR(10) NOT NULL DEFAULT 'all' CHECK (activation_mode IN ('all', 'any')),
    input                 JSONB NOT NULL DEFAULT '{}',
    output                JSONB NOT NULL DEFAULT '{}',
    error                 JSONB NOT NULL DEFAULT '{}',
    attempt_count         INTEGER NOT NULL DEFAULT 0,
    max_attempts          INTEGER NOT NULL DEFAULT 1,
    next_retry_at         TIMESTAMPTZ,
    wait_key              TEXT NOT NULL DEFAULT '',
    wait_expires_at       TIMESTAMPTZ,
    version               INTEGER NOT NULL DEFAULT 1,
    reserved_for_agent_id UUID,
    summary               TEXT NOT NULL DEFAULT '',
    readonly_dirs         JSONB DEFAULT '[]',
    full_control_dirs     JSONB DEFAULT '[]',
    completed_at          TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- 一个模板节点在一个任务里只有一个实例(运行快照)
    UNIQUE(task_id, template_node_id)
);
CREATE INDEX idx_task_nodes_task ON task_nodes(task_id);
CREATE INDEX idx_task_nodes_status ON task_nodes(status);
CREATE INDEX idx_task_nodes_reserved ON task_nodes(reserved_for_agent_id);
CREATE INDEX idx_task_nodes_task_status ON task_nodes(task_id, status);
-- 调度扫描:到期重试与到期等待
CREATE INDEX idx_task_nodes_retry_due ON task_nodes(next_retry_at)
    WHERE status = 'waiting' AND next_retry_at IS NOT NULL;
CREATE INDEX idx_task_nodes_wait_due ON task_nodes(wait_expires_at)
    WHERE status = 'waiting' AND wait_key != '';

-- 3.9b task_edges — 模板边的任务快照与激活状态
CREATE TABLE task_edges (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id        INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    source_node_id UUID NOT NULL,
    source_port    TEXT NOT NULL,
    target_node_id UUID NOT NULL,
    target_port    TEXT NOT NULL,
    label          TEXT NOT NULL DEFAULT '',
    config         JSONB NOT NULL DEFAULT '{}',
    resolution     edge_resolution NOT NULL DEFAULT 'pending',
    resolved_at    TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_task_edges_task ON task_edges(task_id);
CREATE INDEX idx_task_edges_target ON task_edges(task_id, target_node_id);

-- 3.9c task_node_attempts — 每次真正执行一条尝试记录
CREATE TABLE task_node_attempts (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id       INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    task_node_id  UUID NOT NULL,
    attempt_no    INTEGER NOT NULL,
    executor_kind executor_kind NOT NULL DEFAULT 'server',
    executor_id   TEXT NOT NULL DEFAULT '',
    status        attempt_status NOT NULL DEFAULT 'running',
    input         JSONB NOT NULL DEFAULT '{}',
    output        JSONB NOT NULL DEFAULT '{}',
    error         JSONB NOT NULL DEFAULT '{}',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    UNIQUE(task_node_id, attempt_no)
);
CREATE INDEX idx_task_node_attempts_node ON task_node_attempts(task_node_id);

-- 3.10 node_transitions — node transition history
CREATE TABLE node_transitions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_node_id   UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
    from_status    task_node_status NOT NULL,
    to_status      task_node_status NOT NULL,
    action         transition_action NOT NULL,
    target_node_id UUID,
    comment        TEXT DEFAULT '',
    operator_id    UUID,
    operator_type  VARCHAR(10) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_node_transitions_node ON node_transitions(task_node_id);
CREATE INDEX idx_node_transitions_time ON node_transitions(created_at);

-- 3.11 comments — comments
CREATE TABLE comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    node_id     UUID,
    source_node_id UUID,
    parent_id   UUID REFERENCES comments(id) ON DELETE CASCADE,
    author_type VARCHAR(10) NOT NULL,
    author_id   UUID NOT NULL,
    content     TEXT NOT NULL,
    comment_type VARCHAR(20) NOT NULL DEFAULT 'text',
    metadata    JSONB DEFAULT '{}',
    mentions    UUID[] DEFAULT '{}',
    edited_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_comments_task ON comments(task_id);
CREATE INDEX idx_comments_node ON comments(node_id);
CREATE INDEX idx_comments_source_node ON comments(source_node_id);
CREATE INDEX idx_comments_parent ON comments(parent_id);

-- 3.13 task_logs — persisted execution logs
CREATE TABLE task_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id    INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    node_id    UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
    type       TEXT NOT NULL CHECK (type IN ('stdout', 'stderr', 'system')),
    content    TEXT NOT NULL,
    timestamp  TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_task_logs_task_timestamp ON task_logs(task_id, timestamp, id);
CREATE INDEX idx_task_logs_task_node_timestamp ON task_logs(task_id, node_id, timestamp, id);

-- 3.13 task_log_chunks — log chunk uploads
CREATE TABLE task_log_chunks (
    id           BIGSERIAL PRIMARY KEY,
    task_node_id UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
    chunk_index  INTEGER NOT NULL,
    data         BYTEA NOT NULL,
    size         INTEGER NOT NULL,
    uploaded_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(task_node_id, chunk_index)
);
CREATE INDEX idx_log_chunks_node ON task_log_chunks(task_node_id);

-- 3.14 memories — shared memory
CREATE TABLE memories (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_task_id INTEGER,
    type           memory_type NOT NULL,
    title          VARCHAR(300) NOT NULL,
    content        TEXT NOT NULL,
    tags           TEXT[] DEFAULT '{}',
    embedding      vector(1536),
    confidence     REAL NOT NULL DEFAULT 0.5,
    verified       BOOLEAN NOT NULL DEFAULT false,
    stale          BOOLEAN NOT NULL DEFAULT false,
    metadata       JSONB DEFAULT '{}',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_memories_workspace ON memories(workspace_id);
CREATE INDEX idx_memories_stale ON memories(stale) WHERE stale = true;

-- 3.15 auth_tokens — authentication tokens
-- lookup_hash: SHA-256 for O(1) database lookup
-- token_hash: bcrypt for secure verification
CREATE TABLE auth_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   VARCHAR(60) NOT NULL,
    lookup_hash  VARCHAR(64) NOT NULL,
    token_type   token_type NOT NULL,
    owner_type   VARCHAR(10) NOT NULL,
    owner_id     UUID NOT NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_tokens_lookup ON auth_tokens(lookup_hash);
CREATE INDEX idx_tokens_owner ON auth_tokens(owner_type, owner_id);
CREATE INDEX idx_auth_tokens_session ON auth_tokens(lookup_hash) WHERE token_type = 'session';

-- 3.16 git_credentials — Git credentials (one per (project, member agent))
CREATE TABLE git_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id      UUID NOT NULL,
    username      VARCHAR(200) NOT NULL DEFAULT 'git',
    encrypted_pat TEXT NOT NULL,
    created_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, agent_id)
);
CREATE INDEX idx_git_credentials_project ON git_credentials(project_id);

-- 3.16b integrations — 工程级外部集成(第一版 github)
-- PAT/webhook secret 用 Server encryption key 加密落库;凭据不下发给 agentd;
-- pat_masked 在写入时预计算,读取掩码不再需要解密
CREATE TABLE integrations (
    id                       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id             UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id               UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    provider                 VARCHAR(50) NOT NULL DEFAULT 'github',
    name                     VARCHAR(200) NOT NULL DEFAULT '',
    config                   JSONB NOT NULL DEFAULT '{}',
    encrypted_secret         TEXT NOT NULL DEFAULT '',
    pat_masked               TEXT NOT NULL DEFAULT '',
    encrypted_webhook_secret TEXT NOT NULL DEFAULT '',
    status                   VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_by               UUID,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_integrations_project ON integrations(project_id);

-- 3.16c workflow_events — 外部事件入库(durable),按 delivery 唯一去重
CREATE TABLE workflow_events (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider       VARCHAR(50) NOT NULL DEFAULT 'github',
    integration_id UUID NOT NULL,
    external_id    TEXT NOT NULL,
    event_type     TEXT NOT NULL DEFAULT '',
    action         TEXT NOT NULL DEFAULT '',
    payload        JSONB NOT NULL DEFAULT '{}',
    status         VARCHAR(20) NOT NULL DEFAULT 'pending',
    attempts       INTEGER NOT NULL DEFAULT 0,
    error          TEXT NOT NULL DEFAULT '',
    received_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at   TIMESTAMPTZ,
    -- 租约判定:claim/回执写时刷新,超时未完成由调度回收重排
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- (provider, integration_id, external_id) 唯一:重放 delivery 不产生重复处理
CREATE UNIQUE INDEX idx_workflow_events_delivery ON workflow_events(provider, integration_id, external_id);
CREATE INDEX idx_workflow_events_status ON workflow_events(status, received_at);

-- 3.16d workflow_event_waiters — flow.wait(github_event) 的等待注册
CREATE TABLE workflow_event_waiters (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id        INTEGER NOT NULL,
    task_node_id   UUID NOT NULL,
    integration_id UUID NOT NULL,
    event_type     TEXT NOT NULL,
    filter         JSONB NOT NULL DEFAULT '{}',
    status         VARCHAR(20) NOT NULL DEFAULT 'waiting',
    expires_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at    TIMESTAMPTZ
);
CREATE INDEX idx_workflow_event_waiters_event ON workflow_event_waiters(integration_id, event_type, status);
CREATE INDEX idx_workflow_event_waiters_task ON workflow_event_waiters(task_id);

-- 3.17 skills — skills
CREATE TABLE skills (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id    UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name            VARCHAR(200) NOT NULL,
    description     TEXT DEFAULT '',
    category        VARCHAR(50) DEFAULT '',
    prompt_template TEXT DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_skills_workspace ON skills(workspace_id);

-- 3.19 mcp_servers — MCP servers
CREATE TABLE mcp_servers (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         VARCHAR(200) NOT NULL,
    url          VARCHAR(500) NOT NULL,
    type         VARCHAR(50) DEFAULT '',
    auth_type    mcp_auth_type NOT NULL DEFAULT 'none',
    env_vars     JSONB DEFAULT '{}',
    status       VARCHAR(20) NOT NULL DEFAULT 'disconnected',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_mcp_servers_workspace ON mcp_servers(workspace_id);

-- 3.20 agent_skills — agent-skill association
CREATE TABLE agent_skills (
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_id   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, skill_id)
);
CREATE INDEX idx_agent_skills_agent ON agent_skills(agent_id);

-- 3.21 agent_mcp_servers — agent-MCP server association
CREATE TABLE agent_mcp_servers (
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    mcp_server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, mcp_server_id)
);
CREATE INDEX idx_agent_mcp_agent ON agent_mcp_servers(agent_id);

-- 3.22 token_usage — token usage
CREATE TABLE token_usage (
    id            BIGSERIAL PRIMARY KEY,
    task_node_id  UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens  INTEGER NOT NULL DEFAULT 0,
    cost_estimate DECIMAL(10,6),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_token_usage_node ON token_usage(task_node_id);
CREATE INDEX idx_token_usage_agent ON token_usage(agent_id);
CREATE INDEX idx_token_usage_time ON token_usage(created_at);

-- 3.23 community_workflows — community workflows
CREATE TABLE community_workflows (
    id                             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                           VARCHAR(200) NOT NULL,
    description                    TEXT DEFAULT '',
    author                         VARCHAR(200) NOT NULL,
    version                        VARCHAR(20) NOT NULL DEFAULT '1.0.0',
    workflow_definition             JSONB NOT NULL,
    required_skills                JSONB DEFAULT '[]',
    required_mcp_servers           JSONB DEFAULT '[]',
    recommended_agent_instructions JSONB DEFAULT '{}',
    downloads                      INTEGER NOT NULL DEFAULT 0,
    is_official                    BOOLEAN NOT NULL DEFAULT false,
    created_at                     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_community_workflows_name ON community_workflows(name);
CREATE INDEX idx_community_workflows_downloads ON community_workflows(downloads DESC);

-- 3.24 project_reviewers — project reviewer configuration
CREATE TABLE project_reviewers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    member_type VARCHAR(10) NOT NULL,
    agent_id    UUID REFERENCES agents(id) ON DELETE CASCADE,
    member_id   UUID REFERENCES members(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, member_type, agent_id),
    UNIQUE(project_id, member_type, member_id)
);
CREATE INDEX idx_project_reviewers_project ON project_reviewers(project_id);

-- 3.25 agent_permissions — agent permission control
CREATE TABLE agent_permissions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    permission    VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50) NOT NULL DEFAULT '*',
    resource_id   UUID DEFAULT NULL,
    granted_by    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NULLS NOT DISTINCT: workspace-wide grants hold resource_id = NULL, and
    -- with the default NULLS DISTINCT those rows never conflict, so duplicate
    -- permission rows accumulate unchecked.
    UNIQUE NULLS NOT DISTINCT (agent_id, permission, resource_type, resource_id)
);
CREATE INDEX idx_agent_permissions_agent ON agent_permissions(agent_id);

-- 3.26 audit_logs — audit logs
CREATE TABLE audit_logs (
    id            BIGSERIAL PRIMARY KEY,
    workspace_id  UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    actor_type    VARCHAR(10) NOT NULL,
    actor_id      UUID NOT NULL,
    action        VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50) NOT NULL,
    resource_id   VARCHAR(100) NOT NULL,
    details       JSONB DEFAULT '{}',
    ip_address    INET,
    user_agent    TEXT,
    request_id    UUID,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_workspace ON audit_logs(workspace_id);
CREATE INDEX idx_audit_actor ON audit_logs(actor_type, actor_id);
CREATE INDEX idx_audit_time ON audit_logs(created_at);
CREATE INDEX idx_audit_request_id ON audit_logs(request_id);

-- 3.27 invitations — invitation links
CREATE TABLE invitations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email        VARCHAR(300) NOT NULL,
    role         VARCHAR(20) NOT NULL DEFAULT 'member',
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    invited_by   UUID,
    expires_at   TIMESTAMPTZ NOT NULL,
    accepted_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_invitations_token ON invitations(token_hash);
CREATE INDEX idx_invitations_email ON invitations(email);

-- 3.28 execution_sessions — execution sessions
CREATE TABLE execution_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    daemon_id        UUID,
    agent_id         UUID,
    task_node_id     UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
    attempt          INTEGER NOT NULL DEFAULT 1,
    status           VARCHAR(20) NOT NULL DEFAULT 'running',
    workdir          TEXT DEFAULT '',
    branch           VARCHAR(200) DEFAULT '',
    base_commit      VARCHAR(40) DEFAULT '',
    head_commit      VARCHAR(40) DEFAULT '',
    claude_session_id VARCHAR(100) DEFAULT '',
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at     TIMESTAMPTZ,
    interrupted_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_execution_sessions_runtime ON execution_sessions(daemon_id);
CREATE INDEX idx_execution_sessions_agent ON execution_sessions(agent_id);
CREATE INDEX idx_execution_sessions_node ON execution_sessions(task_node_id);
CREATE INDEX idx_execution_sessions_status ON execution_sessions(status);
CREATE INDEX idx_execution_sessions_claude_session ON execution_sessions(claude_session_id) WHERE claude_session_id != '';

-- 3.29 daemons — agentd instances running on work machines
-- One daemon per machine; agent instances are reported by the daemon at
-- registration (agents.daemon_id) and authenticated on its behalf via X-Agent-ID.
CREATE TABLE daemons (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    device_name    TEXT NOT NULL DEFAULT '',
    version        TEXT NOT NULL DEFAULT '',
    status         daemon_status NOT NULL DEFAULT 'offline',
    public_key     TEXT NOT NULL DEFAULT '',
    providers      JSONB NOT NULL DEFAULT '[]',
    last_heartbeat TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_daemons_workspace ON daemons(workspace_id);

-- 3.30 custom_themes — 运行时上传的自定义主题（部署后经前端导入入口安装，不改源码）
-- 无外键：created_by 仅记录上传者(展示用)；主题为部署级共享资源
CREATE TABLE custom_themes (
    id          VARCHAR(64) PRIMARY KEY,
    name_zh     VARCHAR(100) NOT NULL,
    name_en     VARCHAR(100) NOT NULL,
    icon        VARCHAR(20) NOT NULL DEFAULT 'sparkles',
    vars        JSONB NOT NULL,
    created_by  UUID,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
