// mcp.go provides the business logic for MCP (Model Context Protocol) server management.
// MCP servers provide external tools and data sources for Agents, supporting health checks, encrypted storage of environment variables, etc.
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

// McpService provides the business logic for MCP server management.
// MCP (Model Context Protocol) servers provide external tools and data sources for Agents.
type McpService struct {
	svc *Service
}

// NewMcpService creates a new McpService instance.
func NewMcpService(svc *Service) *McpService {
	return &McpService{svc: svc}
}

// Create creates a new MCP server.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating the MCP server, including workspace ID, name, URL, type, auth method, etc.
//
// Returns:
//   - types.McpServer: the created MCP server record
//   - error: possible errors (database write failure)
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

// List lists all MCP servers for the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.McpServer: list of MCP servers
//   - error: possible errors (database query failure)
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

// Get retrieves MCP server info by ID.
//
// Parameters:
//   - ctx: request context
//   - id: MCP server ID
//
// Returns:
//   - types.McpServer: MCP server info
//   - error: possible errors (server does not exist)
func (s *McpService) Get(ctx context.Context, id uuid.UUID) (types.McpServer, error) {
	return s.svc.Store.GetMcpServer(ctx, id)
}

// UpdateStatus updates the connection status of an MCP server (connected/disconnected).
//
// Parameters:
//   - ctx: request context
//   - params: parameters for updating the status, including the server ID and the new status
//
// Returns:
//   - types.McpServer: the updated MCP server record
//   - error: possible errors (database update failure)
func (s *McpService) UpdateStatus(ctx context.Context, params types.UpdateMcpServerStatusParams) (types.McpServer, error) {
	server, err := s.svc.Store.UpdateMcpServerStatus(ctx, params)
	if err != nil {
		return types.McpServer{}, err
	}
	return maskMCPServerEnvVars(server), nil
}

// Update updates the configuration of an MCP server.
//
// All pointer parameters support three-state semantics: nil = keep existing value, non-nil = replace.
// envVars additionally supports {} = clear.
//
// Parameters:
//   - ctx: request context
//   - id: MCP server ID
//   - name: new server name (nil = keep)
//   - url: new server URL (nil = keep)
//   - mcpType: new server type (nil = keep)
//   - authType: new auth method (nil = keep)
//   - envVars: environment variables (nil = keep, {} = clear, with value = replace)
//   - status: new connection status (nil = keep)
//
// Returns:
//   - types.McpServer: the updated MCP server record
//   - error: possible errors (server does not exist, database update failure)
func (s *McpService) Update(ctx context.Context, id uuid.UUID, name, url, mcpType *string, authType *string, envVars []byte, status *string) (types.McpServer, error) {
	// Get the current value and merge the non-nil update fields
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

	// envVars three-state semantics
	var encryptedEnvVars pqtype.NullRawMessage
	if envVars == nil {
		encryptedEnvVars = rawToNullRaw(current.EnvVars) // keep existing value
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
	_ = newStatus // status is updated separately via UpdateStatus
	return maskMCPServerEnvVars(server), nil
}

// Delete deletes an MCP server.
//
// Parameters:
//   - ctx: request context
//   - id: MCP server ID
//
// Returns:
//   - error: possible errors (server does not exist, database deletion failure)
func (s *McpService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteMcpServer(ctx, id)
}

// HealthCheck performs a health check on an MCP server, testing reachability via a TCP connection.
// The connection timeout is 5 seconds.
//
// Steps:
//  1. Get the MCP server info
//  2. Extract the host:port from the URL
//  3. Attempt a TCP connection to the target address (5-second timeout)
//  4. Update the server status based on the connection result
//
// Parameters:
//   - ctx: request context
//   - id: MCP server ID
//
// Returns:
//   - types.McpServer: the MCP server record after status update
//   - error: possible errors (server does not exist, database update failure)
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
// MCP environment variable encryption/masking helper functions
// ---------------------------------------------------------------------------

const encryptedMCPEnvMarker = "teammate-mcp-env-v1"

type encryptedMCPEnvVars struct {
	Format string            `json:"format"`
	Values map[string]string `json:"values"`
}

// rawToNullRaw converts a json.RawMessage to a pqtype.NullRawMessage.
// Reusing the same-named helper in the store package is not feasible (service does not import store), so it is provided within this package.
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

// stripURLForDial extracts the host:port from a URL for TCP dialing.
// Supports the http:// and https:// prefixes, and auto-completes default ports (HTTP: 80, HTTPS: 443).
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

// containsColon checks whether a string contains a colon.
func containsColon(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return true
		}
	}
	return false
}
