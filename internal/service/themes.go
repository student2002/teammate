package service

import (
	"context"

	"github.com/teammate/server/internal/types"
)

// ThemeService covers runtime-uploaded custom themes (deployment-shared).
type ThemeService struct {
	svc *Service
}

// NewThemeService returns a per-request theme service.
func NewThemeService(svc *Service) *ThemeService {
	return &ThemeService{svc: svc}
}

// Create stores an uploaded custom theme.
func (s *ThemeService) Create(ctx context.Context, arg types.CreateCustomThemeParams) (types.CustomTheme, error) {
	return s.svc.Store.CreateCustomTheme(ctx, arg)
}

// List returns all uploaded custom themes.
func (s *ThemeService) List(ctx context.Context) ([]types.CustomTheme, error) {
	return s.svc.Store.ListCustomThemes(ctx)
}

// Delete removes an uploaded custom theme by id; returns whether a row was deleted.
func (s *ThemeService) Delete(ctx context.Context, id string) (bool, error) {
	return s.svc.Store.DeleteCustomTheme(ctx, id)
}
