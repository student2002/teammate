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
//   - BuildCreateAgentParams: builds the database parameters required to create an agent
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

// CreateAgentResult holds the result of a create-agent operation.
type CreateAgentResult struct {
	Agent    types.Agent // created agent info
	APIToken string      // generated API token, used for Agentd daemon authentication
}

// Create creates a new AI agent, generates an API token, and grants default permissions.
//
// Steps:
//  1. Call the Store to create the agent record and generate an API token
//  2. Grant default permissions to the new agent (task:claim/task:execute/task:comment/memory:read)
//  3. A default-permission grant failure does not abort the creation flow (non-fatal error)
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating the agent, including workspace ID, name, provider, instructions, model, etc.
//   - grantedBy: the operator ID that grants the permissions
//
// Returns:
//   - *CreateAgentResult: contains the agent info and API token
//   - error: possible errors (database write failure)
func (s *AgentService) Create(ctx context.Context, params types.CreateAgentParams, grantedBy uuid.UUID) (*CreateAgentResult, error) {
	agent, apiToken, err := s.svc.Store.CreateAgent(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create agent: %w", err)
	}

	agentUUID, err := uuid.Parse(agent.ID)
	if err != nil {
		return nil, fmt.Errorf("parse agent id: %w", err)
	}

	if err := s.svc.Store.GrantDefaultPermissions(ctx, agentUUID, grantedBy); err != nil {
		_ = err
	}

	// Automatically assign the developer role so the agent has permissions required to execute code tasks such as git:push
	permSvc := NewAgentPermissionService(s.svc)
	if err := permSvc.GrantRolePermissions(ctx, agentUUID, "developer", grantedBy); err != nil {
		_ = err
	}

	return &CreateAgentResult{Agent: agent, APIToken: apiToken}, nil
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
	return s.svc.Store.ListAgents(ctx, workspaceID)
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

// RotateToken revokes all existing tokens of the agent and generates a new API token.
// The old token is invalidated immediately; the new token is used for the Agentd daemon to re-authenticate.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//
// Returns:
//   - string: the newly generated API token
//   - error: possible errors (agent not found, token generation failure)
func (s *AgentService) RotateToken(ctx context.Context, agentID uuid.UUID) (string, error) {
	return s.svc.Store.RotateAgentToken(ctx, agentID)
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

// BuildCreateAgentParams builds the database parameters required to create an agent from the request parameters.
// It handles default-value setting for optional fields.
//
// Parameters:
//   - workspaceID: workspace ID
//   - name: agent name
//   - provider: agent provider (claude_code/openclaw/opencode, etc.)
//   - instructions: agent instructions (system prompt)
//   - model: model name (optional)
//   - status: initial status
//   - customEnv: custom environment variables (JSON format, optional)
//   - extraArgs: extra command-line arguments
//
// Returns:
//   - types.CreateAgentParams: the built creation parameters
func BuildCreateAgentParams(workspaceID uuid.UUID, name string, provider string, instructions, model string, status string, customEnv []byte, extraArgs []string) types.CreateAgentParams {
	return types.CreateAgentParams{
		WorkspaceID:  workspaceID.String(),
		Name:         name,
		Provider:     provider,
		Instructions: instructions,
		Model:        &model,
		Status:       status,
		CustomEnv:    customEnv,
		ExtraArgs:    extraArgs,
	}
}
