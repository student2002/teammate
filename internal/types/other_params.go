// other_params.go 定义其余领域（Audit/Auth/Community/ExecutionSession/Git/Invitation/Memory/Notification/Project/Review/Runtime/Search/Workflow）操作的领域参数结构体。
//
// 这些结构体是 sqlc 生成的 db.XxxParams 的 domain 对应物，
// 字段一一对应，类型按 domain 风格映射：
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - sql.NullInt32 → *int32
//   - sql.NullBool → *bool
//   - sql.NullFloat64 → *float64
//   - pqtype.NullRawMessage → json.RawMessage
//   - pqtype.Inet → string
package types

import (
	"encoding/json"
	"time"
)

// === Audit 领域 ===

// CreateAuditLogParams 是创建审计日志的领域参数结构体。
type CreateAuditLogParams struct {
	WorkspaceID  string          `json:"workspace_id"`
	ActorType    string          `json:"actor_type"`
	ActorID      string          `json:"actor_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Details      json.RawMessage `json:"details"`
	IPAddress    string          `json:"ip_address"`
	UserAgent    *string         `json:"user_agent"`
	RequestID    *string         `json:"request_id"`
}

// ListAuditLogsParams 是分页列出审计日志的领域参数结构体。
type ListAuditLogsParams struct {
	WorkspaceID string `json:"workspace_id"`
	Limit       int32  `json:"limit"`
	Offset      int32  `json:"offset"`
}

// ListAuditLogsByActorParams 是按操作者分页列出审计日志的领域参数结构体。
type ListAuditLogsByActorParams struct {
	WorkspaceID string `json:"workspace_id"`
	ActorType   string `json:"actor_type"`
	ActorID     string `json:"actor_id"`
	Limit       int32  `json:"limit"`
	Offset      int32  `json:"offset"`
}

// === Auth 领域 ===

// GetAuthTokenByLookupHashAndTypeParams 是按 lookup_hash 和 token_type 查询 token 的领域参数结构体。
type GetAuthTokenByLookupHashAndTypeParams struct {
	LookupHash string `json:"lookup_hash"`
	TokenType  string `json:"token_type"`
}

// === Community 领域 ===

// CreateCommunityWorkflowParams 是创建社区工作流的领域参数结构体。
type CreateCommunityWorkflowParams struct {
	Name                         string          `json:"name"`
	Description                  *string         `json:"description"`
	Author                       string          `json:"author"`
	Version                      string          `json:"version"`
	WorkflowDefinition           json.RawMessage `json:"workflow_definition"`
	RequiredSkills               json.RawMessage `json:"required_skills"`
	RequiredMcpServers           json.RawMessage `json:"required_mcp_servers"`
	RecommendedAgentInstructions json.RawMessage `json:"recommended_agent_instructions"`
	Downloads                    int32           `json:"downloads"`
	IsOfficial                   bool            `json:"is_official"`
}

// === ExecutionSession 领域 ===

// CompleteExecutionSessionParams 是完成执行会话的领域参数结构体。
type CompleteExecutionSessionParams struct {
	ID         string  `json:"id"`
	HeadCommit *string `json:"head_commit"`
}

// CreateExecutionSessionParams 是创建执行会话的领域参数结构体。
type CreateExecutionSessionParams struct {
	RuntimeID       *string `json:"runtime_id"`
	AgentID         *string `json:"agent_id"`
	TaskNodeID      string  `json:"task_node_id"`
	Attempt         int32   `json:"attempt"`
	Status          string  `json:"status"`
	Workdir         *string `json:"workdir"`
	Branch          *string `json:"branch"`
	BaseCommit      *string `json:"base_commit"`
	ClaudeSessionID *string `json:"claude_session_id"`
}

// GetActiveSessionByAgentAndWorkdirParams 是按 Agent 和 workdir 查询活跃会话的领域参数结构体。
type GetActiveSessionByAgentAndWorkdirParams struct {
	AgentID *string `json:"agent_id"`
	Workdir *string `json:"workdir"`
}

// UpdateExecutionSessionParams 是更新执行会话的领域参数结构体。
type UpdateExecutionSessionParams struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	HeadCommit      *string    `json:"head_commit"`
	CompletedAt     *time.Time `json:"completed_at"`
	InterruptedAt   *time.Time `json:"interrupted_at"`
	ClaudeSessionID *string    `json:"claude_session_id"`
}

// UpdateSessionClaudeIDParams 是更新会话 Claude session ID 的领域参数结构体。
type UpdateSessionClaudeIDParams struct {
	ID              string  `json:"id"`
	ClaudeSessionID *string `json:"claude_session_id"`
}

// === Git 领域 ===

// CreateGitCredentialParams 是创建 Git 凭据的领域参数结构体。
type CreateGitCredentialParams struct {
	ProjectID    string  `json:"project_id"`
	RepoURL      string  `json:"repo_url"`
	Username     string  `json:"username"`
	EncryptedPAT string  `json:"encrypted_pat"`
	CreatedBy    *string `json:"created_by"`
}

// UpdateGitCredentialParams 是更新 Git 凭据的领域参数结构体。
type UpdateGitCredentialParams struct {
	ID           string `json:"id"`
	RepoURL      string `json:"repo_url"`
	Username     string `json:"username"`
	EncryptedPAT string `json:"encrypted_pat"`
}

// === Invitation 领域 ===

// CreateInvitationParams 是创建工作区邀请的领域参数结构体。
type CreateInvitationParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	TokenHash   string  `json:"token_hash"`
	InvitedBy   *string `json:"invited_by"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// === Memory 领域 ===

// CreateMemoryParams 是创建共享记忆的领域参数结构体。
type CreateMemoryParams struct {
	WorkspaceID  string          `json:"workspace_id"`
	SourceTaskID *int32          `json:"source_task_id"`
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	Content      string          `json:"content"`
	Tags         []string        `json:"tags"`
	Confidence   float32         `json:"confidence"`
	Verified     bool            `json:"verified"`
	Metadata     json.RawMessage `json:"metadata"`
}

// ListMemoriesByWorkspaceParams 是按工作区列出记忆的领域参数结构体。
type ListMemoriesByWorkspaceParams struct {
	WorkspaceID   string   `json:"workspace_id"`
	Verified      *bool    `json:"verified"`
	MinConfidence *float64 `json:"min_confidence"`
	Limit         *int32   `json:"limit"`
}

// SearchMemoriesParams 是搜索记忆的领域参数结构体。
type SearchMemoriesParams struct {
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
}

// === Notification 领域 ===

// ListMentionCommentsParams 是列出提及某用户的评论的领域参数结构体。
type ListMentionCommentsParams struct {
	WorkspaceID string `json:"workspace_id"`
	Column2     string `json:"column_2"`
}

// === Project 领域 ===

// CreateProjectParams 是创建项目的领域参数结构体。
type CreateProjectParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	RepoURL     *string `json:"repo_url"`
	Context     *string `json:"context"`
}

// CreateProjectMemberParams 是创建项目成员关联的领域参数结构体。
type CreateProjectMemberParams struct {
	ProjectID  string  `json:"project_id"`
	MemberType string  `json:"member_type"`
	AgentID    *string `json:"agent_id"`
	MemberID   *string `json:"member_id"`
	Role       string  `json:"role"`
}

// CreateProjectReviewerParams 是创建项目审查者指派的领域参数结构体。
type CreateProjectReviewerParams struct {
	ProjectID  string  `json:"project_id"`
	MemberType string  `json:"member_type"`
	AgentID    *string `json:"agent_id"`
	MemberID   *string `json:"member_id"`
}

// GetProjectMemberRoleParams 是查询项目成员角色的领域参数结构体。
type GetProjectMemberRoleParams struct {
	ProjectID string  `json:"project_id"`
	MemberID  *string `json:"member_id"`
}

// IsMemberProjectMemberParams 是判断成员是否为项目成员的领域参数结构体。
type IsMemberProjectMemberParams struct {
	ProjectID string  `json:"project_id"`
	MemberID  *string `json:"member_id"`
}

// ListProjectsByAgentMembershipParams 是按 Agent 成员关系列出项目的领域参数结构体。
type ListProjectsByAgentMembershipParams struct {
	WorkspaceID string  `json:"workspace_id"`
	AgentID     *string `json:"agent_id"`
}

// UpdateProjectParams 是更新项目的领域参数结构体。
type UpdateProjectParams struct {
	ID                string   `json:"id"`
	Name              string   `json:"name"`
	Description       *string  `json:"description"`
	Status            string   `json:"status"`
	RepoURL           *string  `json:"repo_url"`
	Context           *string  `json:"context"`
	DefaultWorkflowID *string  `json:"default_workflow_id"`
	MaxReviewCycles   *int32   `json:"max_review_cycles"`
}

// === Review 领域 ===

// GetReviewNodeAuthorParams 是查询审查节点原作者的领域参数结构体。
type GetReviewNodeAuthorParams struct {
	TaskID int32  `json:"task_id"`
	ID     string `json:"id"`
}

// GetReviewNodeReviewerParams 是查询审查节点审查者的领域参数结构体。
type GetReviewNodeReviewerParams struct {
	ID     string `json:"id"`
	TaskID int32  `json:"task_id"`
}

// === Runtime 领域 ===

// CreateRuntimeParams 是创建 Runtime 记录的领域参数结构体。
type CreateRuntimeParams struct {
	AgentID          string  `json:"agent_id"`
	DaemonID         string  `json:"daemon_id"`
	Provider         string  `json:"provider"`
	Version          *string `json:"version"`
	Status           string  `json:"status"`
	SessionTokenHash *string `json:"session_token_hash"`
	SessionExpiresAt *time.Time `json:"session_expires_at"`
	PublicKey        *string `json:"public_key"`
}

// UpdateRuntimeHeartbeatParams 是更新 Runtime 心跳时间的领域参数结构体。
type UpdateRuntimeHeartbeatParams struct {
	ID            string     `json:"id"`
	LastHeartbeat *time.Time `json:"last_heartbeat"`
}

// UpdateRuntimeStatusParams 是更新 Runtime 状态的领域参数结构体。
type UpdateRuntimeStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// === Search 领域 ===

// SearchAgentsByWorkspaceParams 是按工作区搜索 Agent 的领域参数结构体。
type SearchAgentsByWorkspaceParams struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}

// SearchTasksByWorkspaceParams 是按工作区搜索任务的领域参数结构体。
type SearchTasksByWorkspaceParams struct {
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
}

// SearchTasksByWorkspaceAndProjectParams 是按工作区和项目搜索任务的领域参数结构体。
type SearchTasksByWorkspaceAndProjectParams struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
}

// === Workflow 领域 ===

// CreateTemplateNodeParams 是创建工作流模板节点的领域参数结构体。
type CreateTemplateNodeParams struct {
	TemplateID      string          `json:"template_id"`
	Name            string          `json:"name"`
	Description     *string         `json:"description"`
	SortOrder       int32           `json:"sort_order"`
	NodeType        string          `json:"node_type"`
	AssigneeType    string          `json:"assignee_type"`
	AssigneeID      *string         `json:"assignee_id"`
	TimeoutMinutes  int32           `json:"timeout_minutes"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
	Artifact        json.RawMessage `json:"artifact"`
	MaxRejectCycles int32           `json:"max_reject_cycles"`
	DependsOn       []string        `json:"depends_on"`
}

// CreateWorkflowTemplateParams 是创建工作流模板的领域参数结构体。
type CreateWorkflowTemplateParams struct {
	WorkspaceID     string          `json:"workspace_id"`
	Name            string          `json:"name"`
	Description     *string         `json:"description"`
	IsBuiltin       bool            `json:"is_builtin"`
	TriggerType     string          `json:"trigger_type"`
	TriggerConfig   json.RawMessage `json:"trigger_config"`
	TriggerEnabled  bool            `json:"trigger_enabled"`
	NextRunAt       *time.Time      `json:"next_run_at"`
	LastTriggeredAt *time.Time      `json:"last_triggered_at"`
}

// UpdateTemplateNodeParams 是更新工作流模板节点的领域参数结构体。
type UpdateTemplateNodeParams struct {
	ID              string          `json:"id"`
	Name            string          `json:"name"`
	Description     *string         `json:"description"`
	SortOrder       int32           `json:"sort_order"`
	NodeType        string          `json:"node_type"`
	AssigneeType    string          `json:"assignee_type"`
	AssigneeID      *string         `json:"assignee_id"`
	TimeoutMinutes  int32           `json:"timeout_minutes"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
	Artifact        json.RawMessage `json:"artifact"`
	MaxRejectCycles int32           `json:"max_reject_cycles"`
	DependsOn       []string        `json:"depends_on"`
}

// UpdateWorkflowTemplateParams 是更新工作流模板的领域参数结构体。
type UpdateWorkflowTemplateParams struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Description    *string         `json:"description"`
	TriggerType    string          `json:"trigger_type"`
	TriggerConfig  json.RawMessage `json:"trigger_config"`
	TriggerEnabled bool            `json:"trigger_enabled"`
	NextRunAt      *time.Time      `json:"next_run_at"`
}

// CreateWorkflowTriggerRunParams 是创建工作流触发器运行记录的领域参数结构体。
type CreateWorkflowTriggerRunParams struct {
	WorkspaceID        string          `json:"workspace_id"`
	ProjectID          string          `json:"project_id"`
	WorkflowTemplateID string          `json:"workflow_template_id"`
	TriggerType        string          `json:"trigger_type"`
	ExternalKey        string          `json:"external_key"`
	Status             string          `json:"status"`
	Payload            json.RawMessage `json:"payload"`
}

// MarkWorkflowTriggerRunCompletedParams 是标记触发器运行完成的领域参数结构体。
type MarkWorkflowTriggerRunCompletedParams struct {
	ID     string `json:"id"`
	TaskID *int32 `json:"task_id"`
}

// MarkWorkflowTriggerRunFailedParams 是标记触发器运行失败的领域参数结构体。
type MarkWorkflowTriggerRunFailedParams struct {
	ID    string `json:"id"`
	Error string `json:"error"`
}

// ListDueScheduledWorkflowTemplatesParams 是查询到期调度模板的领域参数结构体。
type ListDueScheduledWorkflowTemplatesParams struct {
	NextRunAt *time.Time `json:"next_run_at"`
	Limit     int32      `json:"limit"`
}

// UpdateWorkflowTemplateTriggerScheduleParams 是更新模板触发调度的领域参数结构体。
type UpdateWorkflowTemplateTriggerScheduleParams struct {
	ID              string     `json:"id"`
	NextRunAt       *time.Time `json:"next_run_at"`
	LastTriggeredAt *time.Time `json:"last_triggered_at"`
}
