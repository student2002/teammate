// search.go 实现全局搜索的业务逻辑，支持按关键词搜索任务和代理。
//
// 本文件包含：
//   - SearchService 结构体：提供搜索相关的业务逻辑封装
//   - SearchTasks：按关键词搜索任务，支持 ILIKE 模糊匹配标题和描述，可选按项目过滤
//   - SearchAgents：按关键词搜索代理，支持 ILIKE 模糊匹配名称，可选按工作区过滤
//   - 搜索结果按创建时间倒序排列
//
// 搜索策略：
//   - 使用 PostgreSQL ILIKE 进行大小写不敏感的模糊匹配
//   - 关键词自动包裹 % 通配符实现子串匹配
//   - 支持全局搜索（不指定范围）和限定范围搜索（指定项目或工作区 ID）
package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// SearchService 提供搜索相关的业务逻辑。
type SearchService struct {
	svc *Service
}

// NewSearchService 创建一个新的 SearchService 实例。
func NewSearchService(svc *Service) *SearchService {
	return &SearchService{svc: svc}
}

// SearchTasks 在指定工作区内按关键词搜索任务，可选按项目 ID 过滤。
// 使用 ILIKE 进行标题和描述的模糊匹配，结果按创建时间倒序排列。
//
// 参数：
//   - ctx: 请求上下文
//   - keyword: 搜索关键词
//   - workspaceID: 工作区 ID
//   - projectID: 可选，按项目 ID 过滤
//
// 返回：
//   - []db.Task: 匹配的任务列表
//   - error: 可能的错误（数据库查询失败）
func (s *SearchService) SearchTasks(ctx context.Context, keyword string, workspaceID uuid.UUID, projectID *uuid.UUID) ([]types.Task, error) {
	pattern := "%" + keyword + "%"
	if projectID != nil {
		return s.svc.Store.SearchTasksByWorkspaceAndProject(ctx, workspaceID, *projectID, pattern)
	}
	return s.svc.Store.SearchTasksByWorkspace(ctx, workspaceID, pattern)
}

// SearchAgents 在指定工作区内按关键词搜索代理。
// 使用 ILIKE 进行名称的模糊匹配，结果按创建时间倒序排列。
//
// 参数：
//   - ctx: 请求上下文
//   - keyword: 搜索关键词
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []db.Agent: 匹配的代理列表
//   - error: 可能的错误（数据库查询失败）
func (s *SearchService) SearchAgents(ctx context.Context, keyword string, workspaceID uuid.UUID) ([]types.Agent, error) {
	return s.svc.Store.SearchAgentsByWorkspace(ctx, workspaceID, "%"+keyword+"%")
}
