// workflow.go implements the business logic for workflow template management.
// A workflow template is an event-driven graph: typed nodes (type_key)
// connected by port-based edges. Create and update validate the whole graph
// against the node registry before any write; update replaces the graph
// atomically and bumps the template version.
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

// ErrInvalidGraph marks a template payload that failed graph or config
// validation; handlers map it to 400 while transport failures map to 500.
var ErrInvalidGraph = errors.New("invalid workflow graph")

// WorkflowService provides the business logic for workflow template management.
type WorkflowService struct {
	svc *Service
}

func NewWorkflowService(svc *Service) *WorkflowService {
	return &WorkflowService{svc: svc}
}

// mustBuiltinWorkflowRegistry builds the process-wide node catalog. A duplicate
// type_key is a programming error and must fail process startup (§8).
func mustBuiltinWorkflowRegistry() *workflow.Registry {
	r, err := workflow.BuiltinRegistry()
	if err != nil {
		panic(fmt.Sprintf("build workflow node registry: %v", err))
	}
	return r
}

// TemplateGraphInput carries the full-graph write payload for a workflow template.
type TemplateGraphInput struct {
	Name        string
	Description *string
	Enabled     bool
	Nodes       []types.CreateTemplateNodeParams
	Edges       []types.CreateTemplateEdgeParams
}

// WorkflowTemplateDetail contains a template together with its nodes and edges.
type WorkflowTemplateDetail struct {
	Template types.WorkflowTemplate       `json:"template"`
	Nodes    []types.WorkflowTemplateNode `json:"nodes"`
	Edges    []types.WorkflowTemplateEdge `json:"edges"`
}

// Create validates the graph and creates a workflow template with its nodes
// and edges in one transaction.
func (s *WorkflowService) Create(ctx context.Context, workspaceID uuid.UUID, in TemplateGraphInput) (*WorkflowTemplateDetail, error) {
	if err := validateTemplateGraph(s.svc.WorkflowRegistry, in); err != nil {
		return nil, err
	}
	template, nodes, edges, err := s.svc.Store.CreateWorkflowTemplate(ctx, types.CreateWorkflowTemplateParams{
		WorkspaceID: workspaceID.String(),
		Name:        in.Name,
		Description: in.Description,
		IsBuiltin:   false,
		Version:     1,
		Enabled:     in.Enabled,
	}, in.Nodes, in.Edges)
	if err != nil {
		return nil, err
	}
	templateUUID, parseErr := uuid.Parse(template.ID)
	if parseErr != nil {
		return nil, fmt.Errorf("parse created template id: %w", parseErr)
	}
	if err := s.SyncScheduleFromGraph(ctx, templateUUID, nodes); err != nil {
		return nil, err
	}
	return &WorkflowTemplateDetail{Template: template, Nodes: nodes, Edges: edges}, nil
}

// Update validates the graph and atomically replaces a template's whole graph;
// the store bumps the template version inside the same transaction.
func (s *WorkflowService) Update(ctx context.Context, id uuid.UUID, in TemplateGraphInput) (*WorkflowTemplateDetail, error) {
	if err := validateTemplateGraph(s.svc.WorkflowRegistry, in); err != nil {
		return nil, err
	}
	template, nodes, edges, err := s.svc.Store.UpdateTemplateGraph(ctx, id, types.UpdateWorkflowTemplateParams{
		ID:          id.String(),
		Name:        in.Name,
		Description: in.Description,
		Enabled:     in.Enabled,
	}, in.Nodes, in.Edges)
	if err != nil {
		return nil, err
	}
	if err := s.SyncScheduleFromGraph(ctx, id, nodes); err != nil {
		return nil, err
	}
	return &WorkflowTemplateDetail{Template: template, Nodes: nodes, Edges: edges}, nil
}

// validateTemplateGraph runs the server-authoritative graph and per-node config
// checks; it is the single trust boundary before any template write.
// All failures wrap ErrInvalidGraph so handlers can map them to 400.
func validateTemplateGraph(registry *workflow.Registry, in TemplateGraphInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w: template name is required", ErrInvalidGraph)
	}
	nodeInputs := make([]workflow.TemplateNodeInput, 0, len(in.Nodes))
	nodeIDs := make(map[string]struct{}, len(in.Nodes))
	sortOrders := make(map[int32]struct{}, len(in.Nodes))
	nameSeen := make(map[string]struct{}, len(in.Nodes))
	for _, n := range in.Nodes {
		if n.ID == nil || strings.TrimSpace(*n.ID) == "" {
			return fmt.Errorf("%w: template node id is required", ErrInvalidGraph)
		}
		// Node ids are UUID primary keys; a malformed id would otherwise fail
		// deep in the store write as an opaque 500.
		if _, parseErr := uuid.Parse(*n.ID); parseErr != nil {
			return fmt.Errorf("%w: template node id %q is not a UUID", ErrInvalidGraph, *n.ID)
		}
		if _, dup := nodeIDs[*n.ID]; dup {
			return fmt.Errorf("%w: duplicate node id %s", ErrInvalidGraph, *n.ID)
		}
		if _, dup := sortOrders[n.SortOrder]; dup {
			return fmt.Errorf("%w: duplicate sort_order %d", ErrInvalidGraph, n.SortOrder)
		}
		// Node names key the run-context aliases (nodes.<名字>.output); a
		// duplicated non-empty name would make that reference ambiguous.
		name := strings.TrimSpace(n.Name)
		if name != "" {
			if _, dup := nameSeen[name]; dup {
				return fmt.Errorf("%w: duplicate node name: %s", ErrInvalidGraph, name)
			}
			nameSeen[name] = struct{}{}
		}
		nodeIDs[*n.ID] = struct{}{}
		sortOrders[n.SortOrder] = struct{}{}
		nodeInputs = append(nodeInputs, workflow.TemplateNodeInput{ID: *n.ID, TypeKey: n.TypeKey, Config: n.Config})
	}
	edgeInputs := make([]workflow.TemplateEdgeInput, 0, len(in.Edges))
	edgeIDs := make(map[string]struct{}, len(in.Edges))
	for _, e := range in.Edges {
		edgeID := ""
		if e.ID != nil && strings.TrimSpace(*e.ID) != "" {
			edgeID = *e.ID
			if _, parseErr := uuid.Parse(edgeID); parseErr != nil {
				return fmt.Errorf("%w: template edge id %q is not a UUID", ErrInvalidGraph, edgeID)
			}
			if _, dup := edgeIDs[edgeID]; dup {
				return fmt.Errorf("%w: duplicate edge id %s", ErrInvalidGraph, edgeID)
			}
			edgeIDs[edgeID] = struct{}{}
		}
		edgeInputs = append(edgeInputs, workflow.TemplateEdgeInput{
			ID:           edgeID,
			SourceNodeID: e.SourceNodeID,
			SourcePort:   e.SourcePort,
			TargetNodeID: e.TargetNodeID,
			TargetPort:   e.TargetPort,
		})
	}
	issues := workflow.ValidateTemplateGraph(registry, nodeInputs, edgeInputs)
	if len(issues) == 0 {
		return nil
	}
	messages := make([]string, 0, len(issues))
	for _, issue := range issues {
		messages = append(messages, issue.Message)
	}
	return fmt.Errorf("%w: %s", ErrInvalidGraph, strings.Join(messages, "; "))
}

// GetTemplate fetches a workflow template by ID (without nodes/edges).
func (s *WorkflowService) GetTemplate(ctx context.Context, id uuid.UUID) (types.WorkflowTemplate, error) {
	return s.svc.Store.GetWorkflowTemplate(ctx, id)
}

// Get fetches a workflow template with its nodes and edges by ID.
func (s *WorkflowService) Get(ctx context.Context, id uuid.UUID) (*WorkflowTemplateDetail, error) {
	template, err := s.svc.Store.GetWorkflowTemplate(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get workflow template: %w", err)
	}
	nodes, err := s.svc.Store.ListTemplateNodes(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}
	edges, err := s.svc.Store.ListTemplateEdges(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("list template edges: %w", err)
	}
	return &WorkflowTemplateDetail{Template: template, Nodes: nodes, Edges: edges}, nil
}

// List lists all workflow templates with their nodes and edges for the given workspace.
func (s *WorkflowService) List(ctx context.Context, workspaceID uuid.UUID) ([]WorkflowTemplateDetail, error) {
	templates, err := s.svc.Store.ListWorkflowTemplates(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workflow templates: %w", err)
	}

	result := make([]WorkflowTemplateDetail, 0, len(templates))
	for _, tpl := range templates {
		tplID, err := uuid.Parse(tpl.ID)
		if err != nil {
			return nil, fmt.Errorf("parse template id: %w", err)
		}
		nodes, err := s.svc.Store.ListTemplateNodes(ctx, tplID)
		if err != nil {
			return nil, fmt.Errorf("list template nodes: %w", err)
		}
		edges, err := s.svc.Store.ListTemplateEdges(ctx, tplID)
		if err != nil {
			return nil, fmt.Errorf("list template edges: %w", err)
		}
		result = append(result, WorkflowTemplateDetail{
			Template: tpl,
			Nodes:    nodes,
			Edges:    edges,
		})
	}
	return result, nil
}

// Delete deletes a workflow template; its nodes and edges are removed by the
// database cascade and its schedule row (if any) is dropped with it.
func (s *WorkflowService) Delete(ctx context.Context, id uuid.UUID) error {
	if err := s.svc.Store.DeleteWorkflowTemplate(ctx, id); err != nil {
		return err
	}
	return s.svc.Store.DeleteTemplateSchedule(ctx, id)
}
