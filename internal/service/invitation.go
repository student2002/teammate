// invitation.go implements the business logic for workspace invitations, including creating invitations, accepting invitations, and listing invitations.
//
// This file contains:
//   - InvitationService struct: the invitation management service, encapsulating invitation creation, acceptance, and query operations
//   - Create: creates a workspace invitation and returns an invitation token, which is used to verify identity when accepting the invitation
//   - Accept: accepts an invitation via the token, automatically creating a member and assigning a workspace role, returning the login result
//   - List: lists all invitation records for the specified workspace, including accepted and unaccepted ones
//
// When accepting an invitation, the system automatically checks whether the member already exists:
//   - Exists: returns the login result directly, using the existing member info
//   - Does not exist: create a new member -> add to the workspace -> assign role -> generate JWT token
package service

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// InvitationService provides the business logic for invitation management.
type InvitationService struct {
	svc *Service
}

// NewInvitationService creates a new InvitationService instance.
func NewInvitationService(svc *Service) *InvitationService {
	return &InvitationService{svc: svc}
}

// Create creates a workspace invitation and returns an invitation token.
// The invitation token is unique and is used to verify identity when accepting the invitation later.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - email: the invitee's email address
//   - role: the workspace role assigned to the invitee (e.g. member, admin)
//   - invitedBy: the inviter's (operator's) ID
//
// Returns:
//   - types.Invitation: the created invitation record
//   - string: invitation token (used as a URL parameter for accepting the invitation)
//   - error: possible errors (duplicate invitation, database write failure)
func (s *InvitationService) Create(ctx context.Context, workspaceID uuid.UUID, email, role string, invitedBy uuid.UUID) (types.Invitation, string, error) {
	return s.svc.Store.CreateInvitation(ctx, workspaceID, email, role, invitedBy)
}

// Accept accepts an invitation via the token; if the member does not exist, it automatically creates a new member and adds them to the workspace.
// Returns the login result for the frontend to use directly.
//
// Steps:
//  1. Look up the invitation record by token
//  2. Mark the invitation as accepted
//  3. Check whether the member already exists:
//     - Exists: return the login result directly
//     - Does not exist: create a new member -> add to the workspace -> assign role -> generate JWT token
//
// Parameters:
//   - ctx: request context
//   - token: invitation token
//   - name: the new member's name (ignored when the member already exists)
//   - jwtSecret: JWT signing key
//
// Returns:
//   - *store.LoginResult: contains the JWT token, expiration time, and member info
//   - error: possible errors (token invalid or expired, member creation failure)
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

// List lists all invitations for the specified workspace, including accepted and unaccepted ones.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//
// Returns:
//   - []types.Invitation: invitation list
//   - error: possible errors (database query failure)
func (s *InvitationService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Invitation, error) {
	return s.svc.Store.ListInvitations(ctx, workspaceID)
}
