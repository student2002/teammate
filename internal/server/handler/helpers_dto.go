// helpers_dto.go provides aliases for db model types used in helpers.go.
package handler

import (
	"github.com/teammate/server/internal/types"
)

// ---- model type aliases, removing the handler layer's direct dependency on db/generated ----
// Note: Project is defined in project_dto.go, NodeType/AssigneeType in workflow_dto.go.

type TaskNode = types.TaskNode
type WorkflowTemplate = types.WorkflowTemplate
type Agent = types.Agent
type Skill = types.Skill
type McpServer = types.McpServer
type Runtime = types.Runtime
