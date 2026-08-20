// memory.go 提供知识记忆的数据访问操作。
//
// 记忆（Memory）是工作区的共享知识库，存储架构决策、命令、约定、洞察等信息。
// 当前通过 ILIKE 文本搜索检索记忆；数据库已预留 embedding vector(1536) 字段，
// pgvector 语义检索（余弦距离排序）待接入 embedding 生成服务后启用。
//
// 记忆条目标签化组织，支持按类型、置信度、验证状态过滤。
// 过时的记忆标记为 stale，不参与搜索。
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// escapeILIKE 转义 ILIKE 查询中的特殊字符（% 和 _）。
//
// 参数：
//   - s: 需要转义的字符串
//
// 返回：
//   - string: 转义后的字符串
func escapeILIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// CreateMemory 创建一条记忆条目记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 记忆创建参数，包含工作区 ID、类型、标题、内容、标签等
//
// 返回：
//   - db.Memory: 创建的记忆记录
//   - error: 创建失败时返回错误
func (s *Store) CreateMemory(ctx context.Context, params types.CreateMemoryParams) (types.Memory, error) {
	dbParams, err := FromDomainCreateMemoryParams(params)
	if err != nil {
		return types.Memory{}, fmt.Errorf("convert create memory params: %w", err)
	}
	m, err := s.q.CreateMemory(ctx, dbParams)
	if err != nil {
		return types.Memory{}, fmt.Errorf("create memory: %w", err)
	}
	return ToDomainMemory(m)
}

// GetMemory 根据 ID 获取一条记忆条目。
func (s *Store) GetMemory(ctx context.Context, id uuid.UUID) (types.Memory, error) {
	m, err := s.q.GetMemory(ctx, id)
	if err != nil {
		return types.Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return ToDomainMemory(m)
}

// ListMemoriesByWorkspace 分页查询指定工作区的记忆条目，支持按验证状态和最低置信度过滤。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//   - verified: 验证状态过滤（nil 表示不过滤）
//   - minConfidence: 最低置信度过滤（nil 表示不过滤）
//   - limit: 返回的最大记录数（nil 表示默认限制）
//
// 返回：
//   - []db.Memory: 记忆列表
//   - error: 查询失败时返回错误
func (s *Store) ListMemoriesByWorkspace(ctx context.Context, workspaceID uuid.UUID, verified *bool, minConfidence *float32, limit *int32) ([]types.Memory, error) {
	memories, err := s.q.ListMemoriesByWorkspace(ctx, FromDomainListMemoriesByWorkspaceParams(workspaceID, verified, minConfidence, limit))
	if err != nil {
		return nil, fmt.Errorf("list memories by workspace: %w", err)
	}
	return ToDomainMemorySlice(memories)
}

// DeleteMemory 根据 ID 删除一条记忆条目。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 记忆的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteMemory(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteMemory(ctx, id); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	return nil
}

// MarkMemoriesStaleByTask 将指定任务关联的所有记忆条目标记为过期（stale）。
//
// 当任务被取消或重新执行时，之前产生的记忆可能过时，需要标记为 stale。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的整数 ID
//
// 返回：
//   - error: 更新失败时返回错误
func (s *Store) MarkMemoriesStaleByTask(ctx context.Context, taskID int32) error {
	if err := s.q.MarkMemoriesStaleByTask(ctx, sql.NullInt32{Int32: taskID, Valid: true}); err != nil {
		return fmt.Errorf("mark memories stale by task: %w", err)
	}
	return nil
}

// MemoryRow 是搜索结果使用的简化记忆行结构体。
//
// 包含记忆的基本信息、向量嵌入和元数据，用于搜索结果返回。
type MemoryRow struct {
	ID           string          `json:"id"`             // 记忆 UUID
	WorkspaceID  string          `json:"workspace_id"`   // 工作区 ID
	SourceTaskID *string         `json:"source_task_id"` // 来源任务 ID（可选）
	Type         string          `json:"type"`           // 记忆类型（architecture/command/convention 等）
	Title        string          `json:"title"`          // 标题
	Content      string          `json:"content"`        // 内容
	Tags         []string        `json:"tags"`           // 标签列表
	Embedding    interface{}     `json:"embedding"`      // 向量嵌入（pgvector）
	Confidence   float32         `json:"confidence"`     // 置信度（0-1）
	Verified     bool            `json:"verified"`       // 是否已验证
	Metadata     json.RawMessage `json:"metadata"`       // 扩展元数据（JSON）
	CreatedAt    string          `json:"created_at"`     // 创建时间
	UpdatedAt    string          `json:"updated_at"`     // 更新时间
}

// SearchMemories 对记忆条目进行 ILIKE 文本搜索，结果限制 100 行以防止内存耗尽。
//
// 搜索范围：
//   - 仅搜索未过期的记忆（stale = false）
//   - 在标题和内容中进行模糊匹配
//   - 按创建时间倒序排列
//
// 参数：
//   - ctx: 请求上下文
//   - query: 搜索关键词
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []MemoryRow: 搜索结果列表
//   - error: 查询失败时返回错误
func (s *Store) SearchMemories(ctx context.Context, query string, workspaceID uuid.UUID) ([]types.SearchMemoriesRow, error) {
	escapedQuery := "%" + escapeILIKE(query) + "%"
	rows, err := s.q.SearchMemories(ctx, db.SearchMemoriesParams{
		WorkspaceID: workspaceID,
		Title:       escapedQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}

	results := make([]types.SearchMemoriesRow, 0, len(rows))
	for _, r := range rows {
		var sourceTaskID *int32
		if r.SourceTaskID.Valid {
			v := r.SourceTaskID.Int32
			sourceTaskID = &v
		}
		results = append(results, types.SearchMemoriesRow{
			ID:           r.ID.String(),
			WorkspaceID:  r.WorkspaceID.String(),
			SourceTaskID: sourceTaskID,
			Type:         string(r.Type),
			Title:        r.Title,
			Content:      r.Content,
			Tags:         r.Tags,
			Confidence:   float32(r.Confidence),
			Verified:     r.Verified,
			Metadata:     nullRawToRaw(r.Metadata),
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return results, nil
}
