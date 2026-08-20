// memory.go 实现共享记忆的业务逻辑，支持创建、列表、删除和文本搜索记忆条目。
//
// 本文件包含：
//   - MemoryService 结构体：记忆管理服务，封装记忆条目的 CRUD 和文本搜索操作
//   - Create：创建记忆条目，包含内容、置信度等信息
//   - ListByWorkspace：按工作区列出记忆条目，支持验证状态、置信度、数量限制过滤
//   - Delete：删除指定的记忆条目
//   - Search：文本搜索记忆条目，使用 ILIKE 模糊匹配返回结果
//
// 数据库已预留 embedding vector(1536) 字段，pgvector 语义检索（余弦距离排序）
// 待接入 embedding 生成服务后启用。
// 共享记忆允许 Agent 之间共享上下文知识，提高多 Agent 协作效率。
// 记忆条目包含验证状态和置信度分数，用于评估记忆的可靠性。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// MemoryService 提供共享记忆管理相关的业务逻辑。
// 记忆条目包含内容、向量嵌入（用于语义搜索）、置信度分数和验证状态。
type MemoryService struct {
	svc *Service
}

// NewMemoryService 创建一个新的 MemoryService 实例。
func NewMemoryService(svc *Service) *MemoryService {
	return &MemoryService{svc: svc}
}

// Create 创建一条记忆条目。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建记忆的参数，包含内容、向量嵌入、任务 ID、工作区 ID 等
//
// 返回：
//   - types.Memory: 创建的记忆条目
//   - error: 创建失败时返回错误
func (s *MemoryService) Create(ctx context.Context, params types.CreateMemoryParams) (types.Memory, error) {
	return s.svc.Store.CreateMemory(ctx, params)
}

// ListByWorkspace 列出指定工作区的记忆条目，支持按验证状态、最低置信度和数量限制进行过滤。
// 所有参数均可选，传入 nil 表示不过滤对应维度。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - verified: 可选，按验证状态过滤（true=已验证，false=未验证）
//   - minConfidence: 可选，最低置信度阈值
//   - limit: 可选，返回的最大条目数
//
// 返回：
//   - []types.Memory: 记忆条目列表
//   - error: 可能的错误（数据库查询失败）
func (s *MemoryService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, verified *bool, minConfidence *float32, limit *int32) ([]types.Memory, error) {
	return s.svc.Store.ListMemoriesByWorkspace(ctx, workspaceID, verified, minConfidence, limit)
}

// Get 根据 ID 获取一条记忆条目。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 记忆条目 ID
//
// 返回：
//   - types.Memory: 记忆条目
//   - error: 可能的错误（记录不存在）
func (s *MemoryService) Get(ctx context.Context, id uuid.UUID) (types.Memory, error) {
	memory, err := s.svc.Store.GetMemory(ctx, id)
	if err != nil {
		return types.Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return memory, nil
}

// Delete 删除一条记忆条目。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 记忆条目 ID
//
// 返回：
//   - error: 可能的错误（记录不存在、数据库删除失败）
func (s *MemoryService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteMemory(ctx, id)
}

// Search 对记忆条目进行文本检索（title/content ILIKE 匹配），返回按相关度排序的结果。
// pgvector 向量语义检索为预留能力（embedding 列保留，暂无查询使用）。
//
// 参数：
//   - ctx: 请求上下文
//   - query: 搜索关键词或语义查询文本
//   - workspaceID: 工作区 ID，限定搜索范围
//
// 返回：
//   - []types.SearchMemoriesRow: 按相关性排序的记忆条目列表
//   - error: 可能的错误（数据库查询失败）
func (s *MemoryService) Search(ctx context.Context, query string, workspaceID uuid.UUID) ([]types.SearchMemoriesRow, error) {
	return s.svc.Store.SearchMemories(ctx, query, workspaceID)
}
