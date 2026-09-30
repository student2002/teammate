// daemon.go — business logic for agentd instance (daemon) management:
// lifecycle from the web (create/list/delete/rotate-token) and resolution of
// effective-agent access for daemon-authenticated requests (X-Agent-ID).
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
)

// DaemonService provides daemon lifecycle business logic.
type DaemonService struct {
	svc *Service
}

func NewDaemonService(svc *Service) *DaemonService {
	return &DaemonService{svc: svc}
}

// CreateDaemonResult carries the created daemon plus the one-time token plaintext.
type CreateDaemonResult struct {
	Daemon types.Daemon
	Token  string
}

// Create inserts a daemon under the workspace and issues its initial td_ token.
func (s *DaemonService) Create(ctx context.Context, workspaceID uuid.UUID, name string) (CreateDaemonResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return CreateDaemonResult{}, fmt.Errorf("daemon name is required")
	}
	daemon, token, err := s.svc.Store.CreateDaemon(ctx, workspaceID, name)
	if err != nil {
		return CreateDaemonResult{}, fmt.Errorf("create daemon: %w", err)
	}
	return CreateDaemonResult{Daemon: daemon, Token: token}, nil
}

// List returns all daemons of the workspace.
func (s *DaemonService) List(ctx context.Context, workspaceID uuid.UUID) ([]types.Daemon, error) {
	daemons, err := s.svc.Store.ListDaemonsByWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list daemons: %w", err)
	}
	return daemons, nil
}

// Delete removes a daemon; its reported agents go offline but keep their configuration.
func (s *DaemonService) Delete(ctx context.Context, workspaceID, daemonID uuid.UUID) error {
	daemon, err := s.svc.Store.GetDaemon(ctx, daemonID)
	if err != nil {
		return fmt.Errorf("get daemon: %w", err)
	}
	if daemon.WorkspaceID != workspaceID {
		return fmt.Errorf("daemon does not belong to this workspace")
	}
	if err := s.svc.Store.DeleteDaemon(ctx, daemonID); err != nil {
		return fmt.Errorf("delete daemon: %w", err)
	}
	return nil
}

// RotateToken revokes the daemon's current token and issues a new one.
func (s *DaemonService) RotateToken(ctx context.Context, workspaceID, daemonID uuid.UUID) (string, error) {
	daemon, err := s.svc.Store.GetDaemon(ctx, daemonID)
	if err != nil {
		return "", fmt.Errorf("get daemon: %w", err)
	}
	if daemon.WorkspaceID != workspaceID {
		return "", fmt.Errorf("daemon does not belong to this workspace")
	}
	token, err := s.svc.Store.RotateDaemonToken(ctx, daemonID)
	if err != nil {
		return "", fmt.Errorf("rotate daemon token: %w", err)
	}
	return token, nil
}

// ResolveEffectiveAgent verifies that the daemon may act on behalf of the agent
// (the agent must have been reported by this daemon) and returns the agent's
// identity used to rewrite auth claims for the request.
func (s *DaemonService) ResolveEffectiveAgent(ctx context.Context, daemonID, agentID uuid.UUID) (types.Agent, error) {
	agent, err := s.svc.Store.GetAgent(ctx, agentID)
	if err != nil {
		return types.Agent{}, fmt.Errorf("agent not found: %w", err)
	}
	if agent.DaemonID != daemonID.String() {
		return types.Agent{}, errors.New("agent is not reported by this daemon")
	}
	return agent, nil
}

// Get returns a daemon by ID.
func (s *DaemonService) Get(ctx context.Context, daemonID uuid.UUID) (types.Daemon, error) {
	return s.svc.Store.GetDaemon(ctx, daemonID)
}

// ListAgents returns all agent instances reported by the daemon.
func (s *DaemonService) ListAgents(ctx context.Context, daemonID uuid.UUID) ([]types.Agent, error) {
	return s.svc.Store.ListAgentsByDaemon(ctx, daemonID)
}

// ReportedAgent is one entry of a daemon's agent report.
type ReportedAgent struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`
}

// RegisterReport is the machine-side registration payload.
type RegisterReport struct {
	DeviceName string          `json:"device_name"`
	Version    string          `json:"version"`
	PublicKey  string          `json:"public_key"`
	Providers  json.RawMessage `json:"providers"`
	Agents     []ReportedAgent `json:"agents"`
}

// RegisteredAgent pairs a reported name with its resolved server identity.
type RegisteredAgent struct {
	Name    string `json:"name"`
	AgentID string `json:"agent_id"`
	Status  string `json:"status"`
}

// DaemonRegisterResult is the register/heartbeat response payload.
type DaemonRegisterResult struct {
	Agents             []RegisteredAgent `json:"agents"`
	DesiredAgents      []ReportedAgent   `json:"desired_agents"`
	HeartbeatIntervalS int               `json:"heartbeat_interval"`
}

// Register applies a machine-side registration: refreshes daemon info and the
// tool-detection snapshot, upserts reported agents (adopting pending web-pushed
// instances with matching names), offlines machine agents absent from the report,
// and returns the desired set (web-pushed instances still waiting for adoption).
func (s *DaemonService) Register(ctx context.Context, daemonID uuid.UUID, report RegisterReport) (DaemonRegisterResult, error) {
	daemon, err := s.svc.Store.GetDaemon(ctx, daemonID)
	if err != nil {
		return DaemonRegisterResult{}, fmt.Errorf("daemon not found: %w", err)
	}
	if len(report.Providers) == 0 {
		report.Providers = json.RawMessage("[]")
	}

	if _, err := s.svc.Store.UpdateDaemonRegistration(ctx, daemon.ID, types.DaemonRegistrationUpdate{
		DeviceName: report.DeviceName,
		Version:    report.Version,
		Providers:  report.Providers,
		PublicKey:  report.PublicKey,
	}); err != nil {
		return DaemonRegisterResult{}, err
	}

	registered := make([]RegisteredAgent, 0, len(report.Agents))
	names := make([]string, 0, len(report.Agents))
	for _, a := range report.Agents {
		a.Name = strings.TrimSpace(a.Name)
		if a.Name == "" {
			continue
		}
		agent, err := s.svc.Store.UpsertDaemonAgent(ctx, daemon.WorkspaceID, daemon.ID, a.Name, a.Provider)
		if err != nil {
			return DaemonRegisterResult{}, err
		}
		registered = append(registered, RegisteredAgent{Name: a.Name, AgentID: agent.ID, Status: agent.Status})
		names = append(names, a.Name)
	}

	if err := s.svc.Store.OfflineUnreportedDaemonAgents(ctx, daemon.ID, names); err != nil {
		return DaemonRegisterResult{}, err
	}

	desired, err := s.DesiredAgents(ctx, daemon.ID)
	if err != nil {
		return DaemonRegisterResult{}, err
	}
	return DaemonRegisterResult{
		Agents:             registered,
		DesiredAgents:      desired,
		HeartbeatIntervalS: 30,
	}, nil
}

// DesiredAgents returns the web-pushed instances waiting to be adopted
// (agentd materializes them into its local config and reports them back).
func (s *DaemonService) DesiredAgents(ctx context.Context, daemonID uuid.UUID) ([]ReportedAgent, error) {
	pending, err := s.svc.Store.ListPendingDaemonAgents(ctx, daemonID)
	if err != nil {
		return nil, err
	}
	out := make([]ReportedAgent, 0, len(pending))
	for _, a := range pending {
		out = append(out, ReportedAgent{Name: a.Name, Provider: a.Provider})
	}
	return out, nil
}

// HeartbeatAgentStatus is one entry of the optional heartbeat agent payload.
type HeartbeatAgentStatus struct {
	Name string `json:"name"`
	Busy bool   `json:"busy"`
}

// Heartbeat refreshes the daemon liveness, applies optional per-agent busy
// status, and returns the desired set.
func (s *DaemonService) Heartbeat(ctx context.Context, daemonID uuid.UUID, agents []HeartbeatAgentStatus) (DaemonRegisterResult, error) {
	ok, err := s.svc.Store.TouchDaemonHeartbeat(ctx, daemonID)
	if err != nil {
		return DaemonRegisterResult{}, err
	}
	if !ok {
		return DaemonRegisterResult{}, errors.New("daemon not found")
	}
	for _, a := range agents {
		a.Name = strings.TrimSpace(a.Name)
		if a.Name == "" {
			continue
		}
		status := types.AgentStatusOnline
		if a.Busy {
			status = types.AgentStatusBusy
		}
		if err := s.svc.Store.SetDaemonAgentStatusByName(ctx, daemonID, a.Name, status); err != nil {
			return DaemonRegisterResult{}, err
		}
	}
	desired, err := s.DesiredAgents(ctx, daemonID)
	if err != nil {
		return DaemonRegisterResult{}, err
	}
	return DaemonRegisterResult{DesiredAgents: desired, HeartbeatIntervalS: 30}, nil
}

// Deregister marks the daemon and its reported agents offline (graceful shutdown).
func (s *DaemonService) Deregister(ctx context.Context, daemonID uuid.UUID) error {
	if err := s.svc.Store.SetAgentsStatusByDaemon(ctx, daemonID, types.AgentStatusOffline); err != nil {
		return err
	}
	return s.svc.Store.SetDaemonOffline(ctx, daemonID)
}

// DaemonCreateAgentResult carries a web-pushed instance plus its server identity.
type DaemonCreateAgentResult struct {
	Agent types.Agent
}

// CreateAgent inserts a web-pushed instance on the daemon's tool node.
// The provider must be present in the daemon's detected-tools snapshot;
// the instance stays pending until the daemon adopts it via registration.
func (s *DaemonService) CreateAgent(ctx context.Context, workspaceID, daemonID uuid.UUID, name, provider string) (types.Agent, error) {
	daemon, err := s.svc.Store.GetDaemon(ctx, daemonID)
	if err != nil {
		return types.Agent{}, fmt.Errorf("daemon not found: %w", err)
	}
	if daemon.WorkspaceID != workspaceID {
		return types.Agent{}, errors.New("daemon does not belong to this workspace")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return types.Agent{}, errors.New("agent name is required")
	}

	var providers []struct {
		Provider  string `json:"provider"`
		Installed bool   `json:"installed"`
	}
	if err := json.Unmarshal(daemon.Providers, &providers); err != nil {
		providers = nil
	}
	detected := false
	for _, p := range providers {
		if p.Provider == provider && p.Installed {
			detected = true
			break
		}
	}
	if !detected {
		return types.Agent{}, fmt.Errorf("provider %q is not detected on daemon %s", provider, daemon.Name)
	}

	agent, err := s.svc.Store.CreateDaemonAgent(ctx, workspaceID, daemonID, name, provider)
	if err != nil {
		return types.Agent{}, err
	}
	agent.DaemonName = daemon.Name
	return agent, nil
}
