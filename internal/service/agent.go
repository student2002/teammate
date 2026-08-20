// agent.go 实现 AI 代理的管理业务逻辑，包括创建、查询、更新、删除代理。
//
// 本文件包含：
//   - AgentService 结构体：代理管理服务，封装代理的完整生命周期操作
//   - Create：创建新代理并自动授予默认权限（task:claim/task:execute/task:comment/memory:read）
//   - Get/List/Update/Delete：代理的 CRUD 操作
//   - UpdateStatus：更新代理状态，包含状态转换校验（offline→online 等）
//   - AddSkill/RemoveSkill/ListSkills：代理的技能绑定管理
//   - AddMcpServer/RemoveMcpServer：代理的 MCP 服务器绑定管理
//   - RotateToken：吊销旧令牌并生成新 API 令牌
//   - BuildCreateAgentParams：构建创建代理所需的数据库参数
//
// 创建代理时自动授予默认权限，并自动分配 developer 角色以获取 git:push 等执行权限。
// 状态转换遵循状态机规则，非法转换会被拒绝。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// AgentService 提供代理管理相关的业务逻辑。
// 包含代理的 CRUD 操作、技能管理、MCP 服务器绑定和令牌管理。
type AgentService struct {
	svc *Service
}

// NewAgentService 创建一个新的 AgentService 实例。
func NewAgentService(svc *Service) *AgentService {
	return &AgentService{svc: svc}
}

// CreateAgentResult 保存创建代理操作的结果。
type CreateAgentResult struct {
	Agent    types.Agent // 创建的代理信息
	APIToken string      // 生成的 API 令牌，用于 Agentd 守护进程认证
}

// Create 创建一个新的 AI 代理，生成 API 令牌，并授予默认权限。
//
// 步骤：
//  1. 调用 Store 创建代理记录并生成 API 令牌
//  2. 为新代理授予默认权限（task:claim/task:execute/task:comment/memory:read）
//  3. 默认权限授予失败不中断创建流程（非致命错误）
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建代理的参数，包含工作区 ID、名称、提供商、指令、模型等
//   - grantedBy: 授予权限的操作者 ID
//
// 返回：
//   - *CreateAgentResult: 包含代理信息和 API 令牌
//   - error: 可能的错误（数据库写入失败）
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

	// 自动分配 developer 角色，使 Agent 拥有 git:push 等执行代码任务所需的权限
	permSvc := NewAgentPermissionService(s.svc)
	if err := permSvc.GrantRolePermissions(ctx, agentUUID, "developer", grantedBy); err != nil {
		_ = err
	}

	return &CreateAgentResult{Agent: agent, APIToken: apiToken}, nil
}

// Get 根据 ID 查询单个代理的信息。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 代理 ID
//
// 返回：
//   - types.Agent: 代理信息
//   - error: 可能的错误（代理不存在）
func (s *AgentService) Get(ctx context.Context, id uuid.UUID) (types.Agent, error) {
	return s.svc.Store.GetAgent(ctx, id)
}

// List 列出指定工作区下的所有代理。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.Agent: 代理列表
//   - error: 可能的错误（数据库查询失败）
func (s *AgentService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Agent, error) {
	return s.svc.Store.ListAgents(ctx, workspaceID)
}

// Update 更新代理的基本信息（名称、指令、模型等）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新代理的参数，包含 ID 和要更新的字段
//
// 返回：
//   - types.Agent: 更新后的代理信息
//   - error: 可能的错误（代理不存在、数据库更新失败）
func (s *AgentService) Update(ctx context.Context, params types.UpdateAgentParams) (types.Agent, error) {
	return s.svc.Store.UpdateAgent(ctx, params)
}

// UpdateStatus 更新代理的状态，包含状态转换校验。
// 只有合法的状态转换才会被接受（如 offline → online）。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 代理 ID
//   - newStatus: 目标状态
//
// 返回：
//   - types.Agent: 更新后的代理信息
//   - error: 可能的错误（代理不存在、状态转换非法）
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

// Delete 删除代理并清理外键引用（技能绑定、MCP 服务器绑定、权限等）。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 代理 ID
//
// 返回：
//   - error: 可能的错误（代理不存在、数据库删除失败）
func (s *AgentService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteAgent(ctx, id)
}

// AddSkill 为代理添加一个技能绑定。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 添加技能的参数，包含代理 ID 和技能 ID
//
// 返回：
//   - types.AgentSkill: 创建的代理-技能绑定记录
//   - error: 可能的错误（技能不存在、数据库写入失败）
func (s *AgentService) AddSkill(ctx context.Context, params types.AddAgentSkillParams) (types.AgentSkill, error) {
	return s.svc.Store.AddAgentSkill(ctx, params)
}

// ListSkills 列出代理已分配的所有技能。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//
// 返回：
//   - []types.ListAgentSkillsRow: 代理技能列表（包含技能详情）
//   - error: 可能的错误（数据库查询失败）
func (s *AgentService) ListSkills(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentSkillsRow, error) {
	return s.svc.Store.ListAgentSkills(ctx, agentID)
}

// RemoveSkill 从代理移除一个技能绑定。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 移除技能的参数，包含代理 ID 和技能 ID
//
// 返回：
//   - error: 可能的错误（绑定不存在、数据库删除失败）
func (s *AgentService) RemoveSkill(ctx context.Context, params types.RemoveAgentSkillParams) error {
	return s.svc.Store.RemoveAgentSkill(ctx, params)
}

// AddMcpServer 为代理绑定一个 MCP 服务器。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 绑定 MCP 服务器的参数，包含代理 ID 和 MCP 服务器 ID
//
// 返回：
//   - types.AgentMcpServer: 创建的代理-MCP 服务器绑定记录
//   - error: 可能的错误（MCP 服务器不存在、数据库写入失败）
func (s *AgentService) AddMcpServer(ctx context.Context, params types.AddAgentMcpServerParams) (types.AgentMcpServer, error) {
	return s.svc.Store.AddAgentMcpServer(ctx, params)
}

// ListMcpServers 列出代理已绑定的所有 MCP 服务器（返回加密的 env_vars，供展示用）。
func (s *AgentService) ListMcpServers(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentMcpServersRow, error) {
	servers, err := s.svc.Store.ListAgentMcpServers(ctx, agentID)
	if err != nil {
		return nil, err
	}
	return servers, nil
}

// ListExecutionMcpServers 列出代理绑定的 MCP 服务器并解密 env_vars（仅限 daemon endpoint 使用）。
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

// RemoveMcpServer 从代理解绑一个 MCP 服务器。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 解绑 MCP 服务器的参数，包含代理 ID 和 MCP 服务器 ID
//
// 返回：
//   - error: 可能的错误（绑定不存在、数据库删除失败）
func (s *AgentService) RemoveMcpServer(ctx context.Context, params types.RemoveAgentMcpServerParams) error {
	return s.svc.Store.RemoveAgentMcpServer(ctx, params)
}

// RotateToken 吊销代理所有现有令牌并生成新的 API 令牌。
// 旧令牌立即失效，新令牌用于 Agentd 守护进程重新认证。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//
// 返回：
//   - string: 新生成的 API 令牌
//   - error: 可能的错误（代理不存在、令牌生成失败）
func (s *AgentService) RotateToken(ctx context.Context, agentID uuid.UUID) (string, error) {
	return s.svc.Store.RotateAgentToken(ctx, agentID)
}

// GetInProgressNodesByAgent 查询指定代理在指定工作区中认领但未完成的节点。
// 用于代理重启后恢复未完成的执行。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.GetInProgressNodesByAgentRow: in_progress 节点列表（含 project_id）
//   - error: 可能的错误（数据库查询失败）
func (s *AgentService) GetInProgressNodesByAgent(ctx context.Context, agentID uuid.UUID, workspaceID uuid.UUID) ([]types.GetInProgressNodesByAgentRow, error) {
	rows, err := s.svc.Store.GetInProgressNodesByAgent(ctx, agentID, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("get in-progress nodes by agent: %w", err)
	}
	return rows, nil
}

// BuildCreateAgentParams 根据请求参数构建创建代理所需的数据库参数。
// 处理可选字段的默认值设置。
//
// 参数：
//   - workspaceID: 工作区 ID
//   - name: 代理名称
//   - provider: 代理提供商（claude_code/openclaw/opencode 等）
//   - instructions: 代理指令（系统提示词）
//   - model: 模型名称（可选）
//   - status: 初始状态
//   - customEnv: 自定义环境变量（JSON 格式，可选）
//   - extraArgs: 额外的命令行参数
//
// 返回：
//   - types.CreateAgentParams: 构建好的创建参数
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
