// memory_dto.go 定义 Memory 相关的请求/响应结构体和数据转换函数。
package handler

import (
	"database/sql"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// MemoryType 记忆类型的别名（domain 幜格：string）。
type MemoryType = string

// buildCreateMemoryParams 根据 handler 层输入构造 types.CreateMemoryParams。
func buildCreateMemoryParams(
	workspaceID uuid.UUID,
	sourceTaskID sql.NullInt32,
	memoryType string,
	title string,
	content string,
	tags []string,
	confidence float32,
	verified bool,
	metadata pqtype.NullRawMessage,
) types.CreateMemoryParams {
	var sourceTask *int32
	if sourceTaskID.Valid {
		v := sourceTaskID.Int32
		sourceTask = &v
	}
	var meta []byte
	if metadata.Valid {
		meta = metadata.RawMessage
	} else {
		meta = []byte("{}")
	}
	return types.CreateMemoryParams{
		WorkspaceID:  workspaceID.String(),
		SourceTaskID:  sourceTask,
		Type:          memoryType,
		Title:         title,
		Content:       content,
		Tags:          tags,
		Confidence:    confidence,
		Verified:      verified,
		Metadata:      meta,
	}
}
