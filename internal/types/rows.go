// rows.go 定义 sqlc 生成的复合查询 Row（涉及多表 JOIN 或聚合）的 domain 对应物。
//
// sqlc 在查询涉及 JOIN/聚合时会生成 XxxRow 结构体，与单表的 Xxx 实体不同。
// 本文件将这些 Row 类型映射为 domain 风格（uuid→string、sql.NullXX→*T 等），
// 供 Store 层方法签名返回、Service 层透传、Handler 层直接序列化。
package types

import (
	"encoding/json"
	"time"
)

// ListAgentMcpServersRow 是列出 Agent 关联的 MCP 服务器（含服务器明细）的 domain Row。
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

// ListAgentSkillsRow 是列出 Agent 关联的技能（含技能明细）的 domain Row。
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

// GetAuthTokenByLookupHashAndTypeRow 是按 hash 和类型查询 token 的 domain Row。
type GetAuthTokenByLookupHashAndTypeRow struct {
	OwnerType string `json:"owner_type"`
	OwnerID   string `json:"owner_id"`
	TokenHash string `json:"token_hash"`
}

// SearchMemoriesRow 是搜索记忆的 domain Row（含 embedding 字段，但 domain 不读 embedding）。
type SearchMemoriesRow struct {
	ID           string          `json:"id"`
	WorkspaceID  string          `json:"workspace_id"`
	SourceTaskID *int32          `json:"source_task_id"`
	Type         string          `json:"type"`
	Title        string          `json:"title"`
	Content      string          `json:"content"`
	Tags         []string        `json:"tags"`
	Embedding    json.RawMessage `json:"embedding,omitempty"` // 实际不读，保留以备扩展
	Confidence   float32         `json:"confidence"`
	Verified     bool            `json:"verified"`
	Metadata     json.RawMessage `json:"metadata"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// ListManualInterventionNodesRow 是列出待人工干预节点的 domain Row（含任务标题）。
type ListManualInterventionNodesRow struct {
	ID        string    `json:"id"`
	TaskID    int32     `json:"task_id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	TaskTitle string    `json:"task_title"`
}

// ListMentionCommentsRow 是列出提及某成员的评论的 domain Row（含任务标题）。
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

// GetReviewQueueRow 是获取审查队列的 domain Row（JOIN tasks+task_nodes+agents）。
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

// GetCompletedTasksOlderThanRow 是查询早于某时间点的已完成任务的 domain Row。
type GetCompletedTasksOlderThanRow struct {
	ID          int32  `json:"id"`
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
}

// GetInProgressNodesByAgentRow 是查询某 Agent 进行中节点的 domain Row（JOIN tasks 获取 project_id）。
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

// GetTokenUsageByAgentRow 是单个 Agent 的 Token 用量聚合 domain Row。
type GetTokenUsageByAgentRow struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	TotalTokens  int64 `json:"total_tokens"`
}

// GetTokenUsageByAgentsRow 是按 Agent 分组的 Token 用量聚合 domain Row。
type GetTokenUsageByAgentsRow struct {
	AgentID      string `json:"agent_id"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

// GetTokenUsageByTaskRow 是单个任务的 Token 用量聚合 domain Row。
type GetTokenUsageByTaskRow struct {
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalTokens  int64   `json:"total_tokens"`
	CostEstimate *string `json:"cost_estimate"`
}

// GetTokenUsageByTaskNodesRow 是按节点分组的 Token 用量聚合 domain Row。
type GetTokenUsageByTaskNodesRow struct {
	TaskNodeID   string `json:"task_node_id"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
}

// GetTemplateStatsRow 是工作流模板统计的 domain Row。
type GetTemplateStatsRow struct {
	UsageCount           int64   `json:"usage_count"`
	AvgCompletionSeconds float64 `json:"avg_completion_seconds"`
	RejectRate           float64 `json:"reject_rate"`
}

// ListMembersByWorkspaceRow 是按工作区列出成员的 domain Row（JOIN workspace_members）。
type ListMembersByWorkspaceRow struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Email             string    `json:"email"`
	PasswordHash      string    `json:"-"` // 不序列化
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	WorkspaceRole     string    `json:"workspace_role"`
	WorkspaceJoinedAt time.Time `json:"workspace_joined_at"`
}
