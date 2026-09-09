// rows.go defines the domain counterparts of sqlc-generated composite query Rows (involving multi-table JOINs or aggregations).
//
// When a query involves JOIN/aggregation, sqlc generates XxxRow structs, which differ from single-table Xxx entities.
// This file maps these Row types into domain style (uuid→string, sql.NullXX→*T, etc.),
// for use as return types of Store-layer methods, pass-through in the Service layer, and direct serialization in the Handler layer.
package types

import (
	"encoding/json"
	"time"
)

// ListAgentMcpServersRow is the domain Row for listing the MCP servers associated with an Agent (including server details).
type ListAgentMcpServersRow struct {
	ID          string          `json:"id"`
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Url         string          `json:"url"`
	Type        *string         `json:"type"`
	AuthType    string          `json:"auth_type"`
	EnvVars     json.RawMessage `json:"env_vars"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	Enabled     bool            `json:"enabled"`
	AssignedAt  time.Time       `json:"assigned_at"`
}

// ListAgentSkillsRow is the domain Row for listing the skills associated with an Agent (including skill details).
type ListAgentSkillsRow struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Name           string    `json:"name"`
	Description    *string   `json:"description"`
	Category       *string   `json:"category"`
	PromptTemplate *string   `json:"prompt_template"`
	CreatedAt      time.Time `json:"created_at"`
	Enabled        bool      `json:"enabled"`
	AssignedAt     time.Time `json:"assigned_at"`
}

// GetAuthTokenByLookupHashAndTypeRow is the domain Row for querying a token by hash and type.
type GetAuthTokenByLookupHashAndTypeRow struct {
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	TokenHash string `json:"token_hash"`
}

// SearchMemoriesRow is the domain Row for searching memories (includes the embedding field, but the domain does not read embedding).
type SearchMemoriesRow struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	SourceTaskID *int32          `json:"source_task_id"`
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	Content      string          `json:"content"`
	Tags         []string        `json:"tags"`
	Embedding    json.RawMessage `json:"embedding,omitempty"` // Not actually read, retained for future extension
	Confidence   float32         `json:"confidence"`
	Verified     bool            `json:"verified"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// ListManualInterventionNodesRow is the domain Row for listing nodes awaiting manual intervention (includes task title).
type ListManualInterventionNodesRow struct {
	ID        string    `json:"id"`
	TaskID    int32     `json:"task_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	TaskTitle string    `json:"task_title"`
}

// ListMentionCommentsRow is the domain Row for listing comments that mention a member (includes task title).
type ListMentionCommentsRow struct {
	ID         string    `json:"id"`
	TaskID     int32     `json:"task_id"`
	Content    string    `json:"content"`
	Mentions   []string  `json:"mentions"`
	CreatedAt  time.Time `json:"created_at"`
	AuthorType string    `json:"author_type"`
	AuthorID   string    `json:"author_id"`
	TaskTitle  string    `json:"task_title"`
}

// GetReviewQueueRow is the domain Row for fetching the review queue (JOIN tasks+task_nodes+agents).
type GetReviewQueueRow struct {
	TaskID       int32     `json:"task_id"`
	TaskTitle    string    `json:"task_title"`
	NodeID       string    `json:"node_id"`
	NodeName     string    `json:"node_name"`
	NodeStatus   string    `json:"node_status"`
	AssigneeType string    `json:"assignee_type"`
	AssigneeID   *string   `json:"assignee_id"`
	AgentName    *string   `json:"agent_name"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// GetCompletedTasksOlderThanRow is the domain Row for querying completed tasks older than a given point in time.
type GetCompletedTasksOlderThanRow struct {
	ID          int32  `json:"id"`
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
}

// GetInProgressNodesByAgentRow is the domain Row for querying in-progress nodes for an Agent (JOIN tasks to get project_id).
type GetInProgressNodesByAgentRow struct {
	ID                   string     `json:"id"`
	TaskID               int32      `json:"task_id"`
	Name                 string     `json:"name"`
	Description          string     `json:"description"`
	SortOrder            int32      `json:"sort_order"`
	NodeType             string     `json:"node_type"`
	Status               string     `json:"status"`
	AssigneeType         string     `json:"assignee_type"`
	AssigneeID           *string    `json:"assignee_id"`
	ReservedForAgentID   *string    `json:"reserved_for_agent_id"`
	RejectCount          int32      `json:"reject_count"`
	MaxRejectCycles      int32      `json:"max_reject_cycles"`
	TimeoutMinutes       int32      `json:"timeout_minutes"`
	Version              int32      `json:"version"`
	CompletedAt          *time.Time `json:"completed_at"`
	CompletedBy          *string    `json:"completed_by"`
	Summary              string     `json:"summary"`
	PreviousSummary      string     `json:"previous_summary"`
	ReservationExpiresAt *time.Time `json:"reservation_expires_at"`
	ReadonlyDirs         json.RawMessage `json:"readonly_dirs"`
	FullControlDirs      json.RawMessage `json:"full_control_dirs"`
	DependsOn            []string   `json:"depends_on"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ProjectID            string     `json:"project_id"`
}

// GetTokenUsageByAgentRow is the domain Row for a single Agent's token usage aggregation.
type GetTokenUsageByAgentRow struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// GetTokenUsageByAgentsRow is the domain Row for token usage aggregation grouped by Agent.
type GetTokenUsageByAgentsRow struct {
	AgentID      string `json:"agent_id"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

// GetTokenUsageByTaskRow is the domain Row for a single task's token usage aggregation.
type GetTokenUsageByTaskRow struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostEstimate *string `json:"cost_estimate"`
}

// GetTokenUsageByTaskNodesRow is the domain Row for token usage aggregation grouped by node.
type GetTokenUsageByTaskNodesRow struct {
	TaskNodeID   string `json:"task_node_id"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

// GetTemplateStatsRow is the domain Row for workflow template statistics.
type GetTemplateStatsRow struct {
	UsageCount           int64   `json:"usage_count"`
	AvgCompletionSeconds float64 `json:"avg_completion_seconds"`
	RejectRate           float64 `json:"reject_rate"`
}

// ListMembersByWorkspaceRow is the domain Row for listing members by workspace (JOIN workspace_members).
type ListMembersByWorkspaceRow struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Email             string    `json:"email"`
	PasswordHash      string    `json:"-"` // Not serialized
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	WorkspaceRole     string    `json:"workspace_role"`
	WorkspaceJoinedAt time.Time `json:"workspace_joined_at"`
}
