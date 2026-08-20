// review.go 实现代码审查的业务逻辑，包括审查队列管理和自我审查检测。
//
// 本文件包含：
//   - ReviewService 结构体：代码审查服务，封装审查队列和自我审查检测
//   - ReviewQueueItem 结构体：审查队列条目，包含任务、节点和审查者信息
//   - GetReviewQueue：获取指定项目的审查队列，返回 pending 和 in_progress 状态的审查节点
//   - SelfReviewCheckResult 结构体：自我审查检查结果，包含作者、审查者和风险级别
//   - CheckSelfReview：检查审查节点是否为自我审查，比较审查者和前序节点作者是否为同一代理
//
// 审查队列列出项目中所有待处理和进行中的审查节点，按创建时间正序排列。
// 自我审查检测用于防止代理审查自己编写的代码，确保代码审查的独立性和客观性。
// 自我审查被判定为高风险行为，需要在系统层面进行规避。
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReviewService 提供代码审查相关的业务逻辑。
type ReviewService struct {
	svc *Service
}

// NewReviewService 创建一个新的 ReviewService 实例。
func NewReviewService(svc *Service) *ReviewService {
	return &ReviewService{svc: svc}
}

// ReviewQueueItem 表示审查队列中的一项，包含任务信息、节点信息和分配的审查者。
type ReviewQueueItem struct {
	TaskID       int32     `json:"task_id"`        // 关联的任务 ID
	TaskTitle    string    `json:"task_title"`     // 任务标题
	NodeID       string    `json:"node_id"`        // 节点 ID
	NodeName     string    `json:"node_name"`      // 节点名称
	NodeStatus   string    `json:"node_status"`    // 节点状态（pending/in_progress）
	AssigneeType string    `json:"assignee_type"`  // 分配者类型（specific_agent/human）
	AssigneeID   *string   `json:"assignee_id"`    // 分配者 ID
	AgentName    *string   `json:"agent_name"`     // 分配的代理名称
	CreatedAt    time.Time `json:"created_at"`     // 创建时间
	UpdatedAt    time.Time `json:"updated_at"`     // 更新时间
}

// GetReviewQueue 获取指定项目的审查队列，返回所有 pending 和 in_progress 状态的审查节点。
// 按创建时间正序排列，最早的审查排在前面。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []ReviewQueueItem: 审查队列列表
//   - error: 可能的错误（数据库查询失败）
func (s *ReviewService) GetReviewQueue(ctx context.Context, projectID uuid.UUID) ([]ReviewQueueItem, error) {
	rows, err := s.svc.Store.GetReviewQueue(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get review queue: %w", err)
	}

	items := make([]ReviewQueueItem, len(rows))
	for i, row := range rows {
		items[i] = ReviewQueueItem{
			TaskID:       row.TaskID,
			TaskTitle:    row.TaskTitle,
			NodeID:       row.NodeID,
			NodeName:     row.NodeName,
			NodeStatus:   row.NodeStatus,
			AssigneeType: row.AssigneeType,
			AssigneeID:   row.AssigneeID,
			AgentName:    row.AgentName,
			CreatedAt:    row.CreatedAt,
			UpdatedAt:    row.UpdatedAt,
		}
	}

	return items, nil
}

// SelfReviewCheckResult 保存自我审查检查的结果。
type SelfReviewCheckResult struct {
	IsSelfReview bool   `json:"is_self_review"` // 是否为自我审查
	AuthorID     string `json:"author_id"`      // 代码作者 ID
	ReviewerID   string `json:"reviewer_id"`    // 审查者 ID
	RiskLevel    string `json:"risk_level"`     // 风险级别（none/high）
}

// CheckSelfReview 检查审查节点是否为自我审查。
// 比较审查者和前序节点（代码作者）是否为同一代理。
// 自我审查是高风险行为，需要避免。
//
// 步骤：
//  1. 获取审查节点的分配者（审查者）
//  2. 获取前序节点的分配者（代码作者），按排序在前的最近节点查找（兼容 0 或 1 起始编号）
//  3. 比较两者是否为同一代理
//  4. 返回检查结果，包含风险级别
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//   - nodeID: 审查节点 ID
//
// 返回：
//   - *SelfReviewCheckResult: 自我审查检查结果
//   - error: 可能的错误（节点不存在）
func (s *ReviewService) CheckSelfReview(ctx context.Context, taskID int32, nodeID uuid.UUID) (*SelfReviewCheckResult, error) {
	reviewerID, err := s.svc.Store.GetReviewNodeReviewer(ctx, nodeID, taskID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("review node not found")
		}
		return nil, fmt.Errorf("get reviewer: %w", err)
	}

	authorID, err := s.svc.Store.GetReviewNodeAuthor(ctx, taskID, nodeID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			reviewerIDStr := ""
			if reviewerID.Valid {
				reviewerIDStr = reviewerID.UUID.String()
			}
			return &SelfReviewCheckResult{
				IsSelfReview: false,
				ReviewerID:   reviewerIDStr,
				RiskLevel:    "none",
			}, nil
		}
		return nil, fmt.Errorf("get author: %w", err)
	}

	isSelfReview := reviewerID.Valid && authorID.Valid && reviewerID.UUID == authorID.UUID

	riskLevel := "none"
	if isSelfReview {
		riskLevel = "high"
	}

	authorIDStr := ""
	if authorID.Valid {
		authorIDStr = authorID.UUID.String()
	}
	reviewerIDStr := ""
	if reviewerID.Valid {
		reviewerIDStr = reviewerID.UUID.String()
	}

	return &SelfReviewCheckResult{
		IsSelfReview: isSelfReview,
		AuthorID:     authorIDStr,
		ReviewerID:   reviewerIDStr,
		RiskLevel:    riskLevel,
	}, nil
}
