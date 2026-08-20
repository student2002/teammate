// comment.go 提供任务评论的创建、列表查询和编辑等 HTTP API 端点。
//
// 评论支持嵌套回复（通过 parent_id）和 @提及（通过 mentions 字段）。
// 评论内容限制为 10000 字符，编辑有时间窗口限制。

package handler

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"
	"github.com/google/uuid"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// CommentHandler 处理任务评论相关的 HTTP 请求，包括创建、列表查询和编辑评论。
type CommentHandler struct {
	Svc *service.Service
}

// NewCommentHandler 创建 CommentHandler 实例。
func NewCommentHandler(svc *service.Service) *CommentHandler {
	return &CommentHandler{Svc: svc}
}

// Routes 返回评论的路由表。
func (h *CommentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateComment)
	r.Get("/", h.ListComments)

	return r
}

// createCommentRequest 创建评论请求体。
type createCommentRequest struct {
	NodeID       *uuid.UUID  `json:"node_id"`        // 评论所属节点 ID（可选，空表示任务级评论）
	SourceNodeID *uuid.UUID  `json:"source_node_id"` // 评论来源节点 ID（可选，用于 handoff）
	ParentID     *uuid.UUID  `json:"parent_id"`      // 父评论 ID（可选，用于回复）
	Content      string      `json:"content"`        // 评论内容
	CommentType  string      `json:"comment_type"`   // 评论类型
	Mentions     []uuid.UUID `json:"mentions"`       // @提及的用户 ID 列表
}

// CreateComment 处理 POST /tasks/{taskId}/comments 端点，为指定任务创建评论，支持回复和@提及。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 请求体：
//   - parent_id: UUID，父评论 ID（可选，用于嵌套回复）
//   - content: string，评论内容（必填，最多 10000 字符）
//   - mentions: UUID[]，@提及的用户 ID 列表
//
// 响应：
//   - 201: 成功创建评论
//   - 400: 参数错误或内容超过限制
//   - 401: 未认证
func (h *CommentHandler) CreateComment(w http.ResponseWriter, r *http.Request) {
	// 解析任务 ID
	taskIDStr := chi.URLParam(r, "taskId")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// 工作区归属已由 TaskAccessMiddleware 验证

	// 获取认证信息
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if !h.requireCommentWrite(w, r, claims) {
		return
	}

	// 解析请求体
	var req createCommentRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// 验证内容长度
	if len(req.Content) > 10000 {
		response.BadRequest(w, "content must be at most 10000 characters")
		return
	}

	// 转换并校验节点归属，防止把评论写入其他任务的节点评论区。
	var nodeID uuid.NullUUID
	if req.NodeID != nil {
		if !h.validateCommentNode(w, r, taskID, *req.NodeID) {
			return
		}
		nodeID = uuid.NullUUID{UUID: *req.NodeID, Valid: true}
	}

	var sourceNodeID uuid.NullUUID
	if req.SourceNodeID != nil {
		if !h.validateCommentNode(w, r, taskID, *req.SourceNodeID) {
			return
		}
		sourceNodeID = uuid.NullUUID{UUID: *req.SourceNodeID, Valid: true}
	}

	// 转换父评论 ID，并校验回复不能跨任务。
	var parentID uuid.NullUUID
	if req.ParentID != nil {
		if !h.validateParentComment(w, r, taskID, *req.ParentID) {
			return
		}
		parentID = uuid.NullUUID{UUID: *req.ParentID, Valid: true}
	}

	mentions := req.Mentions
	if mentions == nil {
		mentions = []uuid.UUID{}
	}

	commentType := strings.TrimSpace(req.CommentType)
	if commentType == "" {
		commentType = "text"
	}
	if !isAllowedCommentType(commentType) {
		response.BadRequest(w, "invalid comment_type")
		return
	}

	// 从认证信息派生作者身份（非请求体），防止伪造
	authorType := claims.UserType
	authorID := claims.UserID

	// 调用 service 创建评论
	commentSvc := service.NewCommentService(h.Svc)
	comment, err := commentSvc.Create(r.Context(), buildCreateCommentParams(
		taskID, nodeID, sourceNodeID, parentID, authorType, authorID, req.Content, commentType, mentions,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, comment)
}

func (h *CommentHandler) currentActorID(ctx context.Context) uuid.UUID {
	claims, ok := svcmw.GetAuthFromContext(ctx)
	if !ok {
		return uuid.Nil
	}
	return claims.UserID
}

func (h *CommentHandler) requireCommentWrite(w http.ResponseWriter, r *http.Request, claims svcmw.AuthClaims) bool {
	if claims.UserType == "agent" {
		has, err := service.NewAgentPermissionService(h.Svc).HasAgentPermissionAny(r.Context(), claims.UserID, types.PermTaskComment)
		if err != nil {
			response.InternalServerError(w, err)
			return false
		}
		if !has {
			response.Forbidden(w, "insufficient permissions")
			return false
		}
		return true
	}

	ws, ok := svcmw.GetWorkspaceFromContext(r.Context())
	if !ok {
		response.Forbidden(w, "workspace context required")
		return false
	}
	if types.MemberRoleLevel(ws.Role) < types.MemberRoleLevel("member") {
		response.Forbidden(w, "insufficient permissions")
		return false
	}
	return true
}

func isAllowedCommentType(commentType string) bool {
	switch commentType {
	case "text", "code_review", "suggestion", "question", "handoff", "decision", "execution_summary":
		return true
	default:
		return false
	}
}

func (h *CommentHandler) validateCommentNode(w http.ResponseWriter, r *http.Request, taskID int32, nodeID uuid.UUID) bool {
	node, err := service.NewNodeService(h.Svc).GetTaskNode(r.Context(), nodeID)
	if err != nil {
		response.BadRequest(w, "invalid node id")
		return false
	}
	if node.TaskID != taskID {
		response.BadRequest(w, "node does not belong to task")
		return false
	}
	return true
}

func (h *CommentHandler) validateParentComment(w http.ResponseWriter, r *http.Request, taskID int32, commentID uuid.UUID) bool {
	comment, err := service.NewCommentService(h.Svc).GetComment(r.Context(), commentID)
	if err != nil {
		response.BadRequest(w, "invalid parent_id")
		return false
	}
	if comment.TaskID != taskID {
		response.BadRequest(w, "parent comment does not belong to task")
		return false
	}
	return true
}

// ListComments 处理 GET /tasks/{taskId}/comments 端点，列出指定任务的所有评论。
//
// 参数：
//   - w: HTTP 响应写入器
//   - r: HTTP 请求
//
// 响应：
//   - 200: 成功返回评论列表
//   - 400: 任务 ID 无效
func (h *CommentHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	// 解析任务 ID
	taskIDStr := chi.URLParam(r, "taskId")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// 工作区归属已由 TaskAccessMiddleware 验证

	commentSvc := service.NewCommentService(h.Svc)
	nodeIDParam := strings.TrimSpace(r.URL.Query().Get("node_id"))
	scope := strings.TrimSpace(r.URL.Query().Get("scope"))

	if nodeIDParam != "" {
		nodeID, err := uuid.Parse(nodeIDParam)
		if err != nil {
			response.BadRequest(w, "invalid node_id")
			return
		}
		if !h.validateCommentNode(w, r, taskID, nodeID) {
			return
		}

		var comments []Comment
		if scope == "execution_context" {
			comments, err = commentSvc.ListExecutionContext(r.Context(), taskID, nodeID, h.currentActorID(r.Context()))
		} else {
			comments, err = commentSvc.ListNode(r.Context(), taskID, nodeID)
		}
		if err != nil {
			response.InternalServerError(w, err)
			return
		}
		response.JSON(w, r, comments)
		return
	}

	var comments []Comment
	var err error
	if scope == "task" {
		comments, err = commentSvc.ListTaskLevel(r.Context(), taskID)
	} else {
		comments, err = commentSvc.List(r.Context(), taskID)
	}
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	response.JSON(w, r, comments)
}

