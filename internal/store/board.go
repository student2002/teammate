// board.go 提供看板数据的查询操作。
//
// 看板将任务按当前节点状态分组到 4 个列中：
//   - pending（待处理）
//   - in_progress（进行中）
//   - completed（已完成）
//   - manual_intervention（需人工介入）
//
// 任务的列位置由其当前活跃节点的状态决定。
// rejected 状态统一映射到 manual_intervention 列。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
)

// BoardColumnTask 表示看板列中的单个任务卡片。
//
// 包含任务基本信息和当前活跃节点的状态，用于看板 UI 渲染。
type BoardColumnTask struct {
	ID                int32           `json:"id"`                  // 任务 ID
	Title             string          `json:"title"`               // 任务标题
	Priority          db.TaskPriority `json:"priority"`            // 任务优先级
	Type              db.TaskType     `json:"type"`                // 任务类型
	CurrentNodeName   string          `json:"current_node_name"`   // 当前节点名称
	CurrentNodeStatus string          `json:"current_node_status"` // 当前节点状态
	CurrentNodeType   string          `json:"current_node_type"`   // 当前节点类型
	AssigneeID        interface{}     `json:"assignee_id"`         // 当前节点分配者 ID
}

// BoardColumn 表示看板中的一列，包含列标识和任务列表。
type BoardColumn struct {
	Key   string            `json:"key"`   // 列标识（如 "pending"、"in_progress"）
	Label string            `json:"label"` // 列显示名称（如 "待处理"）
	Tasks []BoardColumnTask `json:"tasks"` // 该列中的任务列表
}

// ColumnDefs 定义看板的 4 个列及其显示顺序。
//
// 看板列固定为 4 列，rejected 状态统一归入 manual_intervention 列。
var ColumnDefs = []struct {
	Key   string
	Label string
}{
	{"pending", "待处理"},
	{"in_progress", "进行中"},
	{"completed", "已完成"},
	{"manual_intervention", "需人工介入"},
}

// GetBoardData 查询指定项目的看板数据，将任务按当前节点状态分组到各列中。
//
// 执行步骤：
//  1. 查询项目下的所有任务
//  2. 查询这些任务的所有工作流节点
//  3. 对每个任务，找到其当前活跃节点（第一个非 completed 的节点）
//  4. 根据节点状态映射到对应看板列
//  5. 构建 4 列结构返回
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - []BoardColumn: 4 个看板列，每列包含对应状态的任务
//   - error: 查询失败时返回错误
func (s *Store) GetBoardData(ctx context.Context, projectID uuid.UUID) ([]BoardColumn, error) {
	// 获取项目的任务——仅查询看板展示所需的列
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

	// 获取这些任务的所有节点
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

	// 初始化 5 列
	columnMap := make(map[string][]BoardColumnTask, len(ColumnDefs))
	for _, cd := range ColumnDefs {
		columnMap[cd.Key] = make([]BoardColumnTask, 0)
	}

	// 按任务当前节点状态将任务分组为列
	for _, t := range tasks {
		if t.Status == db.TaskStatusCompleted {
			columnMap["completed"] = append(columnMap["completed"], BoardColumnTask{
				ID: t.ID, Title: t.Title, Priority: t.Priority, Type: t.Type,
				CurrentNodeName: "已完成", CurrentNodeStatus: "completed", CurrentNodeType: "", AssigneeID: nil,
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

	// 按定义的列顺序构建结果
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

// MapNodeStatusToColumn 将节点状态映射到看板列标识，review 节点与 standard 节点合并到相同列。
//
// 参数：
//   - status: 节点状态
//
// 返回：
//   - string: 看板列标识
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
