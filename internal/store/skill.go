// skill.go 提供技能管理的数据访问操作。
package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// GetSkill 根据 ID 获取单个技能。
func (s *Store) GetSkill(ctx context.Context, id uuid.UUID) (types.Skill, error) {
	skill, err := s.q.GetSkill(ctx, id)
	if err != nil {
		return types.Skill{}, fmt.Errorf("get skill: %w", err)
	}
	return ToDomainSkill(skill)
}

// CreateSkill 创建一个新的技能。
func (s *Store) CreateSkill(ctx context.Context, params types.CreateSkillParams) (types.Skill, error) {
	dbParams, err := FromDomainCreateSkillParams(params)
	if err != nil {
		return types.Skill{}, fmt.Errorf("convert create skill params: %w", err)
	}
	skill, err := s.q.CreateSkill(ctx, dbParams)
	if err != nil {
		return types.Skill{}, fmt.Errorf("create skill: %w", err)
	}
	return ToDomainSkill(skill)
}

// ListSkills 列出指定工作区的所有技能。
func (s *Store) ListSkills(ctx context.Context, workspaceID uuid.UUID) ([]types.Skill, error) {
	skills, err := s.q.ListSkills(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list skills: %w", err)
	}
	return ToDomainSkillSlice(skills)
}

// DeleteSkill 删除一个技能。
func (s *Store) DeleteSkill(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteSkill(ctx, id); err != nil {
		return fmt.Errorf("delete skill: %w", err)
	}
	return nil
}
