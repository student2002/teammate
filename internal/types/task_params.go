// task_params.go 定义 Task 领域操作的领域参数结构体。
//
// 这些结构体是 sqlc 生成的 db.XxxParams 的 domain 对应物，
// 字段一一对应，类型按 domain 风格映射：
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - sql.NullInt32 → *int32
//   - pqtype.NullRawMessage → json.RawMessage
package types

import (
	"encoding/json"
	"time"
)

// CountTasksByStatusParams 是按状态统计任务的领域参数结构体。
type CountTasksByStatusParams struct {
	WorkspaceID string `json:"workspace_id"`
	Statuses    []string `json:"statuses"`
}

// CreateCommentParams 是创建评论的领域参数结构体。
type CreateCommentParams struct {
	TaskID       int32           `json:"task_id"`
	NodeID       *string         `json:"node_id"`
	SourceNodeID *string         `json:"source_node_id"`
	ParentID     *string         `json:"parent_id"`
	AuthorType   string          `json:"author_type"`
	AuthorID     string          `json:"author_id"`
	Content      string          `json:"content"`
	CommentType  string          `json:"comment_type"`
	Metadata     json.RawMessage `json:"metadata"`
	Mentions     []string        `json:"mentions"`
}

// CreateNodeTransitionParams 是创建节点状态流转记录的领域参数结构体。
type CreateNodeTransitionParams struct {
	TaskNodeID   string  `json:"task_node_id"`
	FromStatus   string  `json:"from_status"`
	ToStatus     string  `json:"to_status"`
	Action       string  `json:"action"`
	TargetNodeID *string `json:"target_node_id"`
	Comment      *string `json:"comment"`
	OperatorID   *string `json:"operator_id"`
	OperatorType string  `json:"operator_type"`
}

// CreateTaskParams 是创建任务的领域参数结构体。
type CreateTaskParams struct {
	ProjectID    string     `json:"project_id"`
	Title        string     `json:"title"`
	Description  *string    `json:"description"`
	Constraints  *string    `json:"constraints"`
	Type         string     `json:"type"`
	Priority     string     `json:"priority"`
	Status       string     `json:"status"`
	AuthorType   string     `json:"author_type"`
	AuthorID     string     `json:"author_id"`
	DueDate      *time.Time `json:"due_date"`
	Labels       []string   `json:"labels"`
	Sequence     int32      `json:"sequence"`
	WorkflowName string     `json:"workflow_name"`
}

// CreateTokenUsageParams 是创建 Token 用量记录的领域参数结构体。
type CreateTokenUsageParams struct {
	TaskNodeID   string  `json:"task_node_id"`
	AgentID      string  `json:"agent_id"`
	InputTokens  int32   `json:"input_tokens"`
	OutputTokens int32   `json:"output_tokens"`
	TotalTokens  int32   `json:"total_tokens"`
	CostEstimate *string `json:"cost_estimate"`
}

// GetCompletedTasksOlderThanParams 是查询早于某时间点的已完成任务的领域参数结构体。
type GetCompletedTasksOlderThanParams struct {
	UpdatedAt time.Time `json:"updated_at"`
}

// GetInProgressNodesByAgentParams 是查询某 Agent 进行中节点的领域参数结构体。
type GetInProgressNodesByAgentParams struct {
	AgentID string `json:"agent_id"`
}

// IsAgentProjectMemberParams 是判断 Agent 是否为项目成员的领域参数结构体。
type IsAgentProjectMemberParams struct {
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

// ListExecutionContextCommentsParams 是列出执行上下文评论的领域参数结构体。
type ListExecutionContextCommentsParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// ListNodeCommentsParams 是列出节点评论的领域参数结构体。
type ListNodeCommentsParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// ListTasksParams 是列出任务的领域参数结构体。
type ListTasksParams struct {
	WorkspaceID string `json:"workspace_id"`
}

// ListTasksPaginatedParams 是分页列出任务的领域参数结构体。
type ListTasksPaginatedParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Statuses    []string `json:"statuses"`
	Limit       int32   `json:"limit"`
	Offset      int32   `json:"offset"`
}

// UpdateCommentParams 是更新评论的领域参数结构体。
type UpdateCommentParams struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
}

// UpdateTaskParams 是更新任务的领域参数结构体。
type UpdateTaskParams struct {
	ID          int32      `json:"id"`               // 任务 ID（int32 serial，非 UUID）
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	Constraints *string    `json:"constraints"`
	Status      string     `json:"status"`
	DueDate     *time.Time `json:"due_date"`
	Priority    string     `json:"priority"`
	Labels      []string   `json:"labels"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// UpdateTaskGitBranchParams 是更新任务关联 Git 分支的领域参数结构体。
type UpdateTaskGitBranchParams struct {
	ID        string  `json:"id"`
	GitBranch *string `json:"git_branch"`
}

// UpdateTaskStatusParams 是更新任务状态的领域参数结构体。
type UpdateTaskStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
