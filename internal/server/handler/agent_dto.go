// agent_dto.go 定义 Agent 相关的请求/响应结构体和数据转换函数。
package handler

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// createAgentRequest 创建代理请求体。
type createAgentRequest struct {
	Name         string          `json:"name"`         // 代理名称
	Provider     string          `json:"provider"`     // 代理提供者（claude/openai 等）
	Instructions string          `json:"instructions"` // 代理执行指令
	Model        string          `json:"model"`        // 使用的模型
	Status       string          `json:"status"`       // 初始状态
	CustomEnv    json.RawMessage `json:"custom_env"`   // 自定义环境变量（JSON）
	ExtraArgs    []string        `json:"extra_args"`   // 额外命令行参数
	GitName      string          `json:"git_name"`     // Git 提交用户名
	GitEmail     string          `json:"git_email"`    // Git 提交邮箱
}

// agentResponse 代理响应 DTO，将 sql.NullXxx 字段转换为普通类型用于 JSON 序列化。
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

// agentToResponse 将代理数据库记录转换为 API 响应，不包含 custom_env。
// 仅用于只读调用者（viewer/agent），防止密钥泄露。
func agentToResponse(a types.Agent) agentResponse {
	return agentResponse{
		ID:           uuid.MustParse(a.ID),
		WorkspaceID:  uuid.MustParse(a.WorkspaceID),
		Name:         a.Name,
		Provider:     a.Provider,
		Instructions: a.Instructions,
		Model:        a.Model,
		Status:       a.Status,
		CustomEnv:    nil, // 默认为 nil；由调用者根据权限设置
		ExtraArgs:    a.ExtraArgs,
		GitName:      a.GitName,
		GitEmail:     a.GitEmail,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
	}
}

// agentToResponseWithEnv 将代理数据库记录转换为 API 响应，包含 custom_env。
// 仅用于写权限调用者（owner/admin/member）。
func agentToResponseWithEnv(a types.Agent) agentResponse {
	var customEnv json.RawMessage
	if len(a.CustomEnv) > 0 && string(a.CustomEnv) != "null" {
		customEnv = a.CustomEnv
	}
	resp := agentToResponse(a)
	resp.CustomEnv = customEnv
	return resp
}

// createAgentResponse 创建代理响应体，包含新生成的 API Token。
type createAgentResponse struct {
	agentResponse
	APIToken string `json:"api_token,omitempty"` // 新代理的 API Token（仅创建时返回）
}

// updateAgentRequest 更新代理请求体。
type updateAgentRequest struct {
	Instructions string          `json:"instructions"` // 代理执行指令
	Model        string          `json:"model"`        // 使用模型
	Status       string          `json:"status"`       // 代理状态
	CustomEnv    json.RawMessage `json:"custom_env"`   // 自定义环境变量
	ExtraArgs    []string        `json:"extra_args"`   // 额外命令行参数
	GitName      string          `json:"git_name"`     // Git 提交用户名
	GitEmail     string          `json:"git_email"`    // Git 提交邮箱
}

// addSkillRequest 添加技能请求体。
type addSkillRequest struct {
	SkillID uuid.UUID `json:"skill_id"` // 技能 ID
	Enabled bool      `json:"enabled"`  // 是否启用
}

// addMcpServerRequest 添加 MCP 服务器请求体。
type addMcpServerRequest struct {
	McpServerID uuid.UUID `json:"mcp_server_id"` // MCP 服务器 ID
	Enabled     bool      `json:"enabled"`       // 是否启用
}

// maskAgentMcpEnvVarsForDisplay 将 MCP 服务器的 env_vars 脱敏显示。
func maskAgentMcpEnvVarsForDisplay(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	// 处理加密格式 {format:"teammate-mcp-env-v1", values:{KEY:"cipher"}}
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
	// 处理明文格式 {KEY:"value"}
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

// nullToString 辅助函数，将 *string 转换为 string，nil 转换为空字符串。
func nullToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// --- 参数构建函数 ---
// 这些函数让 handler 文件能够构造领域参数结构体，
// 而无需直接导入 db/generated 包。

const (
	AgentProviderClaude = types.AgentProviderClaude
	AgentStatusOffline  = types.AgentStatusOffline
)

// buildCreateAgentParams 根据 handler 层输入构造 types.CreateAgentParams。
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

// nullStringPtr 辅助函数，将字符串转换为 *string，空字符串返回 nil。
func nullStringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// buildUpdateAgentParams 根据 handler 层输入构造 types.UpdateAgentParams。
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

// buildAddAgentSkillParams 构造 types.AddAgentSkillParams。
func buildAddAgentSkillParams(agentID uuid.UUID, skillID uuid.UUID, enabled bool) types.AddAgentSkillParams {
	return types.AddAgentSkillParams{
		AgentID: agentID.String(),
		SkillID: skillID.String(),
		Enabled: enabled,
	}
}

// buildRemoveAgentSkillParams 构造 types.RemoveAgentSkillParams。
func buildRemoveAgentSkillParams(agentID uuid.UUID, skillID uuid.UUID) types.RemoveAgentSkillParams {
	return types.RemoveAgentSkillParams{
		AgentID: agentID.String(),
		SkillID: skillID.String(),
	}
}

// buildAddAgentMcpServerParams 构造 types.AddAgentMcpServerParams。
func buildAddAgentMcpServerParams(agentID uuid.UUID, mcpServerID uuid.UUID, enabled bool) types.AddAgentMcpServerParams {
	return types.AddAgentMcpServerParams{
		AgentID:     agentID.String(),
		McpServerID: mcpServerID.String(),
		Enabled:     enabled,
	}
}

// buildRemoveAgentMcpServerParams 构造 types.RemoveAgentMcpServerParams。
func buildRemoveAgentMcpServerParams(agentID uuid.UUID, mcpServerID uuid.UUID) types.RemoveAgentMcpServerParams {
	return types.RemoveAgentMcpServerParams{
		AgentID:     agentID.String(),
		McpServerID: mcpServerID.String(),
	}
}
