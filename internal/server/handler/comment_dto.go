// comment_dto.go 为 comment.go 提供 domain 类型别名和参数构建器。
package handler

import (
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// ---- domain 类型别名 ----

type Comment = types.Comment

// ---- 参数构建器 ----

// buildCreateCommentParams 从请求字段构建 types.CreateCommentParams。
func buildCreateCommentParams(
	taskID int32,
	nodeID uuid.NullUUID,
	sourceNodeID uuid.NullUUID,
	parentID uuid.NullUUID,
	authorType string,
	authorID uuid.UUID,
	content string,
	commentType string,
	mentions []uuid.UUID,
) types.CreateCommentParams {
	var nodeIDPtr *string
	if nodeID.Valid {
		s := nodeID.UUID.String()
		nodeIDPtr = &s
	}
	var sourceNodeIDPtr *string
	if sourceNodeID.Valid {
		s := sourceNodeID.UUID.String()
		sourceNodeIDPtr = &s
	}
	var parentIDPtr *string
	if parentID.Valid {
		s := parentID.UUID.String()
		parentIDPtr = &s
	}
	mentionStrs := make([]string, 0, len(mentions))
	for _, m := range mentions {
		mentionStrs = append(mentionStrs, m.String())
	}
	// metadata 默认空 JSON
	var metadata []byte
	_ = pqtype.NullRawMessage{}
	metadata = []byte("{}")
	return types.CreateCommentParams{
		TaskID:       taskID,
		NodeID:       nodeIDPtr,
		SourceNodeID: sourceNodeIDPtr,
		ParentID:     parentIDPtr,
		AuthorType:   authorType,
		AuthorID:     authorID.String(),
		Content:      content,
		CommentType:  commentType,
		Metadata:     metadata,
		Mentions:     mentionStrs,
	}
}
