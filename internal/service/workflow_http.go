// workflow_http.go executes ready action.http nodes (plan §11): the config
// expressions (method/url/headers/query/body) are resolved against the run
// context, the request goes out with redirects refused and a 1 MiB response
// cap, and the node closes through the engine callbacks — 2xx completes on
// the success port, any other status or transport failure fails the node
// (retry budget applies).
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/types"
	"github.com/teammate/server/internal/workflow"
)

// maxHTTPNodeBodyBytes caps the response body a node reads (plan §11).
const maxHTTPNodeBodyBytes = 1 << 20

// httpNodeDefaultTimeout applies when the node config carries no
// timeout_minutes.
const httpNodeDefaultTimeout = 30 * time.Second

// errRedirectNotAllowed is returned from the client's CheckRedirect so a
// response redirect is refused instead of followed (plan §11).
var errRedirectNotAllowed = errors.New("http node does not follow redirects")

// errSchemeDisallowed marks a plain http:// URL on a deployment that has not
// opted in via TEAMS_HTTP_NODE_ALLOW_HTTP (plan §11).
var errSchemeDisallowed = errors.New("http scheme is not allowed on this deployment")

// httpNodeHeaderWhitelist is the only response headers copied into the node
// output (plan §11).
var httpNodeHeaderWhitelist = []string{"Content-Type", "Content-Length", "X-Request-Id", "Location"}

// httpNodeConfig is the expression-resolved shape of an action.http config.
type httpNodeConfig struct {
	Method         string
	URL            string
	Headers        map[string]string
	Query          map[string]string
	Body           any
	TimeoutMinutes float64
}

// executeHTTPNode runs one claimed action.http node.
func (s *WorkflowAsyncNodeService) executeHTTPNode(ctx context.Context, node types.TaskNode, attemptNo int) {
	nodeID, err := uuid.Parse(node.ID)
	if err != nil {
		return
	}
	runSvc := NewWorkflowRunService(s.svc)
	fail := func(code, format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		slog.Error("http action node failed", "node_id", node.ID, "code", code, "message", message)
		if err := runSvc.FailNode(ctx, nodeID, int32(attemptNo), NodeFailure{Code: code, Message: message}); err != nil {
			slog.Error("fail http action node", "node_id", node.ID, "err", err)
		}
	}

	task, err := s.svc.Store.GetTask(ctx, node.TaskID)
	if err != nil {
		fail("node_execution_failed", "load task: %v", err)
		return
	}
	resolve := resolveTaskContextFor(task, node.TaskID)

	cfg, err := parseHTTPNodeConfig(node.Config, resolve)
	if err != nil {
		fail("invalid_config", "%v", err)
		return
	}

	output, err := s.runHTTPRequest(ctx, cfg)
	if err != nil {
		switch {
		case errors.Is(err, errRedirectNotAllowed):
			fail("http_redirect_disallowed", "%v", err)
		case errors.Is(err, errSchemeDisallowed):
			fail("http_scheme_disallowed", "%v", err)
		default:
			// Transport errors carry the full URL; the query string may embed
			// credentials, so it is stripped before the error lands in the
			// node error / logs.
			fail("http_request_failed", "%s", sanitizeHTTPError(err))
		}
		return
	}
	if !is2xxStatus(output.Status) {
		fail(fmt.Sprintf("http_status_%d", output.Status), "unexpected status %d", output.Status)
		return
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		fail("node_execution_failed", "marshal output: %v", err)
		return
	}
	if err := runSvc.CompleteNode(ctx, nodeID, int32(attemptNo), NodeResult{Output: encoded}); err != nil {
		slog.Error("complete http action node", "node_id", node.ID, "err", err)
	}
}

// parseHTTPNodeConfig decodes the node config and resolves every string field
// against the run context (plan §4.2).
func parseHTTPNodeConfig(config json.RawMessage, resolve func(string) (any, bool)) (httpNodeConfig, error) {
	var raw struct {
		Method         string            `json:"method"`
		URL            string            `json:"url"`
		Headers        map[string]string `json:"headers"`
		Query          map[string]string `json:"query"`
		Body           json.RawMessage   `json:"body"`
		TimeoutMinutes float64           `json:"timeout_minutes"`
	}
	if err := json.Unmarshal(config, &raw); err != nil {
		return httpNodeConfig{}, fmt.Errorf("decode action.http config: %w", err)
	}
	cfg := httpNodeConfig{
		Method:         raw.Method,
		TimeoutMinutes: raw.TimeoutMinutes,
		Headers:        map[string]string{},
		Query:          map[string]string{},
	}
	cfg.URL = resolveNodeString(raw.URL, resolve)
	if cfg.URL == "" {
		return cfg, fmt.Errorf("action.http url resolved to an empty value")
	}
	for key, value := range raw.Headers {
		cfg.Headers[key] = resolveNodeString(value, resolve)
	}
	for key, value := range raw.Query {
		cfg.Query[key] = resolveNodeString(value, resolve)
	}
	if len(raw.Body) > 0 && string(raw.Body) != "null" {
		var decoded any
		if err := json.Unmarshal(raw.Body, &decoded); err != nil {
			return cfg, fmt.Errorf("decode action.http body: %w", err)
		}
		cfg.Body = renderBodyValue(decoded, resolve)
	}
	return cfg, nil
}

// renderBodyValue evaluates a body value against the context: strings go
// through text template rendering, objects and arrays recursively, every
// other JSON value passes through unchanged.
func renderBodyValue(value any, resolve func(string) (any, bool)) any {
	switch typed := value.(type) {
	case string:
		return workflow.RenderText(typed, resolve)
	case map[string]any:
		for key, item := range typed {
			typed[key] = renderBodyValue(item, resolve)
		}
		return typed
	case []any:
		for i, item := range typed {
			typed[i] = renderBodyValue(item, resolve)
		}
		return typed
	default:
		return value
	}
}

// httpNodeOutput is the node output object of one HTTP execution (plan §11).
type httpNodeOutput struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	// Truncated marks a body cut at the 1 MiB cap: downstream expressions must
	// not mistake it for a complete payload.
	Truncated bool `json:"truncated,omitempty"`
}

// runHTTPRequest sends the resolved request and builds the node output: no
// redirect is followed, the response body is capped at 1 MiB and its JSON is
// decoded when parseable.
func (s *WorkflowAsyncNodeService) runHTTPRequest(ctx context.Context, cfg httpNodeConfig) (httpNodeOutput, error) {
	output := httpNodeOutput{Headers: map[string]string{}}
	parsed, err := url.Parse(cfg.URL)
	if err != nil {
		return output, fmt.Errorf("parse url: %w", err)
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" {
			return output, fmt.Errorf("url scheme must be http or https: %s", parsed.Scheme)
		}
		if !s.svc.HTTPNodeAllowHTTP {
			return output, fmt.Errorf("%w (TEAMS_HTTP_NODE_ALLOW_HTTP)", errSchemeDisallowed)
		}
	}
	if len(cfg.Query) > 0 {
		query := parsed.Query()
		for key, value := range cfg.Query {
			query.Set(key, value)
		}
		parsed.RawQuery = query.Encode()
	}

	var bodyReader io.Reader
	var bodyBytes []byte
	if cfg.Body != nil {
		bodyBytes, err = json.Marshal(cfg.Body)
		if err != nil {
			return output, fmt.Errorf("encode body: %w", err)
		}
		bodyReader = bytes.NewReader(bodyBytes)
	}
	req, err := http.NewRequestWithContext(ctx, cfg.Method, parsed.String(), bodyReader)
	if err != nil {
		return output, fmt.Errorf("build request: %w", err)
	}
	if bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for key, value := range cfg.Headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{
		Timeout: httpNodeTimeout(cfg.TimeoutMinutes),
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirectNotAllowed
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return output, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxHTTPNodeBodyBytes+1))
	if err != nil {
		return output, fmt.Errorf("read response body: %w", err)
	}
	if len(raw) > maxHTTPNodeBodyBytes {
		raw = raw[:maxHTTPNodeBodyBytes]
		output.Truncated = true
	}

	output.Status = resp.StatusCode
	for _, key := range httpNodeHeaderWhitelist {
		if value := resp.Header.Get(key); value != "" {
			output.Headers[key] = value
		}
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		decoded = string(raw)
	}
	output.Body = decoded
	return output, nil
}

// httpNodeTimeout derives the request timeout: config.timeout_minutes when
// positive, the bounded default otherwise.
func httpNodeTimeout(minutes float64) time.Duration {
	if minutes > 0 {
		return time.Duration(minutes * float64(time.Minute))
	}
	return httpNodeDefaultTimeout
}

// is2xxStatus reports whether the status counts as success (plan §11).
func is2xxStatus(status int) bool {
	return status >= 200 && status < 300
}

// sanitizeHTTPError strips the URL query and fragment from transport errors
// so credentials embedded in the query string never reach logs or the node
// error output (plan §10.3 secret hygiene, applied to http nodes).
func sanitizeHTTPError(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if u, parseErr := url.Parse(urlErr.URL); parseErr == nil {
			u.RawQuery = ""
			u.Fragment = ""
			return fmt.Sprintf("%s %s: %v", urlErr.Op, u.String(), urlErr.Err)
		}
	}
	return err.Error()
}
