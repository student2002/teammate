-- 002_remove_fks down: 恢复被移除的外键约束与唯一约束回滚

-- 唯一约束回滚
ALTER TABLE workflow_template_nodes DROP CONSTRAINT IF EXISTS workflow_template_nodes_template_sort_order_key;

-- 恢复外键（回滚顺序与 up 相反）
ALTER TABLE workflow_trigger_runs
    ADD CONSTRAINT workflow_trigger_runs_task_id_fkey FOREIGN KEY (task_id) REFERENCES tasks(id) ON DELETE SET NULL;

ALTER TABLE invitations
    ADD CONSTRAINT invitations_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES members(id) ON DELETE SET NULL;

ALTER TABLE agent_permissions
    ADD CONSTRAINT agent_permissions_granted_by_fkey FOREIGN KEY (granted_by) REFERENCES members(id) ON DELETE SET NULL;

ALTER TABLE git_credentials
    ADD CONSTRAINT git_credentials_created_by_fkey FOREIGN KEY (created_by) REFERENCES members(id) ON DELETE SET NULL;

ALTER TABLE memories
    ADD CONSTRAINT memories_source_task_id_fkey FOREIGN KEY (source_task_id) REFERENCES tasks(id) ON DELETE SET NULL;

ALTER TABLE execution_sessions
    ADD CONSTRAINT execution_sessions_agent_id_fkey FOREIGN KEY (agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE execution_sessions
    ADD CONSTRAINT execution_sessions_runtime_id_fkey FOREIGN KEY (runtime_id) REFERENCES runtimes(id) ON DELETE SET NULL;

ALTER TABLE comments
    ADD CONSTRAINT comments_source_node_id_fkey FOREIGN KEY (source_node_id) REFERENCES task_nodes(id) ON DELETE SET NULL;

ALTER TABLE comments
    ADD CONSTRAINT comments_node_id_fkey FOREIGN KEY (node_id) REFERENCES task_nodes(id) ON DELETE CASCADE;

ALTER TABLE node_transitions
    ADD CONSTRAINT node_transitions_target_node_id_fkey FOREIGN KEY (target_node_id) REFERENCES task_nodes(id) ON DELETE CASCADE;

ALTER TABLE task_nodes
    ADD CONSTRAINT task_nodes_completed_by_fkey FOREIGN KEY (completed_by) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE task_nodes
    ADD CONSTRAINT task_nodes_reserved_for_agent_id_fkey FOREIGN KEY (reserved_for_agent_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE task_nodes
    ADD CONSTRAINT task_nodes_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES agents(id) ON DELETE SET NULL;

ALTER TABLE projects
    ADD CONSTRAINT projects_default_workflow_id_fkey FOREIGN KEY (default_workflow_id) REFERENCES workflow_templates(id);

ALTER TABLE workflow_template_nodes
    ADD CONSTRAINT workflow_template_nodes_assignee_id_fkey FOREIGN KEY (assignee_id) REFERENCES agents(id) ON DELETE SET NULL;
