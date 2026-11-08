// integration.go defines the domain types of project-level external
// integrations (plan §7.4): the integration row and its store parameters.
// Secrets live only in the encrypted columns; nothing here carries plaintext.
package types

import (
	"encoding/json"
	"time"
)

// Integration is one project-scoped external integration (first version:
// github). encrypted_secret holds the PAT, encrypted_webhook_secret the
// webhook secret — both AES-GCM encrypted with the server key. pat_masked is
// the precomputed display mask, so reads never decrypt.
type Integration struct {
	ID                     string          `json:"id" db:"id"`
	WorkspaceID            string          `json:"workspace_id" db:"workspace_id"`
	ProjectID              string          `json:"project_id" db:"project_id"`
	Provider               string          `json:"provider" db:"provider"`
	Name                   string          `json:"name" db:"name"`
	Config                 json.RawMessage `json:"config" db:"config"`
	EncryptedSecret        string          `json:"-" db:"encrypted_secret"`
	PatMasked              string          `json:"pat_masked" db:"pat_masked"`
	EncryptedWebhookSecret string          `json:"-" db:"encrypted_webhook_secret"`
	Status                 string          `json:"status" db:"status"`
	CreatedBy              *string         `json:"created_by" db:"created_by"`
	CreatedAt              time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at" db:"updated_at"`
}

// CreateIntegrationParams is the domain parameter struct for creating one
// integration. Secret fields carry the already-encrypted ciphertext; PatMasked
// the precomputed display mask.
type CreateIntegrationParams struct {
	WorkspaceID            string          `json:"workspace_id"`
	ProjectID              string          `json:"project_id"`
	Provider               string          `json:"provider"`
	Name                   string          `json:"name"`
	Config                 json.RawMessage `json:"config"`
	EncryptedSecret        string          `json:"-"`
	PatMasked              string          `json:"-"`
	EncryptedWebhookSecret string          `json:"-"`
	Status                 string          `json:"status"`
	CreatedBy              *string         `json:"created_by"`
}

// UpdateIntegrationParams is the domain parameter struct for the full update
// of one integration. Secret fields carry the already-encrypted ciphertext;
// PatMasked the precomputed display mask.
type UpdateIntegrationParams struct {
	Name                   string          `json:"name"`
	Config                 json.RawMessage `json:"config"`
	EncryptedSecret        string          `json:"-"`
	PatMasked              string          `json:"-"`
	EncryptedWebhookSecret string          `json:"-"`
	Status                 string          `json:"status"`
}

// IntegrationStatus values of the integrations.status column.
const (
	IntegrationStatusPending   = "pending"
	IntegrationStatusConnected = "connected"
	IntegrationStatusError     = "error"
)

// WorkspaceIntegration pairs one integration with its project's display name
// for workspace-wide integration pickers (the workflow editor).
type WorkspaceIntegration struct {
	Integration Integration `json:"-"`
	ProjectName string      `json:"project_name"`
}
