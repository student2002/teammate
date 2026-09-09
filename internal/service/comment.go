// comment.go implements the business logic for task comments, including creating, listing, and updating comments.
//
// This file contains:
//   - CommentService struct: the comment management service, encapsulating CRUD operations on comments
//   - Create: creates a comment on the specified task, supporting the associated mentioned member list
//   - List: lists all comments of the specified task, sorted by creation time in ascending order
//   - Update: updates the comment content and mention list within a 5-minute editing window
//
// Comments support mentions (@mention); editing has a 5-minute time window restriction.
// After the editing window expires, modifying comment content is not allowed, ensuring the historical traceability of comments.
package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CommentService provides the business logic for comment management.
type CommentService struct {
	svc *Service
}

func NewCommentService(svc *Service) *CommentService {
	return &CommentService{svc: svc}
}

// Create creates a comment on the specified task, supporting the associated mentioned member list.
func (s *CommentService) Create(ctx context.Context, params types.CreateCommentParams) (types.Comment, error) {
	comment, err := s.svc.Store.CreateComment(ctx, params)
	if err != nil {
		return types.Comment{}, err
	}
	// After creation, push mention:trigger to the @mentioned Agents to notify them to view the comment
	s.publishMentionTriggers(ctx, params.TaskID, uuidFromStr(comment.ID), params.Mentions)
	return comment, nil
}

// List lists all comments of the specified task, sorted by creation time in ascending order.
func (s *CommentService) List(ctx context.Context, taskID int32) ([]types.Comment, error) {
	return s.svc.Store.ListComments(ctx, taskID)
}

// ListTaskLevel lists the task-level comments of the specified task.
func (s *CommentService) ListTaskLevel(ctx context.Context, taskID int32) ([]types.Comment, error) {
	return s.svc.Store.ListTaskLevelComments(ctx, taskID)
}

// ListNode lists the comments in the specified node's comment section.
func (s *CommentService) ListNode(ctx context.Context, taskID int32, nodeID uuid.UUID) ([]types.Comment, error) {
	return s.svc.Store.ListNodeComments(ctx, taskID, nodeID)
}

// ListExecutionContext lists the comment context to inject when executing the specified node.
func (s *CommentService) ListExecutionContext(ctx context.Context, taskID int32, nodeID uuid.UUID, mentionID uuid.UUID) ([]types.Comment, error) {
	return s.svc.Store.ListExecutionContextComments(ctx, taskID, nodeID, mentionID)
}

// GetComment queries a single comment record by ID.
func (s *CommentService) GetComment(ctx context.Context, commentID uuid.UUID) (types.Comment, error) {
	comment, err := s.svc.Store.GetComment(ctx, commentID)
	if err != nil {
		return types.Comment{}, fmt.Errorf("get comment: %w", err)
	}
	return comment, nil
}

// Update updates the comment content and mention list within a 5-minute editing window.
// After 5 minutes, modification is not allowed and an error is returned.
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
	// Only publish mention:trigger for newly added @mentions, to avoid duplicate triggers on edit
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

// publishMentionTriggers pushes a mention:trigger event to the @mentioned Agents after a comment is created/updated.
// Agents trigger polling upon receiving the event (internal/agent/daemon.go handleMentionTrigger).
// Mentioned human members are not pushed via SSE (no runtime/SSE connection); they use the existing pull-based notification (ListMentionComments).
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
