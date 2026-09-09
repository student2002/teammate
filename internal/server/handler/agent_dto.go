// agent_dto.go defines request/response structs and data conversion functions related to Agents.
package handler

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// createAgentRequest create agent request body.
type createAgentRequest struct {
	Name         string          `json:"name"`         // agent name
	Provider     string          `json:"provider"`     // agent provider (claude/openai, etc.)
	Instructions string          `json:"instructions"` // agent execution instructions
	Model        string          `json:"model"`        // model to use
	Status       string          `json:"status"`       // initial status
	CustomEnv    json.RawMessage `json:"custom_env"`   // custom environment variables (JSON)
	ExtraArgs    []string        `json:"extra_args"`   // extra command-line arguments
	GitName      string          `json:"git_name"`     // Git commit username
	GitEmail     string          `json:"git_email"`    // Git commit email
}

// agentResponse agent response DTO, converts sql.NullXxx fields to plain types for JSON serialization.
type agentResponse struct {
	ID           uuid.UUID       `json:"id"`
	WorkspaceID  uuid.UUID       `json:"workspace_id"`
	Name         string          `json:"name"`
	Provider     string          `json:"provider"`
	Instructions string          `json:"instructions"`
	Model        string          `json:"model"`
	Status       string          `json:"status"`
	CustomEnv    json.RawMessage `json:"custom_env,omitempty"`
	ExtraArgs    []string        `json:"extra_args"`
	GitName      string          `json:"git_name"`
	GitEmail     string          `json:"git_email"`
	InputTokens  int64           `json:"input_tokens"`
	OutputTokens int64           `json:"output_tokens"`
	CreatedAt    interface{}     `json:"created_at"`
	UpdatedAt    interface{}     `json:"updated_at"`
}

// agentToResponse converts an agent database record to an API response, without custom_env.
// Only used by read-only callers (viewer/agent) to prevent secret leakage.
func agentToResponse(a types.Agent) agentResponse {
	return agentResponse{
		ID:           uuid.MustParse(a.ID),
		WorkspaceID:  uuid.MustParse(a.WorkspaceID),
		Name:         a.Name,
		Provider:     a.Provider,
		Instructions: a.Instructions,
		Model:        a.Model,
		Status:       a.Status,
		CustomEnv:    nil, // defaults to nil; set by the caller based on permissions
		ExtraArgs:    a.ExtraArgs,
		GitName:      a.GitName,
		GitEmail:     a.GitEmail,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}

// agentToResponseWithEnv converts an agent database record to an API response, including custom_env.
// Only used by callers with write permission (owner/admin/member).
func agentToResponseWithEnv(a types.Agent) agentResponse {
	var customEnv json.RawMessage
	if len(a.CustomEnv) > 0 && string(a.CustomEnv) != "null" {
		customEnv = a.CustomEnv
	}
	resp := agentToResponse(a)
	resp.CustomEnv = customEnv
	return resp
}

// createAgentResponse create agent response body, including the newly generated API Token.
type createAgentResponse struct {
	agentResponse
	APIToken string `json:"api_token,omitempty"` // API Token of the new agent (only returned on creation)
}

// updateAgentRequest update agent request body.
type updateAgentRequest struct {
	Instructions string          `json:"instructions"` // agent execution instructions
	Model        string          `json:"model"`        // model to use
	Status       string          `json:"status"`       // agent status
	CustomEnv    json.RawMessage `json:"custom_env"`   // custom environment variables
	ExtraArgs    []string        `json:"extra_args"`   // extra command-line arguments
	GitName      string          `json:"git_name"`     // Git commit username
	GitEmail     string          `json:"git_email"`    // Git commit email
}

// addSkillRequest add skill request body.
type addSkillRequest struct {
	SkillID uuid.UUID `json:"skill_id"` // skill ID
	Enabled bool      `json:"enabled"`  // whether to enable
}

// addMcpServerRequest add MCP server request body.
type addMcpServerRequest struct {
	McpServerID uuid.UUID `json:"mcp_server_id"` // MCP server ID
	Enabled     bool      `json:"enabled"`       // whether to enable
}

// maskAgentMcpEnvVarsForDisplay masks the MCP server's env_vars for display.
func maskAgentMcpEnvVarsForDisplay(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// handle encrypted format {format:"teammate-mcp-env-v1", values:{KEY:"cipher"}}
	var encrypted struct {
		Format string            `json:"format"`
		Values map[string]string `json:"values"`
	}
	if err := json.Unmarshal(raw, &encrypted); err == nil && encrypted.Format == "teammate-mcp-env-v1" {
		masked := make(map[string]string, len(encrypted.Values))
		for key := range encrypted.Values {
			masked[key] = "********"
		}
		data, _ := json.Marshal(masked)
		return data
	}
	// handle plaintext format {KEY:"value"}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return json.RawMessage(`{}`)
	}
	masked := make(map[string]string, len(values))
	for key := range values {
		masked[key] = "********"
	}
	data, err := json.Marshal(masked)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return data
}

// nullToString helper function, converts *string to string, nil to empty string.
func nullToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// --- parameter builder functions ---
// These functions let handler files construct domain parameter structs
// without directly importing the db/generated package.

const (
	AgentProviderClaude = types.AgentProviderClaude
	AgentStatusOffline  = types.AgentStatusOffline
)

// buildCreateAgentParams constructs types.CreateAgentParams from handler-layer input.
func buildCreateAgentParams(workspaceID uuid.UUID, name string, provider string, instructions string, model string, status string, customEnv []byte, extraArgs []string, gitName string, gitEmail string) types.CreateAgentParams {
	modelPtr := nullStringPtr(model)
	gitNamePtr := nullStringPtr(gitName)
	gitEmailPtr := nullStringPtr(gitEmail)
	return types.CreateAgentParams{
		WorkspaceID:  workspaceID.String(),
		Name:         name,
		Provider:     provider,
		Instructions: instructions,
		Model:        modelPtr,
		Status:       status,
		CustomEnv:    customEnv,
		ExtraArgs:    extraArgs,
		GitName:      gitNamePtr,
		GitEmail:     gitEmailPtr,
	}
}

// nullStringPtr helper function, converts a string to *string, empty string returns nil.
func nullStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// buildUpdateAgentParams constructs types.UpdateAgentParams from handler-layer input.
func buildUpdateAgentParams(id uuid.UUID, instructions string, model string, status string, customEnv []byte, extraArgs []string, gitName string, gitEmail string) types.UpdateAgentParams {
	modelPtr := nullStringPtr(model)
	gitNamePtr := nullStringPtr(gitName)
	gitEmailPtr := nullStringPtr(gitEmail)
	return types.UpdateAgentParams{
		ID:            id.String(),
		Name:          "",
		Provider:      "",
		Instructions:  instructions,
		Model:         modelPtr,
		Status:        status,
		CustomEnv:     customEnv,
		ExtraArgs:     extraArgs,
		GitName:       gitNamePtr,
		GitEmail:      gitEmailPtr,
	}
}

// buildAddAgentSkillParams constructs types.AddAgentSkillParams.
func buildAddAgentSkillParams(agentID uuid.UUID, skillID uuid.UUID, enabled bool) types.AddAgentSkillParams {
	return types.AddAgentSkillParams{
		AgentID: agentID.String(),
		SkillID: skillID.String(),
		Enabled: enabled,
	}
}

// buildRemoveAgentSkillParams constructs types.RemoveAgentSkillParams.
func buildRemoveAgentSkillParams(agentID uuid.UUID, skillID uuid.UUID) types.RemoveAgentSkillParams {
	return types.RemoveAgentSkillParams{
		AgentID: agentID.String(),
		SkillID: skillID.String(),
	}
}

// buildAddAgentMcpServerParams constructs types.AddAgentMcpServerParams.
func buildAddAgentMcpServerParams(agentID uuid.UUID, mcpServerID uuid.UUID, enabled bool) types.AddAgentMcpServerParams {
	return types.AddAgentMcpServerParams{
		AgentID:     agentID.String(),
		McpServerID: mcpServerID.String(),
		Enabled:     enabled,
	}
}

// buildRemoveAgentMcpServerParams constructs types.RemoveAgentMcpServerParams.
func buildRemoveAgentMcpServerParams(agentID uuid.UUID, mcpServerID uuid.UUID) types.RemoveAgentMcpServerParams {
	return types.RemoveAgentMcpServerParams{
		AgentID:     agentID.String(),
		McpServerID: mcpServerID.String(),
	}
}
