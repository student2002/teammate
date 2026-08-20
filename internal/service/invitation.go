// invitation.go 实现工作区邀请的业务逻辑，包括创建邀请、接受邀请、列出邀请。
//
// 本文件包含：
//   - InvitationService 结构体：邀请管理服务，封装邀请的创建、接受、查询操作
//   - Create：创建工作区邀请并返回邀请令牌，令牌用于后续接受邀请时验证身份
//   - Accept：通过令牌接受邀请，自动创建成员并分配工作区角色，返回登录结果
//   - List：列出指定工作区的所有邀请记录，包括已接受和未接受的
//
// 接受邀请时，系统会自动检查成员是否已存在：
//   - 已存在：直接返回登录结果，使用现有成员信息
//   - 不存在：创建新成员 → 添加到工作区 → 分配角色 → 生成 JWT 令牌
package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// InvitationService 提供邀请管理相关的业务逻辑。
type InvitationService struct {
	svc *Service
}

// NewInvitationService 创建一个新的 InvitationService 实例。
func NewInvitationService(svc *Service) *InvitationService {
	return &InvitationService{svc: svc}
}

// Create 创建一个工作区邀请并返回邀请令牌。
// 邀请令牌是唯一的，用于后续接受邀请时验证身份。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//   - email: 被邀请人的邮箱地址
//   - role: 分配给被邀请人的工作区角色（如 member、admin）
//   - invitedBy: 邀请人（操作者）的 ID
//
// 返回：
//   - types.Invitation: 创建的邀请记录
//   - string: 邀请令牌（用于接受邀请的 URL 参数）
//   - error: 可能的错误（重复邀请、数据库写入失败）
func (s *InvitationService) Create(ctx context.Context, workspaceID uuid.UUID, email, role string, invitedBy uuid.UUID) (types.Invitation, string, error) {
	return s.svc.Store.CreateInvitation(ctx, workspaceID, email, role, invitedBy)
}

// Accept 通过令牌接受邀请，如果成员不存在则自动创建新成员并分配到工作区。
// 返回登录结果供前端直接使用。
//
// 步骤：
//  1. 根据令牌查找邀请记录
//  2. 将邀请标记为已接受
//  3. 检查成员是否已存在：
//     - 已存在：直接返回登录结果
//     - 不存在：创建新成员 → 添加到工作区 → 分配角色 → 生成 JWT 令牌
//
// 参数：
//   - ctx: 请求上下文
//   - token: 邀请令牌
//   - name: 新成员的名称（已存在成员时忽略）
//   - jwtSecret: JWT 签名密钥
//
// 返回：
//   - *store.LoginResult: 包含 JWT 令牌、过期时间和成员信息
//   - error: 可能的错误（令牌无效或已过期、成员创建失败）
func (s *InvitationService) Accept(ctx context.Context, token string, name string, jwtSecret string) (*store.LoginResult, error) {
	inv, err := s.svc.Store.GetInvitationByToken(ctx, token)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("invalid or expired invitation")
		}
		return nil, fmt.Errorf("lookup invitation: %w", err)
	}

	invID, _ := uuid.Parse(inv.ID)
	_, err = s.svc.Store.AcceptInvitation(ctx, invID)
	if err != nil {
		return nil, fmt.Errorf("accept invitation: %w", err)
	}

	_, err = s.svc.Store.GetMemberByEmail(ctx, inv.Email)
	if err == nil {
		return s.svc.Store.Login(ctx, inv.Email, "", jwtSecret)
	}

	memberName := name
	if memberName == "" {
		memberName = inv.Email
	}
	member, err := s.svc.Store.CreateMember(ctx, types.CreateMemberParams{
		Name:  memberName,
		Email: inv.Email,
	})
	if err != nil {
		return nil, fmt.Errorf("create member: %w", err)
	}

	_, err = s.svc.Store.CreateWorkspaceMember(ctx, types.CreateWorkspaceMemberParams{
		WorkspaceID: inv.ID,
		MemberID:    member.ID,
		Role:        inv.Role,
	})
	if err != nil {
		return nil, fmt.Errorf("create workspace member: %w", err)
	}

	memberUUID, err := uuid.Parse(member.ID)
	if err != nil {
		return nil, fmt.Errorf("parse member id: %w", err)
	}
	token, expiresAt, jti, err := store.GenerateJWT(memberUUID, "member", jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	return &store.LoginResult{
		Token:     token,
		ExpiresAt: expiresAt,
		JTI:       jti,
		Member:    member,
	}, nil
}

// List 列出指定工作区的所有邀请，包括已接受和未接受的。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 ID
//
// 返回：
//   - []types.Invitation: 邀请列表
//   - error: 可能的错误（数据库查询失败）
func (s *InvitationService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Invitation, error) {
	return s.svc.Store.ListInvitations(ctx, workspaceID)
}
