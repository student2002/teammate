// community.go provides the business logic for community workflows.
// Community workflows are reusable workflow templates contributed by the community that users can import into their own workspaces.
package service

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CommunityService provides the business logic for community workflows.
type CommunityService struct {
	svc *Service
}

func NewCommunityService(svc *Service) *CommunityService {
	return &CommunityService{svc: svc}
}

// Create creates a new community workflow.
func (s *CommunityService) Create(ctx context.Context, params types.CreateCommunityWorkflowParams) (types.CommunityWorkflow, error) {
	return s.svc.Store.CreateCommunityWorkflow(ctx, params)
}

// List lists all community workflows.
func (s *CommunityService) List(ctx context.Context) ([]types.CommunityWorkflow, error) {
	return s.svc.Store.ListCommunityWorkflows(ctx)
}

// Get retrieves a community workflow by ID.
func (s *CommunityService) Get(ctx context.Context, id uuid.UUID) (types.CommunityWorkflow, error) {
	return s.svc.Store.GetCommunityWorkflow(ctx, id)
}

// ImportWorkflowResult holds the result of importing a community workflow.
type ImportWorkflowResult struct {
	Template       types.WorkflowTemplate  `json:"template"`
	SourceWorkflow types.CommunityWorkflow `json:"source_workflow"`
}

// ImportWorkflow imports a community workflow into the specified workspace,
// creating a workflow template with the nodes parsed from
// workflow_definition. Assignee identity (assignee_id) is dropped: agent and
// member UUIDs are workspace-scoped and never portable across workspaces.
func (s *CommunityService) ImportWorkflow(ctx context.Context, id uuid.UUID, workspaceID uuid.UUID) (*ImportWorkflowResult, error) {
	cw, err := s.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("import community workflow: %w", err)
	}

	nodes, err := parseWorkflowDefinition(cw.WorkflowDefinition)
	if err != nil {
		return nil, fmt.Errorf("parse workflow_definition: %w", err)
	}

	var descPtr *string
	if cw.Description != "" {
		d := cw.Description
		descPtr = &d
	}
	template, _, err := s.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: workspaceID.String(),
		Name:        cw.Name,
		Description: descPtr,
		IsBuiltin:   false,
	}, nodes)
	if err != nil {
		return nil, fmt.Errorf("create workflow template: %w", err)
	}

	return &ImportWorkflowResult{
		Template:       template,
		SourceWorkflow: cw,
	}, nil
}

// workflowDefinition is the portable node list carried by a community
// workflow entry. Field semantics match CreateTemplateNodeParams; optional
// fields fall back to the same defaults the template-create handler applies.
type workflowDefinition struct {
	Nodes []definitionNode `json:"nodes"`
}

type definitionNode struct {
	Name            string          `json:"name"`
	Description     *string         `json:"description"`
	SortOrder       int32           `json:"sort_order"`
	NodeType        string          `json:"node_type"`
	AssigneeType    string          `json:"assignee_type"`
	TimeoutMinutes  int32           `json:"timeout_minutes"`
	ReadonlyDirs    json.RawMessage `json:"readonly_dirs"`
	FullControlDirs json.RawMessage `json:"full_control_dirs"`
	Artifact        json.RawMessage `json:"artifact"`
	MaxRejectCycles int32           `json:"max_reject_cycles"`
	DependsOn       []string        `json:"depends_on"`
}

// parseWorkflowDefinition decodes a community workflow definition into
// template node params. Defaults: node_type=standard, assignee_type=any_agent,
// sort_order=1..n in array order, max_reject_cycles=5.
func parseWorkflowDefinition(raw json.RawMessage) ([]types.CreateTemplateNodeParams, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("definition is empty")
	}
	var def workflowDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, err
	}
	if len(def.Nodes) == 0 {
		return nil, fmt.Errorf("definition has no nodes")
	}

	nodes := make([]types.CreateTemplateNodeParams, 0, len(def.Nodes))
	for i, n := range def.Nodes {
		if n.Name == "" {
			return nil, fmt.Errorf("node %d: name is required", i+1)
		}
		nodeType := n.NodeType
		if nodeType == "" {
			nodeType = types.NodeTypeStandard
		}
		switch nodeType {
		case types.NodeTypeStandard, types.NodeTypeReview, types.NodeTypeManual:
		default:
			return nil, fmt.Errorf("node %d: invalid node_type %q", i+1, nodeType)
		}
		assigneeType := n.AssigneeType
		if assigneeType == "" {
			assigneeType = types.AssigneeTypeAnyAgent
		}
		switch assigneeType {
		case types.AssigneeTypeAnyAgent, types.AssigneeTypeSpecificAgent, types.AssigneeTypeHuman, types.AssigneeTypeAuto:
		default:
			return nil, fmt.Errorf("node %d: invalid assignee_type %q", i+1, assigneeType)
		}
		sortOrder := n.SortOrder
		if sortOrder <= 0 {
			sortOrder = int32(i + 1)
		}
		maxRejectCycles := n.MaxRejectCycles
		if maxRejectCycles <= 0 {
			maxRejectCycles = 5
		}

		nodes = append(nodes, types.CreateTemplateNodeParams{
			Name:            n.Name,
			Description:     n.Description,
			SortOrder:       sortOrder,
			NodeType:        nodeType,
			AssigneeType:    assigneeType,
			TimeoutMinutes:  n.TimeoutMinutes,
			ReadonlyDirs:    n.ReadonlyDirs,
			FullControlDirs: n.FullControlDirs,
			Artifact:        n.Artifact,
			MaxRejectCycles: maxRejectCycles,
			DependsOn:       n.DependsOn,
		})
	}
	return nodes, nil
}
