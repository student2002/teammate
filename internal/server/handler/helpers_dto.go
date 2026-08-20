// helpers_dto.go 为 helpers.go 中使用的 db 模型类型提供别名。
package handler

import (
	"github.com/teammate/server/internal/types"
)

// ---- 模型类型别名，消除 handler 层对 db/generated 的直接依赖 ----
// Note: Project is defined in project_dto.go, NodeType/AssigneeType in workflow_dto.go.

type TaskNode = types.TaskNode
type WorkflowTemplate = types.WorkflowTemplate
type Agent = types.Agent
type Skill = types.Skill
type McpServer = types.McpServer
type Runtime = types.Runtime
