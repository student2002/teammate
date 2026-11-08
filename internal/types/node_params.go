// node_params.go defines the domain parameter structs for TaskNode/Subtask domain operations.
//
// These structs are the domain counterparts of the sqlc-generated db.XxxParams,
// with fields mapped one-to-one and types mapped in domain style:
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - pqtype.NullRawMessage → json.RawMessage
package types

import (
	"encoding/json"
	"time"
)

// CreateTaskNodeParams is the domain parameter struct for creating a task node.
type CreateTaskNodeParams struct {
	TaskID          int32           `json:"task_id"`
	TemplateNodeID  string          `json:"template_node_id"`
	Name            string          `json:"name"`
	SortOrder       int32           `json:"sort_order"`
	TypeKey         string          `json:"type_key"`
	Config          json.RawMessage `json:"config"`
	Status          string          `json:"status"`
	ActivationMode  string          `json:"activation_mode"`
	MaxAttempts     int32           `json:"max_attempts"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
}

// CreateSubtaskParams is the domain parameter struct for creating a subtask.
type CreateSubtaskParams struct {
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
	ParentTaskID *int32     `json:"parent_task_id"`
}

// CreateTaskLogParams is the domain parameter struct for creating a task log.
type CreateTaskLogParams struct {
	TaskID    int32     `json:"task_id"`
	NodeID    string    `json:"node_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ListTaskLogsByTaskNodeParams is the domain parameter struct for listing task logs by node.
type ListTaskLogsByTaskNodeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}
