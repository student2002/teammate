// auth_dto.go 定义认证相关的请求/响应结构体。
package handler

import (
	"time"

	"github.com/teammate/server/internal/types"
)

// authResponse 认证响应体，包含 Token 和用户信息。
type authResponse struct {
	Token       string        `json:"token"`        // JWT Token
	ExpiresAt   time.Time     `json:"expires_at"`   // Token 过期时间
	Member      types.Member  `json:"member"`       // 用户信息（domain 幜格，不含 PasswordHash）
	WorkspaceID string        `json:"workspace_id"` // 工作区 ID
	Role        string        `json:"role"`         // 用户角色
}
