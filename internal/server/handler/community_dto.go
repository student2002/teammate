// community_dto.go 为 community.go 提供参数构建器。
package handler

import (
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// ---- 参数构建器 ----

// buildCreateCommunityWorkflowParams 从请求字段构建 types.CreateCommunityWorkflowParams。
func buildCreateCommunityWorkflowParams(
	name string,
	description string,
	author string,
	version string,
	workflowDefinition []byte,
	requiredSkills pqtype.NullRawMessage,
	requiredMcpServers pqtype.NullRawMessage,
	recommendedAgentInstructions pqtype.NullRawMessage,
	isOfficial bool,
) types.CreateCommunityWorkflowParams {
	var descPtr *string
	if description != "" {
		d := description
		descPtr = &d
	}
	return types.CreateCommunityWorkflowParams{
		Name:                         name,
		Description:                  descPtr,
		Author:                       author,
		Version:                      version,
		WorkflowDefinition:           workflowDefinition,
		RequiredSkills:               nullRawToBytes(requiredSkills),
		RequiredMcpServers:           nullRawToBytes(requiredMcpServers),
		RecommendedAgentInstructions: nullRawToBytes(recommendedAgentInstructions),
	}
}

// nullRawToBytes 将 pqtype.NullRawMessage 转换为 []byte，无效时返回空 []byte。
func nullRawToBytes(n pqtype.NullRawMessage) []byte {
	if n.Valid {
		return n.RawMessage
	}
	return []byte{}
}
