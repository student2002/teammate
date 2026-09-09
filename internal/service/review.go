// review.go implements the business logic for code review, including review queue management and self-review detection.
//
// This file contains:
//   - ReviewService struct: code review service, encapsulating the review queue and self-review detection
//   - ReviewQueueItem struct: review queue entry, containing task, node, and reviewer information
//   - GetReviewQueue: retrieves the review queue for a specified project, returning pending and in_progress review nodes
//   - SelfReviewCheckResult struct: self-review check result, containing author, reviewer, and risk level
//   - CheckSelfReview: checks whether a review node is a self-review, comparing whether the reviewer and the preceding node's author are the same agent
//
// The review queue lists all pending and in-progress review nodes in a project, ordered by creation time ascending.
// Self-review detection is used to prevent an agent from reviewing its own code, ensuring the independence and objectivity of code review.
// Self-review is considered a high-risk behavior and must be avoided at the system level.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReviewService provides the business logic for code review.
type ReviewService struct {
	svc *Service
}

// NewReviewService creates a new ReviewService instance.
func NewReviewService(svc *Service) *ReviewService {
	return &ReviewService{svc: svc}
}

// ReviewQueueItem represents an item in the review queue, containing task information, node information, and the assigned reviewer.
type ReviewQueueItem struct {
	TaskID       int32     `json:"task_id"`        // associated task ID
	TaskTitle    string    `json:"task_title"`     // task title
	NodeID       string    `json:"node_id"`        // node ID
	NodeName     string    `json:"node_name"`      // node name
	NodeStatus   string    `json:"node_status"`    // node status (pending/in_progress)
	AssigneeType string    `json:"assignee_type"`  // assignee type (specific_agent/human)
	AssigneeID   *string   `json:"assignee_id"`    // assignee ID
	AgentName    *string   `json:"agent_name"`     // name of the assigned agent
	CreatedAt    time.Time `json:"created_at"`     // creation time
	UpdatedAt    time.Time `json:"updated_at"`     // update time
}

// GetReviewQueue retrieves the review queue for the specified project, returning all pending and in_progress review nodes.
// Ordered by creation time ascending, with the earliest reviews first.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - []ReviewQueueItem: review queue list
//   - error: possible error (database query failure)
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

// SelfReviewCheckResult holds the result of a self-review check.
type SelfReviewCheckResult struct {
	IsSelfReview bool   `json:"is_self_review"` // whether it is a self-review
	AuthorID     string `json:"author_id"`      // code author ID
	ReviewerID   string `json:"reviewer_id"`    // reviewer ID
	RiskLevel    string `json:"risk_level"`     // risk level (none/high)
}

// CheckSelfReview checks whether a review node is a self-review.
// It compares whether the reviewer and the assignee of the preceding node (code author) are the same agent.
// Self-review is a high-risk behavior and should be avoided.
//
// Steps:
//  1. Get the assignee of the review node (reviewer)
//  2. Get the assignee of the preceding node (code author), looking up the nearest node sorted earlier (compatible with 0- or 1-based numbering)
//  3. Compare whether the two are the same agent
//  4. Return the check result, including the risk level
//
// Parameters:
//   - ctx: request context
//   - taskID: task ID
//   - nodeID: review node ID
//
// Returns:
//   - *SelfReviewCheckResult: self-review check result
//   - error: possible error (node does not exist)
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
