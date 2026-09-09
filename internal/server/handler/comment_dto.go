// comment_dto.go provides domain type aliases and parameter builders for comment.go.
package handler

import (
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	"github.com/teammate/server/internal/types"
)

// ---- domain type aliases ----

type Comment = types.Comment

// ---- parameter builders ----

// buildCreateCommentParams builds types.CreateCommentParams from request fields.
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
	// metadata defaults to empty JSON
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
