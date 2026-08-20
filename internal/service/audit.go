// audit.go 实现审计日志的业务逻辑，记录系统中的关键操作。
//
// 本文件包含：
//   - AuditService 结构体：审计日志管理服务，封装日志的记录和查询操作
//   - Log：创建审计日志记录，自动从上下文提取 request_id 并持久化
//   - List：列出指定工作区的审计日志，支持分页查询，按创建时间倒序排列
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/contextx"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// AuditService 提供审计日志管理相关的业务逻辑。
type AuditService struct {
	svc *Service
}

// AuditLogEntry 是审计日志条目的类型别名，使 handler 层无需直接导入 store 包。
type AuditLogEntry = store.AuditLogEntry

// NewAuditService 创建一个新的 AuditService 实例。
func NewAuditService(svc *Service) *AuditService {
	return &AuditService{svc: svc}
}

// Log 创建一条审计日志记录，自动从上下文中填充 request_id。
func (s *AuditService) Log(ctx context.Context, entry store.AuditLogEntry) error {
	if entry.RequestID == uuid.Nil {
		entry.RequestID = contextx.GetRequestIDFromContext(ctx)
	}
	return s.svc.Store.LogAudit(ctx, entry)
}

// List 列出指定工作区的审计日志，支持分页查询。
func (s *AuditService) List(ctx context.Context, workspaceID uuid.UUID, limit, offset int32) ([]types.AuditLog, error) {
	return s.svc.Store.ListAuditLogs(ctx, workspaceID, limit, offset)
}
