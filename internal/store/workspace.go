// workspace.go 提供工作区和成员管理的数据访问操作。
//
// 工作区（Workspace）是团队的顶级组织单元，包含项目、Agent、成员。
// 每个成员在工作区中有角色（owner/admin/member/viewer）。
//
// 新工作区创建时自动种子里建 5 个内置工作流模板：
//   - 标准开发流程（7 节点）
//   - 快速修复流程（3 节点）
//   - 纯审查流程（1 节点）
//   - 文档编写流程（3 节点）
//   - 数据处理流程（4 节点）
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateWorkspace 创建工作区并种子里建的 5 个内置工作流模板。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 工作区创建参数，包含名称、描述、Issue 前缀等
//
// 返回：
//   - types.Workspace: 创建的工作区记录
//   - error: 创建失败时返回错误
func (s *Store) CreateWorkspace(ctx context.Context, params types.CreateWorkspaceParams) (types.Workspace, error) {
	dbParams, err := FromDomainCreateWorkspaceParams(params)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("convert create workspace params: %w", err)
	}
	ws, err := s.q.CreateWorkspace(ctx, dbParams)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if err := s.SeedBuiltinTemplates(ctx, ws.ID); err != nil {
		slog.Warn("workspace created but builtin templates failed", "workspace_id", ws.ID, "err", err)
	}
	return ToDomainWorkspace(ws)
}

// CreateWorkspaceForMember 创建工作区并在同一事务内把创建者加入为 owner。
func (s *Store) CreateWorkspaceForMember(ctx context.Context, memberID uuid.UUID, params types.CreateWorkspaceParams) (types.Workspace, error) {
	dbParams, err := FromDomainCreateWorkspaceParams(params)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("convert create workspace params: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("begin create workspace transaction: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)
	ws, err := qtx.CreateWorkspace(ctx, dbParams)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if _, err := qtx.CreateWorkspaceMember(ctx, db.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    memberID,
		Role:        "owner",
	}); err != nil {
		return types.Workspace{}, fmt.Errorf("create workspace owner membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return types.Workspace{}, fmt.Errorf("commit create workspace transaction: %w", err)
	}

	if err := s.SeedBuiltinTemplates(ctx, ws.ID); err != nil {
		slog.Warn("workspace created but builtin templates failed", "workspace_id", ws.ID, "err", err)
	}
	return ToDomainWorkspace(ws)
}

// CreateWorkspaceWithOwnerInTx 在单个事务中创建工作区并将指定成员添加为 owner。
// 与 CreateWorkspaceForMember 的区别：不种子内置模板（由调用方控制），
// 用于 OAuth 流程中需要自定义种子行为的场景。
func (s *Store) CreateWorkspaceWithOwnerInTx(ctx context.Context, memberID uuid.UUID, params types.CreateWorkspaceParams) (types.Workspace, error) {
	dbParams, err := FromDomainCreateWorkspaceParams(params)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("convert create workspace params: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("begin create workspace tx: %w", err)
	}
	defer tx.Rollback()

	qtx := s.q.WithTx(tx)
	ws, err := qtx.CreateWorkspace(ctx, dbParams)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("create workspace: %w", err)
	}
	if _, err := qtx.CreateWorkspaceMember(ctx, db.CreateWorkspaceMemberParams{
		WorkspaceID: ws.ID,
		MemberID:    memberID,
		Role:        "owner",
	}); err != nil {
		return types.Workspace{}, fmt.Errorf("create workspace owner membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return types.Workspace{}, fmt.Errorf("commit create workspace tx: %w", err)
	}
	return ToDomainWorkspace(ws)
}

// GetWorkspace 根据 ID 查询单个工作区记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 工作区的 UUID
//
// 返回：
//   - db.Workspace: 工作区记录
//   - error: 查询失败时返回错误
func (s *Store) GetWorkspace(ctx context.Context, id uuid.UUID) (types.Workspace, error) {
	ws, err := s.q.GetWorkspace(ctx, id)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return ToDomainWorkspace(ws)
}

// ListWorkspaces 查询所有工作区记录。
//
// 参数：
//   - ctx: 请求上下文
//
// 返回：
//   - []db.Workspace: 工作区列表
//   - error: 查询失败时返回错误
func (s *Store) ListWorkspaces(ctx context.Context) ([]types.Workspace, error) {
	wss, err := s.q.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return ToDomainWorkspaceSlice(wss)
}

// ListWorkspacesByMemberID 查询成员所属的工作区列表。
func (s *Store) ListWorkspacesByMemberID(ctx context.Context, memberID uuid.UUID) ([]types.Workspace, error) {
	wss, err := s.q.ListWorkspacesByMemberID(ctx, memberID)
	if err != nil {
		return nil, fmt.Errorf("list member workspaces: %w", err)
	}
	return ToDomainWorkspaceSlice(wss)
}

// UpdateWorkspace 更新工作区的基本信息。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含工作区 ID 和要更新的字段
//
// 返回：
//   - db.Workspace: 更新后的工作区记录
//   - error: 更新失败时返回错误
func (s *Store) UpdateWorkspace(ctx context.Context, params types.UpdateWorkspaceParams) (types.Workspace, error) {
	dbParams, err := FromDomainUpdateWorkspaceParams(params)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("convert update workspace params: %w", err)
	}
	ws, err := s.q.UpdateWorkspace(ctx, dbParams)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("update workspace: %w", err)
	}
	return ToDomainWorkspace(ws)
}

// DeleteWorkspace 根据 ID 删除工作区记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 工作区的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteWorkspace(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteWorkspace(ctx, id); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}

// CreateMember 创建一个新成员记录。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 成员创建参数，包含名称、邮箱等
//
// 返回：
//   - db.Member: 创建的成员记录
//   - error: 创建失败时返回错误
func (s *Store) CreateMember(ctx context.Context, params types.CreateMemberParams) (types.Member, error) {
	dbParams, err := FromDomainCreateMemberParams(params)
	if err != nil {
		return types.Member{}, fmt.Errorf("convert create member params: %w", err)
	}
	m, err := s.q.CreateMember(ctx, dbParams)
	if err != nil {
		return types.Member{}, fmt.Errorf("create member: %w", err)
	}
	return ToDomainMember(m)
}

// ListMembersByWorkspace 查询指定工作区内的所有成员（含角色信息）。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []db.ListMembersByWorkspaceRow: 成员列表（包含角色）
//   - error: 查询失败时返回错误
func (s *Store) ListMembersByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.ListMembersByWorkspaceRow, error) {
	members, err := s.q.ListMembersByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return ToDomainListMembersByWorkspaceRowSlice(members)
}

// GetMemberByEmail 通过邮箱查询成员记录。
//
// 参数：
//   - ctx: 请求上下文
//   - email: 成员邮箱
//
// 返回：
//   - db.Member: 成员记录
//   - error: 查询失败时返回错误
func (s *Store) GetMemberByEmail(ctx context.Context, email string) (types.Member, error) {
	m, err := s.q.GetMemberByEmail(ctx, email)
	if err != nil {
		return types.Member{}, fmt.Errorf("get member by email: %w", err)
	}
	return ToDomainMember(m)
}

// GetFirstWorkspaceForMember 获取成员所属的第一个工作区成员关系（按创建时间排序）。
// 用于 OAuth 登录时查找成员已有工作区。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员 ID
//
// 返回：
//   - db.WorkspaceMember: 工作区成员关系记录
//   - error: 查询失败时返回错误
func (s *Store) GetFirstWorkspaceForMember(ctx context.Context, memberID uuid.UUID) (types.WorkspaceMember, error) {
	wm, err := s.q.GetFirstWorkspaceForMember(ctx, memberID)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("get first workspace for member: %w", err)
	}
	return ToDomainWorkspaceMember(wm)
}

// UpdateMemberPasswordHash 更新成员的密码哈希。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含成员 ID 和新的密码哈希
//
// 返回：
//   - error: 更新失败时返回错误
func (s *Store) UpdateMemberPasswordHash(ctx context.Context, params types.UpdateMemberPasswordHashParams) error {
	id, err := stringToUUID(params.ID)
	if err != nil {
		return fmt.Errorf("convert member id: %w", err)
	}
	if err := s.q.UpdateMemberPasswordHash(ctx, db.UpdateMemberPasswordHashParams{
		ID:           id,
		PasswordHash: params.PasswordHash,
	}); err != nil {
		return fmt.Errorf("update member password hash: %w", err)
	}
	return nil
}

// UpdateMemberRole 更新成员在工作区中的角色。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 更新参数，包含工作区 ID、成员 ID 和新角色
//
// 返回：
//   - db.WorkspaceMember: 更新后的工作区成员记录
//   - error: 更新失败时返回错误
func (s *Store) UpdateMemberRole(ctx context.Context, params types.UpdateMemberRoleParams) (types.WorkspaceMember, error) {
	dbParams, err := FromDomainUpdateMemberRoleParams(params)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("convert update member role params: %w", err)
	}
	wm, err := s.q.UpdateMemberRole(ctx, dbParams)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("update member role: %w", err)
	}
	return ToDomainWorkspaceMember(wm)
}

// DeleteMember 根据 ID 删除成员记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 成员的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteMember(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// 002_remove_fks 后以下列不再有外键，须显式置空（FK 策略：应用层保证完整性）：
	//   git_credentials.created_by、agent_permissions.granted_by、invitations.invited_by
	if _, err := tx.ExecContext(ctx,
		`UPDATE git_credentials SET created_by = NULL WHERE created_by = $1`, id); err != nil {
		return fmt.Errorf("clear git_credentials.created_by: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE agent_permissions SET granted_by = NULL WHERE granted_by = $1`, id); err != nil {
		return fmt.Errorf("clear agent_permissions.granted_by: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE invitations SET invited_by = NULL WHERE invited_by = $1`, id); err != nil {
		return fmt.Errorf("clear invitations.invited_by: %w", err)
	}

	if err := s.q.WithTx(tx).DeleteMember(ctx, id); err != nil {
		return fmt.Errorf("delete member: %w", err)
	}

	return tx.Commit()
}

// GetMember 根据 ID 查询单个成员记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 成员的 UUID
//
// 返回：
//   - db.Member: 成员记录
//   - error: 查询失败时返回错误
func (s *Store) GetMember(ctx context.Context, id uuid.UUID) (types.Member, error) {
	m, err := s.q.GetMember(ctx, id)
	if err != nil {
		return types.Member{}, fmt.Errorf("get member: %w", err)
	}
	return ToDomainMember(m)
}

// GetWorkspaceOwner 查询指定工作区的 owner 成员。
//
// 遍历所有成员，返回角色为 "owner" 的成员。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - db.ListMembersByWorkspaceRow: owner 成员记录
//   - error: 查询失败或无 owner 时返回错误
func (s *Store) GetWorkspaceOwner(ctx context.Context, workspaceID uuid.UUID) (types.ListMembersByWorkspaceRow, error) {
	members, err := s.q.ListMembersByWorkspace(ctx, workspaceID)
	if err != nil {
		return types.ListMembersByWorkspaceRow{}, fmt.Errorf("list members: %w", err)
	}
	for _, m := range members {
		if m.WorkspaceRole == "owner" {
			return ToDomainListMembersByWorkspaceRow(m)
		}
	}
	return types.ListMembersByWorkspaceRow{}, fmt.Errorf("no owner found for workspace %s", workspaceID)
}

// SeedBuiltinTemplates 为新工作区创建 5 个内置工作流模板。
//
// 每个模板使用独立事务创建以避免并发死锁，单个模板/节点失败不影响后续模板创建。
//
// 内置模板：
//  1. 标准开发流程（7 节点）：需求分析 → 技术设计 → 编码实现 → 自测验证 → 代码审查 → 集成测试 → 部署上线
//  2. 快速修复流程（3 节点）：问题定位 → 修复编码 → 代码审查
//  3. 纯审查流程（1 节点）：代码审查
//  4. 文档编写流程（3 节点）：资料收集 → 文档撰写 → 文档审校
//  5. 数据处理流程（4 节点）：数据采集 → 数据清洗 → 数据分析 → 结果验证
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - error: 所有模板创建失败时返回聚合错误
func (s *Store) SeedBuiltinTemplates(ctx context.Context, workspaceID uuid.UUID) error {
	type nodeDef struct {
		Name         string
		Description  string
		SortOrder    int32
		NodeType     db.NodeType
		AssigneeType db.AssigneeType
		Timeout      int32
	}

	type templateDef struct {
		Name        string
		Description string
		Nodes       []nodeDef
	}

	templates := []templateDef{
		{
			Name:        "标准开发流程",
			Description: "完整的标准开发流程，包含从需求分析到部署上线的7个执行节点",
			Nodes: []nodeDef{
				{Name: "需求分析", Description: "分析需求文档，明确功能范围和验收标准", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "技术设计", Description: "设计技术方案，确定架构和接口", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "编码实现", Description: "按照设计方案编写代码", SortOrder: 3, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 120},
				{Name: "自测验证", Description: "编写并运行单元测试，验证基本功能", SortOrder: 4, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "代码审查", Description: "审查代码质量、规范性和潜在问题", SortOrder: 5, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "集成测试", Description: "运行集成测试，验证模块间协作", SortOrder: 6, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "部署上线", Description: "将代码部署到生产环境", SortOrder: 7, NodeType: db.NodeTypeManual, AssigneeType: db.AssigneeTypeHuman, Timeout: 0},
			},
		},
		{
			Name:        "快速修复流程",
			Description: "适用于Bug修复的精简流程，3个执行节点快速闭环",
			Nodes: []nodeDef{
				{Name: "问题定位", Description: "复现并定位问题根因", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
				{Name: "修复编码", Description: "编写修复代码", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "代码审查", Description: "审查修复代码的正确性", SortOrder: 3, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
			},
		},
		{
			Name:        "纯审查流程",
			Description: "仅包含代码审查节点的轻量流程",
			Nodes: []nodeDef{
				{Name: "代码审查", Description: "审查代码质量、规范性和潜在问题", SortOrder: 1, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
			},
		},
		{
			Name:        "文档编写流程",
			Description: "适用于文档类任务的3个执行节点流程",
			Nodes: []nodeDef{
				{Name: "资料收集", Description: "收集和整理相关资料与信息", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "文档撰写", Description: "撰写文档内容", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 120},
				{Name: "文档审校", Description: "审校文档的准确性和完整性", SortOrder: 3, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
			},
		},
		{
			Name:        "数据处理流程",
			Description: "适用于数据处理类任务的4个执行节点流程",
			Nodes: []nodeDef{
				{Name: "数据采集", Description: "从数据源采集原始数据", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "数据清洗", Description: "清洗和预处理数据", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "数据分析", Description: "分析数据并生成洞察", SortOrder: 3, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 120},
				{Name: "结果验证", Description: "验证分析结果的准确性", SortOrder: 4, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
			},
		},
	}

	var errs []error
	for _, t := range templates {
		// 每个模板使用一个事务，以避免并发创建工作区时发生死锁
		tx, txErr := s.db.BeginTx(ctx, nil)
		if txErr != nil {
			slog.Warn("SeedBuiltinTemplates: failed to begin tx", "name", t.Name, "err", txErr)
			errs = append(errs, fmt.Errorf("begin tx for template %q: %w", t.Name, txErr))
			continue
		}
		qtx := s.q.WithTx(tx)

		tpl, err := qtx.CreateWorkflowTemplate(ctx, normalizeCreateWorkflowTemplateParams(db.CreateWorkflowTemplateParams{
			WorkspaceID: workspaceID,
			Name:        t.Name,
			Description: sql.NullString{String: t.Description, Valid: true},
			IsBuiltin:   true,
		}))
		if err != nil {
			tx.Rollback()
			slog.Warn("SeedBuiltinTemplates: failed to create template", "name", t.Name, "err", err)
			errs = append(errs, fmt.Errorf("template %q: %w", t.Name, err))
			continue
		}

		nodeErr := false
		for _, n := range t.Nodes {
			_, err := qtx.CreateTemplateNode(ctx, db.CreateTemplateNodeParams{
				TemplateID:     tpl.ID,
				Name:           n.Name,
				Description:    sql.NullString{String: n.Description, Valid: true},
				SortOrder:      n.SortOrder,
				NodeType:       n.NodeType,
				AssigneeType:   n.AssigneeType,
				TimeoutMinutes: n.Timeout,
				DependsOn:      []uuid.UUID{},
			})
			if err != nil {
				slog.Warn("SeedBuiltinTemplates: failed to create node", "node", n.Name, "template", t.Name, "err", err)
				errs = append(errs, fmt.Errorf("node %q in template %q: %w", n.Name, t.Name, err))
				nodeErr = true
				break
			}
		}
		if nodeErr {
			tx.Rollback()
			continue
		}
		if err := tx.Commit(); err != nil {
			slog.Warn("SeedBuiltinTemplates: failed to commit tx", "name", t.Name, "err", err)
			errs = append(errs, fmt.Errorf("commit template %q: %w", t.Name, err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// GetWorkspaceMember 根据工作区 ID 和成员 ID 查询工作区成员关系。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含工作区 ID 和成员 ID
//
// 返回：
//   - db.WorkspaceMember: 工作区成员关系记录
//   - error: 查询失败时返回错误
func (s *Store) GetWorkspaceMember(ctx context.Context, params types.GetWorkspaceMemberParams) (types.WorkspaceMember, error) {
	dbParams, err := FromDomainGetWorkspaceMemberParams(params)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("convert get workspace member params: %w", err)
	}
	wm, err := s.q.GetWorkspaceMember(ctx, dbParams)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("get workspace member: %w", err)
	}
	return ToDomainWorkspaceMember(wm)
}

// CreateWorkspaceMember 将成员添加到工作区。
func (s *Store) CreateWorkspaceMember(ctx context.Context, params types.CreateWorkspaceMemberParams) (types.WorkspaceMember, error) {
	dbParams, err := FromDomainCreateWorkspaceMemberParams(params)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("convert create workspace member params: %w", err)
	}
	member, err := s.q.CreateWorkspaceMember(ctx, dbParams)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("create workspace member: %w", err)
	}
	return ToDomainWorkspaceMember(member)
}

// GetWorkspaceMemberRole 查询工作区成员的角色。
//
// 参数：
//   - ctx: 请求上下文
//   - params: 查询参数，包含工作区 ID 和成员 ID
//
// 返回：
//   - string: 成员角色（owner/admin/member/viewer）
//   - error: 查询失败时返回错误
func (s *Store) GetWorkspaceMemberRole(ctx context.Context, params types.GetWorkspaceMemberRoleParams) (string, error) {
	dbParams, err := FromDomainGetWorkspaceMemberRoleParams(params)
	if err != nil {
		return "", fmt.Errorf("convert get workspace member role params: %w", err)
	}
	role, err := s.q.GetWorkspaceMemberRole(ctx, dbParams)
	if err != nil {
		return "", fmt.Errorf("get workspace member role: %w", err)
	}
	return role, nil
}
