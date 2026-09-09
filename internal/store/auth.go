// auth.go provides data access operations related to authentication and authorization.
//
// It includes user login/registration, JWT Token generation, API Token exchange for session Tokens,
// password reset, login lockout, Git credential management, and other features.
//
// Authentication flow:
//   - Human users: email + password -> bcrypt verification -> JWT Token (valid for 24 hours)
//   - Agent: API Token -> SHA-256 lookup -> bcrypt verification -> session Token (valid for 7 days)
//
// Security features:
//   - Token storage uses dual hashing: bcrypt for secure storage + SHA-256 for efficient lookup
//   - 5 failed logins lock the account for 15 minutes (Redis-backed)
//   - Password reset Tokens expire after 1 hour
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// LoginResult encapsulates the return result of a login operation.
//
// It includes the JWT Token, expiration time, JTI (Token unique identifier), member info, workspace ID, and role.
type LoginResult struct {
	Token       string        // JWT Token string
	ExpiresAt   time.Time     // Token expiration time
	JTI         string        // Token unique identifier (used for Token revocation)
	Member      types.Member  // member info (domain struct, without PasswordHash)
	WorkspaceID uuid.UUID     // workspace ID
	Role        string        // member role (owner/admin/member/viewer)
}

// RegisterResult encapsulates the return result of a registration operation.
//
// Its structure is identical to LoginResult; auto-login happens after registration.
type RegisterResult struct {
	Token       string        // JWT Token string
	ExpiresAt   time.Time     // Token expiration time
	JTI         string        // Token unique identifier
	Member      types.Member  // member info (domain struct, without PasswordHash)
	WorkspaceID uuid.UUID     // workspace ID
	Role        string        // member role
}

// Login authenticates a member by email and password.
//
// Steps:
//  1. Query the member record by email
//  2. Verify the password with bcrypt
//  3. Query the member's first workspace membership
//  4. Generate a JWT Token (valid for 24 hours)
//
// Parameters:
//   - ctx: request context
//   - email: the member's email
//   - password: the plaintext password
//   - jwtSecret: the JWT signing secret
//
// Returns:
//   - *LoginResult: the login result, including the Token and member info
//   - error: error returned when login fails (e.g. email does not exist, wrong password)
func (s *Store) Login(ctx context.Context, email, password, jwtSecret string) (*LoginResult, error) {
	member, err := s.q.GetMemberByEmail(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	if member.PasswordHash == "" {
		return nil, fmt.Errorf("invalid email or password")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(member.PasswordHash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid email or password")
	}

	// Find the member's first workspace membership to obtain workspace_id and role
	var workspaceID uuid.UUID
	var role string
	err = s.db.QueryRowContext(ctx,
		`SELECT workspace_id, role FROM workspace_members WHERE member_id = $1 ORDER BY created_at LIMIT 1`,
		member.ID).Scan(&workspaceID, &role)
	if err != nil {
		return nil, fmt.Errorf("member has no workspace membership")
	}

	token, expiresAt, jti, err := GenerateJWT(member.ID, "member", jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	domainMember, _ := ToDomainMember(member)
	return &LoginResult{
		Token:       token,
		ExpiresAt:   expiresAt,
		JTI:         jti,
		Member:      domainMember,
		WorkspaceID: workspaceID,
		Role:        role,
	}, nil
}

// Register registers a new user, automatically creating a workspace and setting the user as owner.
//
// Steps:
//  1. Hash the password with bcrypt
//  2. Create the workspace (name: "{username}'s Workspace")
//  3. Seed the 5 built-in workflow templates
//  4. Create the member record
//  5. Create the workspace membership (role: owner)
//  6. Update the password hash
//  7. Generate a JWT Token
//
// Parameters:
//   - ctx: request context
//   - name: the user's name
//   - email: the user's email
//   - password: the plaintext password
//   - jwtSecret: the JWT signing secret
//
// Returns:
//   - *RegisterResult: the registration result, including the Token and member info
//   - error: error returned when registration fails
func (s *Store) Register(ctx context.Context, name, email, password, jwtSecret string) (*RegisterResult, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// Every new user gets their own workspace
	ws, err := s.q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		Name:        name + "'s Workspace",
		Description: nullString("Auto-created workspace for " + name),
		IssuePrefix: "TM",
		IsDefault:   true,
	})
	if err != nil {
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	workspaceID := ws.ID
	if err := s.SeedBuiltinTemplates(ctx, ws.ID); err != nil {
		slog.Warn("workspace created but builtin templates failed", "workspace_id", ws.ID, "err", err)
	}

	// The first user in their own workspace becomes the owner
	role := "owner"

	member, err := s.q.CreateMember(ctx, db.CreateMemberParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return nil, fmt.Errorf("create member: %w", err)
	}

	// Add the member to the workspace with the owner role
	_, err = s.q.CreateWorkspaceMember(ctx, db.CreateWorkspaceMemberParams{
		WorkspaceID: workspaceID,
		MemberID:    member.ID,
		Role:        role,
	})
	if err != nil {
		return nil, fmt.Errorf("create workspace member: %w", err)
	}

	if err := s.q.UpdateMemberPasswordHash(ctx, db.UpdateMemberPasswordHashParams{
		ID:           member.ID,
		PasswordHash: string(hash),
	}); err != nil {
		return nil, fmt.Errorf("update password hash: %w", err)
	}
	member.PasswordHash = string(hash)

	token, expiresAt, jti, err := GenerateJWT(member.ID, "member", jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}

	domainMember, _ := ToDomainMember(member)
	return &RegisterResult{
		Token:       token,
		ExpiresAt:   expiresAt,
		JTI:         jti,
		Member:      domainMember,
		WorkspaceID: workspaceID,
		Role:        role,
	}, nil
}

// GenerateJWT generates a JWT Token containing a jti claim for the user, valid for 24 hours.
//
// JWT Claims include:
//   - jti: Token unique identifier (used for Token revocation)
//   - user_id: the user ID
//   - user_type: the user type ("member" or "agent")
//   - exp/iat: expiration time and issued-at time
//
// Parameters:
//   - userID: the user ID
//   - userType: the user type
//   - jwtSecret: the JWT signing secret
//
// Returns:
//   - string: the JWT Token string
//   - time.Time: the expiration time
//   - string: the JTI (Token unique identifier)
//   - error: error returned when generation fails
func GenerateJWT(userID uuid.UUID, userType string, jwtSecret string) (string, time.Time, string, error) {
	expiresAt := time.Now().Add(24 * time.Hour)
	jti := uuid.New().String()

	claims := jwt.MapClaims{
		"jti":       jti,
		"user_id":   userID.String(),
		"user_type": userType,
		"exp":       expiresAt.Unix(),
		"iat":       time.Now().Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(jwtSecret))
	if err != nil {
		return "", time.Time{}, "", err
	}

	return tokenStr, expiresAt, jti, nil
}

// SessionTokenResult encapsulates the return result of a session Token exchange.
//
// The session Token is used for authenticated communication between the Agent daemon and the Server.
type SessionTokenResult struct {
	SessionToken string    // session Token string
	ExpiresAt    time.Time // expiration time (7 days)
	AgentID      uuid.UUID // Agent ID
}

// ExchangeAPITokenForSession exchanges an API Token for a session Token.
//
// Steps:
//  1. Compute the SHA-256 lookup hash of the API Token
//  2. Look up the matching Token record in the auth_tokens table
//  3. Verify the API Token with bcrypt
//  4. Generate a session Token (format: st_{agent_id_short}_{32_hex_random})
//  5. Hash the session Token with bcrypt and store it
//  6. The session Token is valid for 7 days
//
// Parameters:
//   - ctx: request context
//   - apiToken: the Agent's API Token plaintext
//
// Returns:
//   - *SessionTokenResult: the session Token result
//   - error: error returned when the exchange fails (e.g. Token invalid or expired)
func (s *Store) ExchangeAPITokenForSession(ctx context.Context, apiToken string) (*SessionTokenResult, error) {
	// Compute a SHA-256 lookup hash for efficient database queries
	lookupHash := sha256.Sum256([]byte(apiToken))
	lookupHashStr := hex.EncodeToString(lookupHash[:])

	// Look up the API Token by lookup_hash
	var tokenHash string
	var ownerType string
	var ownerIDStr string
	var tokenID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT id, token_hash, owner_type, owner_id FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'api' AND expires_at > NOW()`,
		lookupHashStr).Scan(&tokenID, &tokenHash, &ownerType, &ownerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid or expired api token")
	}

	// Verify the API Token with bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(apiToken)); err != nil {
		return nil, fmt.Errorf("invalid or expired api token")
	}

	if ownerType != "agent" {
		return nil, fmt.Errorf("only agents can exchange api tokens for session tokens")
	}

	ownerID, err := uuid.Parse(ownerIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid owner_id in token record")
	}

	// Generate the session Token: st_{agent_id_short}_{32_hex_random}
	sessionToken, err := generateSessionToken(ownerID)
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	// Store the session Token's bcrypt hash and SHA-256 lookup hash
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(sessionToken), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash session token: %w", err)
	}
	sessionLookupHash := sha256.Sum256([]byte(sessionToken))
	sessionLookupHashStr := hex.EncodeToString(sessionLookupHash[:])

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, lookup_hash, token_type, owner_type, owner_id, expires_at)
		 VALUES ($1, $2, 'session', 'agent', $3, $4)`,
		string(bcryptHash), sessionLookupHashStr, ownerID, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("store session token: %w", err)
	}

	return &SessionTokenResult{
		SessionToken: sessionToken,
		ExpiresAt:    expiresAt,
		AgentID:      ownerID,
	}, nil
}

// generateSessionToken generates a session Token with the format st_{agent_id_short}_{32_hex_random}.
//
// Token structure:
//   - st_: fixed prefix identifying a Session Token
//   - agent_id_short: the first 8 characters of the Agent ID after removing hyphens
//   - 32_hex_random: the hexadecimal representation of 16 random bytes
//
// Parameters:
//   - agentID: the Agent's UUID
//
// Returns:
//   - string: the generated session Token plaintext
//   - error: error returned when random number generation fails
func generateSessionToken(agentID uuid.UUID) (string, error) {
	idShort := strings.ReplaceAll(agentID.String(), "-", "")[:8]
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("st_%s_%s", idShort, hex.EncodeToString(randomBytes)), nil
}

// DeleteSessionToken deletes the matching session record by the Token plaintext (matched via the SHA-256 lookup hash).
//
// Parameters:
//   - ctx: request context
//   - token: the session Token plaintext
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) DeleteSessionToken(ctx context.Context, token string) error {
	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'session'`,
		lookupHash)
	return err
}

// WhoamiInfo encapsulates the authenticated user info returned by the whoami endpoint.
//
// Supports both member and agent user types; fields differ based on the type.
type WhoamiInfo struct {
	ID          uuid.UUID `json:"id"`               // user ID
	Name        string    `json:"name"`             // user name
	UserType    string    `json:"user_type"`        // user type ("member" or "agent")
	WorkspaceID uuid.UUID `json:"workspace_id"`     // workspace ID
	Email       string    `json:"email,omitempty"`  // email (member only)
	Role        string    `json:"role,omitempty"`   // role (member only)
	Status      string    `json:"status,omitempty"` // status (agent only)
}

// GetWhoamiInfo queries user info by owner type and ID (supports both member and agent types).
//
// Parameters:
//   - ctx: request context
//   - ownerType: user type ("member" or "agent")
//   - ownerID: user ID
//
// Returns:
//   - *WhoamiInfo: user info
//   - error: error returned when the query fails
func (s *Store) GetWhoamiInfo(ctx context.Context, ownerType string, ownerID uuid.UUID) (*WhoamiInfo, error) {
	switch ownerType {
	case "member":
		member, err := s.q.GetMember(ctx, ownerID)
		if err != nil {
			return nil, fmt.Errorf("get member: %w", err)
		}
		// Find the member's workspace membership
		var workspaceID uuid.UUID
		var role string
		err = s.db.QueryRowContext(ctx,
			`SELECT workspace_id, role FROM workspace_members WHERE member_id = $1 ORDER BY created_at LIMIT 1`,
			member.ID).Scan(&workspaceID, &role)
		if err != nil {
			workspaceID = uuid.UUID{}
			role = ""
		}
		return &WhoamiInfo{
			ID:          member.ID,
			Name:        member.Name,
			UserType:    "member",
			WorkspaceID: workspaceID,
			Email:       member.Email,
			Role:        role,
		}, nil
	case "agent":
		agent, err := s.q.GetAgent(ctx, ownerID)
		if err != nil {
			return nil, fmt.Errorf("get agent: %w", err)
		}
		return &WhoamiInfo{
			ID:          agent.ID,
			Name:        agent.Name,
			UserType:    "agent",
			WorkspaceID: agent.WorkspaceID,
			Status:      string(agent.Status),
		}, nil
	default:
		return nil, fmt.Errorf("unknown owner type: %s", ownerType)
	}
}

// UpdateRuntimePublicKey updates the public key field of a Runtime.
//
// The public key is used to verify secure communication between the Agent and the Server.
//
// Parameters:
//   - ctx: request context
//   - runtimeID: the Runtime's UUID
//   - publicKey: the RSA public key string
//
// Returns:
//   - error: error returned when the update fails
func (s *Store) UpdateRuntimePublicKey(ctx context.Context, runtimeID uuid.UUID, publicKey string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runtimes SET public_key = $1, updated_at = NOW() WHERE id = $2`,
		publicKey, runtimeID)
	return err
}

// GetRuntimeByID queries a single Runtime record by ID.
//
// Parameters:
//   - ctx: request context
//   - runtimeID: the Runtime's UUID
//
// Returns:
//   - types.Runtime: the Runtime record
//   - error: error returned when the query fails
func (s *Store) GetRuntimeByID(ctx context.Context, runtimeID uuid.UUID) (types.Runtime, error) {
	r, err := s.q.GetRuntime(ctx, runtimeID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("get runtime by id: %w", err)
	}
	return ToDomainRuntime(r)
}

// GetAuthTokenByLookupHashAndType queries a non-expired auth token by lookup_hash and token_type.
//
// Parameters:
//   - ctx: request context
//   - lookupHash: the SHA-256 lookup hash of the token
//   - tokenType: the token type (session or api)
//
// Returns:
//   - db.GetAuthTokenByLookupHashAndTypeRow: includes owner_type, owner_id, token_hash
//   - error: error returned when the query fails
func (s *Store) GetAuthTokenByLookupHashAndType(ctx context.Context, lookupHash string, tokenType string) (types.GetAuthTokenByLookupHashAndTypeRow, error) {
	row, err := s.q.GetAuthTokenByLookupHashAndType(ctx, db.GetAuthTokenByLookupHashAndTypeParams{
		LookupHash: lookupHash,
		TokenType:  db.TokenType(tokenType),
	})
	if err != nil {
		return types.GetAuthTokenByLookupHashAndTypeRow{}, fmt.Errorf("get auth token by lookup hash: %w", err)
	}
	return types.GetAuthTokenByLookupHashAndTypeRow{
		OwnerType: row.OwnerType,
		OwnerID:   row.OwnerID.String(),
		TokenHash: row.TokenHash,
	}, nil
}

// GetLatestPublicKeyForAgent queries the most recently updated public key for the specified Agent.
//
// Parameters:
//   - ctx: request context
//   - agentID: the Agent's UUID
//
// Returns:
//   - string: the RSA public key string
//   - error: error returned when the query fails (e.g. no public key record)
func (s *Store) GetLatestPublicKeyForAgent(ctx context.Context, agentID uuid.UUID) (string, error) {
	var publicKey string
	err := s.db.QueryRowContext(ctx,
		`SELECT public_key FROM runtimes
		 WHERE agent_id = $1 AND public_key IS NOT NULL AND public_key != ''
		 ORDER BY updated_at DESC LIMIT 1`,
		agentID).Scan(&publicKey)
	if err != nil {
		return "", fmt.Errorf("no public key found for agent: %w", err)
	}
	return publicKey, nil
}

// GetGitCredentialsByProject queries all Git credential records for the specified project.
//
// Parameters:
//   - ctx: request context
//   - projectID: project UUID
//
// Returns:
//   - []types.GitCredential: the Git credential list
//   - error: error returned when the query fails
func (s *Store) GetGitCredentialsByProject(ctx context.Context, projectID uuid.UUID) ([]types.GitCredential, error) {
	creds, err := s.q.ListGitCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list git credentials by project: %w", err)
	}
	return ToDomainGitCredentialSlice(creds)
}

// CreateGitCredential creates a new Git credential record.
//
// The credential contains an encrypted PAT (Personal Access Token), used for repository access authentication.
//
// Parameters:
//   - ctx: request context
//   - arg: credential creation parameters
//
// Returns:
//   - types.GitCredential: the created credential record
//   - error: error returned when creation fails
func (s *Store) CreateGitCredential(ctx context.Context, arg types.CreateGitCredentialParams) (types.GitCredential, error) {
	projectUUID, err := uuid.Parse(arg.ProjectID)
	if err != nil {
		return types.GitCredential{}, fmt.Errorf("parse project id: %w", err)
	}
	var createdBy uuid.NullUUID
	if arg.CreatedBy != nil {
		cu, err := uuid.Parse(*arg.CreatedBy)
		if err != nil {
			return types.GitCredential{}, fmt.Errorf("parse created_by: %w", err)
		}
		createdBy = uuid.NullUUID{UUID: cu, Valid: true}
	}
	cred, err := s.q.CreateGitCredential(ctx, db.CreateGitCredentialParams{
		ProjectID:    projectUUID,
		RepoUrl:      arg.RepoURL,
		Username:     arg.Username,
		EncryptedPat: arg.EncryptedPAT,
		CreatedBy:    createdBy,
	})
	if err != nil {
		return types.GitCredential{}, fmt.Errorf("create git credential: %w", err)
	}
	return ToDomainGitCredential(cred)
}

// UpdateGitCredential updates an existing Git credential record.
//
// Parameters:
//   - ctx: request context
//   - arg: credential update parameters
//
// Returns:
//   - types.GitCredential: the updated credential record
//   - error: error returned when the update fails
func (s *Store) UpdateGitCredential(ctx context.Context, arg types.UpdateGitCredentialParams) (types.GitCredential, error) {
	id, err := uuid.Parse(arg.ID)
	if err != nil {
		return types.GitCredential{}, fmt.Errorf("parse git credential id: %w", err)
	}
	cred, err := s.q.UpdateGitCredential(ctx, db.UpdateGitCredentialParams{
		ID:           id,
		RepoUrl:      arg.RepoURL,
		Username:     arg.Username,
		EncryptedPat: arg.EncryptedPAT,
	})
	if err != nil {
		return types.GitCredential{}, fmt.Errorf("update git credential: %w", err)
	}
	return ToDomainGitCredential(cred)
}

// GetGitCredential queries a single Git credential record by ID.
//
// Parameters:
//   - ctx: request context
//   - id: credential UUID
//
// Returns:
//   - types.GitCredential: the credential record
//   - error: error returned when the query fails
func (s *Store) GetGitCredential(ctx context.Context, id uuid.UUID) (types.GitCredential, error) {
	cred, err := s.q.GetGitCredential(ctx, id)
	if err != nil {
		return types.GitCredential{}, fmt.Errorf("get git credential: %w", err)
	}
	return ToDomainGitCredential(cred)
}

// ChangePassword changes a member's password after verifying the old password (bcrypt hash storage).
//
// Steps:
//  1. Query the member record
//  2. Verify the old password (bcrypt verification)
//  3. Hash the new password with bcrypt
//  4. Update the password hash
//
// Parameters:
//   - ctx: request context
//   - memberID: member UUID
//   - oldPassword: the old password
//   - newPassword: the new password
//
// Returns:
//   - error: error returned when the change fails (e.g. wrong old password)
func (s *Store) ChangePassword(ctx context.Context, memberID uuid.UUID, oldPassword, newPassword string) error {
	member, err := s.q.GetMember(ctx, memberID)
	if err != nil {
		return fmt.Errorf("member not found")
	}

	if member.PasswordHash == "" {
		return fmt.Errorf("account uses OAuth, password change not available")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(member.PasswordHash), []byte(oldPassword)); err != nil {
		return fmt.Errorf("incorrect old password")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	if err := s.q.UpdateMemberPasswordHash(ctx, db.UpdateMemberPasswordHashParams{
		ID:           memberID,
		PasswordHash: string(hash),
	}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	return nil
}

// CheckLoginLockout checks via Redis whether the account is temporarily locked due to too many failed logins.
//
// Uses Redis to store the lockout state, key format: login_lockout:{email}
// Lockout duration: 15 minutes
//
// Parameters:
//   - ctx: request context
//   - email: the user's email
//   - rdb: Redis client
//
// Returns:
//   - error: error returned when the account is locked; otherwise nil
func (s *Store) CheckLoginLockout(ctx context.Context, email string, rdb *redis.Client) error {
	if rdb == nil {
		return nil
	}
	key := fmt.Sprintf("login_lockout:%s", email)
	val, err := rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return nil // Redis error, allow login
	}
	return fmt.Errorf("account temporarily locked due to too many failed login attempts, please try again later (locked until: %s)", val)
}

// RecordLoginFailure records a login failure via Redis, locking the account for 15 minutes after 5 accumulated failures.
//
// Uses a Redis counter, key format: login_attempts:{email}
// On the first failure, sets a 15-minute expiration; upon reaching 5 failures, writes the lockout key.
//
// Parameters:
//   - ctx: request context
//   - email: the user's email
//   - rdb: Redis client
func (s *Store) RecordLoginFailure(ctx context.Context, email string, rdb *redis.Client) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("login_attempts:%s", email)
	count, err := rdb.Incr(ctx, key).Result()
	if err != nil {
		return
	}
	if count == 1 {
		rdb.Expire(ctx, key, 15*time.Minute)
	}
	if count >= 5 {
		// Lock the account for 15 minutes
		lockoutKey := fmt.Sprintf("login_lockout:%s", email)
		lockoutUntil := time.Now().Add(15 * time.Minute).Format(time.RFC3339)
		rdb.Set(ctx, lockoutKey, lockoutUntil, 15*time.Minute)
	}
}

// RecordLoginSuccess clears the login failure count via Redis (called on login success).
//
// Deletes the login_attempts:{email} key from Redis.
//
// Parameters:
//   - ctx: request context
//   - email: the user's email
//   - rdb: Redis client
func (s *Store) RecordLoginSuccess(ctx context.Context, email string, rdb *redis.Client) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("login_attempts:%s", email)
	rdb.Del(ctx, key)
}

// CreatePasswordResetToken generates a password reset Token for a member.
//
// Steps:
//  1. Query the member by email (does not leak whether the email exists)
//  2. Generate a 32-byte random Token (format: reset_{hex})
//  3. Hash the Token with bcrypt
//  4. Compute a SHA-256 lookup hash
//  5. Store it in the auth_tokens table, valid for 1 hour
//
// Parameters:
//   - ctx: request context
//   - email: the user's email
//
// Returns:
//   - string: the reset Token plaintext
//   - error: error returned when generation fails
func (s *Store) CreatePasswordResetToken(ctx context.Context, email string) (string, error) {
	member, err := s.q.GetMemberByEmail(ctx, email)
	if err != nil {
		// Do not expose whether this email exists
		return "", nil
	}

	if member.PasswordHash == "" {
		// OAuth user, cannot reset password
		return "", nil
	}

	// Generate the reset Token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate reset token: %w", err)
	}
	token := "reset_" + hex.EncodeToString(tokenBytes)

	// Store the bcrypt hash in token_hash and the SHA-256 hash in lookup_hash
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash reset token: %w", err)
	}
	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])

	// Store in auth_tokens, valid for 1 hour
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, lookup_hash, token_type, owner_type, owner_id, expires_at)
		 VALUES ($1, $2, 'password_reset', 'member', $3, NOW() + INTERVAL '1 hour')`,
		string(bcryptHash), lookupHash, member.ID)
	if err != nil {
		return "", fmt.Errorf("store reset token: %w", err)
	}

	return token, nil
}

// ResetPasswordWithToken uses a valid reset Token to set a new password for a member.
//
// Steps:
//  1. Compute the SHA-256 lookup hash of the Token
//  2. Look up the matching reset Token in the auth_tokens table
//  3. Verify the Token with bcrypt
//  4. Hash the new password with bcrypt
//  5. Update the member's password hash
//  6. Delete the used reset Token
//
// Parameters:
//   - ctx: request context
//   - token: the reset Token plaintext
//   - newPassword: the new password
//
// Returns:
//   - error: error returned when reset fails (e.g. Token invalid or expired)
func (s *Store) ResetPasswordWithToken(ctx context.Context, token, newPassword string) error {
	// Compute the lookup hash for efficient database queries
	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])

	// Look up the reset Token by lookup_hash
	var tokenHash string
	var ownerID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, owner_id FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'password_reset' AND expires_at > NOW()`,
		lookupHash).Scan(&tokenHash, &ownerID)
	if err != nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	// Verify the Token with bcrypt
	if err := bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)); err != nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	// Hash the new password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// Update the member's password
	if err := s.q.UpdateMemberPasswordHash(ctx, db.UpdateMemberPasswordHashParams{
		ID:           ownerID,
		PasswordHash: string(passwordHash),
	}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// Delete the used reset Token
	s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'password_reset'`,
		lookupHash)

	return nil
}
