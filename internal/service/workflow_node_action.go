// workflow_node_action.go implements the scheduler-driven executor of ready
// action.github nodes (S5): the scan claims one node at a time (ready →
// running with an optimistic version check), resolves the config expressions
// against the run context, calls the GitHub API with the project's github
// integration PAT and closes the attempt through the engine's
// CompleteNode/FailNode paths (retry budget and backoff included).
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/crypto"
	"github.com/teammate/server/internal/integration/github"
	"github.com/teammate/server/internal/store"
	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

// actionNodeScanLimit bounds one scan of ready action.github nodes.
const actionNodeScanLimit int32 = 100

// Integration lookup failures caused by a bad node config (as opposed to
// missing data or an execution error): the caller maps them to
// "invalid_config" so the template author can fix the node.
var (
	errBadIntegrationID = errors.New("invalid integration_id")
	errWrongProvider    = errors.New("integration is not a github provider")
)

// WorkflowNodeActionService executes ready GitHub action nodes.
type WorkflowNodeActionService struct {
	svc *Service
}

func NewWorkflowNodeActionService(svc *Service) *WorkflowNodeActionService {
	return &WorkflowNodeActionService{svc: svc}
}

// ProcessGitHubActionNodes claims and executes ready action.github nodes; it
// is the scheduler's entry point (task "workflow_node_actions").
func (s *WorkflowNodeActionService) ProcessGitHubActionNodes(ctx context.Context) error {
	nodes, err := s.svc.Store.ListReadyGitHubActionNodes(ctx, actionNodeScanLimit)
	if err != nil {
		return fmt.Errorf("list ready github action nodes: %w", err)
	}
	for i := range nodes {
		nodeID, err := uuid.Parse(nodes[i].ID)
		if err != nil {
			continue
		}
		claimed, node, attemptNo, err := s.claimGitHubActionNode(ctx, nodeID)
		if err != nil {
			slog.Error("claim github action node", "node_id", nodes[i].ID, "err", err)
			continue
		}
		if !claimed {
			continue
		}
		s.executeGitHubAction(ctx, node, attemptNo)
	}
	return nil
}

// claimGitHubActionNode moves one ready action.github node to running and
// opens its running attempt inside one transaction. A node already claimed by
// another instance misses the version check and reports claimed=false.
func (s *WorkflowNodeActionService) claimGitHubActionNode(ctx context.Context, nodeID uuid.UUID) (bool, types.TaskNode, int, error) {
	var (
		node      types.TaskNode
		attemptNo int
	)
	err := s.svc.Store.RunTx(ctx, func(tx *store.Tx) error {
		loaded, err := tx.GetTaskNode(ctx, nodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if loaded.Status != types.TaskNodeStatusReady || loaded.TypeKey != "action.github" {
			return nil
		}
		loaded.Status = types.TaskNodeStatusRunning
		loaded.AttemptCount++
		if err := NewWorkflowRunService(s.svc).writeNodeRun(ctx, tx, &loaded); err != nil {
			if errors.Is(err, store.ErrNodeVersionConflict) {
				return nil
			}
			return err
		}
		attempt, err := tx.CreateTaskAttempt(ctx, types.CreateTaskAttemptParams{
			TaskID:       loaded.TaskID,
			TaskNodeID:   loaded.ID,
			AttemptNo:    loaded.AttemptCount,
			ExecutorKind: "server",
			Status:       "running",
			Input:        ensureJSON(loaded.Input),
			Output:       json.RawMessage("{}"),
			Error:        json.RawMessage("{}"),
		})
		if err != nil {
			return err
		}
		node = loaded
		attemptNo = int(attempt.AttemptNo)
		return nil
	})
	if err != nil {
		return false, types.TaskNode{}, 0, err
	}
	return attemptNo > 0, node, attemptNo, nil
}

// executeGitHubAction runs one claimed node: resolve config → find the
// project's github integration → call the operation → complete or fail the
// node through the engine callbacks.
func (s *WorkflowNodeActionService) executeGitHubAction(ctx context.Context, node types.TaskNode, attemptNo int) {
	nodeID, err := uuid.Parse(node.ID)
	if err != nil {
		return
	}
	fail := func(code, format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		slog.Error("github action node failed", "node_id", node.ID, "code", code, "message", message)
		if err := NewWorkflowRunService(s.svc).FailNode(ctx, nodeID, int32(attemptNo), NodeFailure{Code: code, Message: message}); err != nil {
			slog.Error("fail github action node", "node_id", node.ID, "err", err)
		}
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		fail("node_execution_failed", "load task: %v", err)
		return
	}
	ctxRoot := decodeRunContext(task.Context)
	if taskRoot, ok := ctxRoot["task"].(map[string]any); ok {
		taskRoot["id"] = node.TaskID
	}
	resolve := func(path string) (any, bool) { return workflow.ResolvePath(ctxRoot, path) }

	cfg, err := parseGitHubActionConfig(node.Config, resolve)
	if err != nil {
		fail("invalid_config", "%v", err)
		return
	}

	projectID, err := uuid.Parse(task.ProjectID)
	if err != nil {
		fail("node_execution_failed", "parse project id: %v", err)
		return
	}
	integration, err := s.loadGitHubIntegration(ctx, projectID, cfg.IntegrationID)
	if err != nil {
		switch {
		case errors.Is(err, errBadIntegrationID), errors.Is(err, errWrongProvider):
			fail("invalid_config", "%v", err)
		case errors.Is(err, sql.ErrNoRows):
			fail("integration_missing", "github integration not found for this project")
		default:
			fail("node_execution_failed", "load integration: %v", err)
		}
		return
	}
	pat, err := crypto.DecryptString(integration.EncryptedSecret)
	if err != nil {
		fail("integration_error", "decrypt integration PAT: %v", err)
		return
	}

	client := github.NewClient(s.svc.GitHubAPIURL, pat)
	output, err := runGitHubOperation(ctx, client, integration, cfg)
	if err != nil {
		fail(githubActionErrorCode(err), "%v", err)
		return
	}
	if err := NewWorkflowRunService(s.svc).CompleteNode(ctx, nodeID, int32(attemptNo), NodeResult{Output: output}); err != nil {
		slog.Error("complete github action node", "node_id", node.ID, "err", err)
	}
}

// githubActionConfig is the expression-resolved shape of an action.github
// node config.
type githubActionConfig struct {
	Operation     string
	IntegrationID string
	Issue         int64
	PR        int64
	Title     string
	Head      string
	Base      string
	Body      string
	Labels    []string
	Assignees []string
	Reviewers []string
}

// loadGitHubIntegration resolves the integration an action.github node uses:
// an explicit config integration_id wins (must belong to the node's project
// and be a github integration), otherwise the project's first github
// integration.
func (s *WorkflowNodeActionService) loadGitHubIntegration(ctx context.Context, projectID uuid.UUID, integrationID string) (types.Integration, error) {
	if integrationID == "" {
		return s.svc.Store.GetProjectIntegration(ctx, projectID, "github")
	}
	id, err := uuid.Parse(integrationID)
	if err != nil {
		return types.Integration{}, fmt.Errorf("integration_id %q: %w", integrationID, errBadIntegrationID)
	}
	integration, err := s.svc.Store.GetIntegration(ctx, id)
	if err != nil {
		return types.Integration{}, err
	}
	if integration.Provider != "github" {
		return types.Integration{}, fmt.Errorf("integration %s: %w", id, errWrongProvider)
	}
	if integration.ProjectID != projectID.String() {
		return types.Integration{}, sql.ErrNoRows
	}
	return integration, nil
}

// parseGitHubActionConfig decodes the node config and resolves every string
// field against the run context (plan §4.2 text templates).
func parseGitHubActionConfig(config json.RawMessage, resolve func(string) (any, bool)) (githubActionConfig, error) {
	var raw struct {
		Operation     string   `json:"operation"`
		IntegrationID string   `json:"integration_id"`
		Issue         string   `json:"issue"`
		PR        string   `json:"pr"`
		Title     string   `json:"title"`
		Head      string   `json:"head"`
		Base      string   `json:"base"`
		Body      string   `json:"body"`
		Labels    []string `json:"labels"`
		Assignees []string `json:"assignees"`
		Reviewers []string `json:"reviewers"`
	}
	if err := json.Unmarshal(config, &raw); err != nil {
		return githubActionConfig{}, fmt.Errorf("decode action.github config: %w", err)
	}
	cfg := githubActionConfig{Operation: raw.Operation, IntegrationID: strings.TrimSpace(raw.IntegrationID)}

	if raw.Issue != "" {
		number, err := resolveNodeNumber(raw.Issue, resolve)
		if err != nil {
			return cfg, fmt.Errorf("issue: %w", err)
		}
		cfg.Issue = number
	}
	if raw.PR != "" {
		number, err := resolveNodeNumber(raw.PR, resolve)
		if err != nil {
			return cfg, fmt.Errorf("pr: %w", err)
		}
		cfg.PR = number
	}
	cfg.Title = resolveNodeString(raw.Title, resolve)
	cfg.Head = resolveNodeString(raw.Head, resolve)
	cfg.Base = resolveNodeString(raw.Base, resolve)
	cfg.Body = resolveNodeString(raw.Body, resolve)
	cfg.Labels = resolveNodeStrings(raw.Labels, resolve)
	cfg.Assignees = resolveNodeStrings(raw.Assignees, resolve)
	cfg.Reviewers = resolveNodeStrings(raw.Reviewers, resolve)
	return cfg, nil
}

// resolveNodeNumber resolves one config value to an issue/PR number: a pure
// placeholder keeps the JSON type (number), mixed text comes back as a string
// parsed with strconv.
func resolveNodeNumber(text string, resolve func(string) (any, bool)) (int64, error) {
	value := workflow.RenderText(text, resolve)
	switch typed := value.(type) {
	case float64:
		return int64(typed), nil
	case string:
		number, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return 0, fmt.Errorf("%q is not an issue number", typed)
		}
		return number, nil
	default:
		return 0, fmt.Errorf("value is not an issue number")
	}
}

// resolveNodeString renders one config string to its final text form.
func resolveNodeString(text string, resolve func(string) (any, bool)) string {
	return stringValueOf(workflow.RenderText(text, resolve))
}

// resolveNodeStrings renders each element of a config string array.
func resolveNodeStrings(items []string, resolve func(string) (any, bool)) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, resolveNodeString(item, resolve))
	}
	return out
}

// stringValueOf renders a resolved value as text (plan §4.2): strings
// verbatim, numbers in literal form, everything else JSON-encoded.
func stringValueOf(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprintf("%v", value)
		}
		return string(encoded)
	}
}

// runGitHubOperation dispatches one operation against the GitHub API and
// returns the node output (plan §3.4 first-version operation set).
func runGitHubOperation(ctx context.Context, client *github.Client, integration types.Integration, cfg githubActionConfig) (json.RawMessage, error) {
	cfgRepo := decodeGithubConfig(integration.Config)
	owner, repo := cfgRepo.RepoOwner, cfgRepo.RepoName

	switch cfg.Operation {
	case "add_comment":
		comment, err := client.CreateIssueComment(ctx, owner, repo, cfg.Issue, cfg.Body)
		if err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"comment_url": comment.HTMLURL})
	case "add_labels":
		if err := client.AddLabels(ctx, owner, repo, cfg.Issue, cfg.Labels); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	case "remove_labels":
		if err := client.RemoveLabels(ctx, owner, repo, cfg.Issue, cfg.Labels); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	case "assign_issue":
		if err := client.AddAssignees(ctx, owner, repo, cfg.Issue, cfg.Assignees); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	case "unassign_issue":
		if err := client.RemoveAssignees(ctx, owner, repo, cfg.Issue, cfg.Assignees); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	case "close_issue":
		if err := client.CloseIssue(ctx, owner, repo, cfg.Issue); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	case "reopen_issue":
		if err := client.ReopenIssue(ctx, owner, repo, cfg.Issue); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	case "create_pull_request":
		pr, err := client.CreatePullRequest(ctx, owner, repo, github.CreatePullRequestParams{
			Title: cfg.Title,
			Head:  cfg.Head,
			Base:  cfg.Base,
			Body:  cfg.Body,
		})
		if err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{
			"pr_number": pr.Number,
			"pr_url":    pr.HTMLURL,
			"html_url":  pr.HTMLURL,
		})
	case "request_reviewers":
		if err := client.RequestReviewers(ctx, owner, repo, cfg.PR, cfg.Reviewers); err != nil {
			return nil, err
		}
		return marshalNodeOutput(map[string]any{"ok": true})
	default:
		return nil, fmt.Errorf("unsupported operation: %s", cfg.Operation)
	}
}

// githubActionErrorCode maps an executor error to the node failure code.
func githubActionErrorCode(err error) string {
	var apiErr *github.APIError
	if errors.As(err, &apiErr) && apiErr.Retryable {
		return "github_api_retryable"
	}
	return "github_api_error"
}

// marshalNodeOutput encodes a node output object.
func marshalNodeOutput(value map[string]any) (json.RawMessage, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal node output: %w", err)
	}
	return encoded, nil
}

// ReclaimLeasedNodes fails server-executed nodes whose claim lease expired
// (the executor process died before reaching a terminal state, P1-1b of the
// S5 review): the FailNode path applies the node's retry policy, so the graph
// never stays stuck on a dead claim.
func (s *WorkflowNodeActionService) ReclaimLeasedNodes(ctx context.Context) error {
	nodes, err := s.svc.Store.ListLeaseExpiredActionNodes(ctx, 50)
	if err != nil {
		return err
	}
	runSvc := NewWorkflowRunService(s.svc)
	for i := range nodes {
		n := &nodes[i]
		attemptNo, has, err := s.svc.Store.GetRunningAttemptNoByNode(ctx, n.ID)
		if err != nil {
			return err
		}
		if !has {
			continue
		}
		nodeID, parseErr := uuid.Parse(n.ID)
		if parseErr != nil {
			return fmt.Errorf("parse node id: %w", parseErr)
		}
		// A node completed by its worker between the scan and this call misses
		// the running-state check; skip it and keep reclaiming the rest.
		if err := runSvc.FailNode(ctx, nodeID, attemptNo, NodeFailure{
			Code:    "node_execution_timeout",
			Message: "server executor lease expired",
		}); err != nil {
			if errors.Is(err, ErrNodeNotRunning) {
				continue
			}
			return err
		}
	}
	return nil
}
