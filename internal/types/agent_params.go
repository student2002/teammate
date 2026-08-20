// agent_params.go 定义 Agent/Skill/Mcp 领域操作的领域参数结构体。
//
// 这些结构体是 sqlc 生成的 db.XxxParams 的 domain 对应物，
// 字段一一对应，类型按 domain 风格映射：
//   - uuid.UUID → string
//   - uuid.NullUUID → *string
//   - sql.NullString → *string
//   - pqtype.NullRawMessage → json.RawMessage
package types

import (
	"encoding/json"
)

// AddAgentMcpServerParams 是关联 Agent 与 MCP 服务器的领域参数结构体。
type AddAgentMcpServerParams struct {
	AgentID     string `json:"agent_id"`
	McpServerID string `json:"mcp_server_id"`
	Enabled     bool   `json:"enabled"`
}

// AddAgentSkillParams 是关联 Agent 与技能的领域参数结构体。
type AddAgentSkillParams struct {
	AgentID string `json:"agent_id"`
	SkillID string `json:"skill_id"`
	Enabled bool   `json:"enabled"`
}

// CreateAgentParams 是创建 Agent 的领域参数结构体。
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

// CreateMcpServerParams 是创建 MCP 服务器的领域参数结构体。
type CreateMcpServerParams struct {
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	URL         string          `json:"url"`
	Type        *string         `json:"type"`
	AuthType    string          `json:"auth_type"`
	EnvVars     json.RawMessage `json:"env_vars"`
}

// CreateSkillParams 是创建技能的领域参数结构体。
type CreateSkillParams struct {
	WorkspaceID   string  `json:"workspace_id"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	Category      *string `json:"category"`
	PromptTemplate *string `json:"prompt_template"`
}

// RemoveAgentMcpServerParams 是解除 Agent 与 MCP 服务器关联的领域参数结构体。
type RemoveAgentMcpServerParams struct {
	AgentID     string `json:"agent_id"`
	McpServerID string `json:"mcp_server_id"`
}

// RemoveAgentSkillParams 是解除 Agent 与技能关联的领域参数结构体。
type RemoveAgentSkillParams struct {
	AgentID string `json:"agent_id"`
	SkillID string `json:"skill_id"`
}

// UpdateAgentParams 是更新 Agent 的领域参数结构体。
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

// UpdateAgentStatusParams 是更新 Agent 状态的领域参数结构体。
type UpdateAgentStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// UpdateMcpServerParams 是更新 MCP 服务器的领域参数结构体。
type UpdateMcpServerParams struct {
	ID       string          `json:"id"`
	Name     string          `json:"name"`
	URL      string          `json:"url"`
	Type     *string         `json:"type"`
	AuthType string          `json:"auth_type"`
	EnvVars  json.RawMessage `json:"env_vars"`
}

// UpdateMcpServerStatusParams 是更新 MCP 服务器状态的领域参数结构体。
type UpdateMcpServerStatusParams struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// UpdateSkillParams 是更新技能的领域参数结构体。
type UpdateSkillParams struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	Category      *string `json:"category"`
	PromptTemplate *string `json:"prompt_template"`
}

// CreateAgentPermissionParams 是授予 Agent 权限的领域参数结构体。
type CreateAgentPermissionParams struct {
	AgentID      string  `json:"agent_id"`
	Permission   string  `json:"permission"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
	GrantedBy    *string `json:"granted_by"`
}

// HasAgentPermissionParams 是校验 Agent 是否持有指定权限的领域参数结构体。
type HasAgentPermissionParams struct {
	AgentID      string  `json:"agent_id"`
	Permission   string  `json:"permission"`
	ResourceType string  `json:"resource_type"`
	ResourceID   *string `json:"resource_id"`
}

// HasAgentPermissionAnyParams 是校验 Agent 是否持有任一指定权限的领域参数结构体。
type HasAgentPermissionAnyParams struct {
	AgentID    string `json:"agent_id"`
	Permission string `json:"permission"`
}
