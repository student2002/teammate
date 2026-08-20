// auth.go 实现认证和授权的业务逻辑，包括登录、注册、JWT 令牌管理。
//
// 本文件包含：
//   - AuthService 结构体：认证服务，封装 JWT 令牌管理、登录/注册流程、会话交换等
//   - Login：通过邮箱和密码进行认证，使用 Redis 做失败计数和账户锁定
//   - Register：创建新成员账户并返回 JWT 令牌，密码 bcrypt 哈希存储
//   - GenerateToken：为指定用户生成 JWT 令牌
//   - ExchangeAPITokenForSession：将长期 API 令牌交换为短期会话令牌
//   - Logout：吊销会话令牌使其立即失效
//   - Whoami：获取当前已认证用户的信息
//   - UpdateRuntimePublicKey/GetRuntimeByID/GetLatestPublicKeyForAgent：运行时公钥管理
//   - CreateGitCredential/UpdateGitCredential/GetGitCredential：Git 凭据管理（不允许删除，保证任务分支可追溯）
//   - ChangePassword/RequestPasswordReset/ResetPassword：密码管理
//
// 登录使用 Redis 做失败计数（5 次失败锁定 15 分钟）和 JWT jti 吊销支持。
// JWT 令牌的 jti 存入 Redis，支持令牌吊销验证，TTL 与 JWT 过期时间一致。
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

// ValidatePassword 校验密码强度。
// 规则：8-128 位，至少包含一个大写字母、一个小写字母和一个数字。
//
// 参数：
//   - password: 待校验的密码
//
// 返回：
//   - error: 密码不符合要求时返回描述性错误，符合时返回 nil
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

// AuthService 提供认证和授权相关的业务逻辑。
// 包含 JWT 令牌管理、登录/注册流程、会话令牌交换、密码重置等功能。
type AuthService struct {
	svc       *Service
	JWTSecret string // JWT 签名密钥
}

// NewAuthService 创建一个新的 AuthService 实例。
func NewAuthService(svc *Service, jwtSecret string) *AuthService {
	return &AuthService{svc: svc, JWTSecret: jwtSecret}
}

// LoginResult 保存登录操作的结果。
type LoginResult struct {
	Token       string        // JWT 令牌
	ExpiresAt   time.Time     // 令牌过期时间
	Member      types.Member  // 登录的成员信息（domain 幜格，不含 PasswordHash）
	WorkspaceID uuid.UUID     // 默认工作区 ID
	Role        string        // 成员在工作区中的角色
}

// Login 通过邮箱和密码进行成员认证登录。
// 登录前检查账户是否因失败次数过多被锁定，
// 登录成功后将 JWT jti 存入 Redis 以支持令牌吊销。
//
// 步骤：
//  1. 检查账户是否因失败尝试次数过多而被锁定（Redis 速率限制）
//  2. 验证邮箱和密码，生成 JWT 令牌
//  3. 登录失败时记录失败计数（Redis），达到阈值后锁定账户
//  4. 登录成功时清除失败计数（Redis）
//  5. 将 JWT jti 存入 Redis 用于令牌吊销验证（TTL 与 JWT 过期时间一致）
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱地址
//   - password: 用户密码（明文，内部进行 bcrypt 验证）
//
// 返回：
//   - *LoginResult: 包含 JWT 令牌、过期时间、成员信息、工作区 ID 和角色
//   - error: 可能的错误（账户锁定、密码错误、生成令牌失败）
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

	// 将 jti 存入 Redis 用于令牌验证（TTL 与 JWT 过期时间一致）
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

// RegisterResult 保存注册操作的结果。
type RegisterResult struct {
	Token       string        // JWT 令牌
	ExpiresAt   time.Time     // 令牌过期时间
	Member      types.Member  // 创建的成员信息（domain 幜格，不含 PasswordHash）
	WorkspaceID uuid.UUID     // 默认工作区 ID
	Role        string        // 成员在工作区中的角色
}

// Register 创建一个新的成员账户并返回 JWT 令牌。
// 注册成功后将 JWT jti 存入 Redis 以支持令牌吊销。
//
// 步骤：
//  1. 调用 Store 创建成员账户（密码 bcrypt 哈希存储）
//  2. 生成 JWT 令牌
//  3. 将 JWT jti 存入 Redis 用于令牌吊销验证
//
// 参数：
//   - ctx: 请求上下文
//   - name: 用户名称
//   - email: 用户邮箱地址（唯一）
//   - password: 用户密码（明文，内部进行 bcrypt 哈希）
//
// 返回：
//   - *RegisterResult: 包含 JWT 令牌、过期时间、成员信息、工作区 ID 和角色
//   - error: 可能的错误（邮箱已存在、数据库写入失败）
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

// EnsureOAuthWorkspace 为 OAuth 登录创建工作区和成员关系。
// 如果成员不存在，创建新工作区（以 workspaceName 命名）并将成员添加为 owner。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员 ID
//   - workspaceName: 工作区名称
//
// 返回：
//   - types.Workspace: 创建的工作区
//   - error: 可能的错误（数据库操作失败）
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

// GenerateToken 为指定用户生成 JWT 令牌。
//
// 参数：
//   - ctx: 请求上下文
//   - userID: 用户 ID
//   - userType: 用户类型（member 或 agent）
//   - workspaceID: 工作区 ID
//   - role: 用户在工作区中的角色
//
// 返回：
//   - string: 生成的 JWT 令牌
//   - time.Time: 令牌过期时间
//   - error: 可能的错误（令牌生成失败）
func (s *AuthService) GenerateToken(userID uuid.UUID, userType string, workspaceID uuid.UUID, role string) (string, time.Time, error) {
	token, expiresAt, _, err := store.GenerateJWT(userID, userType, s.JWTSecret)
	return token, expiresAt, err
}

// SessionTokenResult 保存会话令牌交换操作的结果。
type SessionTokenResult struct {
	SessionToken string    // 会话令牌
	ExpiresAt    time.Time // 会话令牌过期时间
	AgentID      uuid.UUID // 关联的代理 ID
}

// ExchangeAPITokenForSession 将 API 令牌交换为会话令牌。
// API 令牌是长期有效的，会话令牌是短期的，用于 Agentd 守护进程的日常通信。
//
// 参数：
//   - ctx: 请求上下文
//   - apiToken: API 令牌
//
// 返回：
//   - *SessionTokenResult: 包含会话令牌、过期时间和代理 ID
//   - error: 可能的错误（API 令牌无效或已过期）
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

// Logout 吊销当前会话令牌，使其立即失效。
//
// 参数：
//   - ctx: 请求上下文
//   - token: 要吊销的会话令牌
//
// 返回：
//   - error: 可能的错误（数据库删除失败）
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.svc.Store.DeleteSessionToken(ctx, token)
}

// WhoamiInfo 是已认证用户的详细信息类型别名。
type WhoamiInfo = store.WhoamiInfo

// Whoami 获取当前已认证用户的信息，包括成员详情和工作区角色。
//
// 参数：
//   - ctx: 请求上下文
//   - ownerType: 所有者类型（member 或 agent）
//   - ownerID: 所有者 ID
//
// 返回：
//   - *WhoamiInfo: 用户详细信息
//   - error: 可能的错误（用户不存在）
func (s *AuthService) Whoami(ctx context.Context, ownerType string, ownerID uuid.UUID) (*WhoamiInfo, error) {
	return s.svc.Store.GetWhoamiInfo(ctx, ownerType, ownerID)
}

// UpdateRuntimePublicKey 更新运行时的公钥，用于 Git 操作的 SSH 认证。
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: 运行时 ID
//   - publicKey: 新的 SSH 公钥
//
// 返回：
//   - error: 可能的错误（运行时不存在、数据库更新失败）
func (s *AuthService) UpdateRuntimePublicKey(ctx context.Context, runtimeID uuid.UUID, publicKey string) error {
	return s.svc.Store.UpdateRuntimePublicKey(ctx, runtimeID, publicKey)
}

// GetRuntimeByID 根据 ID 获取运行时信息。
//
// 参数：
//   - ctx: 请求上下文
//   - runtimeID: 运行时 ID
//
// 返回：
//   - types.Runtime: 运行时信息
//   - error: 可能的错误（运行时不存在）
func (s *AuthService) GetRuntimeByID(ctx context.Context, runtimeID uuid.UUID) (types.Runtime, error) {
	return s.svc.Store.GetRuntimeByID(ctx, runtimeID)
}

// GetLatestPublicKeyForAgent 获取指定代理的最新公钥。
//
// 参数：
//   - ctx: 请求上下文
//   - agentID: 代理 ID
//
// 返回：
//   - string: 最新的 SSH 公钥
//   - error: 可能的错误（代理不存在、无公钥记录）
func (s *AuthService) GetLatestPublicKeyForAgent(ctx context.Context, agentID uuid.UUID) (string, error) {
	return s.svc.Store.GetLatestPublicKeyForAgent(ctx, agentID)
}

// GetGitCredentialsByProject 获取指定项目的所有 Git 凭据。
// 凭据中的 PAT（Personal Access Token）使用 RSA + AES 加密存储。
//
// 参数：
//   - ctx: 请求上下文
//   - projectID: 项目 ID
//
// 返回：
//   - []types.GitCredential: Git 凭据列表
//   - error: 可能的错误（数据库查询失败）
func (s *AuthService) GetGitCredentialsByProject(ctx context.Context, projectID uuid.UUID) ([]types.GitCredential, error) {
	return s.svc.Store.GetGitCredentialsByProject(ctx, projectID)
}

// CreateGitCredential 创建一个新的 Git 凭据。
//
// 参数：
//   - ctx: 请求上下文
//   - arg: 创建 Git 凭据的参数
//
// 返回：
//   - types.GitCredential: 创建的 Git 凭据
//   - error: 可能的错误（数据库写入失败）
func (s *AuthService) CreateGitCredential(ctx context.Context, arg types.CreateGitCredentialParams) (types.GitCredential, error) {
	return s.svc.Store.CreateGitCredential(ctx, arg)
}

// UpdateGitCredential 更新一个已有的 Git 凭据。
//
// 参数：
//   - ctx: 请求上下文
//   - arg: 更新 Git 凭据的参数
//
// 返回：
//   - types.GitCredential: 更新后的 Git 凭据
//   - error: 可能的错误（凭据不存在、数据库更新失败）
func (s *AuthService) UpdateGitCredential(ctx context.Context, arg types.UpdateGitCredentialParams) (types.GitCredential, error) {
	return s.svc.Store.UpdateGitCredential(ctx, arg)
}

// GetGitCredential 根据 ID 获取单个 Git 凭据。
//
// 参数：
//   - ctx: 请求上下文
//   - id: Git 凭据 ID
//
// 返回：
//   - types.GitCredential: Git 凭据信息
//   - error: 可能的错误（凭据不存在）
func (s *AuthService) GetGitCredential(ctx context.Context, id uuid.UUID) (types.GitCredential, error) {
	return s.svc.Store.GetGitCredential(ctx, id)
}

// ChangePassword 在验证旧密码后修改成员密码。
// 新密码使用 bcrypt 哈希存储。
//
// 参数：
//   - ctx: 请求上下文
//   - memberID: 成员 ID
//   - oldPassword: 当前密码（用于验证）
//   - newPassword: 新密码
//
// 返回：
//   - error: 可能的错误（旧密码错误、成员不存在）
func (s *AuthService) ChangePassword(ctx context.Context, memberID uuid.UUID, oldPassword, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	return s.svc.Store.ChangePassword(ctx, memberID, oldPassword, newPassword)
}

// RequestPasswordReset 为指定邮箱生成密码重置令牌。
// 如果邮箱不存在或属于 OAuth 用户，返回空字符串且不报错（防止邮箱枚举攻击）。
//
// 参数：
//   - ctx: 请求上下文
//   - email: 用户邮箱地址
//
// 返回：
//   - string: 密码重置令牌（邮箱不存在时返回空字符串）
//   - error: 可能的错误（数据库操作失败）
func (s *AuthService) RequestPasswordReset(ctx context.Context, email string) (string, error) {
	return s.svc.Store.CreatePasswordResetToken(ctx, email)
}

// APIKeyAuthResult 保存 API key 或会话令牌认证的结果。
type APIKeyAuthResult struct {
	UserID   uuid.UUID // 用户 ID
	UserType string    // 用户类型："member" 或 "agent"
}

// AuthenticateAPIKey 验证 API key 或会话令牌并返回认证结果。
// 通过 SHA-256 哈希查询 auth_tokens 表，再用 bcrypt 验证令牌安全性。
//
// 参数：
//   - ctx: 请求上下文
//   - tokenStr: 待验证的令牌字符串（st_ 前缀为会话令牌，tm_ 前缀为 API 令牌）
//
// 返回：
//   - APIKeyAuthResult: 认证结果，包含用户 ID 和类型
//   - error: 令牌无效、过期或查询失败时返回错误
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

// sha256Hash 计算字符串的 SHA-256 哈希值并以十六进制返回。
func sha256Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// ResetPassword 使用有效的重置令牌重置成员密码。
// 令牌验证成功后立即作废，防止重复使用。
//
// 参数：
//   - ctx: 请求上下文
//   - token: 密码重置令牌
//   - newPassword: 新密码
//
// 返回：
//   - error: 可能的错误（令牌无效或已过期、数据库更新失败）
func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	return s.svc.Store.ResetPasswordWithToken(ctx, token, newPassword)
}
