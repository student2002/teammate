-- 002_remove_fks: Remove soft references / redundant foreign keys per FK strategy; application layer explicitly nulls them
-- Design basis: docs/data-storage-design.md "no foreign key (xxx explicitly nulled)" annotation + docs/implementation-vs-design-deviations.md #4/#5
-- Integrity is guaranteed by the application layer (each Store's delete transaction explicitly nulls them; see the corresponding store-layer methods).

-- 1) workflow_template_nodes.assignee_id — DeleteAgent explicitly nulled
ALTER TABLE workflow_template_nodes DROP CONSTRAINT IF EXISTS workflow_template_nodes_assignee_id_fkey;

-- 2) projects.default_workflow_id — DeleteWorkflowTemplate explicitly nulled
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_default_workflow_id_fkey;

-- 3) task_nodes.assignee_id / reserved_for_agent_id / completed_by — DeleteAgent explicitly nulled
ALTER TABLE task_nodes DROP CONSTRAINT IF EXISTS task_nodes_assignee_id_fkey;
ALTER TABLE task_nodes DROP CONSTRAINT IF EXISTS task_nodes_reserved_for_agent_id_fkey;
ALTER TABLE task_nodes DROP CONSTRAINT IF EXISTS task_nodes_completed_by_fkey;

-- 4) node_transitions.target_node_id — redundant cascade removed (nodes only deleted along with their task)
ALTER TABLE node_transitions DROP CONSTRAINT IF EXISTS node_transitions_target_node_id_fkey;

-- 5) comments.node_id / source_node_id — redundant cascade / historical reference (covered by task_id cascade)
ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_node_id_fkey;
ALTER TABLE comments DROP CONSTRAINT IF EXISTS comments_source_node_id_fkey;

-- 6) execution_sessions.runtime_id / agent_id — DeleteAgent explicitly nulled
ALTER TABLE execution_sessions DROP CONSTRAINT IF EXISTS execution_sessions_runtime_id_fkey;
ALTER TABLE execution_sessions DROP CONSTRAINT IF EXISTS execution_sessions_agent_id_fkey;

-- 7) memories.source_task_id — DeleteProject explicitly nulled
ALTER TABLE memories DROP CONSTRAINT IF EXISTS memories_source_task_id_fkey;

-- 8) git_credentials.created_by — DeleteMember explicitly nulled
ALTER TABLE git_credentials DROP CONSTRAINT IF EXISTS git_credentials_created_by_fkey;

-- 9) agent_permissions.granted_by — DeleteMember explicitly nulled
ALTER TABLE agent_permissions DROP CONSTRAINT IF EXISTS agent_permissions_granted_by_fkey;

-- 10) invitations.invited_by — DeleteMember explicitly nulled
ALTER TABLE invitations DROP CONSTRAINT IF EXISTS invitations_invited_by_fkey;

-- 11) workflow_trigger_runs.task_id — DeleteProject explicitly nulled
ALTER TABLE workflow_trigger_runs DROP CONSTRAINT IF EXISTS workflow_trigger_runs_task_id_fkey;

-- 12) deviation #5: add UNIQUE(template_id, sort_order) to workflow_template_nodes, isomorphic to task_nodes
-- (Before migrating, ensure no existing duplicate data; clean duplicates via application layer first if any)
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
