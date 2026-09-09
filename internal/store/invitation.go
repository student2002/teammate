// invitation.go provides data access operations for workspace invitations.
//
// Invitation flow: an admin creates an invitation -> generates a Token -> sends an invitation link ->
// the invitee accepts -> the invitation status is updated to accepted.
//
// The invitation Token is stored as a SHA-256 hash and is valid for 7 days.
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

// CreateInvitation creates a workspace invitation for a new member.
//
// Steps:
//  1. Generate a 32-byte random Token
//  2. Compute the SHA-256 hash of the Token
//  3. Store the hash in the invitations table, valid for 7 days
//  4. Return the invitation record and the Token plaintext (used to send the invitation link)
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//   - email: the invitee's email
//   - role: the granted role (e.g. "member", "admin")
//   - invitedBy: the inviter ID
//
// Returns:
//   - types.Invitation: the created invitation record
//   - string: the invitation Token plaintext (used to generate the invitation link)
//   - error: returns an error if creation fails
func (s *Store) CreateInvitation(ctx context.Context, workspaceID uuid.UUID, email, role string, invitedBy uuid.UUID) (types.Invitation, string, error) {
	// Generate the invitation Token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return types.Invitation{}, "", fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	// Hash the Token for storage
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

// GetInvitationByToken looks up an invitation record by the SHA-256 hash of the invitation Token.
//
// Parameters:
//   - ctx: request context
//   - token: the invitation Token plaintext
//
// Returns:
//   - types.Invitation: the invitation record
//   - error: returns an error if the query fails (e.g. Token invalid or expired)
func (s *Store) GetInvitationByToken(ctx context.Context, token string) (types.Invitation, error) {
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])
	inv, err := s.q.GetInvitationByToken(ctx, tokenHash)
	if err != nil {
		return types.Invitation{}, fmt.Errorf("get invitation by token: %w", err)
	}
	return ToDomainInvitation(inv)
}

// AcceptInvitation marks the invitation status as accepted.
//
// Parameters:
//   - ctx: request context
//   - id: the UUID of the invitation record
//
// Returns:
//   - types.Invitation: the updated invitation record
//   - error: returns an error if the update fails
func (s *Store) AcceptInvitation(ctx context.Context, id uuid.UUID) (types.Invitation, error) {
	inv, err := s.q.AcceptInvitation(ctx, id)
	if err != nil {
		return types.Invitation{}, fmt.Errorf("accept invitation: %w", err)
	}
	return ToDomainInvitation(inv)
}

// ListInvitations queries all invitation records for the specified workspace.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//
// Returns:
//   - []types.Invitation: the list of invitation records
//   - error: returns an error if the query fails
func (s *Store) ListInvitations(ctx context.Context, workspaceID uuid.UUID) ([]types.Invitation, error) {
	invs, err := s.q.ListInvitations(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list invitations: %w", err)
	}
	return ToDomainInvitationSlice(invs)
}

// DeleteInvitation deletes an invitation record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: the UUID of the invitation record
//
// Returns:
//   - error: returns an error if deletion fails
func (s *Store) DeleteInvitation(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteInvitation(ctx, id); err != nil {
		return fmt.Errorf("delete invitation: %w", err)
	}
	return nil
}
