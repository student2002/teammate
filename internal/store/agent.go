// agent.go 提供 AI 代理（Agent）的数据访问操作。
//
// 包含 Agent 的完整生命周期管理：创建、查询、更新、删除，
// 以及 API Token 生成、轮换和撤销，技能和 MCP 服务器关联管理。
//
// API Token 格式：tm_{agent_id_short}_{40_random_hex}
// Token 存储采用双重哈希：bcrypt 用于安全存储，SHA-256 用于高效查找。
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

// CreateAgent 创建新的 Agent 并生成 API Token。
//
// 执行步骤：
//  1. 插入 Agent 记录到 agents 表
//  2. 生成格式为 tm_{agent_id_short}_{40_hex_chars} 的 API Token
//  3. 使用 bcrypt 哈希 Token 并存储到 auth_tokens 表
//  4. 计算 SHA-256 查找哈希用于高效数据库查询
//  5. Token 有效期 365 天
//
// 参数：
//   - ctx: 请求上下文，支持超时和取消
//   - params: Agent 创建参数，包含名称、提供商、指令等
//
// 返回：
//   - db.Agent: 创建的 Agent 记录
//   - string: 生成的 API Token 明文（仅在创建时返回一次）
//   - error: 创建失败时返回错误
func (s *Store) CreateAgent(ctx context.Context, params types.CreateAgentParams) (types.Agent, string, error) {
	dbParams, err := FromDomainCreateAgentParams(params)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("convert create agent params: %w", err)
	}
	agent, err := s.q.CreateAgent(ctx, dbParams)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("create agent: %w", err)
	}

	// 生成 API Token：tm_{agent_id_short}_{40_hex_chars}
	apiToken, err := GenerateAgentToken(agent.ID)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("generate agent token: %w", err)
	}

	// 在 auth_tokens 中存储 bcrypt 哈希（安全、加盐、慢哈希）
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(apiToken), bcrypt.DefaultCost)
	if err != nil {
		return types.Agent{}, "", fmt.Errorf("hash agent token: %w", err)
	}

	// 计算 SHA-256 查找哈希以实现高效的数据库查询
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

// GetAgent 根据 ID 查询单个 Agent 记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: Agent 的 UUID 标识符
//
// 返回：
//   - db.Agent: Agent 记录
//   - error: 查询失败时返回错误
func (s *Store) GetAgent(ctx context.Context, id uuid.UUID) (types.Agent, error) {
	agent, err := s.q.GetAgent(ctx, id)
	if err != nil {
		return types.Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return ToDomainAgent(agent)
}

// ListAgents 查询指定工作区内所有 Agent 记录。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []db.Agent: Agent 列表
//   - error: 查询失败时返回错误
func (s *Store) ListAgents(ctx context.Context, workspaceID uuid.UUID) ([]types.Agent, error) {
	agents, err := s.q.ListAgents(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	return ToDomainAgentSlice(agents)
}

// UpdateAgent 更新 Agent 的基本信息（名称、指令、模型等）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含 Agent ID 和要更新的字段
//
// 返回：
//   - db.Agent: 更新后的 Agent 记录
//   - error: 更新失败时返回错误
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

// UpdateAgentStatus 更新 Agent 的运行状态（online/offline/busy/paused）。
//
// 状态流转必须符合 ValidStatusTransitions 定义的合法转换。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 状态更新参数，包含 Agent ID 和目标状态
//
// 返回：
//   - db.Agent: 更新后的 Agent 记录
//   - error: 更新失败时返回错误
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

// DeleteAgent 删除指定 Agent 及其关联数据（事务操作）。
//
// 执行步骤：
//  1. 清理 auth_tokens 表中的多态引用（owner_type='agent'）
//  2. 删除 Agent 记录，触发 ON DELETE CASCADE 自动清理：
//     - CASCADE 删除：runtimes、memories、project_members、agent_skills 等
//     - SET NULL 保留：execution_sessions、task_nodes 中的引用
//
// 参数：
//   - ctx: 请求上下文
//   - id: 要删除的 Agent UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 清理 auth_tokens（owner_type/owner_id 多态，无外键约束）
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE owner_type = 'agent' AND owner_id = $1`, id); err != nil {
		return fmt.Errorf("cleanup auth_tokens: %w", err)
	}

	// 002_remove_fks 后以下列不再有外键，须显式置空（FK 策略：应用层保证完整性）：
	//   workflow_template_nodes.assignee_id、task_nodes.assignee_id/reserved_for_agent_id/completed_by、
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

	// 删除 Agent 后剩余 CASCADE（保留级联清理）：runtimes、memories、project_members、
	//   project_reviewers、agent_skills、agent_mcp_servers、agent_permissions、token_usage、
	//   node_transitions（通过 task_nodes→tasks 级联删除）
	if _, err := tx.ExecContext(ctx, `DELETE FROM agents WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}

	return tx.Commit()
}

// AddAgentSkill 为 Agent 关联一个技能（插入 agent_skills 记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 技能关联参数，包含 Agent ID 和 Skill ID
//
// 返回：
//   - db.AgentSkill: 创建的关联记录
//   - error: 创建失败时返回错误
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

// ListAgentSkills 查询指定 Agent 关联的所有技能列表。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - []db.ListAgentSkillsRow: 技能列表（包含技能详情）
//   - error: 查询失败时返回错误
func (s *Store) ListAgentSkills(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentSkillsRow, error) {
	skills, err := s.q.ListAgentSkills(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent skills: %w", err)
	}
	return ToDomainListAgentSkillsRowSlice(skills)
}

// RemoveAgentSkill 移除 Agent 与技能的关联关系。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 移除参数，包含 Agent ID 和 Skill ID
//
// 返回：
//   - error: 移除失败时返回错误
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

// AddAgentMcpServer 为 Agent 关联一个 MCP 服务器（插入 agent_mcp_servers 记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: MCP 服务器关联参数，包含 Agent ID 和 McpServer ID
//
// 返回：
//   - db.AgentMcpServer: 创建的关联记录
//   - error: 创建失败时返回错误
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

// ListAgentMcpServers 列出 Agent 已关联的 MCP 服务器。
func (s *Store) ListAgentMcpServers(ctx context.Context, agentID uuid.UUID) ([]types.ListAgentMcpServersRow, error) {
	servers, err := s.q.ListAgentMcpServers(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("list agent mcp servers: %w", err)
	}
	return ToDomainListAgentMcpServersRowSlice(servers)
}

// RemoveAgentMcpServer 移除 Agent 与 MCP 服务器的关联关系。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 移除参数，包含 Agent ID 和 McpServer ID
//
// 返回：
//   - error: 移除失败时返回错误
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

// UpdateMcpServer 更新 MCP 服务器的全部字段。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含 ID、名称、URL、类型、认证方式、环境变量、状态
//
// 返回：
//   - db.McpServer: 更新后的 MCP 服务器记录
//   - error: 更新失败时返回错误
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

// UpdateSkill 更新技能的全部字段。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含 ID、名称、描述、分类、提示模板
//
// 返回：
//   - types.Skill: 更新后的技能记录
//   - error: 更新失败时返回错误
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

// GenerateAgentToken 生成格式为 tm_{agent_id_short}_{40_random_hex} 的 API Token。
//
// Token 结构：
//   - tm_: 固定前缀，标识 Teammate Token
//   - agent_id_short: Agent ID 去除连字符后的前 8 位
//   - 40_random_hex: 20 字节随机数的十六进制表示
//
// 参数：
//   - agentID: Agent 的 UUID
//
// 返回：
//   - string: 生成的 API Token 明文
//   - error: 随机数生成失败时返回错误
func GenerateAgentToken(agentID uuid.UUID) (string, error) {
	idShort := strings.ReplaceAll(agentID.String(), "-", "")[:8]
	randomBytes := make([]byte, 20)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("tm_%s_%s", idShort, hex.EncodeToString(randomBytes)), nil
}

// RevokeAgentTokens 撤销指定 Agent 的所有 API Token（删除 auth_tokens 表中的相关记录）。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - error: 撤销失败时返回错误
func (s *Store) RevokeAgentTokens(ctx context.Context, agentID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE owner_type = 'agent' AND owner_id = $1`, agentID)
	if err != nil {
		return fmt.Errorf("revoke agent tokens: %w", err)
	}
	return nil
}

// RotateAgentToken 吊销所有旧 Token 并生成新 Token。
//
// 执行步骤：
//  1. 调用 RevokeAgentTokens 删除所有现有 Token
//  2. 生成新的 API Token
//  3. 使用 bcrypt 哈希新 Token
//  4. 计算 SHA-256 查找哈希
//  5. 将新 Token 哈希存储到 auth_tokens 表，有效期 365 天
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - string: 新生成的 API Token 明文
//   - error: 轮换失败时返回错误
func (s *Store) RotateAgentToken(ctx context.Context, agentID uuid.UUID) (string, error) {
	// 撤销所有现有 Token
	if err := s.RevokeAgentTokens(ctx, agentID); err != nil {
		return "", err
	}

	// 生成新的 API Token
	apiToken, err := GenerateAgentToken(agentID)
	if err != nil {
		return "", fmt.Errorf("generate agent token: %w", err)
	}

	// 存储新 Token 的哈希（bcrypt）
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(apiToken), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash agent token: %w", err)
	}

	// 计算 SHA-256 查找哈希以实现高效的数据库查询
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

// ValidStatusTransitions 定义允许的 Agent 状态流转映射表。
//
// 状态机：
//   - offline → online
//   - online → busy, offline, paused
//   - busy → online, paused
//   - paused → online, offline
var ValidStatusTransitions = map[string][]string{
	types.AgentStatusOffline: {types.AgentStatusOnline},
	types.AgentStatusOnline:  {types.AgentStatusBusy, types.AgentStatusOffline, types.AgentStatusPaused},
	types.AgentStatusBusy:    {types.AgentStatusOnline, types.AgentStatusPaused},
	types.AgentStatusPaused:  {types.AgentStatusOnline, types.AgentStatusOffline},
}

// ValidateAgentStatusTransition 校验 Agent 状态流转是否合法。
//
// 参数：
//   - from: 当前状态
//   - to: 目标状态
//
// 返回：
//   - bool: 流转是否合法
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

// nullRawMessage 辅助函数，将原始 JSON 字节转换为 NullRawMessage 类型。
//
// 参数：
//   - data: JSON 字节数组
//
// 返回：
//   - pqtype.NullRawMessage: 可空的 JSON 消息
func nullRawMessage(data []byte) pqtype.NullRawMessage {
	if data == nil {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: data, Valid: true}
}

// nullString 辅助函数，将字符串转换为 NullString 类型。
//
// 空字符串转换为 Valid=false 的 NullString。
//
// 参数：
//   - s: 输入字符串
//
// 返回：
//   - sql.NullString: 可空字符串
func nullString(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// nullUUID 辅助函数，将 UUID 指针转换为 NullUUID 类型。
//
// nil 指针转换为 Valid=false 的 NullUUID。
//
// 参数：
//   - id: UUID 指针
//
// 返回：
//   - uuid.NullUUID: 可空 UUID
func nullUUID(id *uuid.UUID) uuid.NullUUID {
	if id == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *id, Valid: true}
}
