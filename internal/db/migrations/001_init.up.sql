-- 001_init.sql
-- Teammate 数据库初始化迁移
-- 本文件为已应用的基线迁移：新增/变更表结构请新建 002_*.up.sql，勿修改本文件

-- ============================================================
-- 0. 扩展
-- ============================================================

CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================
-- 1. 枚举类型（必须在 CREATE TABLE 之前定义）
-- ============================================================

CREATE TYPE agent_provider AS ENUM ('claude', 'openclaw', 'opencode', 'atomcode', 'mimocode', 'copilot', 'hermes', 'gemini', 'pi', 'cursor', 'kimi', 'kiro');
CREATE TYPE agent_status AS ENUM ('online', 'offline', 'busy', 'paused');
CREATE TYPE project_status AS ENUM ('planned', 'active', 'paused', 'completed', 'archived');
CREATE TYPE node_type AS ENUM ('standard', 'review', 'manual');
CREATE TYPE assignee_type AS ENUM ('any_agent', 'specific_agent', 'human', 'auto');
CREATE TYPE workflow_trigger_type AS ENUM ('manual', 'schedule', 'github_issue');
CREATE TYPE task_type AS ENUM ('story', 'bug', 'task');
CREATE TYPE task_priority AS ENUM ('urgent', 'high', 'medium', 'low');
CREATE TYPE task_status AS ENUM ('active', 'completed', 'cancelled');
CREATE TYPE task_node_status AS ENUM ('pending', 'in_progress', 'completed', 'rejected', 'manual_intervention');
CREATE TYPE transition_action AS ENUM ('approve', 'reject', 'manual', 'reclaim', 'timeout', 'interrupt_ack');
CREATE TYPE runtime_status AS ENUM ('online', 'offline', 'error');
CREATE TYPE memory_type AS ENUM ('architecture', 'command', 'convention', 'decision', 'insight', 'environment');
CREATE TYPE token_type AS ENUM ('api', 'session', 'task', 'password_reset');
CREATE TYPE mcp_auth_type AS ENUM ('none', 'api_key', 'oauth');

-- ============================================================
-- 2. 序列
-- ============================================================

CREATE SEQUENCE IF NOT EXISTS tasks_id_seq;

-- ============================================================
-- 3. 表结构
-- ============================================================

-- 3.1 workspaces — 工作区
CREATE TABLE workspaces (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         VARCHAR(200) NOT NULL,
    description  TEXT DEFAULT '',
    issue_prefix VARCHAR(10) NOT NULL DEFAULT 'MUL',
    is_default   BOOLEAN NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 3.2 members — 成员（人类）
CREATE TABLE members (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(200) NOT NULL,
    email         VARCHAR(300) NOT NULL UNIQUE,
    password_hash VARCHAR(200) NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_members_email ON members(email);

-- 3.2b workspace_members — 工作区成员关联（多对多）
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

-- 3.3 agents — AI 代理
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
    git_name        VARCHAR(200) DEFAULT '',
    git_email       VARCHAR(300) DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agents_workspace ON agents(workspace_id);
CREATE INDEX idx_agents_status ON agents(status);

-- 3.4 workflow_templates — 工作流模板
CREATE TABLE workflow_templates (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name         VARCHAR(200) NOT NULL,
    description  TEXT DEFAULT '',
    is_builtin   BOOLEAN NOT NULL DEFAULT false,
    trigger_type workflow_trigger_type NOT NULL DEFAULT 'manual',
    trigger_config JSONB NOT NULL DEFAULT '{}',
    trigger_enabled BOOLEAN NOT NULL DEFAULT true,
    next_run_at TIMESTAMPTZ,
    last_triggered_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wf_templates_workspace ON workflow_templates(workspace_id);
CREATE INDEX idx_wf_templates_due_triggers ON workflow_templates(next_run_at)
    WHERE trigger_enabled = true AND trigger_type = 'schedule' AND next_run_at IS NOT NULL;

-- 3.5 workflow_template_nodes — 模板节点定义
CREATE TABLE workflow_template_nodes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id      UUID NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    name             VARCHAR(200) NOT NULL,
    description      TEXT DEFAULT '',
    sort_order       INTEGER NOT NULL,
    node_type        node_type NOT NULL DEFAULT 'standard',
    assignee_type    assignee_type NOT NULL DEFAULT 'any_agent',
    assignee_id      UUID REFERENCES agents(id) ON DELETE SET NULL,
    timeout_minutes  INTEGER NOT NULL DEFAULT 60,
    max_reject_cycles INTEGER NOT NULL DEFAULT 5,
    readonly_dirs    JSONB DEFAULT '[]',
    full_control_dirs JSONB DEFAULT '[]',
    artifact         JSONB DEFAULT '[]',
    depends_on       UUID[] DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_wf_template_nodes_template ON workflow_template_nodes(template_id);

-- 3.6 projects — 工程
CREATE TABLE projects (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id          UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                  VARCHAR(200) NOT NULL,
    description           TEXT DEFAULT '',
    icon                  VARCHAR(50) DEFAULT '',
    status                project_status NOT NULL DEFAULT 'planned',
    repo_url              VARCHAR(500) DEFAULT '',
    context               TEXT DEFAULT '',
    default_workflow_id   UUID REFERENCES workflow_templates(id),
    max_review_cycles     INTEGER NOT NULL DEFAULT 5,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workspace_id, name)
);
CREATE INDEX idx_projects_workspace ON projects(workspace_id);

-- 3.7 project_members — 工程成员
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

-- 3.8 tasks — 任务（id 为工作空间内自增整数）
CREATE TABLE tasks (
    id             INTEGER PRIMARY KEY DEFAULT nextval('tasks_id_seq'),
    project_id     UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_name  TEXT NOT NULL DEFAULT '',
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

-- 3.9 task_nodes — 任务节点实例
CREATE TABLE task_nodes (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id               INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    name                  VARCHAR(200) NOT NULL,
    description           TEXT DEFAULT '',
    sort_order            INTEGER NOT NULL,
    node_type             node_type NOT NULL DEFAULT 'standard',
    status                task_node_status NOT NULL DEFAULT 'pending',
    assignee_type         assignee_type NOT NULL DEFAULT 'any_agent',
    assignee_id           UUID REFERENCES agents(id) ON DELETE SET NULL,
    reserved_for_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    reject_count          INTEGER NOT NULL DEFAULT 0,
    max_reject_cycles     INTEGER NOT NULL DEFAULT 5,
    timeout_minutes       INTEGER NOT NULL DEFAULT 60,
    version               INTEGER NOT NULL DEFAULT 1,
    completed_at          TIMESTAMPTZ,
    completed_by          UUID REFERENCES agents(id) ON DELETE SET NULL,
    summary               TEXT NOT NULL DEFAULT '',
    previous_summary      TEXT NOT NULL DEFAULT '',
    reservation_expires_at TIMESTAMPTZ,
    readonly_dirs      JSONB DEFAULT '[]',
    full_control_dirs  JSONB DEFAULT '[]',
    depends_on       UUID[] DEFAULT '{}',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(task_id, sort_order)
);
CREATE INDEX idx_task_nodes_task ON task_nodes(task_id);
CREATE INDEX idx_task_nodes_status ON task_nodes(status);
CREATE INDEX idx_task_nodes_assignee ON task_nodes(assignee_id);
CREATE INDEX idx_task_nodes_task_status ON task_nodes(task_id, status);
CREATE INDEX idx_task_nodes_timeout ON task_nodes(status, updated_at)
    WHERE status = 'in_progress' AND node_type != 'manual' AND assignee_type != 'human';

-- 3.10 node_transitions — 节点流转历史
CREATE TABLE node_transitions (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_node_id   UUID NOT NULL REFERENCES task_nodes(id) ON DELETE CASCADE,
    from_status    task_node_status NOT NULL,
    to_status      task_node_status NOT NULL,
    action         transition_action NOT NULL,
    target_node_id UUID REFERENCES task_nodes(id) ON DELETE CASCADE,
    comment        TEXT DEFAULT '',
    operator_id    UUID,
    operator_type  VARCHAR(10) NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_node_transitions_node ON node_transitions(task_node_id);
CREATE INDEX idx_node_transitions_time ON node_transitions(created_at);

-- 3.11 comments — 评论
CREATE TABLE comments (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id     INTEGER NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    node_id     UUID REFERENCES task_nodes(id) ON DELETE CASCADE,
    source_node_id UUID REFERENCES task_nodes(id) ON DELETE SET NULL,
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

-- 3.12 runtimes — 运行时
CREATE TABLE runtimes (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id           UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    daemon_id          VARCHAR(200) NOT NULL,
    provider           agent_provider NOT NULL,
    version            VARCHAR(50) DEFAULT '',
    status             runtime_status NOT NULL DEFAULT 'offline',
    session_token_hash VARCHAR(64) DEFAULT '',
    session_expires_at TIMESTAMPTZ,
    public_key         TEXT DEFAULT '',
    last_heartbeat     TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_runtimes_agent ON runtimes(agent_id);
CREATE INDEX idx_runtimes_daemon ON runtimes(daemon_id);
CREATE INDEX idx_runtimes_heartbeat ON runtimes(last_heartbeat);

-- 3.13 task_logs — 持久化执行日志
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

-- 3.13 task_log_chunks — 日志分块上传
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

-- 3.14 memories — 共享记忆
CREATE TABLE memories (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id   UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    source_task_id INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
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

-- 3.15 auth_tokens — 认证令牌
-- lookup_hash: SHA-256 用于 O(1) 数据库查找
-- token_hash:  bcrypt 用于安全验证
CREATE TABLE auth_tokens (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    token_hash   VARCHAR(60) NOT NULL,
    lookup_hash  VARCHAR(64) NOT NULL,
    token_type   token_type NOT NULL,
    owner_type   VARCHAR(10) NOT NULL,
    owner_id     UUID NOT NULL,
    runtime_id   UUID REFERENCES runtimes(id) ON DELETE CASCADE,
    expires_at   TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_tokens_lookup ON auth_tokens(lookup_hash);
CREATE INDEX idx_tokens_owner ON auth_tokens(owner_type, owner_id);
CREATE INDEX idx_auth_tokens_session ON auth_tokens(lookup_hash) WHERE token_type = 'session';

-- 3.16 git_credentials — Git 凭据
CREATE TABLE git_credentials (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repo_url      VARCHAR(500) NOT NULL,
    username      VARCHAR(200) NOT NULL DEFAULT 'git',
    encrypted_pat TEXT NOT NULL,
    created_by    UUID REFERENCES members(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, repo_url)
);
CREATE INDEX idx_git_credentials_project ON git_credentials(project_id);

-- 3.16b workflow_trigger_runs — 工作流触发历史与外部事件去重
CREATE TABLE workflow_trigger_runs (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id         UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    project_id           UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_template_id UUID NOT NULL REFERENCES workflow_templates(id) ON DELETE CASCADE,
    trigger_type         workflow_trigger_type NOT NULL,
    external_key         TEXT NOT NULL DEFAULT '',
    status               VARCHAR(20) NOT NULL,
    task_id              INTEGER REFERENCES tasks(id) ON DELETE SET NULL,
    payload              JSONB NOT NULL DEFAULT '{}',
    error                TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(workflow_template_id, external_key)
);
CREATE INDEX idx_workflow_trigger_runs_workspace ON workflow_trigger_runs(workspace_id, created_at DESC);
CREATE INDEX idx_workflow_trigger_runs_template ON workflow_trigger_runs(workflow_template_id, created_at DESC);

-- 3.17 sse_event_buffer — SSE 事件缓冲区
CREATE TABLE sse_event_buffer (
    id         BIGSERIAL PRIMARY KEY,
    runtime_id UUID NOT NULL REFERENCES runtimes(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    event_data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_sse_buffer_runtime ON sse_event_buffer(runtime_id);
CREATE INDEX idx_sse_buffer_time ON sse_event_buffer(created_at);

-- 3.18 skills — 技能
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

-- 3.19 mcp_servers — MCP 服务器
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

-- 3.20 agent_skills — 代理-技能关联
CREATE TABLE agent_skills (
    agent_id   UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    skill_id   UUID NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, skill_id)
);
CREATE INDEX idx_agent_skills_agent ON agent_skills(agent_id);

-- 3.21 agent_mcp_servers — 代理-MCP 服务器关联
CREATE TABLE agent_mcp_servers (
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    mcp_server_id UUID NOT NULL REFERENCES mcp_servers(id) ON DELETE CASCADE,
    enabled       BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, mcp_server_id)
);
CREATE INDEX idx_agent_mcp_agent ON agent_mcp_servers(agent_id);

-- 3.22 token_usage — Token 用量
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

-- 3.23 community_workflows — 社区工作流
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

-- 3.24 project_reviewers — 工程审查员配置
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

-- 3.25 agent_permissions — Agent 权限控制
CREATE TABLE agent_permissions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id      UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    permission    VARCHAR(50) NOT NULL,
    resource_type VARCHAR(50) NOT NULL DEFAULT '*',
    resource_id   UUID DEFAULT NULL,
    granted_by    UUID REFERENCES members(id) ON DELETE SET NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, permission, resource_type, resource_id)
);
CREATE INDEX idx_agent_permissions_agent ON agent_permissions(agent_id);

-- 3.26 audit_logs — 审计日志
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

-- 3.27 invitations — 邀请链接
CREATE TABLE invitations (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    email        VARCHAR(300) NOT NULL,
    role         VARCHAR(20) NOT NULL DEFAULT 'member',
    token_hash   VARCHAR(64) NOT NULL UNIQUE,
    invited_by   UUID REFERENCES members(id) ON DELETE SET NULL,
    expires_at   TIMESTAMPTZ NOT NULL,
    accepted_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_invitations_token ON invitations(token_hash);
CREATE INDEX idx_invitations_email ON invitations(email);

-- 3.28 execution_sessions — 执行会话
CREATE TABLE execution_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    runtime_id       UUID REFERENCES runtimes(id) ON DELETE SET NULL,
    agent_id         UUID REFERENCES agents(id) ON DELETE SET NULL,
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
CREATE INDEX idx_execution_sessions_runtime ON execution_sessions(runtime_id);
CREATE INDEX idx_execution_sessions_agent ON execution_sessions(agent_id);
CREATE INDEX idx_execution_sessions_node ON execution_sessions(task_node_id);
CREATE INDEX idx_execution_sessions_status ON execution_sessions(status);
CREATE INDEX idx_execution_sessions_claude_session ON execution_sessions(claude_session_id) WHERE claude_session_id != '';
