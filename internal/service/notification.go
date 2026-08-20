// notification.go 实现通知的业务逻辑，聚合工作区内的人工干预节点和提及评论。
//
// 本文件包含：
//   - NotificationService 结构体：通知管理服务，聚合多种类型的通知
//   - NotificationItem 结构体：通知条目，包含类型、标题、描述、时间和关联任务 ID
//   - ListNotifications：列出指定工作区和成员的通知，合并人工干预和提及两类通知
//
// 通知类型包括：
//   - manual_intervention：需要人类处理的工作流节点
//   - mention：在评论中被 @mention 的成员
//
// 两类通知合并后返回，供前端统一渲染通知中心。
// 人工干预通知面向工作区所有成员，提及通知仅针对被 @mention 的特定成员。
package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NotificationService 提供通知管理相关的业务逻辑。
type NotificationService struct {
	svc *Service
}

// NewNotificationService 创建一个新的 NotificationService 实例。
func NewNotificationService(svc *Service) *NotificationService {
	return &NotificationService{svc: svc}
}

// NotificationItem 表示一条通知，包含通知类型（manual_intervention 或 mention）、
// 标题、描述、创建时间和关联的任务 ID。
type NotificationItem struct {
	ID          string    `json:"id"`           // 通知唯一标识
	Type        string    `json:"type"`         // 通知类型：manual_intervention 或 mention
	Title       string    `json:"title"`        // 通知标题（通常为任务标题）
	Description string    `json:"description"`  // 通知描述（节点名称或评论内容）
	CreatedAt   time.Time `json:"created_at"`   // 通知创建时间
	TaskID      int32     `json:"task_id"`      // 关联的任务 ID
}

// ListNotifications 列出指定工作区和成员的通知，包括人工干预节点和提及评论。
// 两类通知合并后返回，供前端统一渲染通知中心。
//
// 步骤：
//  1. 查询工作区内所有 manual_intervention 状态的节点，生成人工干预通知
//  2. 如果指定了成员 ID，查询该成员被 @mention 的评论，生成提及通知
//  3. 合并两类通知并返回
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID，用于隔离通知
//   - memberID: 成员 ID，用于查询提及评论（传入 uuid.Nil 则跳过提及查询）
//
// 返回：
//   - []NotificationItem: 通知列表
//   - error: 可能的错误（数据库查询失败）
func (s *NotificationService) ListNotifications(ctx context.Context, workspaceID uuid.UUID, memberID uuid.UUID) ([]NotificationItem, error) {
	notifications := make([]NotificationItem, 0)

	manualNodes, err := s.svc.Store.ListManualInterventionNodes(ctx, workspaceID)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("list manual intervention nodes: %w", err)
	}
	for _, node := range manualNodes {
		notifications = append(notifications, NotificationItem{
			ID:          node.ID,
			Type:        "manual_intervention",
			Title:       node.TaskTitle,
			Description: "Node '" + node.Name + "' requires manual intervention",
			CreatedAt:   node.CreatedAt,
			TaskID:      node.TaskID,
		})
	}

	if memberID != uuid.Nil {
		mentions, err := s.svc.Store.ListMentionComments(ctx, workspaceID, memberID)
		if err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("list mention comments: %w", err)
		}
		for _, m := range mentions {
			notifications = append(notifications, NotificationItem{
				ID:          m.ID,
				Type:        "mention",
				Title:       m.TaskTitle,
				Description: m.Content,
				CreatedAt:   m.CreatedAt,
				TaskID:      m.TaskID,
			})
		}
	}

	return notifications, nil
}
