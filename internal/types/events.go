// events.go defines the constants and structs related to SSE (Server-Sent Events).
//
// This file contains:
//   - SSEEvent: the struct definition of a Server-Sent Event
//   - Event* constants: the standard SSE event types
//
// SSE is used for real-time event push from server → agent, supporting Last-Event-ID reconnection.
package types

import "encoding/json"

// SSEEvent defines the structure of a Server-Sent Event.
// It is used for real-time event push from server → agent.
type SSEEvent struct {
	ID    string          `json:"id"`    // Unique event ID, supporting Last-Event-ID reconnection
	Event string          `json:"event"` // Event type (e.g. node:pending, task:interrupt)
	Data  json.RawMessage `json:"data"`  // Event data payload (JSON)
}

// Standard SSE event-type constants.
const (
	EventNodePending            = "node:pending"             // A new node is pending claim
	EventNodeContinuationInvite = "node:continuation_invite" // Continuation-right invitation after a node completes
	EventMentionTrigger         = "mention:trigger"          // @mention trigger
	EventTaskInterrupt          = "task:interrupt"           // Task interruption (control event)
	EventNodeTimeout            = "node:timeout"             // Node timeout (control event)
	EventSyncRequired           = "sync:required"            // Full sync required (control event)
	EventNodeRejectRollback     = "node:reject_rollback"     // Review-reject rollback (control event)
	EventPermissionChanged      = "permission:changed"       // Permission change (control event)
)
