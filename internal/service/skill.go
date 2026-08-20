// skill.go 提供技能管理的业务逻辑。
// 技能是可复用的知识片段，分配给 Agent 后会影响其执行行为。
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// SkillService 提供技能管理相关的业务逻辑。
type SkillService struct {
	svc *Service
}

func NewSkillService(svc *Service) *SkillService {
	return &SkillService{svc: svc}
}

// Get 根据 ID 获取单个技能。
func (s *SkillService) Get(ctx context.Context, id uuid.UUID) (types.Skill, error) {
	return s.svc.Store.GetSkill(ctx, id)
}

// Create 创建一个新的技能。
func (s *SkillService) Create(ctx context.Context, params types.CreateSkillParams) (types.Skill, error) {
	return s.svc.Store.CreateSkill(ctx, params)
}

// List 列出指定工作区的所有技能。
func (s *SkillService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Skill, error) {
	return s.svc.Store.ListSkills(ctx, workspaceID)
}

// Delete 删除一个技能。
func (s *SkillService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteSkill(ctx, id)
}

// Update 更新技能字段。nil 字段保持现有值，非 nil 字段替换现有值。
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
