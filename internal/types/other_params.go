// other_params.go defines the domain parameter structs for the other domains (Audit/Auth/Community/ExecutionSession/Git/Invitation/Memory/Notification/Project/Review/Runtime/Search/Workflow) operations.
//
// These structs are the domain counterparts of the sqlc-generated db.XxxParams,
// with fields mapped one-to-one and types mapped in domain style:
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

// === Audit domain ===

// CreateAuditLogParams is the domain parameter struct for creating an audit log.
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

// ListAuditLogsParams is the domain parameter struct for paginating audit logs.
type ListAuditLogsParams struct {
	WorkspaceID string `json:"workspace_id"`
	Limit       int32  `json:"limit"`
	Offset      int32  `json:"offset"`
}

// ListAuditLogsByActorParams is the domain parameter struct for paginating audit logs by actor.
type ListAuditLogsByActorParams struct {
	WorkspaceID string `json:"workspace_id"`
	ActorType   string `json:"actor_type"`
	ActorID     string `json:"actor_id"`
	Limit       int32  `json:"limit"`
	Offset      int32  `json:"offset"`
}

// === Auth domain ===

// GetAuthTokenByLookupHashAndTypeParams is the domain parameter struct for querying a token by lookup_hash and token_type.
type GetAuthTokenByLookupHashAndTypeParams struct {
	LookupHash string `json:"lookup_hash"`
	TokenType  string `json:"token_type"`
}

// === Community domain ===

// CreateCommunityWorkflowParams is the domain parameter struct for creating a community workflow.
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

// === ExecutionSession domain ===

// CompleteExecutionSessionParams is the domain parameter struct for completing an execution session.
type CompleteExecutionSessionParams struct {
	ID         string  `json:"id"`
	HeadCommit *string `json:"head_commit"`
}

// CreateExecutionSessionParams is the domain parameter struct for creating an execution session.
type CreateExecutionSessionParams struct {
	DaemonID        *string `json:"daemon_id"`
	AgentID         *string `json:"agent_id"`
	TaskNodeID      string  `json:"task_node_id"`
	Attempt         int32   `json:"attempt"`
	Status          string  `json:"status"`
	Workdir         *string `json:"workdir"`
	Branch          *string `json:"branch"`
	BaseCommit      *string `json:"base_commit"`
	ClaudeSessionID *string `json:"claude_session_id"`
}

// GetActiveSessionByAgentAndWorkdirParams is the domain parameter struct for querying an active session by Agent and workdir.
type GetActiveSessionByAgentAndWorkdirParams struct {
	AgentID *string `json:"agent_id"`
	Workdir *string `json:"workdir"`
}

// UpdateExecutionSessionParams is the domain parameter struct for updating an execution session.
type UpdateExecutionSessionParams struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	HeadCommit      *string    `json:"head_commit"`
	CompletedAt     *time.Time `json:"completed_at"`
	InterruptedAt   *time.Time `json:"interrupted_at"`
	ClaudeSessionID *string    `json:"claude_session_id"`
}

// UpdateSessionClaudeIDParams is the domain parameter struct for updating a session's Claude session ID.
type UpdateSessionClaudeIDParams struct {
	ID              string  `json:"id"`
	ClaudeSessionID *string `json:"claude_session_id"`
}

// === Git domain ===

// CreateGitCredentialParams is the domain parameter struct for creating a Git credential.
type CreateGitCredentialParams struct {
	ProjectID    string  `json:"project_id"`
	AgentID      string  `json:"agent_id"`
	Username     string  `json:"username"`
	EncryptedPAT string  `json:"encrypted_pat"`
	CreatedBy    *string `json:"created_by"`
}

// UpdateGitCredentialParams is the domain parameter struct for updating a Git credential.
// The agent binding is immutable, so only username and PAT are updatable.
type UpdateGitCredentialParams struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	EncryptedPAT string `json:"encrypted_pat"`
}

// === Invitation domain ===

// CreateInvitationParams is the domain parameter struct for creating a workspace invitation.
type CreateInvitationParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Email       string  `json:"email"`
	Role        string  `json:"role"`
	TokenHash   string  `json:"token_hash"`
	InvitedBy   *string `json:"invited_by"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// === Memory domain ===

// CreateMemoryParams is the domain parameter struct for creating a shared memory.
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

// ListMemoriesByWorkspaceParams is the domain parameter struct for listing memories by workspace.
type ListMemoriesByWorkspaceParams struct {
	WorkspaceID   string   `json:"workspace_id"`
	Verified      *bool    `json:"verified"`
	MinConfidence *float64 `json:"min_confidence"`
	Limit         *int32   `json:"limit"`
}

// SearchMemoriesParams is the domain parameter struct for searching memories.
type SearchMemoriesParams struct {
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
}

// === Notification domain ===

// ListMentionCommentsParams is the domain parameter struct for listing comments that mention a user.
type ListMentionCommentsParams struct {
	WorkspaceID string `json:"workspace_id"`
	Column2     string `json:"column_2"`
}

// === Project domain ===

// CreateProjectParams is the domain parameter struct for creating a project.
type CreateProjectParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Icon        *string `json:"icon"`
	Status      string  `json:"status"`
	RepoURL     *string `json:"repo_url"`
	Context     *string `json:"context"`
}

// CreateProjectMemberParams is the domain parameter struct for creating a project member association.
type CreateProjectMemberParams struct {
	ProjectID  string  `json:"project_id"`
	MemberType string  `json:"member_type"`
	AgentID    *string `json:"agent_id"`
	MemberID   *string `json:"member_id"`
	Role       string  `json:"role"`
}

// CreateProjectReviewerParams is the domain parameter struct for creating a project reviewer assignment.
type CreateProjectReviewerParams struct {
	ProjectID  string  `json:"project_id"`
	MemberType string  `json:"member_type"`
	AgentID    *string `json:"agent_id"`
	MemberID   *string `json:"member_id"`
}

// GetProjectMemberRoleParams is the domain parameter struct for querying a project member's role.
type GetProjectMemberRoleParams struct {
	ProjectID string  `json:"project_id"`
	MemberID  *string `json:"member_id"`
}

// IsMemberProjectMemberParams is the domain parameter struct for determining whether a member is a project member.
type IsMemberProjectMemberParams struct {
	ProjectID string  `json:"project_id"`
	MemberID  *string `json:"member_id"`
}

// ListProjectsByAgentMembershipParams is the domain parameter struct for listing projects by Agent membership.
type ListProjectsByAgentMembershipParams struct {
	WorkspaceID string  `json:"workspace_id"`
	AgentID     *string `json:"agent_id"`
}

// UpdateProjectParams is the domain parameter struct for updating a project.
type UpdateProjectParams struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     *string `json:"description"`
	Status          string  `json:"status"`
	RepoURL         *string `json:"repo_url"`
	Context         *string `json:"context"`
	MaxReviewCycles *int32  `json:"max_review_cycles"`
}

// === Runtime domain ===


// UpdateRuntimeHeartbeatParams is the domain parameter struct for updating a Runtime's heartbeat time.
type UpdateRuntimeHeartbeatParams struct {
	ID            string     `json:"id"`
	LastHeartbeat *time.Time `json:"last_heartbeat"`
}


// === Search domain ===

// SearchAgentsByWorkspaceParams is the domain parameter struct for searching Agents by workspace.
type SearchAgentsByWorkspaceParams struct {
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
}

// SearchTasksByWorkspaceParams is the domain parameter struct for searching tasks by workspace.
type SearchTasksByWorkspaceParams struct {
	WorkspaceID string `json:"workspace_id"`
	Title       string `json:"title"`
}

// SearchTasksByWorkspaceAndProjectParams is the domain parameter struct for searching tasks by workspace and project.
type SearchTasksByWorkspaceAndProjectParams struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
}

// === Workflow domain ===

// CreateTemplateNodeParams is the domain parameter struct for creating a workflow template node.
type CreateTemplateNodeParams struct {
	TemplateID string          `json:"template_id"`
	ID         *string         `json:"id"` // node identity; nil lets the database generate one. Stable ids keep edge references valid across full graph replacement.
	Name       string          `json:"name"`
	SortOrder  int32           `json:"sort_order"`
	TypeKey    string          `json:"type_key"`
	Config     json.RawMessage `json:"config"`
	UIConfig   json.RawMessage `json:"ui_config"`
}

// CreateTemplateEdgeParams is the domain parameter struct for creating a workflow template edge.
type CreateTemplateEdgeParams struct {
	TemplateID   string          `json:"template_id"`
	ID           *string         `json:"id"` // edge identity; nil lets the database generate one.
	SourceNodeID string          `json:"source_node_id"`
	SourcePort   string          `json:"source_port"`
	TargetNodeID string          `json:"target_node_id"`
	TargetPort   string          `json:"target_port"`
	Label        string          `json:"label"`
	SortOrder    int32           `json:"sort_order"`
	Config       json.RawMessage `json:"config"`
}

// CreateWorkflowTemplateParams is the domain parameter struct for creating a workflow template.
type CreateWorkflowTemplateParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	IsBuiltin   bool    `json:"is_builtin"`
	Version     int32   `json:"version"`
	Enabled     bool    `json:"enabled"`
}

// UpdateWorkflowTemplateParams is the domain parameter struct for updating a workflow template.
// version is owned by the Service layer and not written here.
type UpdateWorkflowTemplateParams struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
	Enabled     bool    `json:"enabled"`
}
