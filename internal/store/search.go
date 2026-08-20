// search.go 提供全局搜索的数据访问操作。
//
// 本文件包含：
//   - SearchTasksByWorkspace：在指定工作区内按关键词搜索任务（ILIKE 模糊匹配标题和描述）
//   - SearchTasksByWorkspaceAndProject：在指定工作区和项目内按关键词搜索任务
//   - SearchAgentsByWorkspace：在指定工作区内按关键词搜索代理（ILIKE 模糊匹配名称）
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// SearchTasksByWorkspace 在指定工作区内按关键词搜索任务。
// 使用 ILIKE 进行标题和描述的模糊匹配，结果按创建时间倒序排列。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - pattern: 搜索模式（已包含 % 通配符）
//
// 返回：
//   - []types.Task: 匹配的任务列表
//   - error: 可能的错误（数据库查询失败）
func (s *Store) SearchTasksByWorkspace(ctx context.Context, workspaceID uuid.UUID, pattern string) ([]types.Task, error) {
	tasks, err := s.q.SearchTasksByWorkspace(ctx, FromDomainSearchTasksByWorkspaceParams(workspaceID, pattern))
	if err != nil {
		return nil, fmt.Errorf("search tasks by workspace: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// SearchTasksByWorkspaceAndProject 在指定工作区和项目内按关键词搜索任务。
// 使用 ILIKE 进行标题和描述的模糊匹配，结果按创建时间倒序排列。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - projectID: 项目 ID
//   - pattern: 搜索模式（已包含 % 通配符）
//
// 返回：
//   - []types.Task: 匹配的任务列表
//   - error: 可能的错误（数据库查询失败）
func (s *Store) SearchTasksByWorkspaceAndProject(ctx context.Context, workspaceID, projectID uuid.UUID, pattern string) ([]types.Task, error) {
	tasks, err := s.q.SearchTasksByWorkspaceAndProject(ctx, FromDomainSearchTasksByWorkspaceAndProjectParams(workspaceID, projectID, pattern))
	if err != nil {
		return nil, fmt.Errorf("search tasks by workspace and project: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// SearchAgentsByWorkspace 在指定工作区内按关键词搜索代理。
// 使用 ILIKE 进行名称的模糊匹配，结果按创建时间倒序排列。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - pattern: 搜索模式（已包含 % 通配符）
//
// 返回：
//   - []types.Agent: 匹配的代理列表
//   - error: 可能的错误（数据库查询失败）
func (s *Store) SearchAgentsByWorkspace(ctx context.Context, workspaceID uuid.UUID, pattern string) ([]types.Agent, error) {
	agents, err := s.q.SearchAgentsByWorkspace(ctx, FromDomainSearchAgentsByWorkspaceParams(workspaceID, pattern))
	if err != nil {
		return nil, fmt.Errorf("search agents by workspace: %w", err)
	}
	return ToDomainAgentSlice(agents)
}
