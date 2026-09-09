// board.go implements the business logic for board data, used to retrieve a project's board columns and their node data.
//
// This file contains:
//   - BoardService struct: the board service, encapsulating query operations for board data
//   - GetBoardData: retrieves the board data for a specified project, returning a list of board columns containing nodes
//
// Board columns are grouped by the node's real state (5 kinds):
//   - pending: pending nodes
//   - in_progress: in-progress nodes
//   - completed: completed nodes
//   - rejected: rejected nodes
//   - manual_intervention: nodes requiring manual intervention
//
// Each column contains the list of workflow nodes in the corresponding state, for the frontend to render the board view.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
)

// BoardService provides the board-related business logic.
type BoardService struct {
	svc *Service
}

// BoardColumnTask is a type alias for a board column task so the handler layer does not need to import the store package directly.
type BoardColumnTask = store.BoardColumnTask

// BoardColumn is a type alias for a board column so the handler layer does not need to import the store package directly.
type BoardColumn = store.BoardColumn

// NewBoardService creates a new BoardService instance.
func NewBoardService(svc *Service) *BoardService {
	return &BoardService{svc: svc}
}

// GetBoardData retrieves the board data for the specified project, returning a list of board columns containing nodes.
//
// Steps:
//  1. Query the workflow nodes of all tasks under the project
//  2. Group by the node's real state (5 kinds: pending/in_progress/completed/rejected/manual_intervention)
//  3. Assemble into board column structures, each column containing the state name and its node list
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID, used to filter all nodes under the project
//
// Returns:
//   - []store.BoardColumn: list of board columns, each column containing the column name and node list
//   - error: possible errors (database query failure)
func (s *BoardService) GetBoardData(ctx context.Context, projectID uuid.UUID) ([]store.BoardColumn, error) {
	columns, err := s.svc.Store.GetBoardData(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("get board data: %w", err)
	}
	return columns, nil
}
