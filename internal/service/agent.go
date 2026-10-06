// agent.go implements the business logic for AI agent management, including creating, querying, updating, and deleting agents.
//
// This file contains:
//   - AgentService struct: the agent management service, encapsulating the full lifecycle operations of an agent
//   - Create: creates a new agent and automatically grants default permissions (task:claim/task:execute/task:comment/memory:read)
//   - Get/List/Update/Delete: CRUD operations for agents
//   - UpdateStatus: updates the agent status, including state transition validation (offline→online, etc.)
//   - AddSkill/RemoveSkill/ListSkills: agent skill binding management
//   - AddMcpServer/RemoveMcpServer: agent MCP server binding management
//   - RotateToken: revokes the old token and generates a new API token
//
// Creating an agent automatically grants default permissions and assigns the developer role to obtain execution permissions such as git:push.
// State transitions follow state-machine rules; illegal transitions are rejected.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// AgentService provides the business logic for agent management.
// It includes agent CRUD operations, skill management, MCP server binding, and token management.
type AgentService struct {
	svc *Service
}

// NewAgentService creates a new AgentService instance.
func NewAgentService(svc *Service) *AgentService {
	return &AgentService{svc: svc}
}



// Get retrieves the info of a single agent by ID.
//
// Parameters:
//   - ctx: request context
//   - id: agent ID
//
// Returns:
//   - types.Agent: agent info
//   - error: possible errors (agent not found)
func (s *AgentService) Get(ctx context.Context, id uuid.UUID) (types.Agent, error) {
	return s.svc.Store.GetAgent(ctx, id)
}

// List lists all agents under the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.Agent: agent list
//   - error: possible errors (database query failure)
func (s *AgentService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Agent, error) {
	return s.svc.Store.ListAgentsWithStats(ctx, workspaceID)
}

// ListExecutions returns the agent's recent execution sessions with task context.
func (s *AgentService) ListExecutions(ctx context.Context, agentID uuid.UUID, limit int32) ([]types.AgentExecution, error) {
	return s.svc.Store.ListAgentExecutions(ctx, agentID, limit)
}

// Update updates the agent's basic info (name, instructions, model, etc.).
//
// Parameters:
//   - ctx: request context
//   - params: parameters for updating the agent, including the ID and the fields to update
//
// Returns:
//   - types.Agent: updated agent info
//   - error: possible errors (agent not found, database update failure)
func (s *AgentService) Update(ctx context.Context, params types.UpdateAgentParams) (types.Agent, error) {
	return s.svc.Store.UpdateAgent(ctx, params)
}

// UpdateStatus updates the agent's status, including state transition validation.
// Only legal state transitions are accepted (e.g. offline → online).
//
// Parameters:
//   - ctx: request context
//   - id: agent ID
//   - newStatus: target status
//
// Returns:
//   - types.Agent: updated agent info
//   - error: possible errors (agent not found, illegal state transition)
func (s *AgentService) UpdateStatus(ctx context.Context, id uuid.UUID, newStatus string) (types.Agent, error) {
	agent, err := s.svc.Store.GetAgent(ctx, id)
	if err != nil {
		return types.Agent{}, fmt.Errorf("get agent: %w", err)
	}

	if !store.ValidateAgentStatusTransition(agent.Status, newStatus) {
		return types.Agent{}, fmt.Errorf("invalid status transition: %s -> %s", agent.Status, newStatus)
	}

	return s.svc.Store.UpdateAgentStatus(ctx, types.UpdateAgentStatusParams{
		ID:     id.String(),
		Status: newStatus,
	})
}

// Delete deletes the agent and cleans up foreign-key references (skill bindings, MCP server bindings, permissions, etc.).
//
// Parameters:
//   - ctx: request context
//   - id: agent ID
//
// Returns:
//   - error: possible errors (agent not found, database deletion failure)
func (s *AgentService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteAgent(ctx, id)
}

// AddSkill adds a skill binding to the agent.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for adding a skill, including the agent ID and skill ID
//
// Returns:
//   - types.AgentSkill: the created agent-skill binding record
//   - error: possible errors (skill not found, database write failure)
func (s *AgentService) AddSkill(ctx context.Context, params types.AddAgentSkillParams) (types.AgentSkill, error) {
	return s.svc.Store.AddAgentSkill(ctx, params)
}

// ListSkills lists all skills assigned to the agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//
// Returns:
//   - []types.ListAgentSkillsRow: agent skill list (includes skill details)
//   - error: possible errors (database query failure)
func (s *AgentService) ListSkills(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentSkillsRow, error) {
	return s.svc.Store.ListAgentSkills(ctx, agentID)
}

// RemoveSkill removes a skill binding from the agent.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for removing a skill, including the agent ID and skill ID
//
// Returns:
//   - error: possible errors (binding not found, database deletion failure)
func (s *AgentService) RemoveSkill(ctx context.Context, params types.RemoveAgentSkillParams) error {
	return s.svc.Store.RemoveAgentSkill(ctx, params)
}

// AddMcpServer binds an MCP server to the agent.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for binding an MCP server, including the agent ID and MCP server ID
//
// Returns:
//   - types.AgentMcpServer: the created agent-MCP server binding record
//   - error: possible errors (MCP server not found, database write failure)
func (s *AgentService) AddMcpServer(ctx context.Context, params types.AddAgentMcpServerParams) (types.AgentMcpServer, error) {
	return s.svc.Store.AddAgentMcpServer(ctx, params)
}

// ListMcpServers lists all MCP servers bound to the agent (returns encrypted env_vars for display).
func (s *AgentService) ListMcpServers(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentMcpServersRow, error) {
	servers, err := s.svc.Store.ListAgentMcpServers(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return servers, nil
}

// ListExecutionMcpServers lists the MCP servers bound to the agent and decrypts env_vars (for daemon endpoint use only).
func (s *AgentService) ListExecutionMcpServers(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentMcpServersRow, error) {
	servers, err := s.svc.Store.ListAgentMcpServers(ctx, agentID)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		decrypted, err := decryptMCPEnvVars(servers[i].EnvVars, servers[i].EnvVars != nil)
		if err != nil {
			return nil, fmt.Errorf("decrypt agent mcp env vars: %w", err)
		}
		servers[i].EnvVars = decrypted.RawMessage
	}
	return servers, nil
}

// RemoveMcpServer unbinds an MCP server from the agent.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for unbinding an MCP server, including the agent ID and MCP server ID
//
// Returns:
//   - error: possible errors (binding not found, database deletion failure)
func (s *AgentService) RemoveMcpServer(ctx context.Context, params types.RemoveAgentMcpServerParams) error {
	return s.svc.Store.RemoveAgentMcpServer(ctx, params)
}

// GetInProgressNodesByAgent queries the nodes claimed but not yet completed by the specified agent in the specified workspace.
// Used to resume unfinished executions after an agent restart.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.GetInProgressNodesByAgentRow: list of in_progress nodes (including project_id)
//   - error: possible errors (database query failure)
func (s *AgentService) GetInProgressNodesByAgent(ctx context.Context, agentID uuid.UUID, workspaceID uuid.UUID) ([]types.GetInProgressNodesByAgentRow, error) {
	rows, err := s.svc.Store.GetInProgressNodesByAgent(ctx, agentID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("get in-progress nodes by agent: %w", err)
	}
	return rows, nil
}


