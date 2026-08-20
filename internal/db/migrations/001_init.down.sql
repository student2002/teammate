-- 001_init.down.sql
-- 按依赖关系的逆序删除所有对象

DROP TABLE IF EXISTS invitations;
DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS agent_permissions;
DROP TABLE IF EXISTS project_reviewers;
DROP TABLE IF EXISTS community_workflows;
DROP TABLE IF EXISTS token_usage;
DROP TABLE IF EXISTS agent_mcp_servers;
DROP TABLE IF EXISTS agent_skills;
DROP TABLE IF EXISTS mcp_servers;
DROP TABLE IF EXISTS skills;
DROP TABLE IF EXISTS sse_event_buffer;
DROP TABLE IF EXISTS workflow_trigger_runs;
DROP TABLE IF EXISTS git_credentials;
DROP TABLE IF EXISTS auth_tokens;
DROP TABLE IF EXISTS memories;
DROP TABLE IF EXISTS task_log_chunks;
DROP TABLE IF EXISTS runtimes;
DROP TABLE IF EXISTS comments;
DROP TABLE IF EXISTS node_transitions;
DROP TABLE IF EXISTS task_nodes;
DROP TABLE IF EXISTS tasks;
DROP TABLE IF EXISTS project_members;
DROP TABLE IF EXISTS projects;
DROP TABLE IF EXISTS workflow_template_nodes;
DROP TABLE IF EXISTS workflow_templates;
DROP TABLE IF EXISTS agents;
DROP TABLE IF EXISTS workspace_members;
DROP TABLE IF EXISTS members;
DROP TABLE IF EXISTS workspaces;

DROP SEQUENCE IF EXISTS tasks_id_seq;

DROP TYPE IF EXISTS mcp_auth_type;
DROP TYPE IF EXISTS token_type;
DROP TYPE IF EXISTS memory_type;
DROP TYPE IF EXISTS runtime_status;
DROP TYPE IF EXISTS transition_action;
DROP TYPE IF EXISTS task_node_status;
DROP TYPE IF EXISTS task_status;
DROP TYPE IF EXISTS task_priority;
DROP TYPE IF EXISTS task_type;
DROP TYPE IF EXISTS assignee_type;
DROP TYPE IF EXISTS node_type;
DROP TYPE IF EXISTS workflow_trigger_type;
DROP TYPE IF EXISTS project_status;
DROP TYPE IF EXISTS agent_status;
DROP TYPE IF EXISTS agent_provider;

DROP EXTENSION IF EXISTS vector;
