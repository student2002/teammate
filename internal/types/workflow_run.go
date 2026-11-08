// workflow_run.go defines the domain types of the V2 workflow run engine:
// the run snapshot structs (task edges, node attempts) and the parameter
// structs used by the store's transactional engine operations.
package types

import (
	"encoding/json"
	"time"
)

// TaskEdge is one edge instance of a task run (a run snapshot of a template
// edge). resolution tracks the §5.2 lifecycle: pending → activated/skipped.
type TaskEdge struct {
	ID           string          `json:"id" db:"id"`                         // Edge UUID
	TaskID       int32           `json:"task_id" db:"task_id"`               // Owning task ID
	SourceNodeID string          `json:"source_node_id" db:"source_node_id"` // Source task node UUID
	SourcePort   string          `json:"source_port" db:"source_port"`       // Output port on the source node
	TargetNodeID string          `json:"target_node_id" db:"target_node_id"` // Target task node UUID
	TargetPort   string          `json:"target_port" db:"target_port"`       // Input port on the target node
	Label        string          `json:"label" db:"label"`                   // Edge label
	Config       json.RawMessage `json:"config" db:"config"`                 // Edge configuration snapshot (JSON)
	Resolution   string          `json:"resolution" db:"resolution"`         // pending / activated / skipped
	ResolvedAt   *time.Time      `json:"resolved_at" db:"resolved_at"`       // Resolution time
	CreatedAt    time.Time       `json:"created_at" db:"created_at"`         // Creation time
}

// TaskNodeAttempt is one real execution of a task node (plan §6.3): created
// when the node starts an attempt, closed with its output or structured error.
type TaskNodeAttempt struct {
	ID           string          `json:"id" db:"id"`                       // Attempt UUID
	TaskID       int32           `json:"task_id" db:"task_id"`             // Owning task ID
	TaskNodeID   string          `json:"task_node_id" db:"task_node_id"`   // Owning node UUID
	AttemptNo    int32           `json:"attempt_no" db:"attempt_no"`       // 1-based attempt number
	ExecutorKind string          `json:"executor_kind" db:"executor_kind"` // server / agentd / human
	ExecutorID   string          `json:"executor_id" db:"executor_id"`     // Executor identity (agent id etc.)
	Status       string          `json:"status" db:"status"`               // running / succeeded / failed / cancelled
	Input        json.RawMessage `json:"input" db:"input"`                 // Input snapshot (JSON)
	Output       json.RawMessage `json:"output" db:"output"`               // Execution output (JSON)
	Error        json.RawMessage `json:"error" db:"error"`                 // Failure detail (JSON)
	StartedAt    time.Time       `json:"started_at" db:"started_at"`       // Attempt start time
	CompletedAt  *time.Time      `json:"completed_at" db:"completed_at"`   // Attempt end time
}

// CreateTaskForRunParams is the domain parameter struct for instantiating the
// task main record of a workflow run.
type CreateTaskForRunParams struct {
	ProjectID          string          `json:"project_id"`
	WorkflowName       string          `json:"workflow_name"`
	WorkflowTemplateID *string         `json:"workflow_template_id"`
	WorkflowVersion    int             `json:"workflow_version"`
	TriggerPayload     json.RawMessage `json:"trigger_payload"`
	Context            json.RawMessage `json:"context"`
	Title              string          `json:"title"`
	AuthorType         string          `json:"author_type"`
	AuthorID           string          `json:"author_id"`
	Sequence           int             `json:"sequence"`
}

// InsertTaskNodeParams is the domain parameter struct for copying one template
// node into the task graph. Trigger nodes pass their terminal fields
// (succeeded status, output, attempt_count 1, completed_at); waiting nodes
// pass the zero values.
type InsertTaskNodeParams struct {
	TaskID          int32           `json:"task_id"`
	TemplateNodeID  string          `json:"template_node_id"`
	Name            string          `json:"name"`
	SortOrder       int             `json:"sort_order"`
	TypeKey         string          `json:"type_key"`
	Config          json.RawMessage `json:"config"`
	Status          string          `json:"status"`
	ActivationMode  string          `json:"activation_mode"`
	Output          json.RawMessage `json:"output"`
	AttemptCount    int             `json:"attempt_count"`
	MaxAttempts     int             `json:"max_attempts"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
	CompletedAt     *time.Time      `json:"completed_at"`
}

// InsertTaskEdgeParams is the domain parameter struct for copying one template
// edge into the task graph (pending resolution).
type InsertTaskEdgeParams struct {
	TaskID       int32           `json:"task_id"`
	SourceNodeID string          `json:"source_node_id"`
	SourcePort   string          `json:"source_port"`
	TargetNodeID string          `json:"target_node_id"`
	TargetPort   string          `json:"target_port"`
	Label        string          `json:"label"`
	Config       json.RawMessage `json:"config"`
}

// UpdateTaskNodeRunParams is the domain parameter struct for the engine's
// optimistic full-state write of one run node. Version is the value the caller
// loaded; the write returns the incremented version.
type UpdateTaskNodeRunParams struct {
	ID            string          `json:"id"`
	Status        string          `json:"status"`
	Input         json.RawMessage `json:"input"`
	Output        json.RawMessage `json:"output"`
	Error         json.RawMessage `json:"error"`
	AttemptCount  int             `json:"attempt_count"`
	MaxAttempts   int             `json:"max_attempts"`
	NextRetryAt   *time.Time      `json:"next_retry_at"`
	WaitKey       string          `json:"wait_key"`
	WaitExpiresAt *time.Time      `json:"wait_expires_at"`
	CompletedAt   *time.Time      `json:"completed_at"`
	// ReservedForAgentID is set when an agentd execution claims the node and
	// rides along every full-state write.
	ReservedForAgentID *string `json:"reserved_for_agent_id"`
	Version            int     `json:"version"`
}

// CreateTaskAttemptParams is the domain parameter struct for the attempt
// upsert keyed by (task_node_id, attempt_no).
type CreateTaskAttemptParams struct {
	TaskID       int32           `json:"task_id"`
	TaskNodeID   string          `json:"task_node_id"`
	AttemptNo    int             `json:"attempt_no"`
	ExecutorKind string          `json:"executor_kind"`
	ExecutorID   string          `json:"executor_id"`
	Status       string          `json:"status"`
	Input        json.RawMessage `json:"input"`
	Output       json.RawMessage `json:"output"`
	Error        json.RawMessage `json:"error"`
	CompletedAt  *time.Time      `json:"completed_at"`
}

// CreateSpawnedTaskParams is the domain parameter struct for the plain task
// created by an action.create_task node: no run graph, type 'task'.
type CreateSpawnedTaskParams struct {
	ProjectID   string `json:"project_id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	AuthorType  string `json:"author_type"`
	AuthorID    string `json:"author_id"`
	Sequence    int    `json:"sequence"`
}
