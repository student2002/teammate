-- 002_remove_fks: 按 FK 策略移除软引用/冗余外键，补齐应用层显式置空
-- 设计依据: docs/数据存储设计.md "无外键（xxx 显式置空）" 标注 + docs/实现与设计偏差记录.md #4/#5
-- 完整性由应用层保证（各 Store 删除事务显式置空，见 store 层对应方法）。

-- 1) workflow_template_nodes.assignee_id — DeleteAgent 显式置空
ALTER TABLE workflow_template_nodes DROP CONSTRAINT IF EXISTS workflow_template_nodes_assignee_id_fkey;

-- 2) projects.default_workflow_id — DeleteWorkflowTemplate 显式置空
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_default_workflow_id_fkey;

-- 3) task_nodes.assignee_id / reserved_for_agent_id / completed_by — DeleteAgent 显式置空
ALTER TABLE task_nodes DROP CONSTRAINT IF EXISTS task_nodes_assignee_id_fkey;
ALTER TABLE task_nodes DROP CONSTRAINT IF EXISTS task_nodes_reserved_for_agent_id_fkey;
ALTER TABLE task_nodes DROP CONSTRAINT IF EXISTS task_nodes_completed_by_fkey;

-- 4) node_transitions.target_node_id — 冗余级联已移除（节点只随任务删除）
ALTER TABLE node_transitions DROP CONSTRAINT IF EXISTS node_transitions_target_node_id_fkey;

-- 5) comments.node_id / source_node_id — 冗余级联/历史引用（随 task_id 级联覆盖）
ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_node_id_fkey;
ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_source_node_id_fkey;

-- 6) execution_sessions.runtime_id / agent_id — DeleteAgent 显式置空
ALTER TABLE execution_sessions DROP CONSTRAINT IF EXISTS execution_sessions_runtime_id_fkey;
ALTER TABLE execution_sessions DROP CONSTRAINT IF EXISTS execution_sessions_agent_id_fkey;

-- 7) memories.source_task_id — DeleteProject 显式置空
ALTER TABLE memories DROP CONSTRAINT IF EXISTS memories_source_task_id_fkey;

-- 8) git_credentials.created_by — DeleteMember 显式置空
ALTER TABLE git_credentials DROP CONSTRAINT IF EXISTS git_credentials_created_by_fkey;

-- 9) agent_permissions.granted_by — DeleteMember 显式置空
ALTER TABLE agent_permissions DROP CONSTRAINT IF EXISTS agent_permissions_granted_by_fkey;

-- 10) invitations.invited_by — DeleteMember 显式置空
ALTER TABLE invitations DROP CONSTRAINT IF EXISTS invitations_invited_by_fkey;

-- 11) workflow_trigger_runs.task_id — DeleteProject 显式置空
ALTER TABLE workflow_trigger_runs DROP CONSTRAINT IF EXISTS workflow_trigger_runs_task_id_fkey;

-- 12) 偏差 #5: workflow_template_nodes 增加 UNIQUE(template_id, sort_order)，与 task_nodes 同构
-- （迁移前需确保无存量重复数据；存在重复时先由应用层清理）
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conname = 'workflow_template_nodes_template_sort_order_key'
          AND conrelid = 'workflow_template_nodes'::regclass
    ) THEN
        ALTER TABLE workflow_template_nodes
            ADD CONSTRAINT workflow_template_nodes_template_sort_order_key UNIQUE (template_id, sort_order);
    END IF;
END $$;
