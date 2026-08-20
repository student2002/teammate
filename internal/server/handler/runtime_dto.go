// runtime_dto.go 为 runtime.go 提供 domain 类型别名和参数构建器。
package handler

import (
	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ---- domain 类型别名 ----

type RuntimeProvider = string
type RuntimeStatus = string

// ---- 常量 ----

const RuntimeStatusOnline = types.RuntimeStatusOnline

// ---- 请求结构体 ----

// registerRuntimeRequest 注册运行时请求体。
type registerRuntimeRequest struct {
	AgentID          uuid.UUID        `json:"agent_id"`             // Agent UUID
	DaemonID         string           `json:"daemon_id"`            // 守护进程 ID
	Provider         RuntimeProvider  `json:"provider"`             // Agent 提供方
	Version          string           `json:"version"`              // 提供方版本
	Status           RuntimeStatus    `json:"status"`               // 运行时状态
	SessionTokenHash string           `json:"session_token_hash"`   // 会话 Token 哈希
	SessionExpiresAt string           `json:"session_expires_at"`   // 会话过期时间
	PublicKey        string           `json:"public_key"`           // 公钥
}

// ---- 参数构建器 ----

// buildCreateRuntimeParams 从请求字段构建 types.CreateRuntimeParams。
func buildCreateRuntimeParams(
	agentID uuid.UUID,
	daemonID string,
	provider RuntimeProvider,
	version string,
	status RuntimeStatus,
	sessionTokenHash string,
	sessionExpiresAt string,
	publicKey string,
) types.CreateRuntimeParams {
	var versionPtr *string
	if version != "" {
		v := version
		versionPtr = &v
	}
	var sthPtr *string
	if sessionTokenHash != "" {
		s := sessionTokenHash
		sthPtr = &s
	}
	var pkPtr *string
	if publicKey != "" {
		p := publicKey
		pkPtr = &p
	}
	return types.CreateRuntimeParams{
		AgentID:          agentID.String(),
		DaemonID:         daemonID,
		Provider:         provider,
		Version:          versionPtr,
		Status:           status,
		SessionTokenHash: sthPtr,
		PublicKey:        pkPtr,
	}
}
