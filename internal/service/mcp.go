// mcp.go 提供 MCP（Model Context Protocol）服务器管理的业务逻辑。
// MCP 服务器为 Agent 提供外部工具和数据源，支持健康检查、加密存储环境变量等功能。
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	servercrypto "github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/types"
)

// McpService 提供 MCP 服务器管理相关的业务逻辑。
// MCP（Model Context Protocol）服务器为 Agent 提供外部工具和数据源。
type McpService struct {
	svc *Service
}

// NewMcpService 创建一个新的 McpService 实例。
func NewMcpService(svc *Service) *McpService {
	return &McpService{svc: svc}
}

// Create 创建一个新的 MCP 服务器。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建 MCP 服务器的参数，包含工作区 ID、名称、URL、类型、认证方式等
//
// 返回：
//   - types.McpServer: 创建的 MCP 服务器记录
//   - error: 可能的错误（数据库写入失败）
func (s *McpService) Create(ctx context.Context, params types.CreateMcpServerParams) (types.McpServer, error) {
	encrypted, err := encryptMCPEnvVars(params.EnvVars, params.EnvVars != nil)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("encrypt mcp env vars: %w", err)
	}
	params.EnvVars = encrypted.RawMessage
	server, err := s.svc.Store.CreateMcpServer(ctx, params)
	if err != nil {
		return types.McpServer{}, err
	}
	return maskMCPServerEnvVars(server), nil
}

// List 列出指定工作区的所有 MCP 服务器。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.McpServer: MCP 服务器列表
//   - error: 可能的错误（数据库查询失败）
func (s *McpService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.McpServer, error) {
	servers, err := s.svc.Store.ListMcpServers(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	for i := range servers {
		servers[i] = maskMCPServerEnvVars(servers[i])
	}
	return servers, nil
}

// Get 根据 ID 获取 MCP 服务器信息。
//
// 参数：
//   - ctx: 请求上下文
//   - id: MCP 服务器 ID
//
// 返回：
//   - types.McpServer: MCP 服务器信息
//   - error: 可能的错误（服务器不存在）
func (s *McpService) Get(ctx context.Context, id uuid.UUID) (types.McpServer, error) {
	return s.svc.Store.GetMcpServer(ctx, id)
}

// UpdateStatus 更新 MCP 服务器的连接状态（connected/disconnected）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新状态的参数，包含服务器 ID 和新状态
//
// 返回：
//   - types.McpServer: 更新后的 MCP 服务器记录
//   - error: 可能的错误（数据库更新失败）
func (s *McpService) UpdateStatus(ctx context.Context, params types.UpdateMcpServerStatusParams) (types.McpServer, error) {
	server, err := s.svc.Store.UpdateMcpServerStatus(ctx, params)
	if err != nil {
		return types.McpServer{}, err
	}
	return maskMCPServerEnvVars(server), nil
}

// Update 更新 MCP 服务器的配置信息。
//
// 所有指针参数支持三态语义：nil=保持现有值，非 nil=替换。
// envVars 额外支持 {} = 清空。
//
// 参数：
//   - ctx: 请求上下文
//   - id: MCP 服务器 ID
//   - name: 新的服务器名称（nil=保持）
//   - url: 新的服务器 URL（nil=保持）
//   - mcpType: 新的服务器类型（nil=保持）
//   - authType: 新的认证方式（nil=保持）
//   - envVars: 环境变量（nil=保持，{} = 清空，有值=替换）
//   - status: 新的连接状态（nil=保持）
//
// 返回：
//   - types.McpServer: 更新后的 MCP 服务器记录
//   - error: 可能的错误（服务器不存在、数据库更新失败）
func (s *McpService) Update(ctx context.Context, id uuid.UUID, name, url, mcpType *string, authType *string, envVars []byte, status *string) (types.McpServer, error) {
	// 获取当前值，合并非 nil 的更新字段
	current, err := s.svc.Store.GetMcpServer(ctx, id)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("get current mcp server: %w", err)
	}

	newName := current.Name
	if name != nil {
		newName = *name
	}
	newURL := current.URL
	if url != nil {
		newURL = *url
	}
	newType := current.Type
	if mcpType != nil {
		newType = *mcpType
	}
	newAuthType := current.AuthType
	if authType != nil {
		newAuthType = *authType
	}
	newStatus := current.Status
	if status != nil {
		newStatus = *status
	}

	// envVars 三态语义
	var encryptedEnvVars pqtype.NullRawMessage
	if envVars == nil {
		encryptedEnvVars = rawToNullRaw(current.EnvVars) // 保持现有值
	} else {
		encryptedEnvVars, err = encryptMCPEnvVars(envVars, true)
		if err != nil {
			return types.McpServer{}, fmt.Errorf("encrypt mcp env vars: %w", err)
		}
	}

	server, err := s.svc.Store.UpdateMcpServer(ctx, types.UpdateMcpServerParams{
		ID:       id.String(),
		Name:     newName,
		URL:      newURL,
		Type:     &newType,
		AuthType: newAuthType,
		EnvVars:  encryptedEnvVars.RawMessage,
	})
	if err != nil {
		return types.McpServer{}, fmt.Errorf("update mcp server: %w", err)
	}
	_ = newStatus // status 通过 UpdateStatus 单独更新
	return maskMCPServerEnvVars(server), nil
}

// Delete 删除一个 MCP 服务器。
//
// 参数：
//   - ctx: 请求上下文
//   - id: MCP 服务器 ID
//
// 返回：
//   - error: 可能的错误（服务器不存在、数据库删除失败）
func (s *McpService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteMcpServer(ctx, id)
}

// HealthCheck 对 MCP 服务器执行健康检查，通过 TCP 连接测试服务器可达性。
// 连接超时时间为 5 秒。
//
// 步骤：
//  1. 获取 MCP 服务器信息
//  2. 从 URL 中提取 host:port
//  3. 尝试 TCP 连接到目标地址（5 秒超时）
//  4. 根据连接结果更新服务器状态
//
// 参数：
//   - ctx: 请求上下文
//   - id: MCP 服务器 ID
//
// 返回：
//   - types.McpServer: 更新状态后的 MCP 服务器记录
//   - error: 可能的错误（服务器不存在、数据库更新失败）
func (s *McpService) HealthCheck(ctx context.Context, id uuid.UUID) (types.McpServer, error) {
	server, err := s.Get(ctx, id)
	if err != nil {
		return types.McpServer{}, err
	}

	newStatus := "disconnected"
	conn, err := net.DialTimeout("tcp", stripURLForDial(server.URL), 5*time.Second)
	if err == nil {
		conn.Close()
		newStatus = "connected"
	}

	return s.UpdateStatus(ctx, types.UpdateMcpServerStatusParams{
		ID:     id.String(),
		Status: newStatus,
	})
}

// ---------------------------------------------------------------------------
// MCP 环境变量加密/脱敏辅助函数
// ---------------------------------------------------------------------------

const encryptedMCPEnvMarker = "teammate-mcp-env-v1"

type encryptedMCPEnvVars struct {
	Format string            `json:"format"`
	Values map[string]string `json:"values"`
}

// rawToNullRaw 将 json.RawMessage 转为 pqtype.NullRawMessage。
// 复用 store 包同名 helper 不现实（service 不 import store），故在本包内提供。
func rawToNullRaw(rm json.RawMessage) pqtype.NullRawMessage {
	if rm == nil {
		return pqtype.NullRawMessage{}
	}
	return pqtype.NullRawMessage{RawMessage: rm, Valid: true}
}

func encryptMCPEnvVars(raw json.RawMessage, valid bool) (pqtype.NullRawMessage, error) {
	if !valid || len(raw) == 0 || string(raw) == "null" {
		return pqtype.NullRawMessage{}, nil
	}
	values, err := parseMCPEnvVars(raw)
	if err != nil {
		return pqtype.NullRawMessage{}, err
	}
	if len(values) == 0 {
		return pqtype.NullRawMessage{RawMessage: json.RawMessage(`{}`), Valid: true}, nil
	}
	encrypted := encryptedMCPEnvVars{Format: encryptedMCPEnvMarker, Values: make(map[string]string, len(values))}
	for key, value := range values {
		ciphertext, err := servercrypto.EncryptPAT(value)
		if err != nil {
			return pqtype.NullRawMessage{}, fmt.Errorf("encrypt %s: %w", key, err)
		}
		encrypted.Values[key] = ciphertext
	}
	data, err := json.Marshal(encrypted)
	if err != nil {
		return pqtype.NullRawMessage{}, fmt.Errorf("marshal encrypted env vars: %w", err)
	}
	return pqtype.NullRawMessage{RawMessage: data, Valid: true}, nil
}

func decryptMCPEnvVars(raw json.RawMessage, valid bool) (pqtype.NullRawMessage, error) {
	if !valid || len(raw) == 0 || string(raw) == "null" {
		return pqtype.NullRawMessage{}, nil
	}
	var encrypted encryptedMCPEnvVars
	if err := json.Unmarshal(raw, &encrypted); err != nil || encrypted.Format != encryptedMCPEnvMarker {
		return pqtype.NullRawMessage{RawMessage: raw, Valid: true}, nil
	}
	values := make(map[string]string, len(encrypted.Values))
	for key, ciphertext := range encrypted.Values {
		plaintext, err := servercrypto.DecryptPAT(ciphertext)
		if err != nil {
			return pqtype.NullRawMessage{}, fmt.Errorf("decrypt %s: %w", key, err)
		}
		values[key] = plaintext
	}
	data, err := json.Marshal(values)
	if err != nil {
		return pqtype.NullRawMessage{}, fmt.Errorf("marshal decrypted env vars: %w", err)
	}
	return pqtype.NullRawMessage{RawMessage: data, Valid: true}, nil
}

func maskMCPServerEnvVars(server types.McpServer) types.McpServer {
	server.EnvVars = maskMCPEnvVars(server.EnvVars)
	return server
}

func maskMCPEnvVars(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	keys := map[string]string{}
	var encrypted encryptedMCPEnvVars
	if err := json.Unmarshal(raw, &encrypted); err == nil && encrypted.Format == encryptedMCPEnvMarker {
		for key := range encrypted.Values {
			keys[key] = "********"
		}
	} else if values, err := parseMCPEnvVars(raw); err == nil {
		for key := range values {
			keys[key] = "********"
		}
	}
	data, err := json.Marshal(keys)
	if err != nil {
		return nil
	}
	return data
}

func parseMCPEnvVars(raw json.RawMessage) (map[string]string, error) {
	var anyValues map[string]any
	if err := json.Unmarshal(raw, &anyValues); err != nil {
		return nil, fmt.Errorf("env_vars must be a JSON object: %w", err)
	}
	values := make(map[string]string, len(anyValues))
	for key, value := range anyValues {
		switch v := value.(type) {
		case string:
			values[key] = v
		case nil:
			continue
		default:
			values[key] = fmt.Sprint(v)
		}
	}
	return values, nil
}

// stripURLForDial 从 URL 中提取 host:port 用于 TCP 拨号。
// 支持 http:// 和 https:// 前缀，自动补全默认端口（HTTP:80，HTTPS:443）。
func stripURLForDial(rawURL string) string {
	u := rawURL
	isHTTPS := false
	if len(u) > 7 && u[:7] == "http://" {
		u = u[7:]
	} else if len(u) > 8 && u[:8] == "https://" {
		u = u[8:]
		isHTTPS = true
	}
	for i := 0; i < len(u); i++ {
		if u[i] == '/' {
			u = u[:i]
			break
		}
	}
	if !containsColon(u) {
		if isHTTPS {
			u = u + ":443"
		} else {
			u = u + ":80"
		}
	}
	return u
}

// containsColon 检查字符串是否包含冒号。
func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}
