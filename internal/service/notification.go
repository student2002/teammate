// notification.go implements the business logic for notifications, aggregating manual-intervention nodes and mention comments within a workspace.
//
// This file contains:
//   - NotificationService struct: the notification management service, aggregating multiple types of notifications
//   - NotificationItem struct: a notification entry, including type, title, description, time, and the associated task ID
//   - ListNotifications: lists notifications for the specified workspace and member, merging two types of notifications: manual intervention and mention
//
// Notification types include:
//   - manual_intervention: workflow nodes that require human handling
//   - mention: members who are @mentioned in comments
//
// The two types of notifications are merged and returned for the frontend to render the notification center uniformly.
// Manual-intervention notifications target all members of the workspace, while mention notifications only target the specific member who was @mentioned.
package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NotificationService provides the business logic for notification management.
type NotificationService struct {
	svc *Service
}

// NewNotificationService creates a new NotificationService instance.
func NewNotificationService(svc *Service) *NotificationService {
	return &NotificationService{svc: svc}
}

// NotificationItem represents a notification, including the notification type (manual_intervention or mention),
// title, description, creation time, and the associated task ID.
type NotificationItem struct {
	ID          string    `json:"id"`           // notification unique identifier
	Type        string    `json:"type"`         // notification type: manual_intervention or mention
	Title       string    `json:"title"`        // notification title (usually the task title)
	Description string    `json:"description"`  // notification description (node name or comment content)
	CreatedAt   time.Time `json:"created_at"`   // notification creation time
	TaskID      int32     `json:"task_id"`      // associated task ID
}

// ListNotifications lists notifications for the specified workspace and member, including manual-intervention nodes and mention comments.
// The two types of notifications are merged and returned for the frontend to render the notification center uniformly.
//
// Steps:
//  1. Query all nodes in the workspace that are in the manual_intervention state, and generate manual-intervention notifications
//  2. If a member ID is specified, query the comments where the member was @mentioned, and generate mention notifications
//  3. Merge the two types of notifications and return
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID, used to isolate notifications
//   - memberID: member ID, used to query mention comments (passing uuid.Nil skips the mention query)
//
// Returns:
//   - []NotificationItem: notification list
//   - error: possible errors (database query failure)
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
