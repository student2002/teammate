// workflow_dto.go defines Workflow-related request/response structs and data conversion functions.
package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// NodeType is an alias for the node type (domain type string).
type NodeType = string

// AssigneeType is an alias for the assignee type (domain type string).
type AssigneeType = string

// CreateTemplateNodeParams is an alias for the create template node parameters (domain type).
type CreateTemplateNodeParams = types.CreateTemplateNodeParams

// node type constants.
const (
	AssigneeTypeSpecificAgent = types.AssigneeTypeSpecificAgent
	AssigneeTypeHuman         = types.AssigneeTypeHuman
)

// templateNodeRequest is the template node request body. ID carries the node's
// identity across node-list replacement: omitting it lets the server generate a
// new UUID; depends_on entries may only reference nodes that declare an ID.
type templateNodeRequest struct {
	ID             *uuid.UUID      `json:"id"`
	Name           string          `json:"name"`
	Description    string          `json:"description"`
	SortOrder      int32           `json:"sort_order"`
	NodeType       NodeType        `json:"node_type"`
	AssigneeType   AssigneeType    `json:"assignee_type"`
	AssigneeID     *uuid.UUID      `json:"assignee_id"`
	TimeoutMinutes int32           `json:"timeout_minutes"`
	ReadonlyDirs   json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
	Artifact       json.RawMessage `json:"artifact"`
	MaxRejectCycles int32          `json:"max_reject_cycles"`
	DependsOn      []uuid.UUID     `json:"depends_on"`
}

// createWorkflowTemplateRequest is the create workflow template request body.
type createWorkflowTemplateRequest struct {
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	IsBuiltin      bool                  `json:"is_builtin"`
	TriggerType    string                `json:"trigger_type"`
	TriggerConfig  json.RawMessage       `json:"trigger_config"`
	TriggerEnabled *bool                 `json:"trigger_enabled"`
	NextRunAt      *time.Time            `json:"next_run_at"`
	Nodes          []templateNodeRequest `json:"nodes"`
}

// updateWorkflowTemplateRequest is the update workflow template request body.
// All metadata fields are pointers: nil keeps the stored value, a provided
// value overwrites it (partial-update semantics).
type updateWorkflowTemplateRequest struct {
	Name           *string               `json:"name"`
	Description    *string               `json:"description"`
	TriggerType    *string               `json:"trigger_type"`
	TriggerConfig  json.RawMessage       `json:"trigger_config"`
	TriggerEnabled *bool                 `json:"trigger_enabled"`
	NextRunAt      *time.Time            `json:"next_run_at"`
	Nodes          []templateNodeRequest `json:"nodes"`
}

// buildCreateTemplateNodeParams builds types.CreateTemplateNodeParams from the handler-layer input.
func buildCreateTemplateNodeParams(
	id *uuid.UUID,
	name string,
	description string,
	sortOrder int32,
	nodeType NodeType,
	assigneeType AssigneeType,
	assigneeID uuid.NullUUID,
	timeoutMinutes int32,
	readonlyDirs pqtype.NullRawMessage,
	fullControlDirs pqtype.NullRawMessage,
	artifact pqtype.NullRawMessage,
	maxRejectCycles int32,
	dependsOn []uuid.UUID,
) types.CreateTemplateNodeParams {
	var idStr *string
	if id != nil {
		s := id.String()
		idStr = &s
	}
	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	var assigneeStr *string
	if assigneeID.Valid {
		s := assigneeID.UUID.String()
		assigneeStr = &s
	}
	depStrs := make([]string, 0, len(dependsOn))
	for _, d := range dependsOn {
		depStrs = append(depStrs, d.String())
	}
	return types.CreateTemplateNodeParams{
		ID:              idStr,
		Name:            name,
		Description:     descPtr,
		SortOrder:       sortOrder,
		NodeType:        nodeType,
		AssigneeType:    assigneeType,
		AssigneeID:      assigneeStr,
		TimeoutMinutes:  timeoutMinutes,
		ReadonlyDirs:    readonlyDirs.RawMessage,
		FullControlDirs: fullControlDirs.RawMessage,
		Artifact:        artifact.RawMessage,
		MaxRejectCycles: maxRejectCycles,
		DependsOn:       depStrs,
	}
}

// validateTemplateNodeDeps rejects depends_on entries that do not reference a
// node declaring its ID within the same request: the node list is fully
// replaced, so a dependency on any other id would be dangling after the update.
func validateTemplateNodeDeps(nodes []templateNodeRequest) error {
	known := make(map[uuid.UUID]struct{}, len(nodes))
	for _, n := range nodes {
		if n.ID == nil {
			continue
		}
		if _, dup := known[*n.ID]; dup {
			return fmt.Errorf("duplicate node id %s in request", n.ID)
		}
		known[*n.ID] = struct{}{}
	}
	for _, n := range nodes {
		for _, dep := range n.DependsOn {
			if _, ok := known[dep]; !ok {
				return fmt.Errorf("node %s depends_on unknown node %s (referenced nodes must declare an id in the same request)", n.Name, dep)
			}
		}
	}
	return nil
}

// maxTemplateNodeTimeoutMinutes bounds node timeout_minutes (0 = no timeout).
// maxTemplateNodeMaxRejectCycles bounds reject cycles before escalation to
// manual intervention; values <= 0 keep the documented "default 5" behavior.
const (
	maxTemplateNodeTimeoutMinutes   = 480
	maxTemplateNodeMaxRejectCycles  = 20
)

// validateTemplateNodeLimits enforces the documented node field bounds
// (timeout 0-480 minutes, max_reject_cycles capped at 20).
func validateTemplateNodeLimits(nodes []templateNodeRequest) error {
	for _, n := range nodes {
		if n.TimeoutMinutes < 0 || n.TimeoutMinutes > maxTemplateNodeTimeoutMinutes {
			return fmt.Errorf("node %s: timeout_minutes must be between 0 and %d", n.Name, maxTemplateNodeTimeoutMinutes)
		}
		if n.MaxRejectCycles > maxTemplateNodeMaxRejectCycles {
			return fmt.Errorf("node %s: max_reject_cycles must be at most %d", n.Name, maxTemplateNodeMaxRejectCycles)
		}
	}
	return nil
}

// buildCreateWorkflowTemplateParams builds types.CreateWorkflowTemplateParams from the handler-layer input.
func buildCreateWorkflowTemplateParams(
	workspaceID uuid.UUID,
	name string,
	description string,
	isBuiltin bool,
	triggerType string,
	triggerConfig json.RawMessage,
	triggerEnabled *bool,
	nextRunAt *time.Time,
) types.CreateWorkflowTemplateParams {
	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	return types.CreateWorkflowTemplateParams{
		WorkspaceID:     workspaceID.String(),
		Name:            name,
		Description:     descPtr,
		IsBuiltin:       isBuiltin,
		TriggerType:     triggerType,
		TriggerConfig:   triggerConfig,
		TriggerEnabled:  triggerEnabledValue(triggerEnabled),
		NextRunAt:       nextRunAt,
	}
}

// buildUpdateWorkflowTemplateParams merges an update request over the current
// template record: nil fields keep the stored value, provided values overwrite.
func buildUpdateWorkflowTemplateParams(
	id uuid.UUID,
	current types.WorkflowTemplate,
	req updateWorkflowTemplateRequest,
) types.UpdateWorkflowTemplateParams {
	name := current.Name
	if req.Name != nil {
		name = *req.Name
	}
	description := current.Description
	if req.Description != nil {
		description = *req.Description
	}
	triggerType := current.TriggerType
	if req.TriggerType != nil {
		triggerType = *req.TriggerType
	}
	triggerConfig := current.TriggerConfig
	if len(req.TriggerConfig) > 0 {
		triggerConfig = req.TriggerConfig
	}
	triggerEnabled := current.TriggerEnabled
	if req.TriggerEnabled != nil {
		triggerEnabled = *req.TriggerEnabled
	}
	nextRunAt := current.NextRunAt
	if req.NextRunAt != nil {
		nextRunAt = req.NextRunAt
	}

	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	return types.UpdateWorkflowTemplateParams{
		ID:             id.String(),
		Name:           name,
		Description:    descPtr,
		TriggerType:    triggerType,
		TriggerConfig:  triggerConfig,
		TriggerEnabled: triggerEnabled,
		NextRunAt:      nextRunAt,
	}
}

func triggerEnabledValue(value *bool) bool {
	if value == nil {
		return true
	}
	return *value
}

// templateResponse is the workflow template response DTO.
type templateResponse struct {
	ID              string          `json:"id"`
	WorkspaceID     string          `json:"workspace_id"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	IsBuiltin       bool            `json:"is_builtin"`
	TriggerType     string          `json:"trigger_type"`
	TriggerConfig   json.RawMessage `json:"trigger_config"`
	TriggerEnabled  bool            `json:"trigger_enabled"`
	NextRunAt       *time.Time      `json:"next_run_at"`
	LastTriggeredAt *time.Time      `json:"last_triggered_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// templateWithNodes is the response struct for a workflow template and its nodes.
type templateWithNodes struct {
	templateResponse
	Nodes []types.WorkflowTemplateNode `json:"nodes"`
}

// toTemplateResponse converts a domain workflow template record to an API response.
func toTemplateResponse(t types.WorkflowTemplate) templateResponse {
	return templateResponse{
		ID:              t.ID,
		WorkspaceID:     t.WorkspaceID,
		Name:            t.Name,
		Description:     t.Description,
		IsBuiltin:       t.IsBuiltin,
		TriggerType:     t.TriggerType,
		TriggerConfig:   t.TriggerConfig,
		TriggerEnabled:  t.TriggerEnabled,
		NextRunAt:       t.NextRunAt,
		LastTriggeredAt: t.LastTriggeredAt,
		CreatedAt:       t.CreatedAt,
		UpdatedAt:       t.UpdatedAt,
	}
}

// normalizeTemplateSortOrder fallback-fixes node sort_order before writing: when missing (<=0) or duplicate,
// it auto-assigns strictly increasing unique sequence numbers to avoid hitting UNIQUE(template_id, sort_order) (deviation #5/#8).
func normalizeTemplateSortOrder(nodes []templateNodeRequest) {
	next := int32(1)
	for i := range nodes {
		if nodes[i].SortOrder < next {
			nodes[i].SortOrder = next
		}
		next = nodes[i].SortOrder + 1
	}
}
