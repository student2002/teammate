// openapi_types.go defines the Go structs for the OpenAPI 3.1 document.
//
// These structs are used by the gen-openapi subcommand to generate the OpenAPI spec
// via chi router reflection. Struct fields use json tags and serialize to conform
// to the OpenAPI 3.1 specification.
//
// Reference spec: https://spec.openapis.org/oas/v3.1.0
package server

// OpenAPIDoc is the root struct of the OpenAPI 3.1 document.
type OpenAPIDoc struct {
	OpenAPI    string                       `json:"openapi"`
	Info       OpenAPIInfo                  `json:"info"`
	Servers    []OpenAPIServer              `json:"servers"`
	Paths      map[string]*OpenAPIPathItem  `json:"paths"`
	Components OpenAPIComponents             `json:"components"`
	Security   []map[string][]interface{}   `json:"security,omitempty"`
}

// OpenAPIInfo contains API metadata.
type OpenAPIInfo struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Contact     *OpenAPIContact `json:"contact,omitempty"`
}

// OpenAPIContact is the API contact information.
type OpenAPIContact struct {
	Name string `json:"name"`
}

// OpenAPIServer defines the API server address.
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// OpenAPIPathItem describes all HTTP method operations under a path.
type OpenAPIPathItem struct {
	Get      *OpenAPIOperation `json:"get,omitempty"`
	Post     *OpenAPIOperation `json:"post,omitempty"`
	Put      *OpenAPIOperation `json:"put,omitempty"`
	Delete   *OpenAPIOperation `json:"delete,omitempty"`
	Patch    *OpenAPIOperation `json:"patch,omitempty"`
	Head     *OpenAPIOperation `json:"head,omitempty"`
	Options  *OpenAPIOperation `json:"options,omitempty"`
}

// Operations returns all non-nil operations under this path item, in HTTP method order.
// Used for counting endpoints.
func (p *OpenAPIPathItem) Operations() []*OpenAPIOperation {
	var ops []*OpenAPIOperation
	if p.Get != nil {
		ops = append(ops, p.Get)
	}
	if p.Post != nil {
		ops = append(ops, p.Post)
	}
	if p.Put != nil {
		ops = append(ops, p.Put)
	}
	if p.Delete != nil {
		ops = append(ops, p.Delete)
	}
	if p.Patch != nil {
		ops = append(ops, p.Patch)
	}
	if p.Head != nil {
		ops = append(ops, p.Head)
	}
	if p.Options != nil {
		ops = append(ops, p.Options)
	}
	return ops
}

// OpenAPIOperation describes the operation of a single HTTP endpoint.
type OpenAPIOperation struct {
	Tags        []string                  `json:"tags"`
	Summary     string                    `json:"summary"`
	Description string                    `json:"description"`
	OperationID string                    `json:"operationId"`
	Parameters  []OpenAPIParameter        `json:"parameters,omitempty"`
	RequestBody *OpenAPIRequestBody       `json:"requestBody,omitempty"`
	Responses   map[string]OpenAPIResponse `json:"responses"`
	Security    []map[string][]interface{} `json:"security,omitempty"`
}

// OpenAPIParameter describes a path/query/header parameter.
type OpenAPIParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Schema      map[string]interface{} `json:"schema"`
}

// OpenAPIRequestBody describes the request body.
type OpenAPIRequestBody struct {
	Required bool                          `json:"required"`
	Content  map[string]OpenAPIMediaType  `json:"content"`
}

// OpenAPIResponse describes a single HTTP response.
type OpenAPIResponse struct {
	Description string                         `json:"description"`
	Content     map[string]OpenAPIMediaType   `json:"content,omitempty"`
}

// OpenAPIMediaType is the MediaType struct shared by OpenAPIRequestBody and OpenAPIResponse.
type OpenAPIMediaType struct {
	Schema   map[string]interface{} `json:"schema"`
	Example  interface{}            `json:"example,omitempty"`
}

// OpenAPIComponents contains reusable schemas, parameters, and security schemes.
type OpenAPIComponents struct {
	Schemas         map[string]OpenAPISchema         `json:"schemas"`
	SecuritySchemes map[string]OpenAPISecurityScheme `json:"securitySchemes"`
}

// OpenAPISchema describes a JSON schema (used in components.schemas).
type OpenAPISchema struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
}

// OpenAPISecurityScheme describes an authentication scheme.
type OpenAPISecurityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	In          string `json:"in,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// newOpenAPIDoc creates and initializes an OpenAPIDoc, filling in fixed info/servers/components.
func newOpenAPIDoc() *OpenAPIDoc {
	return &OpenAPIDoc{
		OpenAPI: "3.1.0",
		Info: OpenAPIInfo{
			Title:       "Teammate API",
			Description: "Teammate HTTP API — auto-generated via chi router reflection. Route registration (routes_*.go) is the single source of truth; rerun gen-openapi after any route change to sync the spec.",
			Version:     "1.0.0",
			Contact:     &OpenAPIContact{Name: "Teammate Team"},
		},
		Servers: []OpenAPIServer{
			{URL: "http://localhost:8080", Description: "Local development"},
			{URL: "/api", Description: "Next.js rewrites proxy"},
		},
		Paths: make(map[string]*OpenAPIPathItem),
		Components: OpenAPIComponents{
			Schemas: map[string]OpenAPISchema{
				"Error": {
					Type:        "object",
					Description: "Standard error response body",
					Properties: map[string]interface{}{
						"error":   map[string]interface{}{"type": "string", "description": "Error code"},
						"message": map[string]interface{}{"type": "string", "description": "Human-readable error description"},
					},
					Required: []string{"error", "message"},
				},
			},
			SecuritySchemes: map[string]OpenAPISecurityScheme{
				"BearerAuth": {
					Type:        "http",
					Scheme:      "bearer",
					BearerFormat: "JWT",
					Description: "JWT obtained after a human user logs in, passed via the Authorization: Bearer <token> header",
				},
				"ApiKeyAuth": {
					Type:        "apiKey",
					In:          "header",
					Name:        "X-API-Key",
					Description: "API Token (tm_ prefix) or session token (st_ prefix) used by Agents",
				},
			},
		},
		Security: []map[string][]interface{}{
			{"BearerAuth": {}},
		},
	}
}
