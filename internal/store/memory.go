// memory.go provides data access operations for knowledge memory.
//
// Memory is the shared knowledge base of a workspace, storing information such as
// architecture decisions, commands, conventions, and insights.
// Currently memories are retrieved via ILIKE text search; the database already reserves
// an embedding vector(1536) field, and pgvector semantic retrieval (cosine distance
// ordering) will be enabled once the embedding generation service is integrated.
//
// Memory entries are organized with tags and support filtering by type, confidence,
// and verification status.
// Outdated memories are marked stale and are excluded from search.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	db "github.com/teammate/server/internal/db/generated"
	"github.com/teammate/server/internal/types"
)

// escapeILIKE escapes special characters (% and _) in ILIKE queries.
//
// Parameters:
//   - s: the string to escape
//
// Returns:
//   - string: the escaped string
func escapeILIKE(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// CreateMemory creates a single memory entry record.
//
// Parameters:
//   - ctx: request context
//   - params: memory creation params, including workspace ID, type, title, content, tags, etc.
//
// Returns:
//   - db.Memory: the created memory record
//   - error: error returned when creation fails
func (s *Store) CreateMemory(ctx context.Context, params types.CreateMemoryParams) (types.Memory, error) {
	dbParams, err := FromDomainCreateMemoryParams(params)
	if err != nil {
		return types.Memory{}, fmt.Errorf("convert create memory params: %w", err)
	}
	m, err := s.q.CreateMemory(ctx, dbParams)
	if err != nil {
		return types.Memory{}, fmt.Errorf("create memory: %w", err)
	}
	return ToDomainMemory(m)
}

// GetMemory retrieves a single memory entry by ID.
func (s *Store) GetMemory(ctx context.Context, id uuid.UUID) (types.Memory, error) {
	m, err := s.q.GetMemory(ctx, id)
	if err != nil {
		return types.Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return ToDomainMemory(m)
}

// ListMemoriesByWorkspace paginates memory entries for the specified workspace,
// supporting filtering by verification status and minimum confidence.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace UUID
//   - verified: verification status filter (nil means no filter)
//   - minConfidence: minimum confidence filter (nil means no filter)
//   - limit: maximum number of records to return (nil means default limit)
//
// Returns:
//   - []db.Memory: memory list
//   - error: error returned when the query fails
func (s *Store) ListMemoriesByWorkspace(ctx context.Context, workspaceID uuid.UUID, verified *bool, minConfidence *float32, limit *int32) ([]types.Memory, error) {
	memories, err := s.q.ListMemoriesByWorkspace(ctx, FromDomainListMemoriesByWorkspaceParams(workspaceID, verified, minConfidence, limit))
	if err != nil {
		return nil, fmt.Errorf("list memories by workspace: %w", err)
	}
	return ToDomainMemorySlice(memories)
}

// DeleteMemory deletes a single memory entry by ID.
//
// Parameters:
//   - ctx: request context
//   - id: memory UUID
//
// Returns:
//   - error: error returned when deletion fails
func (s *Store) DeleteMemory(ctx context.Context, id uuid.UUID) error {
	if err := s.q.DeleteMemory(ctx, id); err != nil {
		return fmt.Errorf("delete memory: %w", err)
	}
	return nil
}

// MarkMemoriesStaleByTask marks all memory entries associated with the specified task as stale.
//
// When a task is cancelled or re-executed, previously generated memories may be outdated
// and need to be marked stale.
//
// Parameters:
//   - ctx: request context
//   - taskID: integer ID of the task
//
// Returns:
//   - error: error returned when the update fails
func (s *Store) MarkMemoriesStaleByTask(ctx context.Context, taskID int32) error {
	if err := s.q.MarkMemoriesStaleByTask(ctx, sql.NullInt32{Int32: taskID, Valid: true}); err != nil {
		return fmt.Errorf("mark memories stale by task: %w", err)
	}
	return nil
}

// MemoryRow is the simplified memory row struct used by search results.
//
// It contains the basic information, vector embedding, and metadata of a memory,
// used for search result returns.
type MemoryRow struct {
	ID           string          `json:"id"`             // memory UUID
	WorkspaceID  string          `json:"workspace_id"`   // workspace ID
	SourceTaskID *string         `json:"source_task_id"` // source task ID (optional)
	Type         string          `json:"type"`           // memory type (architecture/command/convention, etc.)
	Title        string          `json:"title"`          // title
	Content      string          `json:"content"`        // content
	Tags         []string        `json:"tags"`           // tag list
	Embedding    interface{}     `json:"embedding"`      // vector embedding (pgvector)
	Confidence   float32         `json:"confidence"`     // confidence (0-1)
	Verified     bool            `json:"verified"`       // whether verified
	Metadata     json.RawMessage `json:"metadata"`       // extended metadata (JSON)
	CreatedAt    string          `json:"created_at"`     // creation time
	UpdatedAt    string          `json:"updated_at"`     // update time
}

// SearchMemories performs an ILIKE text search on memory entries, limiting results to 100 rows
// to prevent memory exhaustion.
//
// Search scope:
//   - Only non-stale memories are searched (stale = false)
//   - Fuzzy matching is performed against the title and content
//   - Results are ordered by creation time in descending order
//
// Parameters:
//   - ctx: request context
//   - query: search keywords
//   - workspaceID: workspace UUID
//
// Returns:
//   - []MemoryRow: search result list
//   - error: error returned when the query fails
func (s *Store) SearchMemories(ctx context.Context, query string, workspaceID uuid.UUID) ([]types.SearchMemoriesRow, error) {
	escapedQuery := "%" + escapeILIKE(query) + "%"
	rows, err := s.q.SearchMemories(ctx, db.SearchMemoriesParams{
		WorkspaceID: workspaceID,
		Title:       escapedQuery,
	})
	if err != nil {
		return nil, fmt.Errorf("search memories: %w", err)
	}

	results := make([]types.SearchMemoriesRow, 0, len(rows))
	for _, r := range rows {
		var sourceTaskID *int32
		if r.SourceTaskID.Valid {
			v := r.SourceTaskID.Int32
			sourceTaskID = &v
		}
		results = append(results, types.SearchMemoriesRow{
			ID:           r.ID.String(),
			WorkspaceID:  r.WorkspaceID.String(),
			SourceTaskID: sourceTaskID,
			Type:         string(r.Type),
			Title:        r.Title,
			Content:      r.Content,
			Tags:         r.Tags,
			Confidence:   float32(r.Confidence),
			Verified:     r.Verified,
			Metadata:     nullRawToRaw(r.Metadata),
			CreatedAt:    r.CreatedAt,
			UpdatedAt:    r.UpdatedAt,
		})
	}
	return results, nil
}
