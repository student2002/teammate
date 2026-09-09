// memory.go provides HTTP API endpoints for creating, listing, deleting, and text-searching shared memories (Memory).
//
// Shared memories are a knowledge base shared among Agents; currently memories are retrieved via ILIKE text search.
// The database has a reserved embedding vector(1536) field; pgvector semantic retrieval will be enabled once an embedding generation service is integrated.
// Agents need the memory:create permission to create memories, and the resource:delete permission to delete memories.

package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/sqlc-dev/pqtype"

	svcmw "github.com/teammate/server/internal/server/middleware"
	"github.com/teammate/server/internal/server/response"
	"github.com/teammate/server/internal/service"
	"github.com/teammate/server/internal/types"
)

// MemoryHandler handles HTTP requests related to shared memories, including creating, listing, deleting, and semantic search.
type MemoryHandler struct {
	Svc     *service.Service
	Checker svcmw.WorkspaceAccessCheckerFunc // workspace access checker (injected, not global)
}

// NewMemoryHandler creates a MemoryHandler instance.
func NewMemoryHandler(svc *service.Service, checker svcmw.WorkspaceAccessCheckerFunc) *MemoryHandler {
	return &MemoryHandler{Svc: svc, Checker: checker}
}

// createMemoryRequest create memory request body.
type createMemoryRequest struct {
	WorkspaceID  string          `json:"workspace_id"`   // workspace ID
	SourceTaskID string          `json:"source_task_id"` // source task ID
	Type         string          `json:"type"`           // memory type
	Title        string          `json:"title"`          // memory title
	Content      string          `json:"content"`        // memory content
	Tags         []string        `json:"tags"`           // tag list
	Confidence   float32         `json:"confidence"`     // confidence (0-1)
	Verified     bool            `json:"verified"`       // whether verified
	Metadata     json.RawMessage `json:"metadata"`       // metadata (JSON)
}

func (h *MemoryHandler) resolveWorkspaceForRequest(w http.ResponseWriter, r *http.Request, workspaceIDStr string) (uuid.UUID, string, bool) {
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return uuid.Nil, "", false
	}
	workspaceID, err := uuid.Parse(workspaceIDStr)
	if workspaceIDStr == "" || err != nil {
		response.BadRequest(w, "valid workspace_id is required")
		return uuid.Nil, "", false
	}
	if h.Checker == nil {
		response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
		return uuid.Nil, "", false
	}
	role, err := h.Checker(r.Context(), claims.UserID, claims.UserType, workspaceID)
	if err != nil {
		response.Forbidden(w, "workspace access denied")
		return uuid.Nil, "", false
	}
	return workspaceID, role, true
}

// CreateMemory handles the POST /memories endpoint, creating a new shared memory entry; Agents need the memory:create permission.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Request body:
//   - source_task_id: string, source task ID
//   - type: string, memory type
//   - title: string, memory title
//   - content: string, memory content
//   - tags: string[], tag list
//   - confidence: float, confidence (default 0.5)
//   - verified: bool, whether verified (Agents cannot set to true)
//   - metadata: object, metadata
//
// Response:
//   - 201: memory created successfully
//   - 400: parameter error
//   - 401: not authenticated
//   - 403: insufficient permissions (Agents need memory:create, humans need member+ role)
func (h *MemoryHandler) CreateMemory(w http.ResponseWriter, r *http.Request) {
	// parse request body
	var req createMemoryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	workspaceID, role, ok := h.resolveWorkspaceForRequest(w, r, req.WorkspaceID)
	if !ok {
		return
	}

	// permission check: Agents and human users use different permission models
	if claims.UserType == "agent" {
		// Agents must have the memory:create permission
		permSvc := service.NewAgentPermissionService(h.Svc)
		has, err := permSvc.HasPermission(r.Context(), claims.UserID, types.PermMemoryCreate)
		if err != nil || !has {
			response.Forbidden(w, "agent lacks memory:create permission")
			return
		}
		// Agents cannot set verified=true
		req.Verified = false
	} else {
		// human users need member or higher role
		if types.MemberRoleLevel(role) < 2 {
			response.Forbidden(w, "insufficient permissions: member role or higher required")
			return
		}
	}

	// convert source task ID
	var sourceTaskID sql.NullInt32
	if req.SourceTaskID != "" {
		var tid int32
		if _, err := fmt.Sscanf(req.SourceTaskID, "%d", &tid); err == nil {
			sourceTaskID = sql.NullInt32{Int32: tid, Valid: true}
		}
	}

	// set default tags
	tags := req.Tags
	if tags == nil {
		tags = []string{}
	}

	// set default confidence
	confidence := req.Confidence
	if confidence == 0 {
		confidence = 0.5
	}

	// convert metadata
	var metadata pqtype.NullRawMessage
	if req.Metadata != nil {
		metadata = pqtype.NullRawMessage{RawMessage: req.Metadata, Valid: true}
	} else {
		metadata = pqtype.NullRawMessage{RawMessage: json.RawMessage(`{}`), Valid: true}
	}

	// call service to create the memory
	memSvc := service.NewMemoryService(h.Svc)
	memory, err := memSvc.Create(r.Context(), buildCreateMemoryParams(
		workspaceID,
		sourceTaskID,
		req.Type,
		req.Title,
		req.Content,
		tags,
		confidence,
		req.Verified,
		metadata,
	))
	if err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
	response.JSON(w, r, memory)
}

// ListMemories handles the GET /memories endpoint, listing all shared memories under the workspace, supporting filtering by verification status and confidence.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Query parameters:
//   - verified: bool, filter by verification status
//   - min_confidence: float, minimum confidence filter
//   - limit: int, return count limit
//
// Response:
//   - 200: successfully returns the memory list
//   - 400: query parameter error
//   - 401: not authenticated
func (h *MemoryHandler) ListMemories(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.resolveWorkspaceForRequest(w, r, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}

	memSvc := service.NewMemoryService(h.Svc)

	// parse optional filter parameters
	var verified *bool
	var minConfidence *float32
	var limit *int32

	// parse the verified parameter
	if v := r.URL.Query().Get("verified"); v != "" {
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			response.BadRequest(w, "invalid verified parameter")
			return
		}
		verified = &parsed
	}

	// parse the min_confidence parameter
	if mc := r.URL.Query().Get("min_confidence"); mc != "" {
		parsed, err := strconv.ParseFloat(mc, 32)
		if err != nil {
			response.BadRequest(w, "invalid min_confidence parameter")
			return
		}
		f := float32(parsed)
		minConfidence = &f
	}

	// parse the limit parameter
	if l := r.URL.Query().Get("limit"); l != "" {
		parsed, err := strconv.ParseInt(l, 10, 32)
		if err != nil {
			response.BadRequest(w, "invalid limit parameter")
			return
		}
		n := int32(parsed)
		limit = &n
	}

	// call service to query memories
	memories, err := memSvc.ListByWorkspace(r.Context(), workspaceID, verified, minConfidence, limit)
	if err != nil {
		response.InternalServerError(w, err)
		return
	}
	response.JSON(w, r, memories)
}

// DeleteMemory handles the DELETE /memories/{id} endpoint, deleting the specified shared memory entry; Agents need the resource:delete permission.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Response:
//   - 204: deleted successfully
//   - 400: invalid memory ID
//   - 401: not authenticated
//   - 403: insufficient permissions (Agents need resource:delete, humans need member+ role)
//   - 404: memory does not exist
func (h *MemoryHandler) DeleteMemory(w http.ResponseWriter, r *http.Request) {
	// parse memory ID
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "invalid memory id")
		return
	}

	// get auth info
	claims, ok := svcmw.GetAuthFromContext(r.Context())
	if !ok {
		response.Unauthorized(w, "authentication required")
		return
	}

	// verify the memory belongs to the current workspace
	memSvc := service.NewMemoryService(h.Svc)
	memory, err := memSvc.Get(r.Context(), id)
	if err != nil {
		response.NotFound(w, "memory not found")
		return
	}
	if h.Checker == nil {
		response.InternalServerError(w, fmt.Errorf("workspace access checker not configured"))
		return
	}
	// workspace ID is a domain-model string, needs to be parsed back to uuid.UUID to pass to the Checker
	memoryWsID, _ := uuid.Parse(memory.WorkspaceID)
	role, err := h.Checker(r.Context(), claims.UserID, claims.UserType, memoryWsID)
	if err != nil {
		response.NotFound(w, "memory not found")
		return
	}

	// permission check: who can delete this memory?
	if claims.UserType == "agent" {
		// Agents must have the resource:delete permission
		permSvc := service.NewAgentPermissionService(h.Svc)
		has, err := permSvc.HasPermission(r.Context(), claims.UserID, types.PermResourceDelete)
		if err != nil || !has {
			response.Forbidden(w, "agent lacks resource:delete permission")
			return
		}
	} else {
		// human users need member or higher role (viewer cannot delete)
		if types.MemberRoleLevel(role) < 2 {
			response.Forbidden(w, "insufficient permissions: member role or higher required")
			return
		}
	}

	// call service to delete the memory
	if err := memSvc.Delete(r.Context(), id); err != nil {
		response.InternalServerError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SearchMemories handles the GET /memories/search endpoint, using pgvector semantic search to match shared memory entries.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request
//
// Query parameters:
//   - q: string, search keyword
//
// Response:
//   - 200: successfully returns search results
//   - 401: not authenticated
func (h *MemoryHandler) SearchMemories(w http.ResponseWriter, r *http.Request) {
	// get the search keyword
	q := r.URL.Query().Get("q")

	workspaceID, _, ok := h.resolveWorkspaceForRequest(w, r, r.URL.Query().Get("workspace_id"))
	if !ok {
		return
	}

	// if there is a search keyword, perform semantic search
	if q != "" {
		memSvc := service.NewMemoryService(h.Svc)
		results, err := memSvc.Search(r.Context(), q, workspaceID)
		if err != nil {
			response.InternalServerError(w, err)
			return
		}

		response.JSON(w, r, results)
		return
	}

	// return an empty list when there is no search keyword
	response.JSON(w, r, []interface{}{})
}
