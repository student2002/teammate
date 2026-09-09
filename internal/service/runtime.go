// runtime.go implements the business logic for runtimes (Agentd daemon instances),
// including registration, heartbeat maintenance, state listing, and full sync.
// After registration, it sends a sync:required event via SSE to trigger a full agent sync,
// ensuring the daemon discovers pending nodes created before registration.
package service

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
)

// RuntimeService provides the business logic for runtime management.
type RuntimeService struct {
	svc *Service
}

func NewRuntimeService(svc *Service) *RuntimeService {
	return &RuntimeService{svc: svc}
}

// Register creates a runtime and updates the agent status to online.
// After registration, it sends a sync:required event via SSE so the daemon immediately discovers pending nodes created before registration.
func (s *RuntimeService) Register(ctx context.Context, params types.CreateRuntimeParams) (types.Runtime, error) {
	agentID, err := uuid.Parse(params.AgentID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("parse agent id: %w", err)
	}
	if _, err := s.svc.Store.GetAgent(ctx, agentID); err != nil {
		return types.Runtime{}, fmt.Errorf("agent %s not found: %w", params.AgentID, err)
	}

	runtime, err := s.svc.Store.CreateRuntime(ctx, params)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("create runtime: %w", err)
	}

	// After registering the runtime, set the agent status to online
	if _, err := s.svc.Store.UpdateAgentStatus(ctx, types.UpdateAgentStatusParams{
		ID:     agentID.String(),
		Status: types.AgentStatusOnline,
	}); err != nil {
		_ = err
	}

	s.svc.publishToAgent(ctx, agentID, "sync:required", map[string]interface{}{
		"runtime_id": runtime.ID,
		"reason":     "new_registration",
	})

	return runtime, nil
}

// Heartbeat updates the heartbeat time of a runtime, maintaining its online state.
func (s *RuntimeService) Heartbeat(ctx context.Context, id uuid.UUID) (types.Runtime, error) {
	return s.svc.Store.UpdateRuntimeHeartbeat(ctx, id)
}

// List lists all runtimes.
func (s *RuntimeService) List(ctx context.Context) ([]types.Runtime, error) {
	return s.svc.Store.ListRuntimes(ctx)
}

// ListByWorkspace lists the runtimes of all agents in a specified workspace, using a JOIN query to avoid N+1 issues.
func (s *RuntimeService) ListByWorkspace(ctx context.Context, workspaceID uuid.UUID) ([]types.Runtime, error) {
	return s.svc.Store.ListRuntimesByWorkspace(ctx, workspaceID)
}

// GetRuntimeByID retrieves runtime information by ID.
func (s *RuntimeService) GetRuntimeByID(ctx context.Context, runtimeID uuid.UUID) (types.Runtime, error) {
	runtime, err := s.svc.Store.GetRuntimeByID(ctx, runtimeID)
	if err != nil {
		return types.Runtime{}, fmt.Errorf("get runtime by id: %w", err)
	}
	return runtime, nil
}

// Sync performs a full state sync for the specified runtime, returning all pending nodes, active tasks, and recent mentions.
func (s *RuntimeService) Sync(ctx context.Context, runtimeID uuid.UUID) (*store.SyncResult, error) {
	return s.svc.Store.SyncRuntime(ctx, runtimeID)
}
