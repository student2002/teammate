// audit.go 提供审计日志的数据访问操作。
//
// 记录系统中的敏感操作，用于安全审计和问题追踪。
// 审计日志包含操作者类型/ID、操作类型、资源信息、IP 地址和请求 ID。
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

// CreateAuditLog 创建一条审计日志记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 审计日志参数，包含操作者、操作类型、资源信息等
//
// 返回：
//   - types.AuditLog: 创建的审计日志记录
//   - error: 创建失败时返回错误
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

// ListAuditLogs 分页查询指定工作区的审计日志列表。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//   - limit: 返回的最大记录数
//   - offset: 分页偏移量
//
// 返回：
//   - []types.AuditLog: 审计日志列表
//   - error: 查询失败时返回错误
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

// AuditLogEntry 是创建审计日志的辅助结构体，封装审计所需的全部字段。
//
// 包含操作者信息（类型和 ID）、操作详情（动作、资源类型/ID）、
// 请求元数据（IP 地址、User-Agent、请求 ID）。
//
// 注意：本结构体字段用 uuid.UUID 而非 domain string，因为它是 LogAudit
// 的入参，被 middleware 层以 uuid.UUID 构造。
// 未来若 middleware 层统一改为 string，本结构体字段可同步调整。
type AuditLogEntry struct {
	WorkspaceID  uuid.UUID // 工作区 ID
	ActorType    string    // 操作者类型（"member" 或 "agent"）
	ActorID      uuid.UUID // 操作者 ID
	Action       string    // 操作类型（如 "create_task"、"approve_node"）
	ResourceType string    // 资源类型（如 "task"、"node"）
	ResourceID   string    // 资源 ID
	Details      []byte    // 操作详情（JSON 格式）
	IPAddress    string    // 请求来源 IP 地址
	UserAgent    string    // 请求 User-Agent
	RequestID    uuid.UUID // 请求唯一标识
}

// LogAudit 从辅助结构体创建审计日志，自动处理 IP 地址转换和 JSON 序列化。
//
// 参数：
//   - ctx: 请求上下文
//   - entry: 审计日志条目，包含所有审计字段
//
// 返回：
//   - error: 创建失败时返回错误
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
	// 静默 json 引用避免未导入警告（json 在 rawToNullRaw 路径已使用，但本文件可能孤立）
	_ = json.RawMessage(nil)
	return nil
}
