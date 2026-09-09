// audit.go provides data access operations for audit logs.
//
// It records sensitive operations in the system for security auditing and issue tracing.
// Audit logs include actor type/ID, action type, resource info, IP address, and request ID.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateAuditLog creates an audit log record.
//
// Parameters:
//   - ctx: request context
//   - params: audit log parameters, including actor, action type, resource info, etc.
//
// Returns:
//   - types.AuditLog: the created audit log record
//   - error: error returned when creation fails
func (s *Store) CreateAuditLog(ctx context.Context, params types.CreateAuditLogParams) (types.AuditLog, error) {
	wsID, err := stringToUUID(params.WorkspaceID)
	if err != nil {
		return types.AuditLog{}, fmt.Errorf("convert workspace id: %w", err)
	}
	actorID, err := stringToUUID(params.ActorID)
	if err != nil {
		return types.AuditLog{}, fmt.Errorf("convert actor id: %w", err)
	}
	var requestID uuid.NullUUID
	if params.RequestID != nil {
		if u, err := uuid.Parse(*params.RequestID); err == nil {
			requestID = uuid.NullUUID{UUID: u, Valid: true}
		}
	}
	log, err := s.q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		WorkspaceID:  wsID,
		ActorType:    params.ActorType,
		ActorID:      actorID,
		Action:       params.Action,
		ResourceType: params.ResourceType,
		ResourceID:   params.ResourceID,
		Details:      rawToNullRaw(params.Details),
		IpAddress:    stringToInet(params.IPAddress),
		UserAgent:    ptrToNullString(params.UserAgent),
		RequestID:    requestID,
	})
	if err != nil {
		return types.AuditLog{}, fmt.Errorf("create audit log: %w", err)
	}
	return types.AuditLog{
		ID:           log.ID,
		WorkspaceID:  log.WorkspaceID.String(),
		ActorType:    log.ActorType,
		ActorID:      log.ActorID.String(),
		Action:       log.Action,
		ResourceType: log.ResourceType,
		ResourceID:   log.ResourceID,
		Details:      nullRawToRaw(log.Details),
		IPAddress:    inetToString(log.IpAddress),
		UserAgent:    nullStringToPtr(log.UserAgent),
		RequestID:    nullUUIDToString(log.RequestID),
		CreatedAt:    log.CreatedAt,
	}, nil
}

// ListAuditLogs paginates the audit log list for the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//   - limit: the maximum number of records to return
//   - offset: pagination offset
//
// Returns:
//   - []types.AuditLog: the audit log list
//   - error: error returned when the query fails
func (s *Store) ListAuditLogs(ctx context.Context, workspaceID uuid.UUID, limit, offset int32) ([]types.AuditLog, error) {
	logs, err := s.q.ListAuditLogs(ctx, db.ListAuditLogsParams{
		WorkspaceID: workspaceID,
		Limit:       limit,
		Offset:      offset,
	})
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	out := make([]types.AuditLog, 0, len(logs))
	for _, l := range logs {
		out = append(out, types.AuditLog{
			ID:           l.ID,
			WorkspaceID:  l.WorkspaceID.String(),
			ActorType:    l.ActorType,
			ActorID:      l.ActorID.String(),
			Action:       l.Action,
			ResourceType: l.ResourceType,
			ResourceID:   l.ResourceID,
			Details:      nullRawToRaw(l.Details),
			IPAddress:    inetToString(l.IpAddress),
			UserAgent:    nullStringToPtr(l.UserAgent),
			RequestID:    nullUUIDToString(l.RequestID),
			CreatedAt:    l.CreatedAt,
		})
	}
	return out, nil
}

// AuditLogEntry is a helper struct for creating audit logs, encapsulating all fields needed for auditing.
//
// It contains actor info (type and ID), action details (action, resource type/ID),
// and request metadata (IP address, User-Agent, request ID).
//
// Note: this struct's fields use uuid.UUID instead of domain strings because it is the
// input parameter of LogAudit, constructed by the middleware layer with uuid.UUID.
// In the future, if the middleware layer is uniformly changed to string, this struct's
// fields can be adjusted accordingly.
type AuditLogEntry struct {
	WorkspaceID  uuid.UUID // workspace ID
	ActorType    string    // actor type ("member" or "agent")
	ActorID      uuid.UUID // actor ID
	Action       string    // action type (e.g. "create_task", "approve_node")
	ResourceType string    // resource type (e.g. "task", "node")
	ResourceID   string    // resource ID
	Details      []byte    // action details (JSON format)
	IPAddress    string    // request source IP address
	UserAgent    string    // request User-Agent
	RequestID    uuid.UUID // request unique identifier
}

// LogAudit creates an audit log from the helper struct, automatically handling IP address conversion and JSON serialization.
//
// Parameters:
//   - ctx: request context
//   - entry: the audit log entry, including all audit fields
//
// Returns:
//   - error: error returned when creation fails
func (s *Store) LogAudit(ctx context.Context, entry AuditLogEntry) error {
	var ipAddr pqtype.Inet
	if entry.IPAddress != "" {
		ip := net.ParseIP(entry.IPAddress)
		if ip != nil {
			if ipv4 := ip.To4(); ipv4 != nil {
				ip = ipv4
			}
			bitCount := len(ip) * 8
			ipAddr = pqtype.Inet{
				IPNet: net.IPNet{IP: ip, Mask: net.CIDRMask(bitCount, bitCount)},
				Valid: true,
			}
		}
	}

	var details pqtype.NullRawMessage
	if entry.Details != nil {
		details = pqtype.NullRawMessage{RawMessage: entry.Details, Valid: true}
	} else {
		details = pqtype.NullRawMessage{RawMessage: []byte("{}"), Valid: true}
	}

	var requestID uuid.NullUUID
	if entry.RequestID != uuid.Nil {
		requestID = uuid.NullUUID{UUID: entry.RequestID, Valid: true}
	}

	_, err := s.q.CreateAuditLog(ctx, db.CreateAuditLogParams{
		WorkspaceID:  entry.WorkspaceID,
		ActorType:    entry.ActorType,
		ActorID:      entry.ActorID,
		Action:       entry.Action,
		ResourceType: entry.ResourceType,
		ResourceID:   entry.ResourceID,
		Details:      details,
		IpAddress:    ipAddr,
		UserAgent:    sql.NullString{String: entry.UserAgent, Valid: entry.UserAgent != ""},
		RequestID:    requestID,
	})
	if err != nil {
		return fmt.Errorf("create audit log: %w", err)
	}
	// Silently reference json to avoid an unused-import warning (json is used in the rawToNullRaw path, but this file may be standalone)
	_ = json.RawMessage(nil)
	return nil
}
