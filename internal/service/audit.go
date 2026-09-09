// audit.go implements the business logic for audit logs, recording key operations in the system.
//
// This file contains:
//   - AuditService struct: the audit log management service, encapsulating log recording and query operations
//   - Log: creates an audit log record, automatically extracting request_id from the context and persisting it
//   - List: lists the audit logs of a specified workspace, supports pagination, ordered by creation time descending
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/contextx"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// AuditService provides the business logic for audit log management.
type AuditService struct {
	svc *Service
}

// AuditLogEntry is a type alias for an audit log entry so the handler layer does not need to import the store package directly.
type AuditLogEntry = store.AuditLogEntry

// NewAuditService creates a new AuditService instance.
func NewAuditService(svc *Service) *AuditService {
	return &AuditService{svc: svc}
}

// Log creates an audit log record, automatically filling in request_id from the context.
func (s *AuditService) Log(ctx context.Context, entry store.AuditLogEntry) error {
	if entry.RequestID == uuid.Nil {
		entry.RequestID = contextx.GetRequestIDFromContext(ctx)
	}
	return s.svc.Store.LogAudit(ctx, entry)
}

// List lists the audit logs of a specified workspace, supporting pagination.
func (s *AuditService) List(ctx context.Context, workspaceID uuid.UUID, limit, offset int32) ([]types.AuditLog, error) {
	return s.svc.Store.ListAuditLogs(ctx, workspaceID, limit, offset)
}
