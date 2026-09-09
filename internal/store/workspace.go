// workspace.go provides data access operations for workspace and member management.
//
// A Workspace is the top-level organizational unit of a team, containing projects, Agents, and members.
// Each member has a role within the workspace (owner/admin/member/viewer).
//
// When a new workspace is created, 5 built-in workflow templates are seeded:
//   - Standard development flow (7 nodes)
//   - Quick fix flow (3 nodes)
//   - Review-only flow (1 node)
//   - Documentation writing flow (3 nodes)
//   - Data processing flow (4 nodes)
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

// CreateWorkspace creates a workspace and seeds the 5 built-in workflow templates.
//
// Parameters:
//   - ctx: request context
//   - params: workspace creation parameters, including name, description, Issue prefix, etc.
//
// Returns:
//   - types.Workspace: the created workspace record
//   - error: error if creation fails
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

// CreateWorkspaceForMember creates a workspace and adds the creator as owner within the same transaction.
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

// CreateWorkspaceWithOwnerInTx creates a workspace within a single transaction and adds the specified member as owner.
// Difference from CreateWorkspaceForMember: it does not seed built-in templates (controlled by the caller),
// used in OAuth flows that require custom seeding behavior.
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

// GetWorkspace queries a single workspace record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: UUID of the workspace
//
// Returns:
//   - db.Workspace: the workspace record
//   - error: error if the query fails
func (s *Store) GetWorkspace(ctx context.Context, id uuid.UUID) (types.Workspace, error) {
	ws, err := s.q.GetWorkspace(ctx, id)
	if err != nil {
		return types.Workspace{}, fmt.Errorf("get workspace: %w", err)
	}
	return ToDomainWorkspace(ws)
}

// ListWorkspaces queries all workspace records.
//
// Parameters:
//   - ctx: request context
//
// Returns:
//   - []db.Workspace: list of workspaces
//   - error: error if the query fails
func (s *Store) ListWorkspaces(ctx context.Context) ([]types.Workspace, error) {
	wss, err := s.q.ListWorkspaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list workspaces: %w", err)
	}
	return ToDomainWorkspaceSlice(wss)
}

// ListWorkspacesByMemberID queries the list of workspaces a member belongs to.
func (s *Store) ListWorkspacesByMemberID(ctx context.Context, memberID uuid.UUID) ([]types.Workspace, error) {
	wss, err := s.q.ListWorkspacesByMemberID(ctx, memberID)
	if err != nil {
		return nil, fmt.Errorf("list member workspaces: %w", err)
	}
	return ToDomainWorkspaceSlice(wss)
}

// UpdateWorkspace updates the basic information of a workspace.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including the workspace ID and fields to update
//
// Returns:
//   - db.Workspace: the updated workspace record
//   - error: error if the update fails
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

// DeleteWorkspace deletes a workspace record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: UUID of the workspace
//
// Returns:
//   - error: error if deletion fails
func (s *Store) DeleteWorkspace(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteWorkspace(ctx, id); err != nil {
		return fmt.Errorf("delete workspace: %w", err)
	}
	return nil
}

// CreateMember creates a new member record.
//
// Parameters:
//   - ctx: request context
//   - params: member creation parameters, including name, email, etc.
//
// Returns:
//   - db.Member: the created member record
//   - error: error if creation fails
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

// ListMembersByWorkspace queries all members within the specified workspace (including role info).
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - []db.ListMembersByWorkspaceRow: list of members (including roles)
//   - error: error if the query fails
func (s *Store) ListMembersByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.ListMembersByWorkspaceRow, error) {
	members, err := s.q.ListMembersByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list members: %w", err)
	}
	return ToDomainListMembersByWorkspaceRowSlice(members)
}

// GetMemberByEmail queries a member record by email.
//
// Parameters:
//   - ctx: request context
//   - email: member email
//
// Returns:
//   - db.Member: the member record
//   - error: error if the query fails
func (s *Store) GetMemberByEmail(ctx context.Context, email string) (types.Member, error) {
	m, err := s.q.GetMemberByEmail(ctx, email)
	if err != nil {
		return types.Member{}, fmt.Errorf("get member by email: %w", err)
	}
	return ToDomainMember(m)
}

// GetFirstWorkspaceForMember fetches the first workspace membership of a member (ordered by creation time).
// Used during OAuth login to look up a member's existing workspace.
//
// Parameters:
//   - ctx: request context
//   - memberID: member ID
//
// Returns:
//   - db.WorkspaceMember: the workspace membership record
//   - error: error if the query fails
func (s *Store) GetFirstWorkspaceForMember(ctx context.Context, memberID uuid.UUID) (types.WorkspaceMember, error) {
	wm, err := s.q.GetFirstWorkspaceForMember(ctx, memberID)
	if err != nil {
		return types.WorkspaceMember{}, fmt.Errorf("get first workspace for member: %w", err)
	}
	return ToDomainWorkspaceMember(wm)
}

// UpdateMemberPasswordHash updates a member's password hash.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including member ID and the new password hash
//
// Returns:
//   - error: error if the update fails
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

// UpdateMemberRole updates a member's role within a workspace.
//
// Parameters:
//   - ctx: request context
//   - params: update parameters, including workspace ID, member ID and the new role
//
// Returns:
//   - db.WorkspaceMember: the updated workspace member record
//   - error: error if the update fails
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

// DeleteMember deletes a member record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: UUID of the member
//
// Returns:
//   - error: error if deletion fails
func (s *Store) DeleteMember(ctx context.Context, id uuid.UUID) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// After 002_remove_fks, the following columns no longer have foreign keys; they must be explicitly set to NULL (FK strategy: integrity guaranteed at the application layer):
	//   git_credentials.created_by, agent_permissions.granted_by, invitations.invited_by
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

// GetMember queries a single member record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: UUID of the member
//
// Returns:
//   - db.Member: the member record
//   - error: error if the query fails
func (s *Store) GetMember(ctx context.Context, id uuid.UUID) (types.Member, error) {
	m, err := s.q.GetMember(ctx, id)
	if err != nil {
		return types.Member{}, fmt.Errorf("get member: %w", err)
	}
	return ToDomainMember(m)
}

// GetWorkspaceOwner queries the owner member of the specified workspace.
//
// It iterates over all members and returns the member whose role is "owner".
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - db.ListMembersByWorkspaceRow: the owner member record
//   - error: error if the query fails or there is no owner
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

// SeedBuiltinTemplates creates 5 built-in workflow templates for a new workspace.
//
// Each template is created within its own transaction to avoid concurrent deadlocks; a single template/node failure does not affect subsequent template creation.
//
// Built-in templates:
//  1. Standard development flow (7 nodes): requirements analysis → technical design → coding → self-test verification → code review → integration test → deployment
//  2. Quick fix flow (3 nodes): problem triage → fix coding → code review
//  3. Review-only flow (1 node): code review
//  4. Documentation writing flow (3 nodes): material collection → document drafting → document proofreading
//  5. Data processing flow (4 nodes): data collection → data cleaning → data analysis → result verification
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - error: aggregated error returned when all template creations fail
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
			Name:        "Standard Development Workflow",
			Description: "Complete standard development workflow with 7 execution nodes from requirements analysis to deployment",
			Nodes: []nodeDef{
				{Name: "Requirement Analysis", Description: "Analyze requirement documents, clarify functional scope and acceptance criteria", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Technical Design", Description: "Design technical solution, define architecture and interfaces", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Implementation", Description: "Write code according to the design", SortOrder: 3, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 120},
				{Name: "Unit Testing", Description: "Write and run unit tests to verify basic functionality", SortOrder: 4, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Code Review", Description: "Review code quality, standards compliance, and potential issues", SortOrder: 5, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Integration Testing", Description: "Run integration tests to verify cross-module collaboration", SortOrder: 6, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Deployment", Description: "Deploy code to the production environment", SortOrder: 7, NodeType: db.NodeTypeManual, AssigneeType: db.AssigneeTypeHuman, Timeout: 0},
			},
		},
		{
			Name:        "Quick Fix Workflow",
			Description: "Streamlined workflow for bug fixes with 3 execution nodes for rapid turnaround",
			Nodes: []nodeDef{
				{Name: "Issue Triage", Description: "Reproduce and identify the root cause", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
				{Name: "Fix Implementation", Description: "Write the fix code", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Code Review", Description: "Review the correctness of the fix", SortOrder: 3, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
			},
		},
		{
			Name:        "Review-Only Workflow",
			Description: "Lightweight workflow containing only a code review node",
			Nodes: []nodeDef{
				{Name: "Code Review", Description: "Review code quality, standards compliance, and potential issues", SortOrder: 1, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
			},
		},
		{
			Name:        "Documentation Workflow",
			Description: "3-execution-node workflow for documentation tasks",
			Nodes: []nodeDef{
				{Name: "Research", Description: "Collect and organize relevant materials and information", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Writing", Description: "Write the document content", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 120},
				{Name: "Review", Description: "Proofread for accuracy and completeness", SortOrder: 3, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
			},
		},
		{
			Name:        "Data Processing Workflow",
			Description: "4-execution-node workflow for data processing tasks",
			Nodes: []nodeDef{
				{Name: "Data Collection", Description: "Collect raw data from data sources", SortOrder: 1, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Data Cleaning", Description: "Clean and preprocess data", SortOrder: 2, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 60},
				{Name: "Data Analysis", Description: "Analyze data and generate insights", SortOrder: 3, NodeType: db.NodeTypeStandard, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 120},
				{Name: "Result Validation", Description: "Validate the accuracy of analysis results", SortOrder: 4, NodeType: db.NodeTypeReview, AssigneeType: db.AssigneeTypeAnyAgent, Timeout: 30},
			},
		},
	}

	var errs []error
	for _, t := range templates {
		// Each template uses its own transaction to avoid deadlocks when creating workspaces concurrently
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

// GetWorkspaceMember queries a workspace membership by workspace ID and member ID.
//
// Parameters:
//   - ctx: request context
//   - params: query parameters, including workspace ID and member ID
//
// Returns:
//   - db.WorkspaceMember: the workspace membership record
//   - error: error if the query fails
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

// CreateWorkspaceMember adds a member to a workspace.
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

// GetWorkspaceMemberRole queries the role of a workspace member.
//
// Parameters:
//   - ctx: request context
//   - params: query parameters, including workspace ID and member ID
//
// Returns:
//   - string: the member role (owner/admin/member/viewer)
//   - error: error if the query fails
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
