// auth.go implements the business logic for authentication and authorization, including login, registration, and JWT token management.
//
// This file contains:
//   - AuthService struct: the authentication service, encapsulating JWT token management, login/registration flows, and session exchange
//   - Login: authenticates by email and password, using Redis for failure counting and account lockout
//   - Register: creates a new member account and returns a JWT token, with passwords bcrypt-hashed
//   - GenerateToken: generates a JWT token for the specified user
//   - ExchangeAPITokenForSession: exchanges a long-lived API token for a short-lived session token
//   - Logout: revokes the session token so it becomes invalid immediately
//   - Whoami: retrieves information about the currently authenticated user
//   - UpdateRuntimePublicKey/GetRuntimeByID/GetLatestPublicKeyForAgent: runtime public key management
//   - CreateGitCredential/UpdateGitCredential/GetGitCredential: Git credential management (deletion is not allowed, ensuring task branches remain traceable)
//   - ChangePassword/RequestPasswordReset/ResetPassword: password management
//
// Login uses Redis for failure counting (5 failures lock out for 15 minutes) and supports JWT jti revocation.
// The JWT token's jti is stored in Redis, supporting token revocation verification, with a TTL matching the JWT expiration time.
package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// ValidatePassword validates password strength.
// Rules: 8-128 characters, must contain at least one uppercase letter, one lowercase letter, and one digit.
//
// Parameters:
//   - password: the password to validate
//
// Returns:
//   - error: a descriptive error when the password does not meet requirements, nil otherwise
func ValidatePassword(password string) error {
	if len(password) < 8 {
		return fmt.Errorf("password must be at least 8 characters")
	}
	if len(password) > 128 {
		return fmt.Errorf("password must be at most 128 characters")
	}

	var hasUpper, hasLower, hasDigit bool
	for _, c := range password {
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			hasLower = true
		case c >= '0' && c <= '9':
			hasDigit = true
		}
	}
	if !hasUpper || !hasLower || !hasDigit {
		return fmt.Errorf("password must contain at least one uppercase letter, one lowercase letter, and one digit")
	}
	return nil
}

// AuthService provides the business logic for authentication and authorization.
// It includes JWT token management, login/registration flows, session token exchange, password reset, etc.
type AuthService struct {
	svc       *Service
	JWTSecret string // JWT signing key
}

// NewAuthService creates a new AuthService instance.
func NewAuthService(svc *Service, jwtSecret string) *AuthService {
	return &AuthService{svc: svc, JWTSecret: jwtSecret}
}

// LoginResult holds the result of a login operation.
type LoginResult struct {
	Token       string        // JWT token
	ExpiresAt   time.Time     // token expiration time
	Member      types.Member  // the logged-in member info (domain model, excluding PasswordHash)
	WorkspaceID uuid.UUID     // default workspace ID
	Role        string        // the member's role in the workspace
}

// Login authenticates a member login by email and password.
// Before login it checks whether the account is locked due to too many failures,
// and on success stores the JWT jti in Redis to support token revocation.
//
// Steps:
//  1. Check whether the account is locked due to too many failed attempts (Redis rate limiting)
//  2. Verify the email and password, and generate a JWT token
//  3. On login failure, record the failure count (Redis); lock the account once the threshold is reached
//  4. On login success, clear the failure count (Redis)
//  5. Store the JWT jti in Redis for token revocation verification (TTL matches the JWT expiration time)
//
// Parameters:
//   - ctx: request context
//   - email: user email address
//   - password: user password (plaintext; bcrypt verification is performed internally)
//
// Returns:
//   - *LoginResult: contains the JWT token, expiration time, member info, workspace ID, and role
//   - error: possible errors (account locked, wrong password, token generation failure)
func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	if err := s.svc.Store.CheckLoginLockout(ctx, email, s.svc.Redis); err != nil {
		return nil, err
	}

	result, err := s.svc.Store.Login(ctx, email, password, s.JWTSecret)
	if err != nil {
		s.svc.Store.RecordLoginFailure(ctx, email, s.svc.Redis)
		return nil, err
	}

	s.svc.Store.RecordLoginSuccess(ctx, email, s.svc.Redis)

	// Store the jti in Redis for token verification (TTL matches the JWT expiration time)
	if s.svc.Redis != nil && result.JTI != "" {
		ttl := time.Until(result.ExpiresAt)
		if ttl > 0 {
			if err := s.svc.Redis.Set(ctx, "jwt:"+result.JTI, result.Member.ID, ttl).Err(); err != nil {
				slog.Warn("failed to store JWT jti in Redis, token revocation may not work",
					"jti", result.JTI, "err", err)
			}
		}
	}
	return &LoginResult{
		Token:       result.Token,
		ExpiresAt:   result.ExpiresAt,
		Member:      result.Member,
		WorkspaceID: result.WorkspaceID,
		Role:        result.Role,
	}, nil
}

// RegisterResult holds the result of a registration operation.
type RegisterResult struct {
	Token       string        // JWT token
	ExpiresAt   time.Time     // token expiration time
	Member      types.Member  // the created member info (domain model, excluding PasswordHash)
	WorkspaceID uuid.UUID     // default workspace ID
	Role        string        // the member's role in the workspace
}

// Register creates a new member account and returns a JWT token.
// On success it stores the JWT jti in Redis to support token revocation.
//
// Steps:
//  1. Call Store to create the member account (password is bcrypt-hashed)
//  2. Generate a JWT token
//  3. Store the JWT jti in Redis for token revocation verification
//
// Parameters:
//   - ctx: request context
//   - name: user name
//   - email: user email address (unique)
//   - password: user password (plaintext; bcrypt hashing is performed internally)
//
// Returns:
//   - *RegisterResult: contains the JWT token, expiration time, member info, workspace ID, and role
//   - error: possible errors (email already exists, database write failure)
func (s *AuthService) Register(ctx context.Context, name, email, password string) (*RegisterResult, error) {
	if err := ValidatePassword(password); err != nil {
		return nil, err
	}

	result, err := s.svc.Store.Register(ctx, name, email, password, s.JWTSecret)
	if err != nil {
		return nil, err
	}
	if s.svc.Redis != nil && result.JTI != "" {
		ttl := time.Until(result.ExpiresAt)
		if ttl > 0 {
			if err := s.svc.Redis.Set(ctx, "jwt:"+result.JTI, result.Member.ID, ttl).Err(); err != nil {
				slog.Warn("failed to store JWT jti in Redis, token revocation may not work",
					"jti", result.JTI, "err", err)
			}
		}
	}
	return &RegisterResult{
		Token:       result.Token,
		ExpiresAt:   result.ExpiresAt,
		Member:      result.Member,
		WorkspaceID: result.WorkspaceID,
		Role:        result.Role,
	}, nil
}

// EnsureOAuthWorkspace creates a workspace and member relationship for OAuth login.
// If the member does not exist, it creates a new workspace (named after workspaceName) and adds the member as owner.
//
// Parameters:
//   - ctx: request context
//   - memberID: member ID
//   - workspaceName: workspace name
//
// Returns:
//   - types.Workspace: the created workspace
//   - error: possible errors (database operation failure)
func (s *AuthService) EnsureOAuthWorkspace(ctx context.Context, memberID uuid.UUID, workspaceName string) (types.Workspace, error) {
	descStr := "Personal workspace"
	workspace, err := s.svc.Store.CreateWorkspaceWithOwnerInTx(ctx, memberID, types.CreateWorkspaceParams{
		Name:        workspaceName,
		Description: &descStr,
		IssuePrefix: "MUL",
		IsDefault:   true,
	})
	if err != nil {
		return types.Workspace{}, fmt.Errorf("create oauth workspace: %w", err)
	}
	return workspace, nil
}

// GenerateToken generates a JWT token for the specified user.
//
// Parameters:
//   - ctx: request context
//   - userID: user ID
//   - userType: user type (member or agent)
//   - workspaceID: workspace ID
//   - role: the user's role in the workspace
//
// Returns:
//   - string: the generated JWT token
//   - time.Time: token expiration time
//   - error: possible errors (token generation failure)
func (s *AuthService) GenerateToken(userID uuid.UUID, userType string, workspaceID uuid.UUID, role string) (string, time.Time, error) {
	token, expiresAt, _, err := store.GenerateJWT(userID, userType, s.JWTSecret)
	return token, expiresAt, err
}

// SessionTokenResult holds the result of a session token exchange operation.
type SessionTokenResult struct {
	SessionToken string    // session token
	ExpiresAt    time.Time // session token expiration time
	AgentID      uuid.UUID // associated agent ID
}

// ExchangeAPITokenForSession exchanges an API token for a session token.
// The API token is long-lived, while the session token is short-lived and used for the Agentd daemon's daily communication.
//
// Parameters:
//   - ctx: request context
//   - apiToken: API token
//
// Returns:
//   - *SessionTokenResult: contains the session token, expiration time, and agent ID
//   - error: possible errors (API token invalid or expired)
func (s *AuthService) ExchangeAPITokenForSession(ctx context.Context, apiToken string) (*SessionTokenResult, error) {
	result, err := s.svc.Store.ExchangeAPITokenForSession(ctx, apiToken)
	if err != nil {
		return nil, err
	}
	return &SessionTokenResult{
		SessionToken: result.SessionToken,
		ExpiresAt:    result.ExpiresAt,
		AgentID:      result.AgentID,
	}, nil
}

// Logout revokes the current session token, making it invalid immediately.
//
// Parameters:
//   - ctx: request context
//   - token: the session token to revoke
//
// Returns:
//   - error: possible errors (database deletion failure)
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.svc.Store.DeleteSessionToken(ctx, token)
}

// WhoamiInfo is a type alias for the detailed info of an authenticated user.
type WhoamiInfo = store.WhoamiInfo

// Whoami retrieves information about the currently authenticated user, including member details and workspace role.
//
// Parameters:
//   - ctx: request context
//   - ownerType: owner type (member or agent)
//   - ownerID: owner ID
//
// Returns:
//   - *WhoamiInfo: user detailed info
//   - error: possible errors (user does not exist)
func (s *AuthService) Whoami(ctx context.Context, ownerType string, ownerID uuid.UUID) (*WhoamiInfo, error) {
	return s.svc.Store.GetWhoamiInfo(ctx, ownerType, ownerID)
}

// UpdateRuntimePublicKey updates the runtime's public key, used for SSH authentication in Git operations.
//
// Parameters:
//   - ctx: request context
//   - runtimeID: runtime ID
//   - publicKey: the new SSH public key
//
// Returns:
//   - error: possible errors (runtime does not exist, database update failure)
func (s *AuthService) UpdateRuntimePublicKey(ctx context.Context, runtimeID uuid.UUID, publicKey string) error {
	return s.svc.Store.UpdateRuntimePublicKey(ctx, runtimeID, publicKey)
}

// GetRuntimeByID retrieves runtime info by ID.
//
// Parameters:
//   - ctx: request context
//   - runtimeID: runtime ID
//
// Returns:
//   - types.Runtime: runtime info
//   - error: possible errors (runtime does not exist)
func (s *AuthService) GetRuntimeByID(ctx context.Context, runtimeID uuid.UUID) (types.Runtime, error) {
	return s.svc.Store.GetRuntimeByID(ctx, runtimeID)
}

// GetLatestPublicKeyForAgent retrieves the latest public key for the specified agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: agent ID
//
// Returns:
//   - string: the latest SSH public key
//   - error: possible errors (agent does not exist, no public key record)
func (s *AuthService) GetLatestPublicKeyForAgent(ctx context.Context, agentID uuid.UUID) (string, error) {
	return s.svc.Store.GetLatestPublicKeyForAgent(ctx, agentID)
}

// GetGitCredentialsByProject retrieves all Git credentials for the specified project.
// PATs (Personal Access Tokens) in the credentials are stored using RSA + AES encryption.
//
// Parameters:
//   - ctx: request context
//   - projectID: project ID
//
// Returns:
//   - []types.GitCredential: list of Git credentials
//   - error: possible errors (database query failure)
func (s *AuthService) GetGitCredentialsByProject(ctx context.Context, projectID uuid.UUID) ([]types.GitCredential, error) {
	return s.svc.Store.GetGitCredentialsByProject(ctx, projectID)
}

// CreateGitCredential creates a new Git credential.
//
// Parameters:
//   - ctx: request context
//   - arg: parameters for creating the Git credential
//
// Returns:
//   - types.GitCredential: the created Git credential
//   - error: possible errors (database write failure)
func (s *AuthService) CreateGitCredential(ctx context.Context, arg types.CreateGitCredentialParams) (types.GitCredential, error) {
	return s.svc.Store.CreateGitCredential(ctx, arg)
}

// UpdateGitCredential updates an existing Git credential.
//
// Parameters:
//   - ctx: request context
//   - arg: parameters for updating the Git credential
//
// Returns:
//   - types.GitCredential: the updated Git credential
//   - error: possible errors (credential does not exist, database update failure)
func (s *AuthService) UpdateGitCredential(ctx context.Context, arg types.UpdateGitCredentialParams) (types.GitCredential, error) {
	return s.svc.Store.UpdateGitCredential(ctx, arg)
}

// GetGitCredential retrieves a single Git credential by ID.
//
// Parameters:
//   - ctx: request context
//   - id: Git credential ID
//
// Returns:
//   - types.GitCredential: Git credential info
//   - error: possible errors (credential does not exist)
func (s *AuthService) GetGitCredential(ctx context.Context, id uuid.UUID) (types.GitCredential, error) {
	return s.svc.Store.GetGitCredential(ctx, id)
}

// ChangePassword changes the member's password after verifying the old password.
// The new password is stored as a bcrypt hash.
//
// Parameters:
//   - ctx: request context
//   - memberID: member ID
//   - oldPassword: current password (for verification)
//   - newPassword: new password
//
// Returns:
//   - error: possible errors (wrong old password, member does not exist)
func (s *AuthService) ChangePassword(ctx context.Context, memberID uuid.UUID, oldPassword, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	return s.svc.Store.ChangePassword(ctx, memberID, oldPassword, newPassword)
}

// RequestPasswordReset generates a password reset token for the specified email.
// If the email does not exist or belongs to an OAuth user, it returns an empty string without error (to prevent email enumeration attacks).
//
// Parameters:
//   - ctx: request context
//   - email: user email address
//
// Returns:
//   - string: password reset token (empty string when the email does not exist)
//   - error: possible errors (database operation failure)
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) (string, error) {
	return s.svc.Store.CreatePasswordResetToken(ctx, email)
}

// APIKeyAuthResult holds the result of API key or session token authentication.
type APIKeyAuthResult struct {
	UserID   uuid.UUID // user ID
	UserType string    // user type: "member" or "agent"
}

// AuthenticateAPIKey verifies an API key or session token and returns the authentication result.
// It queries the auth_tokens table by SHA-256 hash, then uses bcrypt to verify token security.
//
// Parameters:
//   - ctx: request context
//   - tokenStr: the token string to verify (st_ prefix for session tokens, tm_ prefix for API tokens)
//
// Returns:
//   - APIKeyAuthResult: the authentication result, containing the user ID and type
//   - error: returned when the token is invalid, expired, or the query fails
func (s *AuthService) AuthenticateAPIKey(ctx context.Context, tokenStr string) (APIKeyAuthResult, error) {
	lookupHash := sha256Hash(tokenStr)

	tokenType := types.TokenTypeAPI
	errMsg := "invalid or expired api key"
	if strings.HasPrefix(tokenStr, "st_") {
		tokenType = types.TokenTypeSession
		errMsg = "invalid or expired session token"
	}

	row, err := s.svc.Store.GetAuthTokenByLookupHashAndType(ctx, lookupHash, tokenType)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return APIKeyAuthResult{}, errors.New(errMsg)
		}
		return APIKeyAuthResult{}, fmt.Errorf("token lookup failed: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(row.TokenHash), []byte(tokenStr)); err != nil {
		return APIKeyAuthResult{}, errors.New(errMsg)
	}

	if row.OwnerType != "member" && row.OwnerType != "agent" {
		return APIKeyAuthResult{}, errors.New("invalid owner_type in token record")
	}

	return APIKeyAuthResult{
		UserID:   uuid.MustParse(row.OwnerID),
		UserType: row.OwnerType,
	}, nil
}

// sha256Hash computes the SHA-256 hash of a string and returns it in hexadecimal form.
func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ResetPassword resets a member's password using a valid reset token.
// After the token is successfully verified, it is invalidated immediately to prevent reuse.
//
// Parameters:
//   - ctx: request context
//   - token: password reset token
//   - newPassword: new password
//
// Returns:
//   - error: possible errors (token invalid or expired, database update failure)
func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	return s.svc.Store.ResetPasswordWithToken(ctx, token, newPassword)
}
