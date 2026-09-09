// comment.go defines the type constants for comments.
//
// This file contains:
//   - CommentType: the comment-type enum, used to distinguish comments by purpose
//   - Predefined constants: text, code_review, suggestion, question
//
// Comment-type descriptions:
//   - text: a plain-text comment for general discussion and notes
//   - code_review: a code-review comment for pull requests or code-review scenarios
//   - suggestion: a code-suggestion comment for proposing improvements and optimizations
//   - question: a question comment for raising issues that need an answer
package types

// CommentType represents the type of a comment, distinguishing plain-text comments, code-review opinions, suggestions, and questions.
type CommentType string

const (
	// CommentTypeText is a plain-text comment for general discussion and notes.
	CommentTypeText CommentType = "text"
	// CommentTypeCodeReview is a code-review comment for pull requests or code-review scenarios.
	CommentTypeCodeReview CommentType = "code_review"
	// CommentTypeSuggestion is a code-suggestion comment for proposing improvements and optimizations.
	CommentTypeSuggestion CommentType = "suggestion"
	// CommentTypeQuestion is a question comment for raising issues that need an answer.
	CommentTypeQuestion CommentType = "question"
	// CommentTypeHandoff is a handoff comment between nodes, written to the downstream node's comment area.
	CommentTypeHandoff CommentType = "handoff"
	// CommentTypeDecision is a manual or review decision comment.
	CommentTypeDecision CommentType = "decision"
	// CommentTypeExecutionSummary is a node execution-summary comment.
	CommentTypeExecutionSummary CommentType = "execution_summary"
)
