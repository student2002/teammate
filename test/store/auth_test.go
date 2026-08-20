// auth_test.go 覆盖认证数据访问的测试。
package store_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/test/testdb"
)

const testJWTSecret = "test-jwt-secret-for-handler-tests"

func TestLogin_Success(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	result, err := s.Login(ctx, member.Email, testMemberPassword, testJWTSecret)
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if result.JTI == "" {
		t.Fatal("expected non-empty JTI")
	}
	if result.Member.ID != member.ID {
		t.Fatalf("expected member ID %s, got %s", member.ID, result.Member.ID)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	_, err := s.Login(ctx, member.Email, "wrong-password", testJWTSecret)
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestLogin_NonexistentEmail(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	_, err := s.Login(ctx, "nonexistent@test.com", "any-password", testJWTSecret)
	if err == nil {
		t.Fatal("expected error for nonexistent email")
	}
}

func TestRegister_Success(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	result, err := s.Register(ctx, "Test User", "reg-"+uuid.New().String()[:8]+"@test.com", "Test123456", testJWTSecret)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if result.Member.Name != "Test User" {
		t.Fatalf("expected name 'Test User', got %s", result.Member.Name)
	}
	// 注册创建工作区和成员——注册清理
	t.Cleanup(func() {
		if testDB != nil {
			_ = testdb.DeleteWorkspace(testDB, result.WorkspaceID.String())
			_ = testdb.DeleteMember(testDB, result.Member.ID)
		}
	})
}

func TestGenerateJWT_Claims(t *testing.T) {
	userID := uuid.New()

	token, expiresAt, jti, err := store.GenerateJWT(userID, "member", testJWTSecret)
	if err != nil {
		t.Fatalf("GenerateJWT: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if jti == "" {
		t.Fatal("expected non-empty JTI")
	}
	if expiresAt.Before(time.Now()) {
		t.Fatal("expected expiry in the future")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("invalid JWT format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT claims: %v", err)
	}
	if _, ok := claims["workspace_id"]; ok {
		t.Fatalf("JWT must not contain workspace_id: %v", claims)
	}
	if _, ok := claims["role"]; ok {
		t.Fatalf("JWT must not contain role: %v", claims)
	}
}

func TestChangePassword_Success(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	err := s.ChangePassword(ctx, uuid.MustParse(member.ID), testMemberPassword, "new-password-123")
	if err != nil {
		t.Fatalf("ChangePassword: %v", err)
	}

	// 验证新密码有效
	result, err := s.Login(ctx, member.Email, "new-password-123", testJWTSecret)
	if err != nil {
		t.Fatalf("Login with new password: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token after password change")
	}
}

func TestChangePassword_WrongOldPassword(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	err := s.ChangePassword(ctx, uuid.MustParse(member.ID), "wrong-old-password", "new-password")
	if err == nil {
		t.Fatal("expected error for wrong old password")
	}
}

func TestCreatePasswordResetToken_And_Reset(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	member := createTestMember(t, s, ws.ID)

	// 创建重置 Token
	token, err := s.CreatePasswordResetToken(ctx, member.Email)
	if err != nil {
		t.Fatalf("CreatePasswordResetToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty reset token")
	}

	// 重置密码
	err = s.ResetPasswordWithToken(ctx, token, "reset-password-123")
	if err != nil {
		t.Fatalf("ResetPasswordWithToken: %v", err)
	}

	// 验证新密码有效
	result, err := s.Login(ctx, member.Email, "reset-password-123", testJWTSecret)
	if err != nil {
		t.Fatalf("Login after reset: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token after password reset")
	}
}

func TestResetPasswordWithToken_InvalidToken(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	err := s.ResetPasswordWithToken(ctx, "invalid-token", "new-password")
	if err == nil {
		t.Fatal("expected error for invalid token")
	}
}

func TestExchangeAPITokenForSession_Success(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	_, apiToken := createTestAgent(t, s, ws.ID)

	result, err := s.ExchangeAPITokenForSession(ctx, apiToken)
	if err != nil {
		t.Fatalf("ExchangeAPITokenForSession: %v", err)
	}
	if result.SessionToken == "" {
		t.Fatal("expected non-empty session token")
	}
}

func TestExchangeAPITokenForSession_InvalidToken(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	_, err := s.ExchangeAPITokenForSession(ctx, "invalid-token-12345")
	if err == nil {
		t.Fatal("expected error for invalid API token")
	}
}

func TestDeleteSessionToken(t *testing.T) {
	s, _ := setupTestStore(t)
	ctx := context.Background()

	ws := createTestWorkspace(t, s)
	_, apiToken := createTestAgent(t, s, ws.ID)

	session, err := s.ExchangeAPITokenForSession(ctx, apiToken)
	if err != nil {
		t.Fatalf("ExchangeAPITokenForSession: %v", err)
	}

	err = s.DeleteSessionToken(ctx, session.SessionToken)
	if err != nil {
		t.Fatalf("DeleteSessionToken: %v", err)
	}
}
