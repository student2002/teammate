// comment.go provides HTTP API endpoints for creating, listing, and editing task comments.
//
// Comments support nested replies (via parent_id) and @mentions (via the mentions field).
// Comment content is limited to 10000 characters, and editing has a time window restriction.

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

// CommentHandler handles HTTP requests related to task comments, including creating, listing, and editing comments.
type CommentHandler struct {
	Svc *service.Service
}

// NewCommentHandler creates a CommentHandler instance.
func NewCommentHandler(svc *service.Service) *CommentHandler {
	return &CommentHandler{Svc: svc}
}

// Routes returns the route table for comments.
func (h *CommentHandler) Routes() chi.Router {
	r := chi.NewRouter()

	r.Post("/", h.CreateComment)
	r.Get("/", h.ListComments)

	return r
}

// createCommentRequest create comment request body.
type createCommentRequest struct {
	NodeID       *uuid.UUID  `json:"node_id"`        // node ID the comment belongs to (optional, empty means task-level comment)
	SourceNodeID *uuid.UUID  `json:"source_node_id"` // comment source node ID (optional, used for handoff)
	ParentID     *uuid.UUID  `json:"parent_id"`      // parent comment ID (optional, used for replies)
	Content      string      `json:"content"`        // comment content
	CommentType  string      `json:"comment_type"`   // comment type
	Mentions     []uuid.UUID `json:"mentions"`       // list of @mentioned user IDs
}

// CreateComment handles the POST /tasks/{taskId}/comments endpoint, creating a comment for the specified task, supporting replies and @mentions.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - parent_id: UUID, parent comment ID (optional, used for nested replies)
//   - content: string, comment content (required, up to 10000 characters)
//   - mentions: UUID[], list of @mentioned user IDs
//
// Response:
//   - 201: comment created successfully
//   - 400: parameter error or content exceeds the limit
//   - 401: not authenticated
func (h *CommentHandler) CreateComment(w http.ResponseWriter, r *http.Request) {
	// parse task ID
	taskIDStr := chi.URLParam(r, "taskId")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// workspace ownership has already been verified by TaskAccessMiddleware

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}
	if !h.requireCommentWrite(w, r, claims) {
		return
	}

	// parse request body
	var req createCommentRequest
	if err := render.Decode(r, &req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// validate content length
	if len(req.Content) > 10000 {
		response.BadRequest(w, "content must be at most 10000 characters")
		return
	}

	// convert and validate node ownership, to prevent writing a comment into another task's node comment section
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

	// convert parent comment ID, and validate that replies cannot cross tasks
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

	// derive author identity from auth info (not the request body), to prevent spoofing
	authorType := claims.UserType
	authorID := claims.UserID

	// call service to create the comment
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

// ListComments handles the GET /tasks/{taskId}/comments endpoint, listing all comments for the specified task.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 200: successfully returns the comment list
//   - 400: invalid task ID
func (h *CommentHandler) ListComments(w http.ResponseWriter, r *http.Request) {
	// parse task ID
	taskIDStr := chi.URLParam(r, "taskId")
	var taskID int32
	if _, err := fmt.Sscanf(taskIDStr, "%d", &taskID); err != nil {
		response.BadRequest(w, "invalid task id")
		return
	}

	// workspace ownership has already been verified by TaskAccessMiddleware

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

