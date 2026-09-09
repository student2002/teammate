// workflow_dto.go defines Workflow-related request/response structs and data conversion functions.
package handler

import (
	"encoding/json"
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

// templateNodeRequest is the template node request body.
type templateNodeRequest struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	SortOrder       int32           `json:"sort_order"`
	NodeType        NodeType        `json:"node_type"`
	AssigneeType    AssigneeType    `json:"assignee_type"`
	AssigneeID      *uuid.UUID      `json:"assignee_id"`
	TimeoutMinutes  int32           `json:"timeout_minutes"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
	Artifact        json.RawMessage `json:"artifact"`
	MaxRejectCycles int32           `json:"max_reject_cycles"`
	DependsOn       []uuid.UUID     `json:"depends_on"`
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
type updateWorkflowTemplateRequest struct {
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	TriggerType    string                `json:"trigger_type"`
	TriggerConfig  json.RawMessage       `json:"trigger_config"`
	TriggerEnabled *bool                 `json:"trigger_enabled"`
	NextRunAt      *time.Time            `json:"next_run_at"`
	Nodes          []templateNodeRequest `json:"nodes"`
}

// buildCreateTemplateNodeParams builds types.CreateTemplateNodeParams from the handler-layer input.
func buildCreateTemplateNodeParams(
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

// buildUpdateWorkflowTemplateParams builds types.UpdateWorkflowTemplateParams from the handler-layer input.
func buildUpdateWorkflowTemplateParams(
	id uuid.UUID,
	name string,
	description string,
	triggerType string,
	triggerConfig json.RawMessage,
	triggerEnabled *bool,
	nextRunAt *time.Time,
) types.UpdateWorkflowTemplateParams {
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
		TriggerEnabled: triggerEnabledValue(triggerEnabled),
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
