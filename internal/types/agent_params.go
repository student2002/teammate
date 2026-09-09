// agent_params.go defines the domain parameter structs for Agent/Skill/Mcp domain operations.
//
// These structs are the domain counterparts of the sqlc-generated db.XxxParams,
// with fields mapped one-to-one and types mapped in domain style:
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - pqtype.NullRawMessage → json.RawMessage
package types

import (
	"encoding/json"
)

// AddAgentMcpServerParams is the domain parameter struct for associating an Agent with an MCP server.
type AddAgentMcpServerParams struct {
	AgentID     string `json:"agent_id"`
	McpServerID string `json:"mcp_server_id"`
	Enabled     bool   `json:"enabled"`
}

// AddAgentSkillParams is the domain parameter struct for associating an Agent with a skill.
type AddAgentSkillParams struct {
	AgentID string `json:"agent_id"`
	SkillID string `json:"skill_id"`
	Enabled bool   `json:"enabled"`
}

// CreateAgentParams is the domain parameter struct for creating an Agent.
type CreateAgentParams struct {
	WorkspaceID  string          `json:"workspace_id"`
	Name         string          `json:"name"`
	Provider     string          `json:"provider"`
	Instructions string          `json:"instructions"`
	Model        *string         `json:"model"`
	Status       string          `json:"status"`
	CustomEnv    json.RawMessage `json:"custom_env"`
	ExtraArgs    []string        `json:"extra_args"`
	GitName      *string         `json:"git_name"`
	GitEmail     *string         `json:"git_email"`
}

// CreateMcpServerParams is the domain parameter struct for creating an MCP server.
type CreateMcpServerParams struct {
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	URL         string          `json:"url"`
	Type        *string         `json:"type"`
	AuthType    string          `json:"auth_type"`
	EnvVars     json.RawMessage `json:"env_vars"`
}

// CreateSkillParams is the domain parameter struct for creating a skill.
type CreateSkillParams struct {
	WorkspaceID   string  `json:"workspace_id"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	Category      *string `json:"category"`
	PromptTemplate *string `json:"prompt_template"`
}

// RemoveAgentMcpServerParams is the domain parameter struct for removing the association between an Agent and an MCP server.
type RemoveAgentMcpServerParams struct {
	AgentID     string `json:"agent_id"`
	McpServerID string `json:"mcp_server_id"`
}

// RemoveAgentSkillParams is the domain parameter struct for removing the association between an Agent and a skill.
type RemoveAgentSkillParams struct {
	AgentID string `json:"agent_id"`
	SkillID string `json:"skill_id"`
}

// UpdateAgentParams is the domain parameter struct for updating an Agent.
type UpdateAgentParams struct {
	ID            string          `json:"id"`
	Name          string          `json:"name"`
	Provider      string          `json:"provider"`
	Instructions  string          `json:"instructions"`
	Model         *string         `json:"model"`
	Status        string          `json:"status"`
	CustomEnv     json.RawMessage `json:"custom_env"`
	ExtraArgs     []string        `json:"extra_args"`
	GitName       *string         `json:"git_name"`
	GitEmail      *string         `json:"git_email"`
}

// UpdateAgentStatusParams is the domain parameter struct for updating an Agent's status.
type UpdateAgentStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// UpdateMcpServerParams is the domain parameter struct for updating an MCP server.
type UpdateMcpServerParams struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	URL      string          `json:"url"`
	Type     *string         `json:"type"`
	AuthType string          `json:"auth_type"`
	EnvVars  json.RawMessage `json:"env_vars"`
}

// UpdateMcpServerStatusParams is the domain parameter struct for updating an MCP server's status.
type UpdateMcpServerStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// UpdateSkillParams is the domain parameter struct for updating a skill.
type UpdateSkillParams struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	Category      *string `json:"category"`
	PromptTemplate *string `json:"prompt_template"`
}

// CreateAgentPermissionParams is the domain parameter struct for granting a permission to an Agent.
type CreateAgentPermissionParams struct {
	AgentID      string  `json:"agent_id"`
	Permission   string  `json:"permission"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
	GrantedBy    *string `json:"granted_by"`
}

// HasAgentPermissionParams is the domain parameter struct for verifying whether an Agent holds the specified permission.
type HasAgentPermissionParams struct {
	AgentID      string  `json:"agent_id"`
	Permission   string  `json:"permission"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
}

// HasAgentPermissionAnyParams is the domain parameter struct for verifying whether an Agent holds any of the specified permissions.
type HasAgentPermissionAnyParams struct {
	AgentID    string `json:"agent_id"`
	Permission string `json:"permission"`
}
