// task_params.go defines the domain parameter structs for Task domain operations.
//
// These structs are the domain counterparts of the sqlc-generated db.XxxParams,
// with fields mapped one-to-one and types mapped in domain style:
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

// CountTasksByStatusParams is the domain parameter struct for counting tasks by status.
type CountTasksByStatusParams struct {
	WorkspaceID string `json:"workspace_id"`
	Statuses    []string `json:"statuses"`
}

// CreateCommentParams is the domain parameter struct for creating a comment.
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

// CreateNodeTransitionParams is the domain parameter struct for creating a node state transition record.
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

// CreateTaskParams is the domain parameter struct for creating a task.
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

// CreateTokenUsageParams is the domain parameter struct for creating a token usage record.
type CreateTokenUsageParams struct {
	TaskNodeID   string  `json:"task_node_id"`
	AgentID      string  `json:"agent_id"`
	InputTokens  int32   `json:"input_tokens"`
	OutputTokens int32   `json:"output_tokens"`
	TotalTokens  int32   `json:"total_tokens"`
	CostEstimate *string `json:"cost_estimate"`
}

// GetCompletedTasksOlderThanParams is the domain parameter struct for querying completed tasks older than a given point in time.
type GetCompletedTasksOlderThanParams struct {
	UpdatedAt time.Time `json:"updated_at"`
}

// GetInProgressNodesByAgentParams is the domain parameter struct for querying in-progress nodes for an Agent.
type GetInProgressNodesByAgentParams struct {
	AgentID string `json:"agent_id"`
}

// IsAgentProjectMemberParams is the domain parameter struct for determining whether an Agent is a project member.
type IsAgentProjectMemberParams struct {
	ProjectID string `json:"project_id"`
	AgentID   string `json:"agent_id"`
}

// ListExecutionContextCommentsParams is the domain parameter struct for listing execution-context comments.
type ListExecutionContextCommentsParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// ListNodeCommentsParams is the domain parameter struct for listing node comments.
type ListNodeCommentsParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// ListTasksParams is the domain parameter struct for listing tasks.
type ListTasksParams struct {
	WorkspaceID string `json:"workspace_id"`
}

// ListTasksPaginatedParams is the domain parameter struct for paginating tasks.
type ListTasksPaginatedParams struct {
	WorkspaceID string  `json:"workspace_id"`
	Statuses    []string `json:"statuses"`
	Limit       int32   `json:"limit"`
	Offset      int32   `json:"offset"`
}

// UpdateCommentParams is the domain parameter struct for updating a comment.
type UpdateCommentParams struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
}

// UpdateTaskParams is the domain parameter struct for updating a task.
type UpdateTaskParams struct {
	ID          int32      `json:"id"`               // Task ID (int32 serial, not UUID)
	Title       string     `json:"title"`
	Description *string    `json:"description"`
	Constraints *string    `json:"constraints"`
	Status      string     `json:"status"`
	DueDate     *time.Time `json:"due_date"`
	Priority    string     `json:"priority"`
	Labels      []string   `json:"labels"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// UpdateTaskGitBranchParams is the domain parameter struct for updating the Git branch associated with a task.
type UpdateTaskGitBranchParams struct {
	ID        string  `json:"id"`
	GitBranch *string `json:"git_branch"`
}

// UpdateTaskStatusParams is the domain parameter struct for updating a task's status.
type UpdateTaskStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}
