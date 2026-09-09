// auth_dto.go defines request/response structs related to authentication.
package handler

import (
	"time"

	"github.com/teammate/server/internal/types"
)

// authResponse authentication response body, containing Token and user info.
type authResponse struct {
	Token       string        `json:"token"`        // JWT Token
	ExpiresAt   time.Time     `json:"expires_at"`   // Token expiration time
	Member      types.Member  `json:"member"`       // user info (domain model, without PasswordHash)
	WorkspaceID string        `json:"workspace_id"` // workspace ID
	Role        string        `json:"role"`         // user role
}
