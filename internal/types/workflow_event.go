// workflow_event.go defines the domain types of the V2 external event
// pipeline (plan §7.5, §7.10): durable webhook events, flow.wait(github_event)
// waiter registrations and the trigger-node lookup row.
package types

import (
	"encoding/json"
	"time"
)

// WorkflowEvent is one durable external delivery stored on workflow_events.
// payload holds the normalized trigger payload (plan §3.3 stable subset).
type WorkflowEvent struct {
	ID            string          `json:"id" db:"id"`
	Provider      string          `json:"provider" db:"provider"`
	IntegrationID string          `json:"integration_id" db:"integration_id"`
	ExternalID    string          `json:"external_id" db:"external_id"`
	EventType     string          `json:"event_type" db:"event_type"`
	Action        string          `json:"action" db:"action"`
	Payload       json.RawMessage `json:"payload" db:"payload"`
	Status        string          `json:"status" db:"status"`
	Attempts      int             `json:"attempts" db:"attempts"`
	Error         string          `json:"error" db:"error"`
	ReceivedAt    time.Time       `json:"received_at" db:"received_at"`
	ProcessedAt   *time.Time      `json:"processed_at" db:"processed_at"`
}

// WorkflowEventStatus values of the workflow_events.status column.
const (
	EventStatusPending    = "pending"
	EventStatusProcessing = "processing"
	EventStatusCompleted  = "completed"
	EventStatusFailed     = "failed"
)

// WorkflowEventWaiter is one flow.wait(github_event) registration. filter is
// reserved for event filtering; the first version stores an empty object and
// does not filter.
type WorkflowEventWaiter struct {
	ID            string          `json:"id" db:"id"`
	TaskID        int32           `json:"task_id" db:"task_id"`
	TaskNodeID    string          `json:"task_node_id" db:"task_node_id"`
	IntegrationID string          `json:"integration_id" db:"integration_id"`
	EventType     string          `json:"event_type" db:"event_type"`
	Filter        json.RawMessage `json:"filter" db:"filter"`
	Status        string          `json:"status" db:"status"`
	ExpiresAt     *time.Time      `json:"expires_at" db:"expires_at"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
	ResolvedAt    *time.Time      `json:"resolved_at" db:"resolved_at"`
}

// WaiterStatus values of the workflow_event_waiters.status column.
const (
	WaiterStatusWaiting  = "waiting"
	WaiterStatusResolved = "resolved"
)

// CreateEventWaiterParams is the domain parameter struct for registering one
// event waiter inside the engine transaction.
type CreateEventWaiterParams struct {
	TaskID        int32           `json:"task_id"`
	TaskNodeID    string          `json:"task_node_id"`
	IntegrationID string          `json:"integration_id"`
	EventType     string          `json:"event_type"`
	Filter        json.RawMessage `json:"filter"`
}

// GitHubTriggerNode is one trigger.github template node bound to an
// integration, joined with its template identity for the event matcher.
type GitHubTriggerNode struct {
	NodeID       string          `json:"node_id" db:"node_id"`
	Config       json.RawMessage `json:"config" db:"config"`
	TemplateID   string          `json:"template_id" db:"template_id"`
	TemplateName string          `json:"template_name" db:"template_name"`
	Enabled      bool            `json:"enabled" db:"enabled"`
}
