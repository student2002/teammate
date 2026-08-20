// task.go 实现任务管理的业务逻辑，包括任务创建、查询、更新、删除，
// 以及任务节点（task_nodes）的状态流转和子任务管理。
//
// 本文件是 Task 领域的 Service 层入口，依赖 Store 层提供的数据访问方法。
// 类型一律使用 internal/types 的 domain 类型；仅剩少量 db.NullTaskStatus 等
// nullable 枚举引用（未来引入 types.NullTaskStatus 后可清理）。
package service

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// TaskService 提供任务管理相关的业务逻辑。
type TaskService struct {
	svc *Service
}

// ParseDueDate 解析截止日期字符串，支持 RFC3339 和 YYYY-MM-DD 格式。
// 解析失败时返回 Valid=false 的 sql.NullTime。
// 使 handler 层无需直接导入 store 包。
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

// NewTaskService 创建一个新的 TaskService 实例。
func NewTaskService(svc *Service) *TaskService {
	return &TaskService{svc: svc}
}

// CreateTaskResult 保存创建任务操作的结果。
type CreateTaskResult struct {
	Task  types.Task       // 创建的任务
	Nodes []types.TaskNode // 根据工作流模板生成的工作流节点
}

// Create 创建一个任务并根据工作流模板生成有序节点。
// 创建后通过 SSE 事件通知代理有待认领的节点。
//
// 步骤：
//  1. 查询工作流模板的节点定义
//  2. 调用 Store 创建任务并生成工作流节点（在事务中完成）
//  3. 通过 SSE 发布 node:pending 事件通知代理
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//   - params: 创建任务的参数，包含标题、描述、优先级、工作流名称等
//   - workflowTemplateID: 工作流模板 ID
//
// 返回：
//   - *CreateTaskResult: 包含任务和生成的节点列表
//   - error: 可能的错误（模板不存在、数据库写入失败）
func (s *TaskService) Create(ctx context.Context, projectID uuid.UUID, params types.CreateTaskParams, workflowTemplateID uuid.UUID) (*CreateTaskResult, error) {
	templateNodes, err := s.svc.Store.ListTemplateNodes(ctx, workflowTemplateID)
	if err != nil {
		return nil, fmt.Errorf("list template nodes: %w", err)
	}

	task, nodes, err := s.svc.Store.CreateTask(ctx, params, templateNodes)
	if err != nil {
		return nil, fmt.Errorf("create task: %w", err)
	}

	s.publishNodePendingEvents(ctx, projectID, nodes)

	return &CreateTaskResult{Task: task, Nodes: nodes}, nil
}

// publishNodePendingEvents 为新创建的任务节点发布 node:pending 或
// node:continuation_invite SSE 事件。
//
// 步骤：
//  1. 查询项目信息以获取工作区 ID
//  2. 遍历节点，找到第一个需要处理的节点：
//     - pending 状态：发布 node:pending 事件（广播到工作区）
//     - in_progress 且有续约权：发布 node:continuation_invite 事件（定向发送）
//  3. 一条事件足以触发所有代理轮询，因此找到第一个即返回
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//   - nodes: 新创建的节点列表
func (s *TaskService) publishNodePendingEvents(ctx context.Context, projectID uuid.UUID, nodes []types.TaskNode) {
	for _, node := range nodes {
		if node.Status == types.TaskNodeStatusPending {
			s.svc.publishToProject(ctx, projectID, types.EventNodePending, map[string]interface{}{
				"task_id":    node.TaskID,
				"node_id":    node.ID,
				"project_id": projectID.String(),
			})
			return
		}
		if node.Status == types.TaskNodeStatusInProgress && node.ReservedForAgentID != nil {
			reservedID, err := uuid.Parse(*node.ReservedForAgentID)
			if err != nil {
				continue
			}
			s.svc.publishToAgent(ctx, reservedID, types.EventNodeContinuationInvite, map[string]interface{}{
				"task_id":    node.TaskID,
				"node_id":    node.ID,
				"project_id": projectID.String(),
			})
			return
		}
	}
}

// Get 根据 ID 获取任务信息。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 任务 ID
//
// 返回：
//   - types.Task: 任务信息
//   - error: 可能的错误（任务不存在）
func (s *TaskService) Get(ctx context.Context, id int32) (types.Task, error) {
	return s.svc.Store.GetTask(ctx, id)
}

// TaskWithNodes 包含任务及其工作流节点和富化元数据。
type TaskWithNodes struct {
	Task         types.Task                `json:"task"`          // 任务基本信息
	Nodes        []types.TaskNode          `json:"nodes"`         // 工作流节点列表
	WorkflowName string                   `json:"workflow_name"` // 工作流模板名称
	GitBranch    string                   `json:"git_branch"`    // 关联的 Git 分支
	NodeTokens   map[string]NodeTokenUsage `json:"node_tokens"`   // 各节点 Token 用量
}

// NodeTokenUsage 封装单个节点的 Token 用量。
type NodeTokenUsage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}

// PaginatedTaskResult 包含分页任务查询的结果。
type PaginatedTaskResult struct {
	Tasks  []TaskWithNodes `json:"tasks"`  // 当前页的任务列表
	Total  int64           `json:"total"`  // 符合条件的总任务数
	Limit  int32           `json:"limit"`  // 每页数量
	Offset int32           `json:"offset"` // 偏移量
}

// ListWithNodes 列出项目中的任务及其节点，按状态过滤。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含项目 ID 和状态过滤条件
//
// 返回：
//   - []TaskWithNodes: 任务列表（含节点和元数据）
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) ListWithNodes(ctx context.Context, params types.ListTasksParams) ([]TaskWithNodes, error) {
	tasks, err := s.svc.Store.ListTasks(ctx, params)
	if err != nil {
		return nil, err
	}
	projectID, _ := uuid.Parse(params.WorkspaceID)
	return s.enrichWithNodes(ctx, tasks, projectID)
}

// ListAllWithNodes 列出项目中的所有任务（不限状态）及其节点。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []TaskWithNodes: 任务列表（含节点和元数据）
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) ListAllWithNodes(ctx context.Context, projectID uuid.UUID) ([]TaskWithNodes, error) {
	tasks, err := s.svc.Store.ListAllTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return s.enrichWithNodes(ctx, tasks, projectID)
}

// ListTasksPaginatedWithNodes 分页查询项目中的任务及其节点，支持状态过滤和搜索。
// 不过滤历史任务，供历史任务页面使用。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//   - status: 任务状态过滤（空字符串表示不过滤状态）
//   - searchQuery: 搜索关键词（匹配标题或描述，空字符串表示不搜索）
//   - limit: 每页数量
//   - offset: 偏移量
//
// 返回：
//   - *PaginatedTaskResult: 分页结果，包含当前页任务列表和总数
//   - error: 可能的错误（数据库查询失败）
//
// 注意：本方法使用 db.NullTaskStatus 等 nullable 枚举构造 db.CountTasksByStatusParams
// 和 db.ListTasksPaginatedParams。未来引入 types.NullTaskStatus 后可统一类型。
func (s *TaskService) ListTasksPaginatedWithNodes(ctx context.Context, projectID uuid.UUID, status string, searchQuery string, limit, offset int32) (*PaginatedTaskResult, error) {
	var statuses []string
	if status != "" {
		statuses = append(statuses, status)
	}

	countParams := types.CountTasksByStatusParams{
		WorkspaceID: projectID.String(),
		Statuses:    statuses,
	}
	total, err := s.svc.Store.CountTasksByStatus(ctx, countParams)
	if err != nil {
		return nil, fmt.Errorf("count tasks: %w", err)
	}

	listParams := types.ListTasksPaginatedParams{
		WorkspaceID: projectID.String(),
		Statuses:    statuses,
		Limit:       limit,
		Offset:      offset,
	}
	tasks, err := s.svc.Store.ListTasksPaginated(ctx, listParams)
	if err != nil {
		return nil, fmt.Errorf("list tasks paginated: %w", err)
	}

	enriched, err := s.enrichWithNodes(ctx, tasks, projectID)
	if err != nil {
		return nil, fmt.Errorf("enrich with nodes: %w", err)
	}

	return &PaginatedTaskResult{
		Tasks:  enriched,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	}, nil
}

// enrichWithNodes 批量加载项目中所有任务的节点并按 task_id 分组。
// 同时为每个任务填充工作流名称和 Git 分支信息。
//
// 步骤：
//  1. 批量查询项目下所有任务的节点（避免 N+1 查询）
//  2. 按 task_id 分组到 map 中
//  3. 遍历任务列表，组装 TaskWithNodes 结构
//
// 参数：
//   - ctx: 请求上下文
//   - tasks: 任务列表
//   - projectID: 项目 ID
//
// 返回：
//   - []TaskWithNodes: 任务列表（含节点和元数据）
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) enrichWithNodes(ctx context.Context, tasks []types.Task, projectID uuid.UUID) ([]TaskWithNodes, error) {
	allNodes, err := s.svc.Store.ListTaskNodesByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}

	nodesByTask := make(map[int32][]types.TaskNode, len(tasks))
	for _, n := range allNodes {
		nodesByTask[n.TaskID] = append(nodesByTask[n.TaskID], n)
	}

	// 批量查询所有节点的 Token 用量（一次查询）
	var allNodeIDs []uuid.UUID
	for _, n := range allNodes {
		if id, err := uuid.Parse(n.ID); err == nil {
			allNodeIDs = append(allNodeIDs, id)
		}
	}
	tokenMap, err := s.svc.Store.GetTokenUsageByTaskNodes(ctx, allNodeIDs)
	if err != nil {
		return nil, err
	}

	result := make([]TaskWithNodes, 0, len(tasks))
	for _, t := range tasks {
		nodes := nodesByTask[t.ID]
		wfName := t.WorkflowName
		gitBranch := ""
		if t.GitBranch != nil {
			gitBranch = *t.GitBranch
		}

		// 构建该任务的节点 Token 用量 map
		nodeTokens := make(map[string]NodeTokenUsage, len(nodes))
		for _, n := range nodes {
			if tu, ok := tokenMap[uuid.MustParse(n.ID)]; ok && tu.TotalTokens > 0 {
				nodeTokens[n.ID] = NodeTokenUsage{
					InputTokens:  tu.InputTokens,
					OutputTokens: tu.OutputTokens,
				}
			}
		}

		result = append(result, TaskWithNodes{
			Task:         t,
			Nodes:        nodes,
			WorkflowName: wfName,
			GitBranch:    gitBranch,
			NodeTokens:   nodeTokens,
		})
	}
	return result, nil
}

// List 列出项目中的任务，按状态过滤。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含项目 ID 和状态过滤条件
//
// 返回：
//   - []types.Task: 任务列表
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) List(ctx context.Context, params types.ListTasksParams) ([]types.Task, error) {
	return s.svc.Store.ListTasks(ctx, params)
}

// ListAll 列出项目中的所有任务（不限状态）。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []types.Task: 任务列表
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) ListAll(ctx context.Context, projectID uuid.UUID) ([]types.Task, error) {
	return s.svc.Store.ListAllTasks(ctx, projectID)
}

// Update 更新任务信息。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新任务的参数，包含 ID 和要更新的字段
//
// 返回：
//   - types.Task: 更新后的任务信息
//   - error: 可能的错误（任务不存在、数据库更新失败）
func (s *TaskService) Update(ctx context.Context, params types.UpdateTaskParams) (types.Task, error) {
	return s.svc.Store.UpdateTask(ctx, params)
}

// Delete 软删除一个任务并取消其所有未完成的节点。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//
// 返回：
//   - error: 可能的错误（任务不存在、数据库删除失败）
func (s *TaskService) Delete(ctx context.Context, taskID int32) error {
	return s.svc.Store.DeleteTask(ctx, taskID)
}

// CancelTaskNodes 取消任务中所有未完成/未取消的节点。
// 将节点状态设为 cancelled，终止执行中的工作流。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//
// 返回：
//   - error: 可能的错误（数据库更新失败）
func (s *TaskService) CancelTaskNodes(ctx context.Context, taskID int32) error {
	return s.svc.Store.CancelTaskNodes(ctx, taskID)
}

// ListTaskNodes 列出指定任务的所有工作流节点。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//
// 返回：
//   - []types.TaskNode: 节点列表
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) ListTaskNodes(ctx context.Context, taskID int32) ([]types.TaskNode, error) {
	return s.svc.Store.ListTaskNodes(ctx, taskID)
}

// ListNodeTransitions 列出指定节点的所有状态转换记录。
// 转换记录用于追踪节点的完整状态变更历史。
//
// 参数：
//   - ctx: 请求上下文
//   - nodeID: 节点 ID
//
// 返回：
//   - []types.NodeTransition: 状态转换记录列表
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) ListNodeTransitions(ctx context.Context, nodeID uuid.UUID) ([]types.NodeTransition, error) {
	return s.svc.Store.ListNodeTransitions(ctx, nodeID)
}

// CreateSubtask 在父任务下创建一个子任务。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 创建子任务的参数，包含父任务 ID、标题、描述等
//
// 返回：
//   - types.Task: 创建的子任务
//   - error: 可能的错误（父任务不存在、数据库写入失败）
func (s *TaskService) CreateSubtask(ctx context.Context, params types.CreateSubtaskParams) (types.Task, error) {
	return s.svc.Store.CreateSubtask(ctx, params)
}

// UpdateTaskGitBranch 更新任务关联的 Git 分支名。
//
// 参数：
//   - ctx: 请求上下文
//   - taskID: 任务 ID
//   - gitBranch: Git 分支名
//
// 返回：
//   - error: 可能的错误（数据库更新失败）
func (s *TaskService) UpdateTaskGitBranch(ctx context.Context, taskID int32, gitBranch string) error {
	return s.svc.Store.UpdateTaskGitBranch(ctx, taskID, gitBranch)
}

// ListSubtasks 列出父任务的所有子任务。
//
// 参数：
//   - ctx: 请求上下文
//   - parentTaskID: 父任务 ID
//
// 返回：
//   - []types.Task: 子任务列表
//   - error: 可能的错误（数据库查询失败）
func (s *TaskService) ListSubtasks(ctx context.Context, parentTaskID int32) ([]types.Task, error) {
	return s.svc.Store.ListSubtasks(ctx, sql.NullInt32{Int32: parentTaskID, Valid: true})
}
