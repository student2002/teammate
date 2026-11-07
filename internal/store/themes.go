package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// CreateCustomTheme stores an uploaded custom theme; duplicate id fails with a unique violation.
func (s *Store) CreateCustomTheme(ctx context.Context, arg types.CreateCustomThemeParams) (types.CustomTheme, error) {
	var createdBy uuid.NullUUID
	if arg.CreatedBy != nil {
		cu, err := uuid.Parse(*arg.CreatedBy)
		if err != nil {
			return types.CustomTheme{}, fmt.Errorf("parse created_by: %w", err)
		}
		createdBy = uuid.NullUUID{UUID: cu, Valid: true}
	}
	m, err := s.q.CreateCustomTheme(ctx, db.CreateCustomThemeParams{
		ID:        arg.ID,
		NameZh:    arg.NameZh,
		NameEn:    arg.NameEn,
		Icon:      arg.Icon,
		Vars:      arg.Vars,
		CreatedBy: createdBy,
	})
	if err != nil {
		return types.CustomTheme{}, fmt.Errorf("create custom theme: %w", err)
	}
	return toDomainCustomTheme(m.ID, m.NameZh, m.NameEn, m.Icon, m.Vars, m.CreatedBy, m.CreatedAt, m.UpdatedAt), nil
}

// ListCustomThemes returns all uploaded themes ordered by creation time.
func (s *Store) ListCustomThemes(ctx context.Context) ([]types.CustomTheme, error) {
	ms, err := s.q.ListCustomThemes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list custom themes: %w", err)
	}
	out := make([]types.CustomTheme, 0, len(ms))
	for _, m := range ms {
		out = append(out, toDomainCustomTheme(m.ID, m.NameZh, m.NameEn, m.Icon, m.Vars, m.CreatedBy, m.CreatedAt, m.UpdatedAt))
	}
	return out, nil
}

// DeleteCustomTheme removes an uploaded theme by id; returns whether a row was deleted.
func (s *Store) DeleteCustomTheme(ctx context.Context, id string) (bool, error) {
	n, err := s.q.DeleteCustomTheme(ctx, id)
	if err != nil {
		return false, fmt.Errorf("delete custom theme: %w", err)
	}
	return n > 0, nil
}

func toDomainCustomTheme(id, nameZh, nameEn, icon string, vars json.RawMessage, createdBy uuid.NullUUID, createdAt, updatedAt time.Time) types.CustomTheme {
	var cb *string
	if createdBy.Valid {
		s := createdBy.UUID.String()
		cb = &s
	}
	return types.CustomTheme{
		ID:        id,
		NameZh:    nameZh,
		NameEn:    nameEn,
		Icon:      icon,
		Vars:      vars,
		CreatedBy: cb,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
