// openapi_reflect.go generates the OpenAPI 3.1 spec by reflecting over the chi router tree.
//
// Core principle:
//   - The r.Get/r.Post/r.Put/r.Delete calls in routes_*.go are the single source of truth for route registration
//   - chi.Walk walks the router tree and reflects out method+pattern, which necessarily matches the code
//   - After any route change, rerun gen-openapi and the spec auto-syncs — zero manual maintenance
//
// Design trade-offs:
//   - Does not parse handler function signatures to extract request/response schemas
//     (that would require AST parsing or runtime reflection, with high complexity and error-prone)
//   - Instead generates a "structurally correct, schema-lean" spec: each endpoint has correct method/path/tags,
//     and request/response uniformly reference generic schemas (object + description)
//   - This is enough for Apifox/Postman to correctly group all endpoints; developers supplement specific schemas as needed
//
// Comparison with the swag annotation approach:
//   - swag: each handler gets 10 lines of comments, manually maintaining @Router/@Param/@Success, prone to drift
//   - reflection: zero comments, the router tree is the source of truth, changing routes auto-syncs the spec
package server

import (
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/teammate/server/internal/service"
)

// BuildRouterForOpenAPI builds the production router tree used for OpenAPI generation.
//
// Differences from server.New():
//   - Does not connect to a real DB/Redis (passes nil)
//   - Does not start Hub/Gateway (passes nil)
//   - Only builds the route structure for chi.Walk to reflect over
//
// Note: middleware (AuthMiddleware/RateLimitMiddleware, etc.) registers normally,
// but since no HTTP request is executed, they will not panic. The router tree structure is identical to production.
func BuildRouterForOpenAPI() (chi.Router, error) {
	cfg := LoadConfig()

	// Build a minimal Server, only setting Config and the necessary nil dependencies
	s := &Server{
		Config:  &cfg,
		DB:      nil, // gen-openapi does not need a real DB
		Redis:   nil, // gen-openapi does not need a real Redis
		Hub:     nil, // SSE Hub not started
		Gateway: nil, // WebSocket Gateway not started
	}

	// Use buildRouter to construct a router tree identical to production
	// service.New(nil, nil, nil) is safe — it only constructs the struct and runs no queries
	svc := buildServiceForReflection(s)
	return s.buildRouter(svc), nil
}

// buildServiceForReflection builds a minimal service.Service for router reflection.
//
// gen-openapi only needs the route structure, not real data access.
// service.New internally calls store.New(pgDB); when pgDB is nil, store methods return errors,
// but this does not affect router tree construction — route registration does not invoke store methods.
func buildServiceForReflection(s *Server) *service.Service {
	return service.New(s.DB, s.Hub, s.Redis)
}

// ReflectOpenAPI walks the chi router tree and generates an OpenAPI 3.1 document.
//
// Generated content:
//   - info: title, version, description
//   - servers: local development, Next.js proxy
//   - paths: method/path/summary/tags for each endpoint
//   - components.schemas: generic Error schema
//   - components.securitySchemes: BearerAuth + ApiKeyAuth
//
// Endpoint metadata (tags/summary) is inferred from the URL path via pathToTagAndSummary,
// no manual comment maintenance required.
func ReflectOpenAPI(router chi.Router) (*OpenAPIDoc, error) {
	doc := newOpenAPIDoc()

	// Collect all routes
	type routeEntry struct {
		method string
		path   string
	}
	var routes []routeEntry

	walkFn := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi.Walk returns HEAD methods (corresponding to GET); we only record explicitly registered methods
		// Skip chi internal routes (e.g. /* static file fallback) and empty methods
		if method == "" || route == "" {
			return nil
		}
		// Normalize the path (fixes review issues 1/2/3):
		//   - Strip the /api prefix: tag inference and Apifox grouping need the original resource path
		//   - chi Mount("/", ...) reflects a * segment; OpenAPI 3.1 only allows {param}, replace with {nodeBase}
		//   - Mounted sub-routers produce a trailing slash (/comments/); OpenAPI treats /x and /x/ as different paths, trim it
		normalized := normalizeRoute(route)
		if normalized == "" {
			return nil
		}
		routes = append(routes, routeEntry{method: method, path: normalized})
		return nil
	}

	if err := chi.Walk(router, walkFn); err != nil {
		return nil, fmt.Errorf("walk router: %w", err)
	}

	// Sort by path+method to ensure deterministic output
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path != routes[j].path {
			return routes[i].path < routes[j].path
		}
		return routes[i].method < routes[j].method
	})

	// Fill the routes into OpenAPI paths
	for _, r := range routes {
		pathItem, exists := doc.Paths[r.path]
		if !exists {
			pathItem = &OpenAPIPathItem{}
			doc.Paths[r.path] = pathItem
		}

		op := buildOperation(r.method, r.path)
		switch strings.ToLower(r.method) {
		case "get":
			pathItem.Get = op
		case "post":
			pathItem.Post = op
		case "put":
			pathItem.Put = op
		case "delete":
			pathItem.Delete = op
		case "patch":
			pathItem.Patch = op
		case "head":
			pathItem.Head = op
		case "options":
			pathItem.Options = op
		}
	}

	// Reflect handler DTO Response structs and register business schema $refs (fixes review issue 8)
	// so success responses reference a concrete schema instead of an opaque object; Apifox can render example responses
	if err := reflectResponseSchemas(doc, resolveHandlerDir()); err != nil {
		// Reflection failure does not abort generation; it degrades to an opaque object (handled in buildStandardResponses)
		// but logs to stderr to remind developers to fix the handler DTO declarations
		fmt.Fprintf(os.Stderr, "warn: reflect response schemas: %v\n", err)
	}

	// Inject business schema $refs into success responses (matching the Response type inferred from the path)
	injectResponseSchemaRefs(doc)

	return doc, nil
}

// buildOperation builds an OpenAPI Operation from method+path.
//
// Metadata inference rules:
//   - tags: inferred from the first path segment (e.g. /auth/login → "Authentication", /workspaces → "Workspaces")
//   - summary: a short description generated from method+path
//   - operationId: method_path_without-params
//   - responses: unified 200/400/401/403/404/500 referencing the generic Error schema
func buildOperation(method, path string) *OpenAPIOperation {
	tag, _ := pathToTag(path)

	// Generate operationId: strip path params, join with underscores
	opID := method + "_" + sanitizePathForID(path)

	// Infer the summary
	summary := inferSummary(method, path)

	op := &OpenAPIOperation{
		Tags:        []string{tag},
		Summary:     summary,
		Description: fmt.Sprintf("%s %s — auto-generated via chi router reflection", method, path),
		OperationID: opID,
		Responses:   buildStandardResponses(method),
	}

	// Inject per-operation security (fixes review issue 6: global only BearerAuth, Agent endpoints could not be marked as ApiKeyAuth)
	// Agent-specific endpoints use API Key (st_/tm_ Token) auth and must explicitly override the global BearerAuth
	security := inferSecurity(path)
	if security != nil {
		op.Security = security
	}

	// POST/PUT/PATCH typically have a request body
	switch strings.ToLower(method) {
	case "post", "put", "patch":
		op.RequestBody = &OpenAPIRequestBody{
			Required: true,
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{
						"type":       "object",
						"description": "Request body structure — see the corresponding handler's DTO definition",
					},
				},
			},
		}
	}

	return op
}

// inferSecurity infers the endpoint's authentication method from the path.
//
// Inference rules (fixes review issue 6: global only BearerAuth, Agent-specific endpoints must be marked as ApiKeyAuth):
//   - Agent-specific endpoints (/runtimes/*, /token-usage, /messages, /logs, /git-branch,
//     /token-exchange, /agents/*/rotate-token, /agents/*/in-progress-nodes,
//     /agents/*/execution/*) use API Key (st_/tm_ Token) auth
//   - Other endpoints use the global BearerAuth (return nil to indicate inheriting the global)
//
// Returns:
//   - nil: inherit the global BearerAuth
//   - []map: per-operation security declaration (with ApiKeyAuth or both allowed)
func inferSecurity(path string) []map[string][]interface{} {
	// Agent-specific endpoint detection: identify by the last path segment and prefixes
	lastSeg := ""
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if !strings.HasPrefix(seg, "{") && seg != "" {
			lastSeg = seg
			break
		}
	}

	// Runtime-related endpoints (agentd daemon registration/heartbeat/sync/public-key/SSE)
	if strings.Contains(path, "/runtimes") {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}, {"BearerAuth": {}}}
	}

	// token-exchange endpoint (API Token → session token, called by agentd)
	if lastSeg == "token-exchange" {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}}
	}

	// Task-level agentd reporting endpoints
	// Note: use strings.Contains rather than last-segment matching, because the last segment of /logs/ws is ws, not logs
	agentTaskPaths := map[string]bool{
		"/messages":    true, // log message reporting
		"/logs":        true, // historical log query (daemon replay) + /logs/ws WebSocket
		"/git-branch":  true, // Git branch reporting
		"/token-usage": true, // token usage reporting
	}
	for key := range agentTaskPaths {
		if strings.Contains(path, "/tasks/") && strings.Contains(path, key) {
			return []map[string][]interface{}{{"ApiKeyAuth": {}}, {"BearerAuth": {}}}
		}
	}

	// Agent self-management endpoints (rotate token, query in-progress nodes, execute MCP)
	agentSelfEndpoints := map[string]bool{
		"rotate-token":        true,
		"in-progress-nodes":   true,
	}
	if strings.Contains(path, "/agents/") && agentSelfEndpoints[lastSeg] {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}, {"BearerAuth": {}}}
	}
	// execution/mcp-servers is daemon-only, ApiKeyAuth only
	if strings.Contains(path, "/agents/") && lastSeg == "mcp-servers" &&
		strings.Contains(path, "/execution/") {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}}
	}

	return nil // inherit the global BearerAuth
}

// buildStandardResponses generates the standard response set for each endpoint.
//
// Infers the success response code from the method:
//   - GET/PUT/PATCH/DELETE → 200
//   - POST → 201 (create) or 200 (action)
//
// Error responses uniformly reference #/components/schemas/Error.
func buildStandardResponses(method string) map[string]OpenAPIResponse {
	successCode := "200"
	if strings.ToLower(method) == "post" {
		successCode = "201"
	}

	resps := map[string]OpenAPIResponse{
		successCode: {
			Description: "Successful response",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{
						"type":        "object",
						"description": "Response body structure — see the corresponding handler's Response DTO",
					},
				},
			},
		},
		"400": {
			Description: "Invalid request parameters",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{
						"$ref": "#/components/schemas/Error",
					},
				},
			},
		},
		"401": {
			Description: "Unauthenticated or token invalid",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
		"403": {
			Description: "Forbidden",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
		"404": {
			Description: "Resource not found",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
		"500": {
			Description: "Internal server error",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
	}

	// DELETE is typically 204 with no response body
	if strings.ToLower(method) == "delete" {
		delete(resps, successCode)
		resps["204"] = OpenAPIResponse{Description: "Deleted successfully, no response body"}
	}

	return resps
}

// pathToTag infers the OpenAPI tag from the URL path.
//
// Inference rules (by first path segment):
//   - /auth/*          → "Authentication"
//   - /workspaces/*    → "Workspaces"
//   - /projects/*      → "Projects"
//   - /tasks/*         → "Tasks"
//   - /agents/*        → "Agents"
//   - /memories/*      → "Memories"
//   - /community/*     → "Community"
//   - /health, /ready  → "System"
//   - others           → "General"
//
// Returns (tagName, tagDescription).
func pathToTag(path string) (string, string) {
	// Strip the leading /
	clean := strings.TrimPrefix(path, "/")

	// Take the first segment
	firstSeg := clean
	if idx := strings.Index(clean, "/"); idx >= 0 {
		firstSeg = clean[:idx]
	}

	switch firstSeg {
	case "auth":
		return "Authentication", "User authentication, registration, password management"
	case "workspaces":
		return "Workspaces", "Workspace CRUD, member management"
	case "projects":
		return "Projects", "Project CRUD, Git credentials"
	case "tasks":
		return "Tasks", "Task CRUD, node operations, comments"
	case "agents":
		return "Agents", "AI agent CRUD, skill/MCP bindings"
	case "memories":
		return "Memories", "Shared memory CRUD, semantic search"
	case "community":
		return "Community", "Community workflow marketplace"
	case "templates":
		return "Workflows", "Workflow template CRUD"
	case "skills":
		return "Skills", "Skill CRUD"
	case "mcp-servers":
		return "MCP", "MCP server CRUD"
	case "runtimes":
		return "Runtimes", "Agent daemon runtime management"
	case "notifications":
		return "Notifications", "Notification list"
	case "search":
		return "Search", "Task, agent search"
	case "board":
		return "Board", "Board data"
	case "review":
		return "Review", "Review queue, self-review detection"
	case "stats":
		return "Stats", "Project/agent/template statistics"
	case "health", "ready":
		return "System", "Health check, Webhook"
	case "token-usage":
		return "Token Usage", "Token usage query and reporting"
	case "git-credentials":
		return "Git Credentials", "Git credential management"
	case "webhooks":
		return "System", "Webhook entry"
	case "agent-roles":
		return "System", "Agent role query"
	default:
		return "General", "Uncategorized endpoint"
	}
}

// normalizeRoute normalizes the path reflected by chi.Walk to conform to the OpenAPI 3.1 path template spec.
//
// Processing rules (fixes review issues 1/2/3):
//   - Strip the /api prefix: tag inference and Apifox grouping need the original resource path, not the /api/* prefix
//   - chi Mount("/", ...) reflects a * segment (wildcard); OpenAPI 3.1 only allows {param}, replace with {nodeBase}
//   - Mounted sub-routers produce a trailing slash (/comments/); OpenAPI treats /x and /x/ as different paths, trim (keep the root /)
//   - Multiple * segments are each replaced with {nodeBase} to ensure the path template is valid
//
// Parameters:
//   - route: the raw path reflected by chi.Walk (e.g. /api/tasks/{taskId}/nodes/*/{id}/approve)
//
// Returns:
//   - string: the normalized path (e.g. /tasks/{taskId}/nodes/{nodeBase}/{id}/approve); an empty string means it should be skipped
func normalizeRoute(route string) string {
	// Strip the /api prefix (fixes issue 3: tag misalignment)
	clean := strings.TrimPrefix(route, "/api")

	// Replace the * segment reflected by chi Mount("/", ...) with {nodeBase} (fixes issue 1: wildcard violates OpenAPI spec)
	// chi uses * to denote mount fallback routes; OpenAPI 3.1 only allows {param} templates
	clean = strings.ReplaceAll(clean, "/*", "/{nodeBase}")
	// Handle a standalone * at the end of the path (e.g. /nodes/*/)
	if clean == "*" || clean == "/*" {
		return "/{nodeBase}"
	}

	// Trim the trailing slash (fixes issue 2: mounted sub-routers produce /comments/ etc.)
	// Keep the root / (the root path is a valid OpenAPI path)
	if len(clean) > 1 && strings.HasSuffix(clean, "/") {
		clean = strings.TrimRight(clean, "/")
	}

	return clean
}

// inferSummary infers a short endpoint summary from method+path.
//
// Inference priority (fixes review issue 7: the original implementation always returned "Create xxx" for POST, semantically wrong for login/logout/claim/approve etc.):
//  1. Special endpoint whitelist (/health, /ready)
//  2. If the last path segment is a known verb (login/logout/claim/approve/reject/...), use the verb mapping directly
//  3. Paths containing an action substring (import/heartbeat/transfer/switch/...) are recognized as the corresponding action
//  4. Fall back to the method→action mapping (GET=Query/POST=Create/PUT=Update/DELETE=Delete/PATCH=Partial update)
func inferSummary(method, path string) string {
	m := strings.ToUpper(method)

	// Priority 1: special endpoint whitelist
	switch path {
	case "/health":
		return "Liveness check"
	case "/ready":
		return "Readiness check"
	}

	// Infer resource name and last segment from the path
	clean := strings.TrimPrefix(path, "/")
	segments := strings.Split(clean, "/")

	// Find the last non-param segment as the resource name
	resource := ""
	lastSeg := ""
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if !strings.HasPrefix(seg, "{") && seg != "" {
			if lastSeg == "" {
				lastSeg = seg
			}
			if resource == "" {
				resource = seg
			}
			break
		}
	}
	if resource == "" && len(segments) >= 2 {
		resource = segments[len(segments)-2]
	}

	// Priority 2: path last-segment verb mapping (covers POST action endpoints: login/logout/claim/approve/reject/...)
	// These path segments are themselves verbs; their semantics take priority over the method's "create"
	verbMap := map[string]string{
		"login":                   "User login",
		"logout":                  "User logout",
		"register":                "User registration",
		"whoami":                  "Query current user",
		"token-exchange":          "Token exchange",
		"change-password":         "Change password",
		"reset-password":          "Reset password",
		"request-password-reset":  "Request password reset",
		"accept-invitation":       "Accept invitation",
		"switch-workspace":        "Switch workspace",
		"claim":                   "Claim node",
		"approve":                 "Approve node",
		"reject":                  "Reject node",
		"manual":                  "Manual intervention",
		"resolve":                 "Resolve manual intervention",
		"skip-claim":              "Skip claim",
		"summary":                 "Update execution summary",
		"interrupt":               "Interrupt task",
		"interrupt-ack":           "Interrupt acknowledgement",
		"complete":                "Complete node",
		"heartbeat":               "Runtime heartbeat",
		"public-key":              "Upload runtime public key",
		"sync":                    "Sync runtime",
		"rotate-token":            "Rotate API token",
		"grant-role":              "Grant role",
		"grant":                   "Grant permission",
		"import":                  "Import community workflow",
		"transfer-ownership":      "Transfer ownership",
		"self-review-check":       "Self-review detection",
		"review-queue":            "Get review queue",
		"agent-stats":             "Get agent statistics",
		"agent-roles":             "List agent roles",
		"health-check":            "MCP health check",
		"in-progress-nodes":       "Query in-progress nodes",
		"git-branch":              "Update Git branch",
	}
	if summary, ok := verbMap[lastSeg]; ok {
		return summary
	}

	// Priority 3: method→action mapping (fallback)
	actionMap := map[string]string{
		"GET":    "Query",
		"POST":   "Create",
		"PUT":    "Update",
		"DELETE": "Delete",
		"PATCH":  "Partial update",
	}
	action := actionMap[m]
	if action == "" {
		action = m
	}

	if resource == "" {
		return fmt.Sprintf("%s %s", m, path)
	}

	return fmt.Sprintf("%s %s", action, resource)
}

// sanitizePathForID converts a path into a valid operationId segment.
//
// Rules (conforming to the OpenAPI operationId pattern ^[a-zA-Z0-9_-]+$):
//   - Strip the leading /
//   - {param} → param (keep the parameter name)
//   - / → _
//   - * → root (chi Mount fallback route; normalizeRoute already converts to {nodeBase}; here handles any residual *)
//   - Strip other illegal characters (e.g. illegal chars following a -)
func sanitizePathForID(path string) string {
	clean := strings.TrimPrefix(path, "/")
	clean = strings.ReplaceAll(clean, "{", "")
	clean = strings.ReplaceAll(clean, "}", "")
	clean = strings.ReplaceAll(clean, "/", "_")
	// Fallback: normalizeRoute already converted /* to /{nodeBase}, but replace any residual * with root
	clean = strings.ReplaceAll(clean, "*", "root")
	return clean
}

// injectResponseSchemaRefs injects business schema $refs into success responses (fixes review issue 8).
//
// The original implementation made all endpoint success responses opaque {"type":"object"}, so Apifox could not render example responses.
// This function infers the matching Response type from the path and replaces the success response's schema with a concrete $ref.
//
// Matching rules (path → Response type name):
//   - /auth/login, /auth/register, /auth/accept-invitation → authResponse
//   - /auth/token-exchange → tokenExchangeResponse
//   - /auth/switch-workspace → switchWorkspaceResponse
//   - /tasks/* (single) → taskResponse
//   - /agents/* (single) → agentResponse or createAgentResponse
//   - /workflows/* (single) → templateResponse
//   - /projects/*/stats → projectStatsResponse
//   - /agents/*/stats/agent-stats → agentStatsResponse
//   - /projects/*/git-credentials (single) → credentialResponse
//
// Unmatched endpoints keep the opaque object (safe degradation).
func injectResponseSchemaRefs(doc *OpenAPIDoc) {
	// Path pattern → Response type name
	type patternMatch struct {
		contains    string
		lastSegIs   string // exact last-segment match (empty means no constraint)
		responseRef string
	}
	matches := []patternMatch{
		// Authentication (specific endpoints first, to avoid being overridden by generic rules)
		{contains: "/auth/login", responseRef: "authResponse"},
		{contains: "/auth/register", responseRef: "authResponse"},
		{contains: "/auth/accept-invitation", responseRef: "authResponse"},
		{contains: "/auth/whoami", responseRef: "authResponse"},
		{contains: "/auth/token-exchange", responseRef: "tokenExchangeResponse"},
		{contains: "/auth/switch-workspace", responseRef: "switchWorkspaceResponse"},
		// Stats (specific paths first, matched before the agent generic rule)
		{contains: "/agents/", lastSegIs: "agent-stats", responseRef: "agentStatsResponse"},
		{contains: "/projects/", lastSegIs: "stats", responseRef: "projectStatsResponse"},
		// Tasks (single first, before the list generic rule)
		{contains: "/tasks/", lastSegIs: "{taskId}", responseRef: "taskResponse"},
		{contains: "/tasks/", lastSegIs: "{id}", responseRef: "taskResponse"},
		// Agents (single first, before the list generic rule)
		{contains: "/agents/", lastSegIs: "{agentId}", responseRef: "agentResponse"},
		{contains: "/agents/", lastSegIs: "{id}", responseRef: "agentResponse"},
		// Workflows (single first)
		{contains: "/workflows/", lastSegIs: "{workflowId}", responseRef: "templateResponse"},
		{contains: "/workflows/", lastSegIs: "{id}", responseRef: "templateResponse"},
		// Git credentials (single first)
		{contains: "/git-credentials", lastSegIs: "{credentialId}", responseRef: "credentialResponse"},
	}

	for path, pi := range doc.Paths {
		for _, m := range []string{"get", "post", "put", "delete", "patch"} {
			op := getOperation(pi, m)
			if op == nil {
				continue
			}

			// Infer the success response code (consistent with buildStandardResponses)
			successCode := "200"
			if m == "post" {
				successCode = "201"
			}
			if m == "delete" {
				successCode = "204"
			}

			// DELETE 204 has no response body; skip schema injection
			if successCode == "204" {
				continue
			}

			resp, ok := op.Responses[successCode]
			if !ok {
				continue
			}

			// Match path → Response type
			lastSeg := ""
			segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
			for i := len(segments) - 1; i >= 0; i-- {
				seg := segments[i]
				if seg != "" {
					lastSeg = seg
					break
				}
			}

			for _, match := range matches {
				if !strings.Contains(path, match.contains) {
					continue
				}
				if match.lastSegIs != "" && lastSeg != match.lastSegIs {
					continue
				}
				// Only inject the $ref if the schema has already been reflected and registered (avoid dangling references)
				if _, exists := doc.Components.Schemas[match.responseRef]; exists {
					if resp.Content == nil {
						resp.Content = map[string]OpenAPIMediaType{}
					}
					resp.Content["application/json"] = OpenAPIMediaType{
						Schema: map[string]interface{}{
							"$ref": fmt.Sprintf("#/components/schemas/%s", match.responseRef),
						},
					}
					op.Responses[successCode] = resp
				}
				break
			}
		}
	}
}

// getOperation returns the operation for the given method from a PathItem, or nil if not registered.
func getOperation(pi *OpenAPIPathItem, method string) *OpenAPIOperation {
	switch method {
	case "get":
		return pi.Get
	case "post":
		return pi.Post
	case "put":
		return pi.Put
	case "delete":
		return pi.Delete
	case "patch":
		return pi.Patch
	case "head":
		return pi.Head
	case "options":
		return pi.Options
	}
	return nil
}
