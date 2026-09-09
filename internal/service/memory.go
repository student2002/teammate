// memory.go implements the business logic for shared memory, supporting creating, listing,
// deleting, and text-searching memory entries.
//
// This file contains:
//   - MemoryService struct: the memory management service, encapsulating CRUD and text-search
//     operations for memory entries
//   - Create: creates a memory entry, including content, confidence, etc.
//   - ListByWorkspace: lists memory entries by workspace, with filters for verified status,
//     confidence, and count limit
//   - Delete: deletes the specified memory entry
//   - Search: text-searches memory entries, using ILIKE fuzzy matching to return results
//
// The database has a reserved embedding vector(1536) field; pgvector semantic retrieval
// (ordered by cosine distance) will be enabled once an embedding generation service is wired in.
// Shared memory allows agents to share contextual knowledge, improving multi-agent collaboration.
// Memory entries carry a verified status and a confidence score to evaluate the reliability of
// the memory.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// MemoryService provides the business logic for shared memory management.
// Memory entries carry content, a vector embedding (for semantic search), a confidence score,
// and a verified status.
type MemoryService struct {
	svc *Service
}

// NewMemoryService creates a new MemoryService instance.
func NewMemoryService(svc *Service) *MemoryService {
	return &MemoryService{svc: svc}
}

// Create creates a memory entry.
//
// Parameters:
//   - ctx: request context
//   - params: parameters for creating the memory, including content, vector embedding,
//     task ID, workspace ID, etc.
//
// Returns:
//   - types.Memory: the created memory entry
//   - error: error returned when creation fails
func (s *MemoryService) Create(ctx context.Context, params types.CreateMemoryParams) (types.Memory, error) {
	return s.svc.Store.CreateMemory(ctx, params)
}

// ListByWorkspace lists memory entries for the given workspace, with optional filters by
// verified status, minimum confidence, and count limit. All parameters are optional; passing
// nil means no filter on that dimension.
//
// Parameters:
//   - ctx: request context
//   - workspaceID: workspace ID
//   - verified: optional, filter by verified status (true=verified, false=unverified)
//   - minConfidence: optional, minimum confidence threshold
//   - limit: optional, maximum number of entries to return
//
// Returns:
//   - []types.Memory: memory entry list
//   - error: possible error (database query failure)
func (s *MemoryService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID, verified *bool, minConfidence *float32, limit *int32) ([]types.Memory, error) {
	return s.svc.Store.ListMemoriesByWorkspace(ctx, workspaceID, verified, minConfidence, limit)
}

// Get fetches a single memory entry by ID.
//
// Parameters:
//   - ctx: request context
//   - id: memory entry ID
//
// Returns:
//   - types.Memory: the memory entry
//   - error: possible error (record does not exist)
func (s *MemoryService) Get(ctx context.Context, id uuid.UUID) (types.Memory, error) {
	memory, err := s.svc.Store.GetMemory(ctx, id)
	if err != nil {
		return types.Memory{}, fmt.Errorf("get memory: %w", err)
	}
	return memory, nil
}

// Delete deletes a memory entry.
//
// Parameters:
//   - ctx: request context
//   - id: memory entry ID
//
// Returns:
//   - error: possible error (record does not exist, database delete failure)
func (s *MemoryService) Delete(ctx context.Context, id uuid.UUID) error {
	return s.svc.Store.DeleteMemory(ctx, id)
}

// Search performs a text search over memory entries (title/content ILIKE match) and returns
// results sorted by relevance. pgvector semantic retrieval is a reserved capability (the
// embedding column is retained but no query uses it yet).
//
// Parameters:
//   - ctx: request context
//   - query: search keyword or semantic query text
//   - workspaceID: workspace ID, scoping the search
//
// Returns:
//   - []types.SearchMemoriesRow: memory entry list sorted by relevance
//   - error: possible error (database query failure)
func (s *MemoryService) Search(ctx context.Context, query string, workspaceID uuid.UUID) ([]types.SearchMemoriesRow, error) {
	return s.svc.Store.SearchMemories(ctx, query, workspaceID)
}
