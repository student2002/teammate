// comment.go 实现任务评论的业务逻辑，包括创建、列表、更新评论。
//
// 本文件包含：
//   - CommentService 结构体：评论管理服务，封装评论的 CRUD 操作
//   - Create：在指定任务上创建评论，支持关联提及的成员列表
//   - List：列出指定任务的所有评论，按创建时间正序排列
//   - Update：在 5 分钟编辑窗口内更新评论内容和提及列表
//
// 评论支持提及（@mention）功能，编辑有 5 分钟的时间窗口限制。
// 超过编辑窗口后不允许修改评论内容，确保评论的历史可追溯性。
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CommentService 提供评论管理相关的业务逻辑。
type CommentService struct {
	svc *Service
}

func NewCommentService(svc *Service) *CommentService {
	return &CommentService{svc: svc}
}

// Create 在指定任务上创建一条评论，支持关联提及的成员列表。
func (s *CommentService) Create(ctx context.Context, params types.CreateCommentParams) (types.Comment, error) {
	comment, err := s.svc.Store.CreateComment(ctx, params)
	if err != nil {
		return types.Comment{}, err
	}
	// 创建后向被 @提及 的 Agent 推送 mention:trigger，通知其查看评论
	s.publishMentionTriggers(ctx, params.TaskID, uuidFromStr(comment.ID), params.Mentions)
	return comment, nil
}

// List 列出指定任务的所有评论，按创建时间正序排列。
func (s *CommentService) List(ctx context.Context, taskID int32) ([]types.Comment, error) {
	return s.svc.Store.ListComments(ctx, taskID)
}

// ListTaskLevel 列出指定任务的任务级评论。
func (s *CommentService) ListTaskLevel(ctx context.Context, taskID int32) ([]types.Comment, error) {
	return s.svc.Store.ListTaskLevelComments(ctx, taskID)
}

// ListNode 列出指定节点评论区的评论。
func (s *CommentService) ListNode(ctx context.Context, taskID int32, nodeID uuid.UUID) ([]types.Comment, error) {
	return s.svc.Store.ListNodeComments(ctx, taskID, nodeID)
}

// ListExecutionContext 列出执行指定节点时需要注入的评论上下文。
func (s *CommentService) ListExecutionContext(ctx context.Context, taskID int32, nodeID uuid.UUID, mentionID uuid.UUID) ([]types.Comment, error) {
	return s.svc.Store.ListExecutionContextComments(ctx, taskID, nodeID, mentionID)
}

// GetComment 根据 ID 查询单条评论记录。
func (s *CommentService) GetComment(ctx context.Context, commentID uuid.UUID) (types.Comment, error) {
	comment, err := s.svc.Store.GetComment(ctx, commentID)
	if err != nil {
		return types.Comment{}, fmt.Errorf("get comment: %w", err)
	}
	return comment, nil
}

// Update 在 5 分钟编辑窗口内更新评论内容和提及列表。
// 超过 5 分钟后不允许修改，返回错误。
func (s *CommentService) Update(ctx context.Context, commentID uuid.UUID, content string, mentions []uuid.UUID) (types.Comment, error) {
	existing, err := s.svc.Store.GetComment(ctx, commentID)
	if err != nil {
		return types.Comment{}, fmt.Errorf("comment not found: %w", err)
	}
	if s.svc.Store.Clock.Now().Sub(existing.CreatedAt) > 5*time.Minute {
		return types.Comment{}, fmt.Errorf("comment editing window has expired (5 minutes)")
	}
	comment, err := s.svc.Store.UpdateComment(ctx, commentID, content, mentions)
	if err != nil {
		return types.Comment{}, err
	}
	// 只对新增的 @提及 发布 mention:trigger，避免编辑重复触发
	existingSet := make(map[string]struct{}, len(existing.Mentions))
	for _, m := range existing.Mentions {
		existingSet[m] = struct{}{}
	}
	var newMentions []string
	for _, m := range mentions {
		ms := m.String()
		if _, ok := existingSet[ms]; !ok {
			newMentions = append(newMentions, ms)
		}
	}
	s.publishMentionTriggers(ctx, comment.TaskID, commentID, newMentions)
	return comment, nil
}

// publishMentionTriggers 在评论创建/更新后，向被 @提及 的 Agent 推送 mention:trigger 事件。
// Agent 收到事件后触发轮询（internal/agent/daemon.go handleMentionTrigger）。
// 被提及的人类成员不推送 SSE（无 runtime/SSE 连接），走现有的拉取式通知（ListMentionComments）。
func (s *CommentService) publishMentionTriggers(ctx context.Context, taskID int32, commentID uuid.UUID, mentions []string) {
	if len(mentions) == 0 || s.svc.Hub == nil {
		return
	}

	task, err := s.svc.Store.GetTask(ctx, taskID)
	if err != nil {
		return
	}
	project, err := s.svc.Store.GetProject(ctx, uuidFromStr(task.ProjectID))
	if err != nil {
		return
	}
	wsUUID, err := uuid.Parse(project.WorkspaceID)
	if err != nil {
		return
	}
	agents, err := s.svc.Store.ListAgents(ctx, wsUUID)
	if err != nil {
		return
	}
	agentIDs := make(map[string]struct{}, len(agents))
	for _, a := range agents {
		agentIDs[a.ID] = struct{}{}
	}

	for _, m := range mentions {
		if _, ok := agentIDs[m]; !ok {
			continue
		}
		agentUUID, err := uuid.Parse(m)
		if err != nil {
			continue
		}
		s.svc.publishToAgent(ctx, agentUUID, types.EventMentionTrigger, map[string]interface{}{
			"task_id":    fmt.Sprintf("%d", taskID),
			"comment_id": commentID.String(),
		})
	}
}
