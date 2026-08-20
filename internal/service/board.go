// board.go 实现看板数据的业务逻辑，用于获取项目的看板列及其节点数据。
//
// 本文件包含：
//   - BoardService 结构体：看板服务，封装看板数据的查询操作
//   - GetBoardData：获取指定项目的看板数据，返回包含节点的看板列列表
//
// 看板列按节点真实状态（5 种）分组：
//   - pending：待处理节点
//   - in_progress：进行中节点
//   - completed：已完成节点
//   - rejected：被拒绝的节点
//   - manual_intervention：需要人工干预的节点
//
// 每列包含对应状态的工作流节点列表，供前端渲染看板视图。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
)

// BoardService 提供看板相关的业务逻辑。
type BoardService struct {
	svc *Service
}

// BoardColumnTask 是看板列任务的类型别名，使 handler 层无需直接导入 store 包。
type BoardColumnTask = store.BoardColumnTask

// BoardColumn 是看板列的类型别名，使 handler 层无需直接导入 store 包。
type BoardColumn = store.BoardColumn

// NewBoardService 创建一个新的 BoardService 实例。
func NewBoardService(svc *Service) *BoardService {
	return &BoardService{svc: svc}
}

// GetBoardData 获取指定项目的看板数据，返回包含节点的看板列列表。
//
// 步骤：
//  1. 查询该项目下所有任务的工作流节点
//  2. 按节点真实状态（5 种：pending/in_progress/completed/rejected/manual_intervention）分组
//  3. 组装为看板列结构，每列包含状态名和对应的节点列表
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID，用于筛选该项目下的所有节点
//
// 返回：
//   - []store.BoardColumn: 看板列列表，每列包含列名和节点列表
//   - error: 可能的错误（数据库查询失败）
func (s *BoardService) GetBoardData(ctx context.Context, projectID uuid.UUID) ([]store.BoardColumn, error) {
	columns, err := s.svc.Store.GetBoardData(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get board data: %w", err)
	}
	return columns, nil
}
