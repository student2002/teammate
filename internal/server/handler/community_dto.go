// community_dto.go provides parameter builders for community.go.
package handler

import (
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// ---- parameter builders ----

// buildCreateCommunityWorkflowParams builds types.CreateCommunityWorkflowParams from request fields.
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

// nullRawToBytes converts a pqtype.NullRawMessage to []byte, returning an empty []byte when invalid.
func nullRawToBytes(n pqtype.NullRawMessage) []byte {
	if n.Valid {
		return n.RawMessage
	}
	return []byte{}
}
