// daemon.go — business logic for agentd instance (daemon) management:
// lifecycle from the web (create/list/delete/rotate-token) and resolution of
// effective-agent access for daemon-authenticated requests (X-Agent-ID).
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
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
// (the agent must have been reported by this daemon within the same workspace)
// and returns the agent's identity used to rewrite auth claims for the request.
func (s *DaemonService) ResolveEffectiveAgent(ctx context.Context, daemonID, agentID uuid.UUID) (types.Agent, error) {
	daemon, err := s.svc.Store.GetDaemon(ctx, daemonID)
	if err != nil {
		return types.Agent{}, fmt.Errorf("daemon not found: %w", err)
	}
	agent, err := s.svc.Store.GetAgent(ctx, agentID)
	if err != nil {
		return types.Agent{}, fmt.Errorf("agent not found: %w", err)
	}
	if agent.DaemonID != daemonID.String() {
		return types.Agent{}, errors.New("agent is not reported by this daemon")
	}
	if agent.WorkspaceID != daemon.WorkspaceID.String() {
		return types.Agent{}, errors.New("agent does not belong to the daemon's workspace")
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

// ReportedAgent is one entry of the desired-agents delivery: the declarative
// full set of the daemon's instances (agentd reconciles its local catalog
// against this set — add changed, remove missing).
type ReportedAgent struct {
	AgentID    string `json:"agent_id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	PersonaKey string `json:"persona_key"`
}

// RegisterReport is the machine-side registration payload (machine info and
// the detected-tools snapshot only; instances are web-created and delivered
// via desired_agents).
type RegisterReport struct {
	DeviceName string          `json:"device_name"`
	Version    string          `json:"version"`
	PublicKey  string          `json:"public_key"`
	Providers  json.RawMessage `json:"providers"`
}

// DaemonRegisterResult is the register/heartbeat response payload. DaemonID
// and WorkspaceID are filled by Register only; the daemon uses them to build
// its event-stream (SSE) URL and to scope all workspace-level calls — the
// daemon token binds the daemon to exactly one workspace, so the server's
// value is authoritative over any locally configured one.
type DaemonRegisterResult struct {
	DaemonID           string          `json:"daemon_id,omitempty"`
	WorkspaceID        string          `json:"workspace_id,omitempty"`
	DesiredAgents      []ReportedAgent `json:"desired_agents"`
	HeartbeatIntervalS int             `json:"heartbeat_interval"`
}

// Register applies a machine-side registration: refreshes daemon info and the
// tool-detection snapshot, and returns the desired set (web-created instances
// still waiting for delivery). Reports never create or modify agent rows.
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

	desired, err := s.DesiredAgents(ctx, daemon.ID)
	if err != nil {
		return DaemonRegisterResult{}, err
	}
	return DaemonRegisterResult{
		DaemonID:           daemon.ID.String(),
		WorkspaceID:        daemon.WorkspaceID.String(),
		DesiredAgents:      desired,
		HeartbeatIntervalS: 30,
	}, nil
}

// DesiredAgents returns the daemon's full instance set — the declarative
// expectation agentd reconciles against (add new, overwrite changed
// agent_id/persona_key, remove deleted).
func (s *DaemonService) DesiredAgents(ctx context.Context, daemonID uuid.UUID) ([]ReportedAgent, error) {
	all, err := s.svc.Store.ListAgentsByDaemon(ctx, daemonID)
	if err != nil {
		return nil, err
	}
	out := make([]ReportedAgent, 0, len(all))
	for _, a := range all {
		out = append(out, ReportedAgent{AgentID: a.ID, Name: a.Name, Provider: a.Provider, PersonaKey: a.PersonaKey})
	}
	return out, nil
}

// HeartbeatAgentStatus is one entry of the optional heartbeat agent payload.
type HeartbeatAgentStatus struct {
	Name string `json:"name"`
	Busy bool   `json:"busy"`
}

// Heartbeat refreshes the daemon liveness, applies the per-agent busy status,
// offlines online instances absent from the report, and returns the desired set.
func (s *DaemonService) Heartbeat(ctx context.Context, daemonID uuid.UUID, agents []HeartbeatAgentStatus) (DaemonRegisterResult, error) {
	ok, err := s.svc.Store.TouchDaemonHeartbeat(ctx, daemonID)
	if err != nil {
		return DaemonRegisterResult{}, err
	}
	if !ok {
		return DaemonRegisterResult{}, errors.New("daemon not found")
	}
	names := make([]string, 0, len(agents))
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
		names = append(names, a.Name)
	}
	if err := s.svc.Store.OfflineUnreportedDaemonAgents(ctx, daemonID, names); err != nil {
		return DaemonRegisterResult{}, err
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

var personaKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,63}$`)

// NormalizePersonaKey validates and normalizes an operator-provided memory
// identity: trimmed and lowercased, it must match the persona charset, or be
// empty (the caller generates a unique value then).
func NormalizePersonaKey(personaKey string) (string, error) {
	key := strings.ToLower(strings.TrimSpace(personaKey))
	if key == "" {
		return "", nil
	}
	if !personaKeyPattern.MatchString(key) {
		return "", fmt.Errorf("invalid persona_key %q: 2-64 chars from [a-z0-9._-], starting with a letter or digit", personaKey)
	}
	return key, nil
}

// GeneratePersonaKey returns a unique, unguessable memory identity for
// instances created without an explicit one (default strict isolation).
func GeneratePersonaKey() string {
	return "persona-" + uuid.NewString()[:8]
}

// CreateAgent inserts a web-created instance under the daemon and grants it
// the default permission set plus the developer role (creation is
// authorization; grant failures do not abort the creation, matching
// AgentService.Create). The provider must be present in the daemon's
// detected-tools snapshot; the instance stays pending until delivered via
// desired_agents and reported online by a heartbeat. personaKey is the
// cross-workspace memory identity: normalized when provided, generated when
// blank, immutable after creation. gitName/gitEmail are the operator-provided
// commit identity (required); instructions is the optional identity prompt.
func (s *DaemonService) CreateAgent(ctx context.Context, workspaceID, daemonID uuid.UUID, name, provider, instructions, personaKey, gitName, gitEmail string, grantedBy uuid.UUID) (types.Agent, error) {
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
	gitName = strings.TrimSpace(gitName)
	gitEmail = strings.TrimSpace(gitEmail)
	if gitName == "" || gitEmail == "" {
		return types.Agent{}, errors.New("git_name and git_email are required")
	}
	normalizedPersonaKey, err := NormalizePersonaKey(personaKey)
	if err != nil {
		return types.Agent{}, err
	}
	personaKey = normalizedPersonaKey
	if personaKey == "" {
		personaKey = GeneratePersonaKey()
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

	agent, err := s.svc.Store.CreateDaemonAgent(ctx, workspaceID, daemonID, name, provider, instructions, personaKey, gitName, gitEmail)
	if err != nil {
		return types.Agent{}, err
	}

	agentUUID, err := uuid.Parse(agent.ID)
	if err != nil {
		return types.Agent{}, fmt.Errorf("parse agent id: %w", err)
	}
	if err := s.svc.Store.GrantDefaultPermissions(ctx, agentUUID, grantedBy); err != nil {
		_ = err
	}
	permSvc := NewAgentPermissionService(s.svc)
	if err := permSvc.GrantRolePermissions(ctx, agentUUID, "developer", grantedBy); err != nil {
		_ = err
	}

	agent.DaemonName = daemon.Name
	return agent, nil
}
