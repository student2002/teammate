// skill.go provides the business logic for skill management.
// Skills are reusable knowledge fragments; assigning a skill to an agent affects its execution behavior.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// SkillService provides the business logic for skill management.
type SkillService struct {
	svc *Service
}

func NewSkillService(svc *Service) *SkillService {
	return &SkillService{svc: svc}
}

// Get retrieves a single skill by ID.
func (s *SkillService) Get(ctx context.Context, id uuid.UUID) (types.Skill, error) {
	return s.svc.Store.GetSkill(ctx, id)
}

// Create creates a new skill.
func (s *SkillService) Create(ctx context.Context, params types.CreateSkillParams) (types.Skill, error) {
	return s.svc.Store.CreateSkill(ctx, params)
}

// List lists all skills in the specified workspace.
func (s *SkillService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Skill, error) {
	return s.svc.Store.ListSkills(ctx, workspaceID)
}

// Delete deletes a skill.
func (s *SkillService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteSkill(ctx, id)
}

// Update updates skill fields. nil fields keep the existing value; non-nil fields replace the existing value.
func (s *SkillService) Update(ctx context.Context, id uuid.UUID, name, description, category, promptTemplate *string) (types.Skill, error) {
	current, err := s.svc.Store.GetSkill(ctx, id)
	if err != nil {
		return types.Skill{}, fmt.Errorf("get skill before update: %w", err)
	}

	nextName := current.Name
	if name != nil {
		nextName = *name
	}
	nextDescription := current.Description
	if description != nil {
		nextDescription = *description
	}
	nextCategory := current.Category
	if category != nil {
		nextCategory = *category
	}
	nextPromptTemplate := current.PromptTemplate
	if promptTemplate != nil {
		nextPromptTemplate = *promptTemplate
	}

	updated, err := s.svc.Store.UpdateSkill(ctx, types.UpdateSkillParams{
		ID:             id.String(),
		Name:           nextName,
		Description:    &nextDescription,
		Category:       &nextCategory,
		PromptTemplate: &nextPromptTemplate,
	})
	if err != nil {
		return types.Skill{}, fmt.Errorf("update skill: %w", err)
	}
	return updated, nil
}
