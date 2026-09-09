// runtime_dto.go provides domain type aliases and parameter builders for runtime.go.
package handler

import (
	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// ---- domain type aliases ----

type RuntimeProvider = string
type RuntimeStatus = string

// ---- constants ----

const RuntimeStatusOnline = types.RuntimeStatusOnline

// ---- request structs ----

// registerRuntimeRequest register runtime request body.
type registerRuntimeRequest struct {
	AgentID          uuid.UUID        `json:"agent_id"`             // Agent UUID
	DaemonID         string           `json:"daemon_id"`            // daemon ID
	Provider         RuntimeProvider  `json:"provider"`             // Agent provider
	Version          string           `json:"version"`              // provider version
	Status           RuntimeStatus    `json:"status"`               // runtime status
	SessionTokenHash string           `json:"session_token_hash"`   // session token hash
	SessionExpiresAt string           `json:"session_expires_at"`   // session expiration time
	PublicKey        string           `json:"public_key"`           // public key
}

// ---- parameter builders ----

// buildCreateRuntimeParams builds types.CreateRuntimeParams from request fields.
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
