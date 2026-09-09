// agent.go provides data access operations for AI agents (Agent).
//
// It covers the full Agent lifecycle management: create, query, update, delete,
// as well as API Token generation, rotation, and revocation, plus skill and MCP server association management.
//
// API Token format: tm_{agent_id_short}_{40_random_hex}
// Token storage uses dual hashing: bcrypt for secure storage, SHA-256 for efficient lookup.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"
	"golang.org/x/crypto/bcrypt"

	"github.com/teammate/server/internal/types"
)

// CreateAgent creates a new Agent and generates an API Token.
//
// Steps:
//  1. Insert the Agent record into the agents table
//  2. Generate an API Token with the format tm_{agent_id_short}_{40_hex_chars}
//  3. Hash the Token with bcrypt and store it in the auth_tokens table
//  4. Compute a SHA-256 lookup hash for efficient database queries
//  5. The Token is valid for 365 days
//
// Parameters:
//   - ctx: request context, supports timeout and cancellation
//   - params: Agent creation parameters, including name, provider, instructions, etc.
//
// Returns:
//   - db.Agent: the created Agent record
//   - string: the generated API Token plaintext (returned only once at creation time)
//   - error: error returned when creation fails
func (s *Store) CreateAgent(ctx context.Context, params types.CreateAgentParams) (types.Agent, string, error) {
	dbParams, err := FromDomainCreateAgentParams(params)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("convert create agent params: %w", err)
	}
	agent, err := s.q.CreateAgent(ctx, dbParams)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("create agent: %w", err)
	}

	// Generate the API Token: tm_{agent_id_short}_{40_hex_chars}
	apiToken, err := GenerateAgentToken(agent.ID)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("generate agent token: %w", err)
	}

	// Store the bcrypt hash in auth_tokens (secure, salted, slow hash)
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(apiToken), bcrypt.DefaultCost)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("hash agent token: %w", err)
	}

	// Compute a SHA-256 lookup hash for efficient database queries
	shaHash := sha256.Sum256([]byte(apiToken))
	lookupHash := hex.EncodeToString(shaHash[:])

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, lookup_hash, token_type, owner_type, owner_id, expires_at)
		 VALUES ($1, $2, 'api', 'agent', $3, NOW() + INTERVAL '365 days')`,
		string(bcryptHash), lookupHash, agent.ID)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("insert auth token: %w", err)
	}

	domainAgent, err := ToDomainAgent(agent)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("convert agent: %w", err)
	}
	return domainAgent, apiToken, nil
}

// GetAgent queries a single Agent record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: the Agent's UUID identifier
//
// Returns:
//   - db.Agent: the Agent record
//   - error: error returned when the query fails
func (s *Store) GetAgent(ctx context.Context, id uuid.UUID) (types.Agent, error) {
	agent, err := s.q.GetAgent(ctx, id)
	if err != nil {
		return types.Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return ToDomainAgent(agent)
}

// ListAgents queries all Agent records within the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - []db.Agent: the Agent list
//   - error: error returned when the query fails
func (s *Store) ListAgents(ctx context.Context, workspaceID uuid.UUID) ([]types.Agent, error) {
	agents, err := s.q.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return ToDomainAgentSlice(agents)
}

// UpdateAgent updates the Agent's basic information (name, instructions, model, etc.).
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including the Agent ID and the fields to update
//
// Returns:
//   - db.Agent: the updated Agent record
//   - error: error returned when the update fails
func (s *Store) UpdateAgent(ctx context.Context, params types.UpdateAgentParams) (types.Agent, error) {
	dbParams, err := FromDomainUpdateAgentParams(params)
	if err != nil {
		return types.Agent{}, fmt.Errorf("convert update agent params: %w", err)
	}
	agent, err := s.q.UpdateAgent(ctx, dbParams)
	if err != nil {
		return types.Agent{}, fmt.Errorf("update agent: %w", err)
	}
	return ToDomainAgent(agent)
}

// UpdateAgentStatus updates the Agent's runtime status (online/offline/busy/paused).
//
// Status transitions must conform to the valid transitions defined by ValidStatusTransitions.
//
// Parameters:
//   - ctx: request context
//   - params: status update parameters, including the Agent ID and the target status
//
// Returns:
//   - db.Agent: the updated Agent record
//   - error: error returned when the update fails
func (s *Store) UpdateAgentStatus(ctx context.Context, params types.UpdateAgentStatusParams) (types.Agent, error) {
	dbParams, err := FromDomainUpdateAgentStatusParams(params)
	if err != nil {
		return types.Agent{}, fmt.Errorf("convert update agent status params: %w", err)
	}
	agent, err := s.q.UpdateAgentStatus(ctx, dbParams)
	if err != nil {
		return types.Agent{}, fmt.Errorf("update agent status: %w", err)
	}
	return ToDomainAgent(agent)
}

// DeleteAgent deletes the specified Agent and its associated data (transactional operation).
//
// Steps:
//  1. Clean up polymorphic references in the auth_tokens table (owner_type='agent')
//  2. Delete the Agent record, triggering ON DELETE CASCADE to automatically clean up:
//     - CASCADE deletes: runtimes, memories, project_members, agent_skills, etc.
//     - SET NULL preserves: references in execution_sessions and task_nodes
//
// Parameters:
//   - ctx: request context
//   - id: the UUID of the Agent to delete
//
// Returns:
//   - error: error returned when the deletion fails
func (s *Store) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Clean up auth_tokens (owner_type/owner_id are polymorphic, with no foreign key constraint)
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE owner_type = 'agent' AND owner_id = $1`, id); err != nil {
		return fmt.Errorf("cleanup auth_tokens: %w", err)
	}

	// After 002_remove_fks the following columns no longer have foreign keys and must be explicitly set to NULL (FK strategy: application layer guarantees integrity):
	//   workflow_template_nodes.assignee_id, task_nodes.assignee_id/reserved_for_agent_id/completed_by,
	//   execution_sessions.runtime_id/agent_id
	if _, err := tx.ExecContext(ctx,
		`UPDATE workflow_template_nodes SET assignee_id = NULL WHERE assignee_id = $1`, id); err != nil {
		return fmt.Errorf("clear workflow_template_nodes.assignee_id: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE task_nodes SET assignee_id = NULL, reserved_for_agent_id = NULL, completed_by = NULL
		 WHERE assignee_id = $1 OR reserved_for_agent_id = $1 OR completed_by = $1`, id); err != nil {
		return fmt.Errorf("clear task_nodes agent refs: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE execution_sessions SET runtime_id = NULL, agent_id = NULL
		 WHERE runtime_id = $1 OR agent_id = $1`, id); err != nil {
		return fmt.Errorf("clear execution_sessions refs: %w", err)
	}

	// After deleting the Agent, remaining CASCADEs (cascading cleanup is preserved): runtimes, memories, project_members,
	//   project_reviewers, agent_skills, agent_mcp_servers, agent_permissions, token_usage,
	//   node_transitions (cascaded via task_nodes -> tasks)
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	return tx.Commit()
}

// AddAgentSkill associates a skill with an Agent (inserts an agent_skills record).
//
// Parameters:
//   - ctx: request context
//   - params: skill association parameters, including the Agent ID and Skill ID
//
// Returns:
//   - db.AgentSkill: the created association record
//   - error: error returned when creation fails
func (s *Store) AddAgentSkill(ctx context.Context, params types.AddAgentSkillParams) (types.AgentSkill, error) {
	dbParams, err := FromDomainAddAgentSkillParams(params)
	if err != nil {
		return types.AgentSkill{}, fmt.Errorf("convert add agent skill params: %w", err)
	}
	skill, err := s.q.AddAgentSkill(ctx, dbParams)
	if err != nil {
		return types.AgentSkill{}, fmt.Errorf("add agent skill: %w", err)
	}
	return ToDomainAgentSkill(skill)
}

// ListAgentSkills queries all skills associated with the specified Agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//
// Returns:
//   - []db.ListAgentSkillsRow: the skill list (includes skill details)
//   - error: error returned when the query fails
func (s *Store) ListAgentSkills(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentSkillsRow, error) {
	skills, err := s.q.ListAgentSkills(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent skills: %w", err)
	}
	return ToDomainListAgentSkillsRowSlice(skills)
}

// RemoveAgentSkill removes the association between an Agent and a skill.
//
// Parameters:
//   - ctx: request context
//   - params: removal parameters, including the Agent ID and Skill ID
//
// Returns:
//   - error: error returned when removal fails
func (s *Store) RemoveAgentSkill(ctx context.Context, params types.RemoveAgentSkillParams) error {
	dbParams, err := FromDomainRemoveAgentSkillParams(params)
	if err != nil {
		return fmt.Errorf("convert remove agent skill params: %w", err)
	}
	if err := s.q.RemoveAgentSkill(ctx, dbParams); err != nil {
		return fmt.Errorf("remove agent skill: %w", err)
	}
	return nil
}

// AddAgentMcpServer associates an MCP server with an Agent (inserts an agent_mcp_servers record).
//
// Parameters:
//   - ctx: request context
//   - params: MCP server association parameters, including the Agent ID and McpServer ID
//
// Returns:
//   - db.AgentMcpServer: the created association record
//   - error: error returned when creation fails
func (s *Store) AddAgentMcpServer(ctx context.Context, params types.AddAgentMcpServerParams) (types.AgentMcpServer, error) {
	dbParams, err := FromDomainAddAgentMcpServerParams(params)
	if err != nil {
		return types.AgentMcpServer{}, fmt.Errorf("convert add agent mcp server params: %w", err)
	}
	server, err := s.q.AddAgentMcpServer(ctx, dbParams)
	if err != nil {
		return types.AgentMcpServer{}, fmt.Errorf("add agent mcp server: %w", err)
	}
	return ToDomainAgentMcpServer(server)
}

// ListAgentMcpServers lists the MCP servers associated with an Agent.
func (s *Store) ListAgentMcpServers(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentMcpServersRow, error) {
	servers, err := s.q.ListAgentMcpServers(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent mcp servers: %w", err)
	}
	return ToDomainListAgentMcpServersRowSlice(servers)
}

// RemoveAgentMcpServer removes the association between an Agent and an MCP server.
//
// Parameters:
//   - ctx: request context
//   - params: removal parameters, including the Agent ID and McpServer ID
//
// Returns:
//   - error: error returned when removal fails
func (s *Store) RemoveAgentMcpServer(ctx context.Context, params types.RemoveAgentMcpServerParams) error {
	dbParams, err := FromDomainRemoveAgentMcpServerParams(params)
	if err != nil {
		return fmt.Errorf("convert remove agent mcp server params: %w", err)
	}
	if err := s.q.RemoveAgentMcpServer(ctx, dbParams); err != nil {
		return fmt.Errorf("remove agent mcp server: %w", err)
	}
	return nil
}

// UpdateMcpServer updates all fields of an MCP server.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including ID, name, URL, type, authentication method, environment variables, status
//
// Returns:
//   - db.McpServer: the updated MCP server record
//   - error: error returned when the update fails
func (s *Store) UpdateMcpServer(ctx context.Context, params types.UpdateMcpServerParams) (types.McpServer, error) {
	dbParams, err := FromDomainUpdateMcpServerParams(params)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("convert update mcp server params: %w", err)
	}
	server, err := s.q.UpdateMcpServer(ctx, dbParams)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("update mcp server: %w", err)
	}
	return ToDomainMcpServer(server)
}

// UpdateSkill updates all fields of a skill.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including ID, name, description, category, prompt template
//
// Returns:
//   - types.Skill: the updated skill record
//   - error: error returned when the update fails
func (s *Store) UpdateSkill(ctx context.Context, params types.UpdateSkillParams) (types.Skill, error) {
	dbParams, err := FromDomainUpdateSkillParams(params)
	if err != nil {
		return types.Skill{}, fmt.Errorf("convert update skill params: %w", err)
	}
	skill, err := s.q.UpdateSkill(ctx, dbParams)
	if err != nil {
		return types.Skill{}, fmt.Errorf("update skill: %w", err)
	}
	return ToDomainSkill(skill)
}

// GenerateAgentToken generates an API Token with the format tm_{agent_id_short}_{40_random_hex}.
//
// Token structure:
//   - tm_: fixed prefix identifying a Teammate Token
//   - agent_id_short: the first 8 characters of the Agent ID after removing hyphens
//   - 40_random_hex: the hexadecimal representation of 20 random bytes
//
// Parameters:
//   - agentID: the Agent's UUID
//
// Returns:
//   - string: the generated API Token plaintext
//   - error: error returned when random number generation fails
func GenerateAgentToken(agentID uuid.UUID) (string, error) {
	idShort := strings.ReplaceAll(agentID.String(), "-", "")[:8]
	randomBytes := make([]byte, 20)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("tm_%s_%s", idShort, hex.EncodeToString(randomBytes)), nil
}

// RevokeAgentTokens revokes all API Tokens of the specified Agent (deletes the related records in the auth_tokens table).
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//
// Returns:
//   - error: error returned when revocation fails
func (s *Store) RevokeAgentTokens(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE owner_type = 'agent' AND owner_id = $1`, agentID)
	if err != nil {
		return fmt.Errorf("revoke agent tokens: %w", err)
	}
	return nil
}

// RotateAgentToken revokes all old Tokens and generates a new Token.
//
// Steps:
//  1. Call RevokeAgentTokens to delete all existing Tokens
//  2. Generate a new API Token
//  3. Hash the new Token with bcrypt
//  4. Compute a SHA-256 lookup hash
//  5. Store the new Token hash in the auth_tokens table, valid for 365 days
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//
// Returns:
//   - string: the newly generated API Token plaintext
//   - error: error returned when rotation fails
func (s *Store) RotateAgentToken(ctx context.Context, agentID uuid.UUID) (string, error) {
	// Revoke all existing Tokens
	if err := s.RevokeAgentTokens(ctx, agentID); err != nil {
		return "", err
	}

	// Generate a new API Token
	apiToken, err := GenerateAgentToken(agentID)
	if err != nil {
		return "", fmt.Errorf("generate agent token: %w", err)
	}

	// Store the new Token's hash (bcrypt)
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(apiToken), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash agent token: %w", err)
	}

	// Compute a SHA-256 lookup hash for efficient database queries
	shaHash := sha256.Sum256([]byte(apiToken))
	lookupHash := hex.EncodeToString(shaHash[:])

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, lookup_hash, token_type, owner_type, owner_id, expires_at)
		 VALUES ($1, $2, 'api', 'agent', $3, NOW() + INTERVAL '365 days')`,
		string(bcryptHash), lookupHash, agentID)
	if err != nil {
		return "", fmt.Errorf("store new agent token: %w", err)
	}

	return apiToken, nil
}

// ValidStatusTransitions defines the mapping of allowed Agent status transitions.
//
// State machine:
//   - offline -> online
//   - online -> busy, offline, paused
//   - busy -> online, paused
//   - paused -> online, offline
var ValidStatusTransitions = map[string][]string{
	types.AgentStatusOffline: {types.AgentStatusOnline},
	types.AgentStatusOnline:  {types.AgentStatusBusy, types.AgentStatusOffline, types.AgentStatusPaused},
	types.AgentStatusBusy:    {types.AgentStatusOnline, types.AgentStatusPaused},
	types.AgentStatusPaused:  {types.AgentStatusOnline, types.AgentStatusOffline},
}

// ValidateAgentStatusTransition validates whether an Agent status transition is legal.
//
// Parameters:
//   - from: the current status
//   - to: the target status
//
// Returns:
//   - bool: whether the transition is legal
func ValidateAgentStatusTransition(from, to string) bool {
	allowed, ok := ValidStatusTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

// nullRawMessage is a helper function that converts raw JSON bytes to the NullRawMessage type.
//
// Parameters:
//   - data: the JSON byte array
//
// Returns:
//   - pqtype.NullRawMessage: a nullable JSON message
func nullRawMessage(data []byte) pqtype.NullRawMessage {
	if data == nil {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: data, Valid: true}
}

// nullString is a helper function that converts a string to the NullString type.
//
// An empty string is converted to a NullString with Valid=false.
//
// Parameters:
//   - s: the input string
//
// Returns:
//   - sql.NullString: a nullable string
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullUUID is a helper function that converts a UUID pointer to the NullUUID type.
//
// A nil pointer is converted to a NullUUID with Valid=false.
//
// Parameters:
//   - id: the UUID pointer
//
// Returns:
//   - uuid.NullUUID: a nullable UUID
func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}
