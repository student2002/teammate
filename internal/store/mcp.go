// mcp.go provides data access operations for MCP server management.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CreateMcpServer creates a new MCP server record.
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

// ListMcpServers lists all MCP servers for the specified workspace.
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

// GetMcpServer retrieves MCP server information by ID.
func (s *Store) GetMcpServer(ctx context.Context, id uuid.UUID) (types.McpServer, error) {
	server, err := s.q.GetMcpServer(ctx, id)
	if err != nil {
		return types.McpServer{}, fmt.Errorf("get mcp server: %w", err)
	}
	return ToDomainMcpServer(server)
}

// UpdateMcpServerStatus updates the connection status of an MCP server.
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

// DeleteMcpServer deletes an MCP server.
func (s *Store) DeleteMcpServer(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteMcpServer(ctx, id); err != nil {
		return fmt.Errorf("delete mcp server: %w", err)
	}
	return nil
}
