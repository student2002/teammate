// integration.go implements the business logic of project-level GitHub
// integrations (plan §10.1, §12.4): CRUD with encrypted secrets, connection
// testing against the GitHub API and the delete reference check. Secrets are
// encrypted at rest with the server AES key and never returned in plaintext.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/integration/github"
	"github.com/teammate/server/internal/types"
)

// ErrIntegrationInUse marks a delete of an integration still referenced by an
// enabled template's trigger.github node (plan §10.1).
var ErrIntegrationInUse = errors.New("integration is referenced by enabled workflow templates")

// ErrIntegrationNotFound marks an integration id that does not exist.
var ErrIntegrationNotFound = errors.New("integration not found")

// ErrInvalidSecret marks a create/update whose PAT or webhook secret cannot be
// encrypted (encryption key not initialized).
var ErrInvalidSecret = errors.New("integration secret cannot be encrypted")

// IntegrationService implements the GitHub integration business logic.
type IntegrationService struct {
	svc *Service
}

func NewIntegrationService(svc *Service) *IntegrationService {
	return &IntegrationService{svc: svc}
}

// UpsertIntegrationInput is the create/update body (plan §12.4). pat and
// webhook_secret are plaintext on input only.
type UpsertIntegrationInput struct {
	Name          string
	RepoOwner     string
	RepoName      string
	PAT           string
	WebhookSecret string
}

// IntegrationView is the API representation of one integration: masked token
// and a webhook-secret-set flag instead of plaintext secrets.
type IntegrationView struct {
	ID               string `json:"id"`
	WorkspaceID      string `json:"workspace_id"`
	ProjectID        string `json:"project_id"`
	Provider         string `json:"provider"`
	Name             string `json:"name"`
	RepoOwner        string `json:"repo_owner"`
	RepoName         string `json:"repo_name"`
	Status           string `json:"status"`
	PatMasked        string `json:"pat_masked"`
	WebhookSecretSet bool   `json:"webhook_secret_set"`
	CreatedBy        string `json:"created_by,omitempty"`
	CreatedAt        string `json:"created_at"`
	UpdatedAt        string `json:"updated_at"`
}

// githubConfig is the config JSONB payload.
type githubConfig struct {
	RepoOwner string `json:"repo_owner"`
	RepoName  string `json:"repo_name"`
}

// Create validates and persists one new GitHub integration.
func (s *IntegrationService) Create(ctx context.Context, workspaceID, projectID uuid.UUID, in UpsertIntegrationInput, createdBy uuid.UUID) (IntegrationView, error) {
	config, encrypted, err := buildEncrypted(in)
	if err != nil {
		return IntegrationView{}, err
	}
	authorID := createdBy.String()
	row, err := s.svc.Store.CreateIntegration(ctx, types.CreateIntegrationParams{
		WorkspaceID:            workspaceID.String(),
		ProjectID:              projectID.String(),
		Provider:               "github",
		Name:                   in.Name,
		Config:                 config,
		EncryptedSecret:        encrypted.pat,
		PatMasked:              maskPlainPat(in.PAT),
		EncryptedWebhookSecret: encrypted.webhookSecret,
		Status:                 types.IntegrationStatusPending,
		CreatedBy:              &authorID,
	})
	if err != nil {
		return IntegrationView{}, fmt.Errorf("create integration: %w", err)
	}
	return toIntegrationView(row), nil
}

// List returns the project's GitHub integrations.
func (s *IntegrationService) List(ctx context.Context, projectID uuid.UUID) ([]IntegrationView, error) {
	rows, err := s.svc.Store.ListIntegrationsByProject(ctx, projectID, "github")
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	out := make([]IntegrationView, 0, len(rows))
	for _, row := range rows {
		out = append(out, toIntegrationView(row))
	}
	return out, nil
}

// Get loads one integration view by id.
func (s *IntegrationService) Get(ctx context.Context, id uuid.UUID) (IntegrationView, error) {
	row, err := s.svc.Store.GetIntegration(ctx, id)
	if err != nil {
		return IntegrationView{}, fmt.Errorf("get integration: %w", err)
	}
	return toIntegrationView(row), nil
}

// ErrIntegrationInvalid marks an integration payload that failed input
// validation; handlers map it to 400.
var ErrIntegrationInvalid = errors.New("invalid integration")

// Update replaces the mutable fields of one integration.
func (s *IntegrationService) Update(ctx context.Context, id uuid.UUID, in UpsertIntegrationInput) (IntegrationView, error) {
	config, encrypted, err := buildEncrypted(in)
	if err != nil {
		return IntegrationView{}, fmt.Errorf("%w: %s", ErrIntegrationInvalid, err.Error())
	}
	current, err := s.svc.Store.GetIntegration(ctx, id)
	if err != nil {
		return IntegrationView{}, fmt.Errorf("get integration: %w", err)
	}
	row, err := s.svc.Store.UpdateIntegration(ctx, id, types.UpdateIntegrationParams{
		Name:                   in.Name,
		Config:                 config,
		EncryptedSecret:        encrypted.pat,
		PatMasked:              maskPlainPat(in.PAT),
		EncryptedWebhookSecret: encrypted.webhookSecret,
		Status:                 current.Status,
	})
	if err != nil {
		return IntegrationView{}, fmt.Errorf("update integration: %w", err)
	}
	return toIntegrationView(row), nil
}

// TestConnection verifies the stored PAT against the GitHub repository
// endpoint (plan §10.1): success sets status=connected, failure status=error.
// detail carries the failure reason (never the PAT); err reports server-level
// faults only.
func (s *IntegrationService) TestConnection(ctx context.Context, id uuid.UUID) (IntegrationView, string, error) {
	row, err := s.svc.Store.GetIntegration(ctx, id)
	if err != nil {
		return IntegrationView{}, "", fmt.Errorf("get integration: %w", err)
	}
	pat, err := crypto.DecryptString(row.EncryptedSecret)
	if err != nil {
		s.markIntegrationStatus(ctx, id, types.IntegrationStatusError)
		view := toIntegrationView(row)
		view.Status = types.IntegrationStatusError
		return view, "stored secret is not decryptable", nil
	}
	cfg := decodeGithubConfig(row.Config)
	client := github.NewClient(s.svc.GitHubAPIURL, pat)
	if _, err := client.GetRepository(ctx, cfg.RepoOwner, cfg.RepoName); err != nil {
		s.markIntegrationStatus(ctx, id, types.IntegrationStatusError)
		view := toIntegrationView(row)
		view.Status = types.IntegrationStatusError
		return view, err.Error(), nil
	}
	updated, err := s.svc.Store.UpdateIntegrationStatus(ctx, id, types.IntegrationStatusConnected)
	if err != nil {
		return IntegrationView{}, "", fmt.Errorf("mark integration connected: %w", err)
	}
	return toIntegrationView(updated), "", nil
}

// Delete removes one integration unless an enabled template's trigger.github
// node still references it (plan §10.1); the check spans all workspaces.
func (s *IntegrationService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.svc.Store.GetIntegration(ctx, id); err != nil {
		return fmt.Errorf("get integration: %w", err)
	}
	nodes, err := s.svc.Store.ListGitHubTriggerNodesByIntegration(ctx, id.String())
	if err != nil {
		return fmt.Errorf("list integration references: %w", err)
	}
	for _, node := range nodes {
		if node.Enabled {
			return fmt.Errorf("%w: template %s", ErrIntegrationInUse, node.TemplateName)
		}
	}
	return s.svc.Store.DeleteIntegration(ctx, id)
}

// GetWebhookSecret loads one integration's decrypted webhook secret for the
// signature check. A missing row returns ErrIntegrationNotFound so the
// webhook entry can answer 404; a row without a stored secret returns ok=false.
func (s *IntegrationService) GetWebhookSecret(ctx context.Context, id uuid.UUID) (secret string, ok bool, err error) {
	row, err := s.svc.Store.GetIntegration(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, ErrIntegrationNotFound
		}
		return "", false, fmt.Errorf("get integration: %w", err)
	}
	if row.EncryptedWebhookSecret == "" {
		return "", false, nil
	}
	plaintext, err := crypto.DecryptString(row.EncryptedWebhookSecret)
	if err != nil {
		return "", false, fmt.Errorf("decrypt webhook secret: %w", err)
	}
	return plaintext, true, nil
}

// encryptedSecrets bundles the two encrypted columns.
type encryptedSecrets struct {
	pat           string
	webhookSecret string
}

// buildEncrypted validates the upsert input and encrypts both secrets.
func buildEncrypted(in UpsertIntegrationInput) (json.RawMessage, encryptedSecrets, error) {
	var encrypted encryptedSecrets
	if in.RepoOwner == "" || in.RepoName == "" {
		return nil, encrypted, fmt.Errorf("repo_owner and repo_name are required")
	}
	if in.PAT == "" {
		return nil, encrypted, fmt.Errorf("pat is required")
	}
	if in.WebhookSecret == "" {
		return nil, encrypted, fmt.Errorf("webhook_secret is required")
	}
	patEncrypted, err := crypto.EncryptString(in.PAT)
	if err != nil {
		return nil, encrypted, fmt.Errorf("%w: %v", ErrInvalidSecret, err)
	}
	secretEncrypted, err := crypto.EncryptString(in.WebhookSecret)
	if err != nil {
		return nil, encrypted, fmt.Errorf("%w: %v", ErrInvalidSecret, err)
	}
	encrypted = encryptedSecrets{pat: patEncrypted, webhookSecret: secretEncrypted}
	config, err := json.Marshal(githubConfig{RepoOwner: in.RepoOwner, RepoName: in.RepoName})
	if err != nil {
		return nil, encrypted, fmt.Errorf("marshal integration config: %w", err)
	}
	return config, encrypted, nil
}

// toIntegrationView maps the stored row to the API view: the precomputed
// masked PAT, boolean webhook secret flag, repo fields decoded from config.
// A row written before pat_masked was stored renders the fixed mask.
func toIntegrationView(row types.Integration) IntegrationView {
	cfg := decodeGithubConfig(row.Config)
	view := IntegrationView{
		ID:               row.ID,
		WorkspaceID:      row.WorkspaceID,
		ProjectID:        row.ProjectID,
		Provider:         row.Provider,
		Name:             row.Name,
		RepoOwner:        cfg.RepoOwner,
		RepoName:         cfg.RepoName,
		Status:           row.Status,
		PatMasked:        row.PatMasked,
		WebhookSecretSet: row.EncryptedWebhookSecret != "",
		CreatedAt:        row.CreatedAt.Format(time.RFC3339),
		UpdatedAt:        row.UpdatedAt.Format(time.RFC3339),
	}
	if view.PatMasked == "" {
		view.PatMasked = "****"
	}
	if row.CreatedBy != nil {
		view.CreatedBy = *row.CreatedBy
	}
	return view
}

// maskPlainPat renders the plaintext PAT as "first4****last4" for display;
// shorter values become a fixed mask. Runs once at create/update and the
// result is stored in pat_masked, so reads never decrypt.
func maskPlainPat(plaintext string) string {
	const mask = "****"
	if len(plaintext) < 12 {
		return mask
	}
	return plaintext[:4] + mask + plaintext[len(plaintext)-4:]
}

// markIntegrationStatus best-effort updates the status column; a failed write
// must not mask the actual test result.
func (s *IntegrationService) markIntegrationStatus(ctx context.Context, id uuid.UUID, status string) {
	if _, err := s.svc.Store.UpdateIntegrationStatus(ctx, id, status); err != nil {
		slog.Error("update integration status", "integration_id", id, "err", err)
	}
}

func decodeGithubConfig(raw []byte) githubConfig {
	var cfg githubConfig
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &cfg)
	}
	return cfg
}

// WorkspaceIntegrationView is one integration view plus its project's display
// name, for workspace-wide pickers.
type WorkspaceIntegrationView struct {
	IntegrationView
	ProjectName string `json:"project_name"`
}

// ListForWorkspace returns every integration in the workspace with its
// project name, ordered by project then creation.
func (s *IntegrationService) ListForWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]WorkspaceIntegrationView, error) {
	rows, err := s.svc.Store.ListWorkspaceIntegrations(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace integrations: %w", err)
	}
	out := make([]WorkspaceIntegrationView, 0, len(rows))
	for _, row := range rows {
		out = append(out, WorkspaceIntegrationView{IntegrationView: toIntegrationView(row.Integration), ProjectName: row.ProjectName})
	}
	return out, nil
}
