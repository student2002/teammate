// comment.go 定义评论相关的类型常量。
//
// 本文件包含：
//   - CommentType：评论类型枚举，用于区分不同用途的评论
//   - 预定义常量：text、code_review、suggestion、question 四种类型
//
// 评论类型说明：
//   - text：普通文本评论，用于一般性讨论和说明
//   - code_review：代码审查评论，用于 Pull Request 或代码审查场景
//   - suggestion：代码建议评论，用于提出改进方案和优化建议
//   - question：问题咨询评论，用于提出需要解答的问题
package types

// CommentType 表示评论的类型，区分普通文本评论、代码审查意见、建议和问题。
type CommentType string

const (
	// CommentTypeText 表示普通文本评论，用于一般性讨论和说明。
	CommentTypeText CommentType = "text"
	// CommentTypeCodeReview 表示代码审查评论，用于 Pull Request 或代码审查场景。
	CommentTypeCodeReview CommentType = "code_review"
	// CommentTypeSuggestion 表示代码建议评论，用于提出改进方案和优化建议。
	CommentTypeSuggestion CommentType = "suggestion"
	// CommentTypeQuestion 表示问题咨询评论，用于提出需要解答的问题。
	CommentTypeQuestion CommentType = "question"
	// CommentTypeHandoff 表示节点间交接评论，写入下游节点评论区。
	CommentTypeHandoff CommentType = "handoff"
	// CommentTypeDecision 表示人工或审查决策评论。
	CommentTypeDecision CommentType = "decision"
	// CommentTypeExecutionSummary 表示节点执行摘要评论。
	CommentTypeExecutionSummary CommentType = "execution_summary"
)
