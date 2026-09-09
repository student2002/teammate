// invitation_test.go covers invitation data access tests.
package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// TestCreateInvitation_TokenHash verifies creating an invitation and returning a Token.
func TestCreateInvitation_TokenHash(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	inviter := createTestMember(t, s, ws.ID)

	inv, token, err := s.CreateInvitation(ctx, uuid.MustParse(ws.ID), "invitee@test.com", "member", uuid.MustParse(inviter.ID))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if inv.Email != "invitee@test.com" {
		t.Fatalf("expected email 'invitee@test.com', got %s", inv.Email)
	}
}

// TestGetInvitationByToken verifies querying invitation info by Token.
func TestGetInvitationByToken(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	inviter := createTestMember(t, s, ws.ID)

	inv, token, err := s.CreateInvitation(ctx, uuid.MustParse(ws.ID), "lookup@test.com", "member", uuid.MustParse(inviter.ID))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	found, err := s.GetInvitationByToken(ctx, token)
	if err != nil {
		t.Fatalf("GetInvitationByToken: %v", err)
	}
	if found.ID != inv.ID {
		t.Fatalf("expected invitation ID %s, got %s", inv.ID, found.ID)
	}
}

// TestAcceptInvitation verifies the accept invitation functionality.
func TestAcceptInvitation(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	inviter := createTestMember(t, s, ws.ID)

	inv, _, err := s.CreateInvitation(ctx, uuid.MustParse(ws.ID), "accept@test.com", "member", uuid.MustParse(inviter.ID))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	accepted, err := s.AcceptInvitation(ctx, uuid.MustParse(inv.ID))
	if err != nil {
		t.Fatalf("AcceptInvitation: %v", err)
	}
	if accepted.AcceptedAt == nil {
		t.Fatal("expected accepted_at to be set")
	}
}

// TestListInvitations verifies listing all invitations by workspace.
func TestListInvitations(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	inviter := createTestMember(t, s, ws.ID)

	// Create 2 invitations
	_, _, err := s.CreateInvitation(ctx, uuid.MustParse(ws.ID), "one@test.com", "member", uuid.MustParse(inviter.ID))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}
	_, _, err = s.CreateInvitation(ctx, uuid.MustParse(ws.ID), "two@test.com", "admin", uuid.MustParse(inviter.ID))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	invitations, err := s.ListInvitations(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	if len(invitations) != 2 {
		t.Fatalf("expected 2 invitations, got %d", len(invitations))
	}
}

// TestDeleteInvitation verifies the delete invitation functionality.
func TestDeleteInvitation(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	inviter := createTestMember(t, s, ws.ID)

	inv, _, err := s.CreateInvitation(ctx, uuid.MustParse(ws.ID), "delete@test.com", "member", uuid.MustParse(inviter.ID))
	if err != nil {
		t.Fatalf("CreateInvitation: %v", err)
	}

	err = s.DeleteInvitation(ctx, uuid.MustParse(inv.ID))
	if err != nil {
		t.Fatalf("DeleteInvitation: %v", err)
	}

	// Verify it's deleted by listing invitations
	invitations, err := s.ListInvitations(ctx, uuid.MustParse(ws.ID))
	if err != nil {
		t.Fatalf("ListInvitations: %v", err)
	}
	for _, i := range invitations {
		if i.ID == inv.ID {
			t.Fatal("expected invitation to be deleted")
		}
	}
}

// TestGetInvitationByToken_InvalidToken verifies that an invalid Token returns an error.
func TestGetInvitationByToken_InvalidToken(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	_, err := s.GetInvitationByToken(ctx, "invalid-token-"+uuid.New().String())
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}
