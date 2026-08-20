// task.go 提供任务和工作流节点的数据访问操作。
//
// 任务（Task）是项目中的工作单元，由工作流模板实例化为有序的工作流节点。
// 本文件包含任务的 CRUD、节点查询、子任务管理、截止日期解析等功能。
//
// 任务创建流程：
//  1. 在事务中创建任务记录
//  2. 设置 sequence（默认为任务 ID）
//  3. 获取项目的 max_review_cycles 配置
//  4. 遍历模板节点创建 task_nodes
//  5. 更新 depends_on 依赖关系
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateTask 在事务中创建任务并根据模板节点实例化工作流节点。
//
// 执行步骤：
//  1. 创建任务记录
//  2. 设置 sequence（默认为任务 ID）
//  3. 获取项目的 max_review_cycles 配置
//  4. 遍历模板节点创建 task_nodes（首节点根据分配类型自动启动）
//  5. 更新 depends_on 依赖关系
//
// 首节点自动启动规则：
//   - specific_agent: 标记为 in_progress，保留给该 Agent
//   - human: 直接标记为 completed
//   - auto: 标记为 in_progress
//   - any_agent: 保持 pending，等待 Agent 认领
//
// 参数：
//   - ctx: 请求上下文
//   - params: 任务创建参数
//   - templateNodes: 工作流模板节点列表
//
// 返回：
//   - types.Task: 创建的任务记录
//   - []types.TaskNode: 创建的工作流节点列表
//   - error: 创建失败时返回错误
func (s *Store) CreateTask(ctx context.Context, params types.CreateTaskParams, templateNodes []types.WorkflowTemplateNode) (types.Task, []types.TaskNode, error) {
	dbParams, err := FromDomainCreateTaskParams(params)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("convert create task params: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)

	task, err := qtx.CreateTask(ctx, dbParams)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("create task: %w", err)
	}

	// 如果未显式提供 sequence，则设置 sequence = id（默认为 0）
	if dbParams.Sequence == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE tasks SET sequence = $1 WHERE id = $1`, task.ID)
		if err != nil {
			return types.Task{}, nil, fmt.Errorf("set task sequence: %w", err)
		}
		task.Sequence = task.ID
	}

	// 获取项目的 max_review_cycles 以传播到任务节点
	project, err := qtx.GetProject(ctx, dbParams.ProjectID)
	if err != nil {
		tx.Rollback()
		return types.Task{}, nil, fmt.Errorf("get project: %w", err)
	}

	dbTaskNodes := make([]db.TaskNode, 0, len(templateNodes))

	// 构建模板节点 ID 到任务节点 ID 的映射，用于解析 depends_on。
	// 我们按 sort_order 创建节点，因此先收集 ID，再更新 depends_on。
	templateToTaskNodeID := make(map[uuid.UUID]uuid.UUID, len(templateNodes))

	for i, tn := range templateNodes {
		status := db.TaskNodeStatusPending
		assigneeType := db.AssigneeType(tn.AssigneeType)
		var assigneeID uuid.NullUUID
		if tn.AssigneeID != nil {
			if u, err := uuid.Parse(*tn.AssigneeID); err == nil {
				assigneeID = uuid.NullUUID{UUID: u, Valid: true}
			}
		}

		// 自动启动第一个节点（索引 0，无论 sort_order 值如何）
		if i == 0 {
			if assigneeType == db.AssigneeTypeSpecificAgent && assigneeID.Valid {
				// 指定 Agent：标记为 in_progress 并为该 Agent 保留
				status = db.TaskNodeStatusInProgress
			} else if assigneeType == db.AssigneeTypeHuman {
				// 人工步骤：作为第一个节点自动完成
				status = db.TaskNodeStatusCompleted
			} else if assigneeType == db.AssigneeTypeAuto {
				// 自动分配人：标记为 in_progress 以便系统自动启动
				status = db.TaskNodeStatusInProgress
			}
			// 对于 "any_agent" 分配类型，保持为 pending 以便 Agent 可以认领
		}

		// 若模板节点设置了 MaxRejectCycles 则使用它，否则回退到项目的 MaxReviewCycles
		maxRejectCycles := int32(tn.MaxRejectCycles)
		if maxRejectCycles == 0 {
			maxRejectCycles = project.MaxReviewCycles
		}

		dbNodeParams, err := FromDomainCreateTaskNodeParams(types.CreateTaskNodeParams{
			TaskID:             task.ID,
			Name:               tn.Name,
			Description:        nullStringToPtr(sql.NullString{String: tn.Description, Valid: tn.Description != ""}),
			SortOrder:          int32(tn.SortOrder),
			NodeType:           string(tn.NodeType),
			Status:             string(status),
			AssigneeType:       string(assigneeType),
			AssigneeID:         nullUUIDToString(assigneeID),
			ReservedForAgentID: nullUUIDToString(assigneeID),
			MaxRejectCycles:    maxRejectCycles,
			TimeoutMinutes:     int32(tn.TimeoutMinutes),
			ReadonlyDirs:       tn.ReadonlyDirs,
			FullControlDirs:    tn.FullControlDirs,
			DependsOn:          []string{}, // 占位符，稍后更新
		})
		if err != nil {
			return types.Task{}, nil, fmt.Errorf("convert create task node params: %w", err)
		}
		taskNode, err := qtx.CreateTaskNode(ctx, dbNodeParams)
		if err != nil {
			return types.Task{}, nil, fmt.Errorf("create task node: %w", err)
		}
		dbTaskNodes = append(dbTaskNodes, taskNode)
		tplID, _ := uuid.Parse(tn.ID)
		templateToTaskNodeID[tplID] = taskNode.ID
	}

	// 现在更新每个任务节点的 depends_on，将模板节点 ID 映射到任务节点 ID
	for i, tn := range templateNodes {
		if len(tn.DependsOn) == 0 {
			continue
		}
		// domain DependsOn 是 []string，需解析为 []uuid.UUID
		resolvedDeps := make([]uuid.UUID, 0, len(tn.DependsOn))
		for _, depTemplateIDStr := range tn.DependsOn {
			depTemplateID, err := uuid.Parse(depTemplateIDStr)
			if err != nil {
				continue
			}
			if taskNodeID, ok := templateToTaskNodeID[depTemplateID]; ok {
				resolvedDeps = append(resolvedDeps, taskNodeID)
			}
		}
		if len(resolvedDeps) > 0 {
			_, err := tx.ExecContext(ctx,
				`UPDATE task_nodes SET depends_on = $1 WHERE id = $2`,
				pq.Array(resolvedDeps), dbTaskNodes[i].ID)
			if err != nil {
				return types.Task{}, nil, fmt.Errorf("update task node depends_on: %w", err)
			}
			dbTaskNodes[i].DependsOn = resolvedDeps
		}
	}

	if err := tx.Commit(); err != nil {
		return types.Task{}, nil, fmt.Errorf("commit tx: %w", err)
	}

	domainTask, err := ToDomainTask(task)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("convert task to domain: %w", err)
	}
	domainNodes, err := ToDomainTaskNodeSlice(dbTaskNodes)
	if err != nil {
		return types.Task{}, nil, fmt.Errorf("convert task nodes to domain: %w", err)
	}
	return domainTask, domainNodes, nil
}

// GetTask 根据 ID 查询单个任务记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 任务的整数 ID
//
// 返回：
//   - types.Task: 任务记录
//   - error: 查询失败时返回错误
func (s *Store) GetTask(ctx context.Context, id int32) (types.Task, error) {
	task, err := s.q.GetTask(ctx, id)
	if err != nil {
		return types.Task{}, fmt.Errorf("get task: %w", err)
	}
	return ToDomainTask(task)
}

// ListTasks 分页查询指定项目内的任务列表。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 分页查询参数
//
// 返回：
//   - []types.Task: 任务列表
//   - error: 查询失败时返回错误
func (s *Store) ListTasks(ctx context.Context, params types.ListTasksParams) ([]types.Task, error) {
	dbParams, err := FromDomainListTasksParams(params)
	if err != nil {
		return nil, fmt.Errorf("convert list tasks params: %w", err)
	}
	tasks, err := s.q.ListTasks(ctx, dbParams)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// ListAllTasks 查询指定项目内的所有任务（不分页）。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - []types.Task: 任务列表
//   - error: 查询失败时返回错误
func (s *Store) ListAllTasks(ctx context.Context, projectID uuid.UUID) ([]types.Task, error) {
	tasks, err := s.q.ListAllTasks(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list all tasks: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// UpdateTask 更新任务的基本信息。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含任务 ID 和要更新的字段
//
// 返回：
//   - types.Task: 更新后的任务记录
//   - error: 更新失败时返回错误
func (s *Store) UpdateTask(ctx context.Context, params types.UpdateTaskParams) (types.Task, error) {
	dbParams, err := FromDomainUpdateTaskParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert update task params: %w", err)
	}
	task, err := s.q.UpdateTask(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("update task: %w", err)
	}
	return ToDomainTask(task)
}

// DeleteTask 软删除任务（将状态设为 cancelled），在事务中同时重置 in_progress 节点为 pending。
//
// 执行步骤：
//  1. 将任务状态设为 cancelled
//  2. 将 in_progress 状态的节点重置为 pending，并清除 assignee_id
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的整数 ID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteTask(ctx context.Context, taskID int32) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 将任务状态设置为已取消
	if _, err := tx.ExecContext(ctx,
		`UPDATE tasks SET status = 'cancelled', updated_at = NOW() WHERE id = $1`, taskID); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}

	// 将进行中的节点重置为待处理（它们将被已取消的任务状态隐藏）
	if _, err := tx.ExecContext(ctx,
		`UPDATE task_nodes SET status = 'pending', assignee_id = NULL, updated_at = NOW()
		 WHERE task_id = $1 AND status = 'in_progress'`, taskID); err != nil {
		return fmt.Errorf("reset task nodes on delete: %w", err)
	}

	return tx.Commit()
}

// CancelTaskNodes 将指定任务中所有 in_progress 状态的节点重置为 pending。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的整数 ID
//
// 返回：
//   - error: 更新失败时返回错误
func (s *Store) CancelTaskNodes(ctx context.Context, taskID int32) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE task_nodes SET status = 'pending', assignee_id = NULL, updated_at = NOW()
		 WHERE task_id = $1 AND status = 'in_progress'`, taskID)
	if err != nil {
		return fmt.Errorf("cancel task nodes: %w", err)
	}
	return nil
}

// ListTaskNodes 查询指定任务的所有工作流节点。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的整数 ID
//
// 返回：
//   - []types.TaskNode: 工作流节点列表
//   - error: 查询失败时返回错误
func (s *Store) ListTaskNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	nodes, err := s.q.ListTaskNodes(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes: %w", err)
	}
	return ToDomainTaskNodeSlice(nodes)
}

// ListTaskNodesByProject 查询指定项目内所有任务的全部工作流节点。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - []types.TaskNode: 工作流节点列表
//   - error: 查询失败时返回错误
func (s *Store) ListTaskNodesByProject(ctx context.Context, projectID uuid.UUID) ([]types.TaskNode, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT tn.id, tn.task_id, tn.name, tn.description, tn.sort_order,
		       tn.node_type, tn.status, tn.assignee_type, tn.assignee_id, tn.reserved_for_agent_id,
		       tn.reject_count, tn.max_reject_cycles, tn.timeout_minutes, tn.version,
		       tn.completed_at, tn.completed_by, tn.summary, tn.previous_summary,
		       tn.reservation_expires_at, tn.created_at, tn.updated_at, tn.depends_on
		FROM task_nodes tn
		WHERE tn.task_id IN (SELECT id FROM tasks WHERE project_id = $1)
		ORDER BY tn.sort_order
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list task nodes by project: %w", err)
	}
	defer rows.Close()

	var dbNodes []db.TaskNode
	for rows.Next() {
		var n db.TaskNode
		if scanErr := rows.Scan(
			&n.ID, &n.TaskID, &n.Name, &n.Description, &n.SortOrder,
			&n.NodeType, &n.Status, &n.AssigneeType, &n.AssigneeID, &n.ReservedForAgentID,
			&n.RejectCount, &n.MaxRejectCycles, &n.TimeoutMinutes, &n.Version,
			&n.CompletedAt, &n.CompletedBy, &n.Summary, &n.PreviousSummary,
			&n.ReservationExpiresAt, &n.CreatedAt, &n.UpdatedAt, pq.Array(&n.DependsOn),
		); scanErr != nil {
			return nil, fmt.Errorf("scan task node: %w", scanErr)
		}
		dbNodes = append(dbNodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("task node rows error: %w", err)
	}
	return ToDomainTaskNodeSlice(dbNodes)
}

// ListNodeTransitions 查询指定节点的所有状态流转记录。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点的 UUID
//
// 返回：
//   - []types.NodeTransition: 流转记录列表
//   - error: 查询失败时返回错误
func (s *Store) ListNodeTransitions(ctx context.Context, nodeID uuid.UUID) ([]types.NodeTransition, error) {
	transitions, err := s.q.ListNodeTransitions(ctx, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node transitions: %w", err)
	}
	return ToDomainNodeTransitionSlice(transitions)
}

// CreateSubtask 创建子任务记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 子任务创建参数
//
// 返回：
//   - types.Task: 创建的子任务记录
//   - error: 创建失败时返回错误
func (s *Store) CreateSubtask(ctx context.Context, params types.CreateSubtaskParams) (types.Task, error) {
	dbParams, err := FromDomainCreateSubtaskParams(params)
	if err != nil {
		return types.Task{}, fmt.Errorf("convert create subtask params: %w", err)
	}
	task, err := s.q.CreateSubtask(ctx, dbParams)
	if err != nil {
		return types.Task{}, fmt.Errorf("create subtask: %w", err)
	}

	// 如果未显式提供 sequence，则设置 sequence = id（默认为 0）
	if dbParams.Sequence == 0 {
		_, err = s.db.ExecContext(ctx, `UPDATE tasks SET sequence = $1 WHERE id = $1`, task.ID)
		if err != nil {
			return types.Task{}, fmt.Errorf("set subtask sequence: %w", err)
		}
		task.Sequence = task.ID
	}

	return ToDomainTask(task)
}

// ListSubtasks 查询指定父任务的所有子任务。
//
// 参数：
//   - ctx: 请求上下文
//   - parentTaskID: 父任务 ID（可为空）
//
// 返回：
//   - []types.Task: 子任务列表
//   - error: 查询失败时返回错误
func (s *Store) ListSubtasks(ctx context.Context, parentTaskID sql.NullInt32) ([]types.Task, error) {
	tasks, err := s.q.ListSubtasks(ctx, parentTaskID)
	if err != nil {
		return nil, fmt.Errorf("list subtasks: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// ParseDueDate 将日期字符串解析为 sql.NullTime，支持 RFC3339 和 YYYY-MM-DD 格式。
//
// 参数：
//   - dateStr: 日期字符串指针（可为 nil）
//
// 返回：
//   - sql.NullTime: 解析后的时间，解析失败时返回 Valid=false
func ParseDueDate(dateStr *string) sql.NullTime {
	if dateStr == nil || *dateStr == "" {
		return sql.NullTime{}
	}
	parsed, err := time.Parse(time.RFC3339, *dateStr)
	if err != nil {
		parsed, err = time.Parse("2006-01-02", *dateStr)
	}
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: parsed, Valid: true}
}

// UpdateTaskGitBranch 更新任务关联的 Git 分支名。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务的整数 ID
//   - gitBranch: Git 分支名
//
// 返回：
//   - error: 更新失败时返回错误
func (s *Store) UpdateTaskGitBranch(ctx context.Context, taskID int32, gitBranch string) error {
	if err := s.q.UpdateTaskGitBranch(ctx, db.UpdateTaskGitBranchParams{
		ID:        taskID,
		GitBranch: sql.NullString{String: gitBranch, Valid: gitBranch != ""},
	}); err != nil {
		return fmt.Errorf("update task git branch: %w", err)
	}
	return nil
}

// ListTasksPaginated 查询指定项目内的任务（分页 + 搜索），不过滤历史任务。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 分页查询参数，包含项目 ID、状态过滤、搜索关键词、limit 和 offset
//
// 返回：
//   - []types.Task: 当前页的任务列表
//   - error: 查询失败时返回错误
func (s *Store) ListTasksPaginated(ctx context.Context, params types.ListTasksPaginatedParams) ([]types.Task, error) {
	dbParams, err := FromDomainListTasksPaginatedParams(params)
	if err != nil {
		return nil, fmt.Errorf("convert list tasks paginated params: %w", err)
	}
	tasks, err := s.q.ListTasksPaginated(ctx, dbParams)
	if err != nil {
		return nil, fmt.Errorf("list tasks paginated: %w", err)
	}
	return ToDomainTaskSlice(tasks)
}

// CountTasksByStatus 统计指定项目和状态下的任务数量（支持搜索）。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 计数参数，包含项目 ID、状态过滤和搜索关键词
//
// 返回：
//   - int64: 符合条件的任务总数
//   - error: 查询失败时返回错误
func (s *Store) CountTasksByStatus(ctx context.Context, params types.CountTasksByStatusParams) (int64, error) {
	dbParams, err := FromDomainCountTasksByStatusParams(params)
	if err != nil {
		return 0, fmt.Errorf("convert count tasks by status params: %w", err)
	}
	count, err := s.q.CountTasksByStatus(ctx, dbParams)
	if err != nil {
		return 0, fmt.Errorf("count tasks by status: %w", err)
	}
	return count, nil
}
