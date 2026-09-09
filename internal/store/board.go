// board.go provides query operations for board data.
//
// The board groups tasks into 4 columns by their current node status:
//   - pending (pending)
//   - in_progress (in progress)
//   - completed (completed)
//   - manual_intervention (requires manual intervention)
//
// A task's column position is determined by the status of its currently active node.
// The rejected status is uniformly mapped to the manual_intervention column.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
)

// BoardColumnTask represents a single task card within a board column.
//
// It includes the task's basic info and the status of its currently active node, used for board UI rendering.
type BoardColumnTask struct {
	ID                int32           `json:"id"`                  // task ID
	Title             string          `json:"title"`               // task title
	Priority          db.TaskPriority `json:"priority"`            // task priority
	Type              db.TaskType     `json:"type"`                // task type
	CurrentNodeName   string          `json:"current_node_name"`   // current node name
	CurrentNodeStatus string          `json:"current_node_status"` // current node status
	CurrentNodeType   string          `json:"current_node_type"`   // current node type
	AssigneeID        interface{}     `json:"assignee_id"`         // current node assignee ID
}

// BoardColumn represents a column in the board, including the column key and the task list.
type BoardColumn struct {
	Key   string            `json:"key"`   // column key (e.g. "pending", "in_progress")
	Label string            `json:"label"` // column display name (e.g. "Pending")
	Tasks []BoardColumnTask `json:"tasks"` // the task list in this column
}

// ColumnDefs defines the 4 columns of the board and their display order.
//
// The board has a fixed set of 4 columns; the rejected status is uniformly placed in the manual_intervention column.
var ColumnDefs = []struct {
	Key   string
	Label string
}{
	{"pending", "Pending"},
	{"in_progress", "In Progress"},
	{"completed", "Completed"},
	{"manual_intervention", "Manual Intervention Required"},
}

// GetBoardData queries the board data for the specified project, grouping tasks into columns by their current node status.
//
// Steps:
//  1. Query all tasks under the project
//  2. Query all workflow nodes for these tasks
//  3. For each task, find its currently active node (the first node that is not completed)
//  4. Map the node status to the corresponding board column
//  5. Build and return the 4-column structure
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - []BoardColumn: 4 board columns, each containing the tasks for the corresponding status
//   - error: error returned when the query fails
func (s *Store) GetBoardData(ctx context.Context, projectID uuid.UUID) ([]BoardColumn, error) {
	// Get the project's tasks -- query only the columns needed for board display
	taskRows, err := s.db.QueryContext(ctx, `
		SELECT id, title, type, priority, status
		FROM tasks
		WHERE project_id = $1
		ORDER BY created_at DESC
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer taskRows.Close()

	type taskRow struct {
		ID       int32
		Title    string
		Type     db.TaskType
		Priority db.TaskPriority
		Status   db.TaskStatus
	}
	tasks := make([]taskRow, 0)
	for taskRows.Next() {
		var t taskRow
		if scanErr := taskRows.Scan(
			&t.ID, &t.Title, &t.Type, &t.Priority, &t.Status,
		); scanErr != nil {
			return nil, fmt.Errorf("scan task row: %w", scanErr)
		}
		tasks = append(tasks, t)
	}
	if err := taskRows.Err(); err != nil {
		return nil, fmt.Errorf("task rows error: %w", err)
	}

	// Get all nodes for these tasks
	nodeRows, err := s.db.QueryContext(ctx, `
		SELECT tn.task_id, tn.name, tn.node_type, tn.status, tn.assignee_id, tn.sort_order
		FROM task_nodes tn
		WHERE tn.task_id IN (
			SELECT id FROM tasks WHERE project_id = $1
		)
		ORDER BY tn.sort_order
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("query nodes: %w", err)
	}
	defer nodeRows.Close()

	type nodeInfo struct {
		TaskID     int32
		Name       string
		NodeType   db.NodeType
		Status     db.TaskNodeStatus
		AssigneeID uuid.NullUUID
		SortOrder  int32
	}
	nodesByTask := make(map[int32][]nodeInfo)
	for nodeRows.Next() {
		var n nodeInfo
		if scanErr := nodeRows.Scan(
			&n.TaskID, &n.Name, &n.NodeType,
			&n.Status, &n.AssigneeID, &n.SortOrder,
		); scanErr != nil {
			return nil, fmt.Errorf("scan node row: %w", scanErr)
		}
		nodesByTask[n.TaskID] = append(nodesByTask[n.TaskID], n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, fmt.Errorf("node rows error: %w", err)
	}

	// Initialize 5 columns
	columnMap := make(map[string][]BoardColumnTask, len(ColumnDefs))
	for _, cd := range ColumnDefs {
		columnMap[cd.Key] = make([]BoardColumnTask, 0)
	}

	// Group tasks into columns by their current node status
	for _, t := range tasks {
		if t.Status == db.TaskStatusCompleted {
			columnMap["completed"] = append(columnMap["completed"], BoardColumnTask{
				ID: t.ID, Title: t.Title, Priority: t.Priority, Type: t.Type,
				CurrentNodeName: "Completed", CurrentNodeStatus: "completed", CurrentNodeType: "", AssigneeID: nil,
			})
			continue
		}
		if t.Status == db.TaskStatusCancelled {
			continue
		}

		nodes := nodesByTask[t.ID]
		if len(nodes) == 0 {
			columnMap["pending"] = append(columnMap["pending"], BoardColumnTask{
				ID: t.ID, Title: t.Title, Priority: t.Priority, Type: t.Type,
				CurrentNodeName: "", CurrentNodeStatus: "pending", CurrentNodeType: "", AssigneeID: nil,
			})
			continue
		}

		var currentNode nodeInfo
		found := false
		for _, n := range nodes {
			if n.Status != db.TaskNodeStatusCompleted {
				currentNode = n
				found = true
				break
			}
		}
		if !found {
			currentNode = nodes[len(nodes)-1]
		}

		columnKey := MapNodeStatusToColumn(currentNode.Status)

		var assigneeID interface{}
		if currentNode.AssigneeID.Valid {
			assigneeID = currentNode.AssigneeID.UUID.String()
		}

		columnMap[columnKey] = append(columnMap[columnKey], BoardColumnTask{
			ID: t.ID, Title: t.Title, Priority: t.Priority, Type: t.Type,
			CurrentNodeName: currentNode.Name, CurrentNodeStatus: string(currentNode.Status),
			CurrentNodeType: string(currentNode.NodeType), AssigneeID: assigneeID,
		})
	}

	// Build the result in the defined column order
	result := make([]BoardColumn, 0, len(ColumnDefs))
	for _, cd := range ColumnDefs {
		result = append(result, BoardColumn{
			Key:   cd.Key,
			Label: cd.Label,
			Tasks: columnMap[cd.Key],
		})
	}

	return result, nil
}

// MapNodeStatusToColumn maps a node status to a board column key; review nodes and standard nodes are merged into the same column.
//
// Parameters:
//   - status: node status
//
// Returns:
//   - string: board column key
func MapNodeStatusToColumn(status db.TaskNodeStatus) string {
	switch status {
	case db.TaskNodeStatusPending:
		return "pending"
	case db.TaskNodeStatusInProgress:
		return "in_progress"
	case db.TaskNodeStatusCompleted:
		return "completed"
	case db.TaskNodeStatusRejected:
		return "manual_intervention"
	case db.TaskNodeStatusManualIntervention:
		return "manual_intervention"
	default:
		return "pending"
	}
}
