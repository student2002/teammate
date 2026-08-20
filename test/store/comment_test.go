// comment_test.go 覆盖评论数据访问的测试。
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

func TestCommentCRUD(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	proj := createTestProject(t, s, ws.ID)
	_, tplNodes := createTestWorkflowTemplate(t, s, ws.ID, 2)

	task, _, err := s.CreateTask(ctx, types.CreateTaskParams{
		ProjectID:  proj.ID,
		Title:      "Comment task",
		Type:       "task",
		Priority:   "medium",
		Status:     "active",
		AuthorType: "member",
		AuthorID:   uuid.New().String(),
	}, tplNodes)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	comment, err := s.CreateComment(ctx, types.CreateCommentParams{
		TaskID:      task.ID,
		AuthorType:  "member",
		AuthorID:    uuid.New().String(),
		Content:     "This is a comment",
		CommentType: "text",
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if comment.Content != "This is a comment" {
		t.Fatalf("expected content match")
	}

	comments, err := s.ListComments(ctx, task.ID)
	if err != nil {
		t.Fatalf("ListComments: %v", err)
	}
	if len(comments) != 1 {
		t.Fatalf("expected 1 comment, got %d", len(comments))
	}

	got, err := s.GetComment(ctx, uuid.MustParse(comment.ID))
	if err != nil {
		t.Fatalf("GetComment: %v", err)
	}
	if got.ID != comment.ID {
		t.Fatalf("expected comment ID match")
	}

	updated, err := s.UpdateComment(ctx, uuid.MustParse(comment.ID), "Updated content", nil)
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
	if updated.Content != "Updated content" {
		t.Fatalf("expected updated content, got %s", updated.Content)
	}
}
