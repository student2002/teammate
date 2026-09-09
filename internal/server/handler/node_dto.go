// node_dto.go defines request/response structs and domain type aliases related to Node.
package handler

import (
	"github.com/google/uuid"

	apitypes "github.com/teammate/server/internal/types"
)

// ---- domain type aliases, removing the handler's direct dependency on db/generated ----
// Note: AssigneeType, AssigneeTypeHuman, NodeType are defined in workflow_dto.go.

type TaskNodeStatus = string

const TaskNodeStatusInProgress = apitypes.TaskNodeStatusInProgress

const NodeTypeStandard = apitypes.NodeTypeStandard

type TransitionAction = string

const TransitionActionInterruptAck = apitypes.TransitionActionInterruptAck

type CreateNodeTransitionParams = apitypes.CreateNodeTransitionParams

// ---- request structs ----

// approveNodeRequest approval request body.
type approveNodeRequest struct {
	Comment string `json:"comment"` // approval comment
}

// rejectNodeRequest rejection request body.
type rejectNodeRequest struct {
	TargetNodeID *uuid.UUID `json:"target_node_id"` // rollback target node ID (optional)
	Comment      string     `json:"comment"`        // rejection comment
}

// manualInterventionRequest manual intervention request body.
type manualInterventionRequest struct {
	Comment string `json:"comment"` // intervention explanation
}

// resolveNodeRequest resolve manual intervention request body.
type resolveNodeRequest struct {
	Comment string     `json:"comment"`  // resolution explanation
	AgentID *uuid.UUID `json:"agent_id"` // optional: reassign to another Agent
	Action  string     `json:"action"`   // resolution method: re_execute (default) or complete
}

// completeNodeRequest complete node request body.
type completeNodeRequest struct {
	Summary string `json:"summary"` // execution summary
}

// interruptAckRequest interrupt acknowledgment request body.
type interruptAckRequest struct {
	Comment string `json:"comment"` // acknowledgment explanation
}

// updateSummaryRequest update summary request body.
type updateSummaryRequest struct {
	Summary string `json:"summary"` // execution summary
}
