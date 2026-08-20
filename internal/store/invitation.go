// invitation.go 提供工作区邀请的数据访问操作。
//
// 邀请流程：管理员创建邀请 → 生成 Token → 发送邀请链接 →
// 被邀请者接受 → 邀请状态更新为已接受。
//
// 邀请 Token 采用 SHA-256 哈希存储，有效期 7 天。
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// CreateInvitation 为新成员创建工作区邀请。
//
// 执行步骤：
//  1. 生成 32 字节随机 Token
//  2. 计算 Token 的 SHA-256 哈希
//  3. 将哈希存储到 invitations 表，有效期 7 天
//  4. 返回邀请记录和 Token 明文（用于发送邀请链接）
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//   - email: 被邀请者邮箱
//   - role: 授予的角色（如 "member"、"admin"）
//   - invitedBy: 邀请者 ID
//
// 返回：
//   - types.Invitation: 创建的邀请记录
//   - string: 邀请 Token 明文（用于生成邀请链接）
//   - error: 创建失败时返回错误
func (s *Store) CreateInvitation(ctx context.Context, workspaceID uuid.UUID, email, role string, invitedBy uuid.UUID) (types.Invitation, string, error) {
	// 生成邀请 Token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return types.Invitation{}, "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// 对 Token 进行哈希以便存储
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	inv, err := s.q.CreateInvitation(ctx, fromDomainCreateInvitationParams(workspaceID, email, role, tokenHash, invitedBy, time.Now().Add(7*24*time.Hour)))
	if err != nil {
		return types.Invitation{}, "", fmt.Errorf("create invitation: %w", err)
	}

	domainInv, err := ToDomainInvitation(inv)
	if err != nil {
		return types.Invitation{}, "", fmt.Errorf("convert invitation to domain: %w", err)
	}
	return domainInv, token, nil
}

// GetInvitationByToken 通过邀请 Token 的 SHA-256 哈希查找邀请记录。
//
// 参数：
//   - ctx: 请求上下文
//   - token: 邀请 Token 明文
//
// 返回：
//   - types.Invitation: 邀请记录
//   - error: 查询失败时返回错误（如 Token 无效或过期）
func (s *Store) GetInvitationByToken(ctx context.Context, token string) (types.Invitation, error) {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	inv, err := s.q.GetInvitationByToken(ctx, tokenHash)
	if err != nil {
		return types.Invitation{}, fmt.Errorf("get invitation by token: %w", err)
	}
	return ToDomainInvitation(inv)
}

// AcceptInvitation 将邀请状态标记为已接受。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 邀请记录的 UUID
//
// 返回：
//   - types.Invitation: 更新后的邀请记录
//   - error: 更新失败时返回错误
func (s *Store) AcceptInvitation(ctx context.Context, id uuid.UUID) (types.Invitation, error) {
	inv, err := s.q.AcceptInvitation(ctx, id)
	if err != nil {
		return types.Invitation{}, fmt.Errorf("accept invitation: %w", err)
	}
	return ToDomainInvitation(inv)
}

// ListInvitations 查询指定工作区的所有邀请记录。
//
// 参数：
//   - ctx: 请求上下文
//   - workspaceID: 工作区 UUID
//
// 返回：
//   - []types.Invitation: 邀请记录列表
//   - error: 查询失败时返回错误
func (s *Store) ListInvitations(ctx context.Context, workspaceID uuid.UUID) ([]types.Invitation, error) {
	invs, err := s.q.ListInvitations(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	return ToDomainInvitationSlice(invs)
}

// DeleteInvitation 根据 ID 删除邀请记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 邀请记录的 UUID
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteInvitation(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteInvitation(ctx, id); err != nil {
		return fmt.Errorf("delete invitation: %w", err)
	}
	return nil
}
