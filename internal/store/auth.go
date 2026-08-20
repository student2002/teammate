// auth.go 提供认证和授权相关的数据访问操作。
//
// 包含用户登录/注册、JWT Token 生成、API Token 交换会话 Token、
// 密码重置、登录锁定、Git 凭据管理等功能。
//
// 认证流程：
//   - 人类用户：邮箱+密码 → bcrypt 校验 → JWT Token（24小时有效）
//   - Agent：API Token → SHA-256 查找 → bcrypt 验证 → 会话 Token（7天有效）
//
// 安全特性：
//   - Token 存储采用双重哈希：bcrypt 安全存储 + SHA-256 高效查找
//   - 登录失败 5 次锁定 15 分钟（Redis 实现）
//   - 密码重置 Token 1 小时过期
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

// LoginResult 封装登录操作的返回结果。
//
// 包含 JWT Token、过期时间、JTI（Token 唯一标识）、成员信息、工作区 ID 和角色。
type LoginResult struct {
	Token       string        // JWT Token 字符串
	ExpiresAt   time.Time     // Token 过期时间
	JTI         string        // Token 唯一标识（用于 Token 撤销）
	Member      types.Member  // 成员信息（domain 幜格，不含 PasswordHash）
	WorkspaceID uuid.UUID     // 工作区 ID
	Role        string        // 成员角色（owner/admin/member/viewer）
}

// RegisterResult 封装注册操作的返回结果。
//
// 结构与 LoginResult 相同，注册后自动登录。
type RegisterResult struct {
	Token       string        // JWT Token 字符串
	ExpiresAt   time.Time     // Token 过期时间
	JTI         string        // Token 唯一标识
	Member      types.Member  // 成员信息（domain 幜格，不含 PasswordHash）
	WorkspaceID uuid.UUID     // 工作区 ID
	Role        string        // 成员角色
}

// Login 通过邮箱和密码认证成员身份。
//
// 执行步骤：
//  1. 根据邮箱查询成员记录
//  2. 使用 bcrypt 校验密码
//  3. 查询成员的第一个工作区成员关系
//  4. 生成 JWT Token（有效期 24 小时）
//
// 参数：
//   - ctx: 请求上下文
//   - email: 成员邮箱
//   - password: 明文密码
//   - jwtSecret: JWT 签名密钥
//
// 返回：
//   - *LoginResult: 登录结果，包含 Token 和成员信息
//   - error: 登录失败时返回错误（如邮箱不存在、密码错误）
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

	// 查找成员的第一个工作区成员关系，以获取 workspace_id 和 role
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

// Register 注册新用户，自动创建工作区并设置为 owner。
//
// 执行步骤：
//  1. 使用 bcrypt 哈希密码
//  2. 创建工作区（名称："{用户名}'s Workspace"）
//  3. 种子里建的 5 个内置工作流模板
//  4. 创建成员记录
//  5. 创建工作区成员关系（角色：owner）
//  6. 更新密码哈希
//  7. 生成 JWT Token
//
// 参数：
//   - ctx: 请求上下文
//   - name: 用户名称
//   - email: 用户邮箱
//   - password: 明文密码
//   - jwtSecret: JWT 签名密钥
//
// 返回：
//   - *RegisterResult: 注册结果，包含 Token 和成员信息
//   - error: 注册失败时返回错误
func (s *Store) Register(ctx context.Context, name, email, password, jwtSecret string) (*RegisterResult, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	// 每个新用户都会拥有自己的工作区
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

	// 自己工作区中的第一个用户成为 owner
	role := "owner"

	member, err := s.q.CreateMember(ctx, db.CreateMemberParams{
		Name:  name,
		Email: email,
	})
	if err != nil {
		return nil, fmt.Errorf("create member: %w", err)
	}

	// 以 owner 角色将成员添加到工作区
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

// GenerateJWT 为用户生成包含 jti 声明的 JWT Token，有效期 24 小时。
//
// JWT Claims 包含：
//   - jti: Token 唯一标识（用于 Token 撤销）
//   - user_id: 用户 ID
//   - user_type: 用户类型（"member" 或 "agent"）
//   - exp/iat: 过期时间和签发时间
//
// 参数：
//   - userID: 用户 ID
//   - userType: 用户类型
//   - jwtSecret: JWT 签名密钥
//
// 返回：
//   - string: JWT Token 字符串
//   - time.Time: 过期时间
//   - string: JTI（Token 唯一标识）
//   - error: 生成失败时返回错误
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

// SessionTokenResult 封装会话 Token 交换的返回结果。
//
// 会话 Token 用于 Agent 守护进程与 Server 的认证通信。
type SessionTokenResult struct {
	SessionToken string    // 会话 Token 字符串
	ExpiresAt    time.Time // 过期时间（7天）
	AgentID      uuid.UUID // Agent ID
}

// ExchangeAPITokenForSession 将 API Token 换取会话 Token。
//
// 执行步骤：
//  1. 计算 API Token 的 SHA-256 查找哈希
//  2. 在 auth_tokens 表中查找匹配的 Token 记录
//  3. 使用 bcrypt 验证 API Token
//  4. 生成会话 Token（格式：st_{agent_id_short}_{32_hex_random}）
//  5. 使用 bcrypt 哈希会话 Token 并存储
//  6. 会话 Token 有效期 7 天
//
// 参数：
//   - ctx: 请求上下文
//   - apiToken: Agent 的 API Token 明文
//
// 返回：
//   - *SessionTokenResult: 会话 Token 结果
//   - error: 交换失败时返回错误（如 Token 无效或过期）
func (s *Store) ExchangeAPITokenForSession(ctx context.Context, apiToken string) (*SessionTokenResult, error) {
	// 计算 SHA-256 查找哈希以实现高效的数据库查询
	lookupHash := sha256.Sum256([]byte(apiToken))
	lookupHashStr := hex.EncodeToString(lookupHash[:])

	// 通过 lookup_hash 查找 API Token
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

	// 使用 bcrypt 校验 API Token
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

	// 生成会话 Token：st_{agent_id_short}_{32_hex_random}
	sessionToken, err := generateSessionToken(ownerID)
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	expiresAt := time.Now().Add(7 * 24 * time.Hour)

	// 存储会话 Token 的 bcrypt 哈希及 SHA-256 查找哈希
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

// generateSessionToken 生成格式为 st_{agent_id_short}_{32_hex_random} 的会话 Token。
//
// Token 结构：
//   - st_: 固定前缀，标识 Session Token
//   - agent_id_short: Agent ID 去除连字符后的前 8 位
//   - 32_hex_random: 16 字节随机数的十六进制表示
//
// 参数：
//   - agentID: Agent 的 UUID
//
// 返回：
//   - string: 生成的会话 Token 明文
//   - error: 随机数生成失败时返回错误
func generateSessionToken(agentID uuid.UUID) (string, error) {
	idShort := strings.ReplaceAll(agentID.String(), "-", "")[:8]
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("st_%s_%s", idShort, hex.EncodeToString(randomBytes)), nil
}

// DeleteSessionToken 根据 Token 原文删除对应的会话记录（通过 SHA-256 查找哈希匹配）。
//
// 参数：
//   - ctx: 请求上下文
//   - token: 会话 Token 明文
//
// 返回：
//   - error: 删除失败时返回错误
func (s *Store) DeleteSessionToken(ctx context.Context, token string) error {
	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'session'`,
		lookupHash)
	return err
}

// WhoamiInfo 封装 whoami 接口返回的已认证用户信息。
//
// 支持 member 和 agent 两种用户类型，字段根据类型有所不同。
type WhoamiInfo struct {
	ID          uuid.UUID `json:"id"`               // 用户 ID
	Name        string    `json:"name"`             // 用户名称
	UserType    string    `json:"user_type"`        // 用户类型（"member" 或 "agent"）
	WorkspaceID uuid.UUID `json:"workspace_id"`     // 工作区 ID
	Email       string    `json:"email,omitempty"`  // 邮箱（仅 member）
	Role        string    `json:"role,omitempty"`   // 角色（仅 member）
	Status      string    `json:"status,omitempty"` // 状态（仅 agent）
}

// GetWhoamiInfo 根据所有者类型和 ID 查询用户信息（支持 member 和 agent 两种类型）。
//
// 参数：
//   - ctx: 请求上下文
//   - ownerType: 用户类型（"member" 或 "agent"）
//   - ownerID: 用户 ID
//
// 返回：
//   - *WhoamiInfo: 用户信息
//   - error: 查询失败时返回错误
func (s *Store) GetWhoamiInfo(ctx context.Context, ownerType string, ownerID uuid.UUID) (*WhoamiInfo, error) {
	switch ownerType {
	case "member":
		member, err := s.q.GetMember(ctx, ownerID)
		if err != nil {
			return nil, fmt.Errorf("get member: %w", err)
		}
		// 查找成员的工作区成员关系
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

// UpdateRuntimePublicKey 更新 Runtime 的公钥字段。
//
// 公钥用于 Agent 与 Server 之间的安全通信验证。
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: Runtime 的 UUID
//   - publicKey: RSA 公钥字符串
//
// 返回：
//   - error: 更新失败时返回错误
func (s *Store) UpdateRuntimePublicKey(ctx context.Context, runtimeID uuid.UUID, publicKey string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE runtimes SET public_key = $1, updated_at = NOW() WHERE id = $2`,
		publicKey, runtimeID)
	return err
}

// GetRuntimeByID 根据 ID 查询单个 Runtime 记录。
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: Runtime 的 UUID
//
// 返回：
//   - types.Runtime: Runtime 记录
//   - error: 查询失败时返回错误
func (s *Store) GetRuntimeByID(ctx context.Context, runtimeID uuid.UUID) (types.Runtime, error) {
	r, err := s.q.GetRuntime(ctx, runtimeID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("get runtime by id: %w", err)
	}
	return ToDomainRuntime(r)
}

// GetAuthTokenByLookupHashAndType 根据 lookup_hash 和 token_type 查询未过期的认证令牌。
//
// 参数：
//   - ctx: 请求上下文
//   - lookupHash: 令牌的 SHA-256 查找哈希
//   - tokenType: 令牌类型（session 或 api）
//
// 返回：
//   - db.GetAuthTokenByLookupHashAndTypeRow: 包含 owner_type、owner_id、token_hash
//   - error: 查询失败时返回错误
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

// GetLatestPublicKeyForAgent 查询指定 Agent 最近一次更新的公钥。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: Agent 的 UUID
//
// 返回：
//   - string: RSA 公钥字符串
//   - error: 查询失败时返回错误（如无公钥记录）
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

// GetGitCredentialsByProject 查询指定项目的所有 Git 凭据记录。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 UUID
//
// 返回：
//   - []types.GitCredential: Git 凭据列表
//   - error: 查询失败时返回错误
func (s *Store) GetGitCredentialsByProject(ctx context.Context, projectID uuid.UUID) ([]types.GitCredential, error) {
	creds, err := s.q.ListGitCredentialsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list git credentials by project: %w", err)
	}
	return ToDomainGitCredentialSlice(creds)
}

// CreateGitCredential 创建一条新的 Git 凭据记录。
//
// 凭据包含加密的 PAT（Personal Access Token），用于仓库访问认证。
//
// 参数：
//   - ctx: 请求上下文
//   - arg: 凭据创建参数
//
// 返回：
//   - types.GitCredential: 创建的凭据记录
//   - error: 创建失败时返回错误
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

// UpdateGitCredential 更新已有的 Git 凭据记录。
//
// 参数：
//   - ctx: 请求上下文
//   - arg: 凭据更新参数
//
// 返回：
//   - types.GitCredential: 更新后的凭据记录
//   - error: 更新失败时返回错误
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

// GetGitCredential 根据 ID 查询单条 Git 凭据记录。
//
// 参数：
//   - ctx: 请求上下文
//   - id: 凭据 UUID
//
// 返回：
//   - types.GitCredential: 凭据记录
//   - error: 查询失败时返回错误
func (s *Store) GetGitCredential(ctx context.Context, id uuid.UUID) (types.GitCredential, error) {
	cred, err := s.q.GetGitCredential(ctx, id)
	if err != nil {
		return types.GitCredential{}, fmt.Errorf("get git credential: %w", err)
	}
	return ToDomainGitCredential(cred)
}

// ChangePassword 在验证旧密码后修改成员密码（bcrypt 哈希存储）。
//
// 执行步骤：
//  1. 查询成员记录
//  2. 验证旧密码（bcrypt 校验）
//  3. 使用 bcrypt 哈希新密码
//  4. 更新密码哈希
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员 UUID
//   - oldPassword: 旧密码
//   - newPassword: 新密码
//
// 返回：
//   - error: 修改失败时返回错误（如旧密码错误）
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

// CheckLoginLockout 通过 Redis 检查账户是否因登录失败次数过多而被临时锁定。
//
// 使用 Redis 存储锁定状态，键格式：login_lockout:{email}
// 锁定时长：15 分钟
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱
//   - rdb: Redis 客户端
//
// 返回：
//   - error: 账户被锁定时返回错误，否则返回 nil
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
		return nil // Redis 错误，允许登录
	}
	return fmt.Errorf("account temporarily locked due to too many failed login attempts, please try again later (locked until: %s)", val)
}

// RecordLoginFailure 通过 Redis 记录一次登录失败，累计 5 次后锁定账户 15 分钟。
//
// 使用 Redis 计数器，键格式：login_attempts:{email}
// 首次失败时设置 15 分钟过期，达到 5 次时写入锁定键。
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱
//   - rdb: Redis 客户端
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
		// 锁定账户 15 分钟
		lockoutKey := fmt.Sprintf("login_lockout:%s", email)
		lockoutUntil := time.Now().Add(15 * time.Minute).Format(time.RFC3339)
		rdb.Set(ctx, lockoutKey, lockoutUntil, 15*time.Minute)
	}
}

// RecordLoginSuccess 通过 Redis 清除登录失败计数（登录成功时调用）。
//
// 删除 Redis 中的 login_attempts:{email} 键。
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱
//   - rdb: Redis 客户端
func (s *Store) RecordLoginSuccess(ctx context.Context, email string, rdb *redis.Client) {
	if rdb == nil {
		return
	}
	key := fmt.Sprintf("login_attempts:%s", email)
	rdb.Del(ctx, key)
}

// CreatePasswordResetToken 为成员生成密码重置 Token。
//
// 执行步骤：
//  1. 根据邮箱查询成员（不泄露邮箱是否存在）
//  2. 生成 32 字节随机 Token（格式：reset_{hex}）
//  3. 使用 bcrypt 哈希 Token
//  4. 计算 SHA-256 查找哈希
//  5. 存储到 auth_tokens 表，有效期 1 小时
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱
//
// 返回：
//   - string: 重置 Token 明文
//   - error: 生成失败时返回错误
func (s *Store) CreatePasswordResetToken(ctx context.Context, email string) (string, error) {
	member, err := s.q.GetMemberByEmail(ctx, email)
	if err != nil {
		// 不要暴露该邮箱是否存在
		return "", nil
	}

	if member.PasswordHash == "" {
		// OAuth 用户，无法重置密码
		return "", nil
	}

	// 生成重置 Token
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("generate reset token: %w", err)
	}
	token := "reset_" + hex.EncodeToString(tokenBytes)

	// 将 bcrypt 哈希存入 token_hash，SHA-256 存入 lookup_hash
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(token), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash reset token: %w", err)
	}
	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])

	// 存入 auth_tokens，有效期 1 小时
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO auth_tokens (token_hash, lookup_hash, token_type, owner_type, owner_id, expires_at)
		 VALUES ($1, $2, 'password_reset', 'member', $3, NOW() + INTERVAL '1 hour')`,
		string(bcryptHash), lookupHash, member.ID)
	if err != nil {
		return "", fmt.Errorf("store reset token: %w", err)
	}

	return token, nil
}

// ResetPasswordWithToken 使用有效的重置 Token 为成员设置新密码。
//
// 执行步骤：
//  1. 计算 Token 的 SHA-256 查找哈希
//  2. 在 auth_tokens 表中查找匹配的重置 Token
//  3. 使用 bcrypt 验证 Token
//  4. 使用 bcrypt 哈希新密码
//  5. 更新成员密码哈希
//  6. 删除已使用的重置 Token
//
// 参数：
//   - ctx: 请求上下文
//   - token: 重置 Token 明文
//   - newPassword: 新密码
//
// 返回：
//   - error: 重置失败时返回错误（如 Token 无效或过期）
func (s *Store) ResetPasswordWithToken(ctx context.Context, token, newPassword string) error {
	// 计算查找哈希以实现高效的数据库查询
	shaHash := sha256.Sum256([]byte(token))
	lookupHash := hex.EncodeToString(shaHash[:])

	// 通过 lookup_hash 查找重置 Token
	var tokenHash string
	var ownerID uuid.UUID
	err := s.db.QueryRowContext(ctx,
		`SELECT token_hash, owner_id FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'password_reset' AND expires_at > NOW()`,
		lookupHash).Scan(&tokenHash, &ownerID)
	if err != nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	// 使用 bcrypt 校验 Token
	if err := bcrypt.CompareHashAndPassword([]byte(tokenHash), []byte(token)); err != nil {
		return fmt.Errorf("invalid or expired reset token")
	}

	// 对新密码进行哈希
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	// 更新成员的密码
	if err := s.q.UpdateMemberPasswordHash(ctx, db.UpdateMemberPasswordHashParams{
		ID:           ownerID,
		PasswordHash: string(passwordHash),
	}); err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	// 删除已使用的重置 Token
	s.db.ExecContext(ctx,
		`DELETE FROM auth_tokens WHERE lookup_hash = $1 AND token_type = 'password_reset'`,
		lookupHash)

	return nil
}
