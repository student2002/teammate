// workflow_dto.go 定义 Workflow 相关的请求/响应结构体和数据转换函数。
package handler

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// NodeType 节点类型的别名（领域类型 string）。
type NodeType = string

// AssigneeType 分配人类型的别名（领域类型 string）。
type AssigneeType = string

// CreateTemplateNodeParams 创建模板节点参数的别名（领域类型）。
type CreateTemplateNodeParams = types.CreateTemplateNodeParams

// 节点类型常量。
const (
	AssigneeTypeSpecificAgent = types.AssigneeTypeSpecificAgent
	AssigneeTypeHuman         = types.AssigneeTypeHuman
)

// templateNodeRequest 模板节点请求体。
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

// createWorkflowTemplateRequest 创建工作流模板请求体。
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

// updateWorkflowTemplateRequest 更新工作流模板请求体。
type updateWorkflowTemplateRequest struct {
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	TriggerType    string                `json:"trigger_type"`
	TriggerConfig  json.RawMessage       `json:"trigger_config"`
	TriggerEnabled *bool                 `json:"trigger_enabled"`
	NextRunAt      *time.Time            `json:"next_run_at"`
	Nodes          []templateNodeRequest `json:"nodes"`
}

// buildCreateTemplateNodeParams 根据 handler 层输入构造 types.CreateTemplateNodeParams。
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

// buildCreateWorkflowTemplateParams 根据 handler 层输入构造 types.CreateWorkflowTemplateParams。
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

// buildUpdateWorkflowTemplateParams 根据 handler 层输入构造 types.UpdateWorkflowTemplateParams。
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

// templateResponse 工作流模板响应 DTO。
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

// templateWithNodes 工作流模板及其节点的响应结构体。
type templateWithNodes struct {
	templateResponse
	Nodes []types.WorkflowTemplateNode `json:"nodes"`
}

// toTemplateResponse 将 domain 工作流模板记录转换为 API 响应。
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

// normalizeTemplateSortOrder 在写入前兜底修正节点 sort_order：缺失(<=0)或重复时
// 自动分配严格递增的唯一序号，避免命中 UNIQUE(template_id, sort_order)（偏差 #5/#8）。
func normalizeTemplateSortOrder(nodes []templateNodeRequest) {
	next := int32(1)
	for i := range nodes {
		if nodes[i].SortOrder < next {
			nodes[i].SortOrder = next
		}
		next = nodes[i].SortOrder + 1
	}
}
