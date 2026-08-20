// mcp.go 提供 MCP 服务器管理的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CreateMcpServer 创建一个新的 MCP 服务器记录。
func (s *Store) CreateMcpServer(ctx context.Context, params types.CreateMcpServerParams) (types.McpServer, error) {
	dbParams, err := fromDomainCreateMcpServerParams(params)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("convert create mcp server params: %w", err)
	}
	server, err := s.q.CreateMcpServer(ctx, dbParams)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("create mcp server: %w", err)
	}
	return ToDomainMcpServer(server)
}

// ListMcpServers 列出指定工作区的所有 MCP 服务器。
func (s *Store) ListMcpServers(ctx context.Context, workspaceID uuid.UUID) ([]types.McpServer, error) {
	servers, err := s.q.ListMcpServers(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	out := make([]types.McpServer, 0, len(servers))
	for _, sv := range servers {
		d, err := ToDomainMcpServer(sv)
		if err != nil {
			return nil, fmt.Errorf("convert mcp server: %w", err)
		}
		out = append(out, d)
	}
	return out, nil
}

// GetMcpServer 根据 ID 获取 MCP 服务器信息。
func (s *Store) GetMcpServer(ctx context.Context, id uuid.UUID) (types.McpServer, error) {
	server, err := s.q.GetMcpServer(ctx, id)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("get mcp server: %w", err)
	}
	return ToDomainMcpServer(server)
}

// UpdateMcpServerStatus 更新 MCP 服务器的连接状态。
func (s *Store) UpdateMcpServerStatus(ctx context.Context, params types.UpdateMcpServerStatusParams) (types.McpServer, error) {
	dbParams, err := fromDomainUpdateMcpServerStatusParams(params)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("convert update mcp server status params: %w", err)
	}
	server, err := s.q.UpdateMcpServerStatus(ctx, dbParams)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("update mcp server status: %w", err)
	}
	return ToDomainMcpServer(server)
}

// DeleteMcpServer 删除一个 MCP 服务器。
func (s *Store) DeleteMcpServer(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteMcpServer(ctx, id); err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	return nil
}
