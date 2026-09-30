// tokens.go — API token issuance and revocation for token owners (agents, daemons).
// Tokens use a dual-hash scheme: SHA-256 lookup hash for indexed lookups,
// bcrypt hash for secure verification. Plaintext is returned only once at issuance.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// generateTokenPlaintext produces a prefixed token: {prefix}_{owner_short}_{random_hex}.
func generateTokenPlaintext(prefix string, ownerID uuid.UUID) (string, error) {
	idShort := strings.ReplaceAll(ownerID.String(), "-", "")[:8]
	randomBytes := make([]byte, 20)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate token random: %w", err)
	}
	return fmt.Sprintf("%s_%s_%s", prefix, idShort, hex.EncodeToString(randomBytes)), nil
}

// issueOwnerToken generates a new API token for the owner (owner_type: "agent" | "daemon"),
// stores its hashes in auth_tokens with a 365-day validity, and returns the plaintext once.
func (s *Store) issueOwnerToken(ctx context.Context, ownerType, prefix string, ownerID uuid.UUID) (string, error) {
	token, err := generateTokenPlaintext(prefix, ownerID)
	if err != nil {
		return "", err
	}

	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash token: %w", err)
	}

	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, lookup_hash, token_type, owner_type, owner_id, expires_at)
		 VALUES ($1, $2, 'api', $3, $4, NOW() + INTERVAL '365 days')`,
		string(bcryptHash), lookupHash, ownerType, ownerID)
	if err != nil {
		return "", fmt.Errorf("insert auth token: %w", err)
	}
	return token, nil
}

// revokeOwnerTokens deletes all API and session tokens of the owner.
func (s *Store) revokeOwnerTokens(ctx context.Context, ownerType string, ownerID uuid.UUID) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE owner_type = $1 AND owner_id = $2`, ownerType, ownerID)
	if err != nil {
		return fmt.Errorf("revoke %s tokens: %w", ownerType, err)
	}
	return nil
}
