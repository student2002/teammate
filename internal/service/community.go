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

// ImportWorkflow imports a community workflow into the specified workspace.
// The imported definition goes through the same full-graph validation as an
// editor save; definitions that do not form a valid V2 graph are rejected.
func (s *CommunityService) ImportWorkflow(ctx context.Context, id uuid.UUID, workspaceID uuid.UUID) (*ImportWorkflowResult, error) {
	cw, err := s.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("import community workflow: %w", err)
	}

	nodes, edges, err := parseWorkflowDefinition(cw.WorkflowDefinition)
	if err != nil {
		return nil, fmt.Errorf("parse workflow_definition: %w", err)
	}

	var descPtr *string
	if cw.Description != "" {
		d := cw.Description
		descPtr = &d
	}
	input := TemplateGraphInput{
		Name:        cw.Name,
		Description: descPtr,
		Enabled:     true,
		Nodes:       nodes,
		Edges:       edges,
	}
	if err := validateTemplateGraph(s.svc.WorkflowRegistry, input); err != nil {
		return nil, fmt.Errorf("validate imported workflow: %w", err)
	}
	template, _, _, err := s.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: workspaceID.String(),
		Name:        cw.Name,
		Description: descPtr,
		IsBuiltin:   false,
		Version:     1,
		Enabled:     true,
	}, nodes, edges)
	if err != nil {
		return nil, fmt.Errorf("create workflow template: %w", err)
	}

	return &ImportWorkflowResult{
		Template:       template,
		SourceWorkflow: cw,
	}, nil
}

// workflowDefinition is the portable graph carried by a community workflow
// entry. Field semantics match the V2 template write contract: importing is
// held to the same full-graph validation as editor saves.
type workflowDefinition struct {
	Nodes []definitionNode `json:"nodes"`
	Edges []definitionEdge `json:"edges"`
}

type definitionNode struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	SortOrder int32           `json:"sort_order"`
	TypeKey   string          `json:"type_key"`
	Config    json.RawMessage `json:"config"`
	UIConfig  json.RawMessage `json:"ui_config"`
}

type definitionEdge struct {
	SourceNodeID string `json:"source_node_id"`
	SourcePort   string `json:"source_port"`
	TargetNodeID string `json:"target_node_id"`
	TargetPort   string `json:"target_port"`
	Label        string `json:"label"`
}

// parseWorkflowDefinition decodes a community workflow definition into
// template node/edge params. Default: sort_order=1..n in array order.
func parseWorkflowDefinition(raw json.RawMessage) ([]types.CreateTemplateNodeParams, []types.CreateTemplateEdgeParams, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("definition is empty")
	}
	var def workflowDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return nil, nil, err
	}
	if len(def.Nodes) == 0 {
		return nil, nil, fmt.Errorf("definition has no nodes")
	}

	nodes := make([]types.CreateTemplateNodeParams, 0, len(def.Nodes))
	for i, n := range def.Nodes {
		if n.ID == "" {
			return nil, nil, fmt.Errorf("node %d: id is required", i+1)
		}
		if n.Name == "" {
			return nil, nil, fmt.Errorf("node %d: name is required", i+1)
		}
		if n.TypeKey == "" {
			return nil, nil, fmt.Errorf("node %d: type_key is required", i+1)
		}
		sortOrder := n.SortOrder
		if sortOrder <= 0 {
			sortOrder = int32(i + 1)
		}

		nodeID := n.ID
		nodes = append(nodes, types.CreateTemplateNodeParams{
			ID:        &nodeID,
			Name:      n.Name,
			SortOrder: sortOrder,
			TypeKey:   n.TypeKey,
			Config:    n.Config,
			UIConfig:  n.UIConfig,
		})
	}

	edges := make([]types.CreateTemplateEdgeParams, 0, len(def.Edges))
	for i, e := range def.Edges {
		if e.SourceNodeID == "" || e.TargetNodeID == "" {
			return nil, nil, fmt.Errorf("edge %d: source_node_id and target_node_id are required", i+1)
		}
		edges = append(edges, types.CreateTemplateEdgeParams{
			SourceNodeID: e.SourceNodeID,
			SourcePort:   e.SourcePort,
			TargetNodeID: e.TargetNodeID,
			TargetPort:   e.TargetPort,
			Label:        e.Label,
		})
	}
	return nodes, edges, nil
}
