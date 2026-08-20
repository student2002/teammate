// node_params.go 定义 TaskNode/Subtask 领域操作的领域参数结构体。
//
// 这些结构体是 sqlc 生成的 db.XxxParams 的 domain 对应物，
// 字段一一对应，类型按 domain 风格映射：
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

// ClaimTaskNodeParams 是 Agent 认领节点的领域参数结构体。
type ClaimTaskNodeParams struct {
	ID         string  `json:"id"`
	AssigneeID *string `json:"assignee_id"`
	Version    int32   `json:"version"`
}

// ClaimTaskNodeByHumanParams 是人类认领节点的领域参数结构体。
type ClaimTaskNodeByHumanParams struct {
	ID         string  `json:"id"`
	AssigneeID *string `json:"assignee_id"`
	Version    int32   `json:"version"`
}

// CreateTaskNodeParams 是创建任务节点的领域参数结构体。
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

// GetNextTaskNodeParams 是获取下一节点的领域参数结构体。
type GetNextTaskNodeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// GetPrevStandardNodeAssigneeParams 是获取前一标准节点 assignee 的领域参数结构体。
type GetPrevStandardNodeAssigneeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// GetPrevTaskNodeParams 是获取前一节点的领域参数结构体。
type GetPrevTaskNodeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}

// GetTaskNodeBySortOrderParams 是按 sort_order 获取节点的领域参数结构体。
type GetTaskNodeBySortOrderParams struct {
	TaskID    int32 `json:"task_id"`
	SortOrder int32 `json:"sort_order"`
}

// ReclaimTaskNodeParams 是重新认领节点的领域参数结构体。
type ReclaimTaskNodeParams struct {
	ID      string `json:"id"`
	Version int32  `json:"version"`
}

// ResetRejectCountParams 是重置节点驳回计数的领域参数结构体。
type ResetRejectCountParams struct {
	ID string `json:"id"`
}

// UpdateNodeSummaryParams 是更新节点摘要的领域参数结构体。
type UpdateNodeSummaryParams struct {
	ID              string `json:"id"`
	Summary         string `json:"summary"`
	PreviousSummary string `json:"previous_summary"`
}

// UpdateTaskNodeStatusParams 是更新节点状态的领域参数结构体。
//
// 注意：底层 SQL `UpdateTaskNodeStatus` 是"全字段覆盖 UPDATE"——
// SET 子句会覆盖 assignee_type/assignee_id/reserved_for_agent_id/reject_count/reservation_expires_at，
// WHERE 子句用 version 和旧 status 做乐观锁校验。
// 因此调用方必须从当前节点快照回填全部字段（不是只传新 status）。
type UpdateTaskNodeStatusParams struct {
	ID          string     `json:"id"`
	Status      string     `json:"status"`                 // 新状态（SET status = $2）
	AssigneeType string    `json:"assignee_type"`          // SET assignee_type = $3
	AssigneeID  *string    `json:"assignee_id"`            // SET assignee_id = $4
	ReservedForAgentID *string `json:"reserved_for_agent_id"` // SET reserved_for_agent_id = $5
	RejectCount int32      `json:"reject_count"`           // SET reject_count = $6
	CompletedAt *time.Time `json:"completed_at"`           // SET completed_at = $7
	CompletedBy *string    `json:"completed_by"`           // SET completed_by = $8
	ReservationExpiresAt *time.Time `json:"reservation_expires_at"` // SET reservation_expires_at = $9
	Version     int32      `json:"version"`                // WHERE version = $10（旧版本号，UPDATE 后自增）
	ExpectedCurrentStatus string `json:"expected_current_status"` // WHERE status = $11（旧状态校验）
}

// CreateSubtaskParams 是创建子任务的领域参数结构体。
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

// CreateTaskLogParams 是创建任务日志的领域参数结构体。
type CreateTaskLogParams struct {
	TaskID    int32     `json:"task_id"`
	NodeID    string    `json:"node_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ListTaskLogsByTaskNodeParams 是按节点列出任务日志的领域参数结构体。
type ListTaskLogsByTaskNodeParams struct {
	TaskID int32  `json:"task_id"`
	NodeID string `json:"node_id"`
}
