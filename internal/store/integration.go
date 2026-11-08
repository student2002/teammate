// integration.go provides data access for project-level external
// integrations (plan §7.4). The store converts between sqlc-generated types
// and types.Integration; encrypted secret columns map 1:1.
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// ToDomainIntegration converts one sqlc integration row to the domain type.
func ToDomainIntegration(row db.Integration) types.Integration {
	integration := types.Integration{
		ID:                     row.ID.String(),
		WorkspaceID:            row.WorkspaceID.String(),
		ProjectID:              row.ProjectID.String(),
		Provider:               row.Provider,
		Name:                   row.Name,
		Config:                 row.Config,
		EncryptedSecret:        row.EncryptedSecret,
		PatMasked:              row.PatMasked,
		EncryptedWebhookSecret: row.EncryptedWebhookSecret,
		Status:                 row.Status,
		CreatedAt:              row.CreatedAt,
		UpdatedAt:              row.UpdatedAt,
	}
	if row.CreatedBy.Valid {
		id := row.CreatedBy.UUID.String()
		integration.CreatedBy = &id
	}
	return integration
}

// CreateIntegration inserts one integration row.
func (s *Store) CreateIntegration(ctx context.Context, params types.CreateIntegrationParams) (types.Integration, error) {
	workspaceID, err := uuid.Parse(params.WorkspaceID)
	if err != nil {
		return types.Integration{}, fmt.Errorf("convert workspace id: %w", err)
	}
	projectID, err := uuid.Parse(params.ProjectID)
	if err != nil {
		return types.Integration{}, fmt.Errorf("convert project id: %w", err)
	}
	var createdBy uuid.NullUUID
	if params.CreatedBy != nil {
		if id, parseErr := uuid.Parse(*params.CreatedBy); parseErr == nil {
			createdBy = uuid.NullUUID{UUID: id, Valid: true}
		}
	}
	row, err := s.q.CreateIntegration(ctx, db.CreateIntegrationParams{
		WorkspaceID:            workspaceID,
		ProjectID:              projectID,
		Provider:               params.Provider,
		Name:                   params.Name,
		Config:                 params.Config,
		EncryptedSecret:        params.EncryptedSecret,
		PatMasked:              params.PatMasked,
		EncryptedWebhookSecret: params.EncryptedWebhookSecret,
		Status:                 params.Status,
		CreatedBy:              createdBy,
	})
	if err != nil {
		return types.Integration{}, fmt.Errorf("create integration: %w", err)
	}
	return ToDomainIntegration(row), nil
}

// GetIntegration loads one integration by id.
func (s *Store) GetIntegration(ctx context.Context, id uuid.UUID) (types.Integration, error) {
	row, err := s.q.GetIntegration(ctx, id)
	if err != nil {
		return types.Integration{}, fmt.Errorf("get integration: %w", err)
	}
	return ToDomainIntegration(row), nil
}

// ListIntegrationsByProject loads a project's integrations of one provider.
func (s *Store) ListIntegrationsByProject(ctx context.Context, projectID uuid.UUID, provider string) ([]types.Integration, error) {
	rows, err := s.q.ListIntegrationsByProject(ctx, db.ListIntegrationsByProjectParams{ProjectID: projectID, Provider: provider})
	if err != nil {
		return nil, fmt.Errorf("list integrations: %w", err)
	}
	out := make([]types.Integration, 0, len(rows))
	for _, row := range rows {
		out = append(out, ToDomainIntegration(row))
	}
	return out, nil
}

// GetProjectIntegration loads the project's first integration of one provider
// (creation order); used by the action.github executor.
func (s *Store) GetProjectIntegration(ctx context.Context, projectID uuid.UUID, provider string) (types.Integration, error) {
	row, err := s.q.GetProjectIntegration(ctx, db.GetProjectIntegrationParams{ProjectID: projectID, Provider: provider})
	if err != nil {
		return types.Integration{}, fmt.Errorf("get project integration: %w", err)
	}
	return ToDomainIntegration(row), nil
}

// UpdateIntegration replaces the mutable fields of one integration.
func (s *Store) UpdateIntegration(ctx context.Context, id uuid.UUID, params types.UpdateIntegrationParams) (types.Integration, error) {
	row, err := s.q.UpdateIntegration(ctx, db.UpdateIntegrationParams{
		ID:                     id,
		Name:                   params.Name,
		Config:                 params.Config,
		EncryptedSecret:        params.EncryptedSecret,
		PatMasked:              params.PatMasked,
		EncryptedWebhookSecret: params.EncryptedWebhookSecret,
		Status:                 params.Status,
	})
	if err != nil {
		return types.Integration{}, fmt.Errorf("update integration: %w", err)
	}
	return ToDomainIntegration(row), nil
}

// UpdateIntegrationStatus writes only the status column (connection test).
func (s *Store) UpdateIntegrationStatus(ctx context.Context, id uuid.UUID, status string) (types.Integration, error) {
	row, err := s.q.UpdateIntegrationStatus(ctx, db.UpdateIntegrationStatusParams{ID: id, Status: status})
	if err != nil {
		return types.Integration{}, fmt.Errorf("update integration status: %w", err)
	}
	return ToDomainIntegration(row), nil
}

// DeleteIntegration removes one integration row.
func (s *Store) DeleteIntegration(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteIntegration(ctx, id); err != nil {
		return fmt.Errorf("delete integration: %w", err)
	}
	return nil
}

// ListWorkspaceIntegrations returns every integration in the workspace with
// its project's display name, ordered by project then creation.
func (s *Store) ListWorkspaceIntegrations(ctx context.Context, workspaceID uuid.UUID) ([]types.WorkspaceIntegration, error) {
	rows, err := s.q.ListWorkspaceIntegrations(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace integrations: %w", err)
	}
	out := make([]types.WorkspaceIntegration, 0, len(rows))
	for _, row := range rows {
		integration := types.Integration{
			ID:                     row.ID.String(),
			WorkspaceID:            row.WorkspaceID.String(),
			ProjectID:              row.ProjectID.String(),
			Provider:               row.Provider,
			Name:                   row.Name,
			Config:                 row.Config,
			EncryptedSecret:        row.EncryptedSecret,
			EncryptedWebhookSecret: row.EncryptedWebhookSecret,
			Status:                 row.Status,
			CreatedAt:              row.CreatedAt,
			UpdatedAt:              row.UpdatedAt,
		}
		if row.CreatedBy.Valid {
			id := row.CreatedBy.UUID.String()
			integration.CreatedBy = &id
		}
		out = append(out, types.WorkspaceIntegration{Integration: integration, ProjectName: row.ProjectName})
	}
	return out, nil
}
