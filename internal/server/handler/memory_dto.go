// memory_dto.go defines request/response structs and data conversion functions related to Memory.
package handler

import (
	"database/sql"

	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// MemoryType alias for memory type (domain model: string).
type MemoryType = string

// buildCreateMemoryParams constructs types.CreateMemoryParams from handler-layer input.
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
