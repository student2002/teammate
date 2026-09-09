// node_params.go defines the domain parameter structs for TaskNode/Subtask domain operations.
//
// These structs are the domain counterparts of the sqlc-generated db.XxxParams,
// with fields mapped one-to-one and types mapped in domain style:
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - sql.NullTime → *time.Time
//   - []uuid.UUID → []string
//   - pqtype.NullRawMessage → json.RawMessage
package types

import (
	"encoding/json"
	"time"
)

// ClaimTaskNodeParams is the domain parameter struct for an Agent claiming a node.
type ClaimTaskNodeParams struct {
	ID         string  `json:"id"`
	AssigneeID *string `json:"assignee_id"`
	Version    int32   `json:"version"`
}

// ClaimTaskNodeByHumanParams is the domain parameter struct for a human claiming a node.
type ClaimTaskNodeByHumanParams struct {
	ID         string  `json:"id"`
	AssigneeID *string `json:"assignee_id"`
	Version    int32   `json:"version"`
}

// CreateTaskNodeParams is the domain parameter struct for creating a task node.
type CreateTaskNodeParams struct {
	TaskID            int32           `json:"task_id"`
	Name              string          `json:"name"`
	Description       *string         `json:"description"`
	SortOrder         int32           `json:"sort_order"`
	NodeType          string          `json:"node_type"`
	Status            string          `json:"status"`
	AssigneeType      string          `json:"assignee_type"`
	AssigneeID        *string         `json:"assignee_id"`
	ReservedForAgentID *string        `json:"reserved_for_agent_id"`
	MaxRejectCycles   int32           `json:"max_reject_cycles"`
	TimeoutMinutes    int32           `json:"timeout_minutes"`
	ReadonlyDirs      json.RawMessage `json:"readonly_dirs"`
	FullControlDirs   json.RawMessage `json:"full_control_dirs"`
	DependsOn         []string        `json:"depends_on"`
}

// GetNextTaskNodeParams is the domain parameter struct for getting the next node.
type GetNextTaskNodeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// GetPrevStandardNodeAssigneeParams is the domain parameter struct for getting the assignee of the previous standard node.
type GetPrevStandardNodeAssigneeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// GetPrevTaskNodeParams is the domain parameter struct for getting the previous node.
type GetPrevTaskNodeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// GetTaskNodeBySortOrderParams is the domain parameter struct for getting a node by sort_order.
type GetTaskNodeBySortOrderParams struct {
	TaskID    int32 `json:"task_id"`
	SortOrder int32 `json:"sort_order"`
}

// ReclaimTaskNodeParams is the domain parameter struct for reclaiming a node.
type ReclaimTaskNodeParams struct {
	ID      string `json:"id"`
	Version int32  `json:"version"`
}

// ResetRejectCountParams is the domain parameter struct for resetting a node's reject count.
type ResetRejectCountParams struct {
	ID string `json:"id"`
}

// UpdateNodeSummaryParams is the domain parameter struct for updating a node's summary.
type UpdateNodeSummaryParams struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	PreviousSummary string `json:"previous_summary"`
}

// UpdateTaskNodeStatusParams is the domain parameter struct for updating a node's status.
//
// Note: the underlying SQL `UpdateTaskNodeStatus` is a "full-field overwrite UPDATE" —
// the SET clause overwrites assignee_type/assignee_id/reserved_for_agent_id/reject_count/reservation_expires_at,
// and the WHERE clause uses version and the old status for optimistic-lock verification.
// Therefore the caller must backfill all fields from the current node snapshot (not just the new status).
type UpdateTaskNodeStatusParams struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`                 // New status (SET status = $2)
	AssigneeType string    `json:"assignee_type"`          // SET assignee_type = $3
	AssigneeID  *string    `json:"assignee_id"`            // SET assignee_id = $4
	ReservedForAgentID *string `json:"reserved_for_agent_id"` // SET reserved_for_agent_id = $5
	RejectCount int32      `json:"reject_count"`           // SET reject_count = $6
	CompletedAt *time.Time `json:"completed_at"`           // SET completed_at = $7
	CompletedBy *string    `json:"completed_by"`           // SET completed_by = $8
	ReservationExpiresAt *time.Time `json:"reservation_expires_at"` // SET reservation_expires_at = $9
	Version     int32      `json:"version"`                // WHERE version = $10 (old version number, auto-incremented after UPDATE)
	ExpectedCurrentStatus string `json:"expected_current_status"` // WHERE status = $11 (old status verification)
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
