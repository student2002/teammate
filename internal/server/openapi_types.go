// openapi_types.go 定义 OpenAPI 3.1 文档的 Go 结构体。
//
// 这些结构体用于 gen-openapi 子命令通过 chi 路由反射生成 OpenAPI spec。
// 结构体字段使用 json tag,序列化后符合 OpenAPI 3.1 规范。
//
// 参考规范:https://spec.openapis.org/oas/v3.1.0
package server

// OpenAPIDoc 是 OpenAPI 3.1 文档的根结构体。
type OpenAPIDoc struct {
	OpenAPI    string                       `json:"openapi"`
	Info       OpenAPIInfo                  `json:"info"`
	Servers    []OpenAPIServer              `json:"servers"`
	Paths      map[string]*OpenAPIPathItem  `json:"paths"`
	Components OpenAPIComponents             `json:"components"`
	Security   []map[string][]interface{}   `json:"security,omitempty"`
}

// OpenAPIInfo 包含 API 元数据。
type OpenAPIInfo struct {
	Title       string         `json:"title"`
	Description string         `json:"description"`
	Version     string         `json:"version"`
	Contact     *OpenAPIContact `json:"contact,omitempty"`
}

// OpenAPIContact 是 API 联系信息。
type OpenAPIContact struct {
	Name string `json:"name"`
}

// OpenAPIServer 定义 API 服务器地址。
type OpenAPIServer struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// OpenAPIPathItem 描述一个路径下的所有 HTTP 方法操作。
type OpenAPIPathItem struct {
	Get      *OpenAPIOperation `json:"get,omitempty"`
	Post     *OpenAPIOperation `json:"post,omitempty"`
	Put      *OpenAPIOperation `json:"put,omitempty"`
	Delete   *OpenAPIOperation `json:"delete,omitempty"`
	Patch    *OpenAPIOperation `json:"patch,omitempty"`
	Head     *OpenAPIOperation `json:"head,omitempty"`
	Options  *OpenAPIOperation `json:"options,omitempty"`
}

// Operations 返回该 path item 下所有非空操作,按 HTTP 方法顺序。
// 用于统计端点数。
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

// OpenAPIOperation 描述单个 HTTP 端点的操作。
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

// OpenAPIParameter 描述路径/查询/头参数。
type OpenAPIParameter struct {
	Name        string `json:"name"`
	In          string `json:"in"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
	Schema      map[string]interface{} `json:"schema"`
}

// OpenAPIRequestBody 描述请求体。
type OpenAPIRequestBody struct {
	Required bool                          `json:"required"`
	Content  map[string]OpenAPIMediaType  `json:"content"`
}

// OpenAPIResponse 描述单个 HTTP 响应。
type OpenAPIResponse struct {
	Description string                         `json:"description"`
	Content     map[string]OpenAPIMediaType   `json:"content,omitempty"`
}

// OpenAPIRequestBody 与 OpenAPIResponse 共用的 MediaType 结构。
type OpenAPIMediaType struct {
	Schema   map[string]interface{} `json:"schema"`
	Example  interface{}            `json:"example,omitempty"`
}

// OpenAPIComponents 包含可复用的 schema、参数、安全方案。
type OpenAPIComponents struct {
	Schemas         map[string]OpenAPISchema         `json:"schemas"`
	SecuritySchemes map[string]OpenAPISecurityScheme `json:"securitySchemes"`
}

// OpenAPISchema 描述一个 JSON schema(用于 components.schemas)。
type OpenAPISchema struct {
	Type        string                 `json:"type"`
	Description string                 `json:"description"`
	Properties  map[string]interface{} `json:"properties,omitempty"`
	Required    []string               `json:"required,omitempty"`
}

// OpenAPISecurityScheme 描述认证方案。
type OpenAPISecurityScheme struct {
	Type        string `json:"type"`
	Scheme      string `json:"scheme,omitempty"`
	BearerFormat string `json:"bearerFormat,omitempty"`
	In          string `json:"in,omitempty"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
}

// newOpenAPIDoc 创建并初始化一个 OpenAPIDoc,填充固定的 info/servers/components。
func newOpenAPIDoc() *OpenAPIDoc {
	return &OpenAPIDoc{
		OpenAPI: "3.1.0",
		Info: OpenAPIInfo{
			Title:       "Teammate API",
			Description: "Teammate HTTP API — 由 chi 路由反射自动生成。路由注册(routes_*.go)是唯一真相源,任何路由变更后重跑 gen-openapi 即可同步 spec。",
			Version:     "1.0.0",
			Contact:     &OpenAPIContact{Name: "Teammate 团队"},
		},
		Servers: []OpenAPIServer{
			{URL: "http://localhost:8080", Description: "本地开发"},
			{URL: "/api", Description: "Next.js rewrites 代理"},
		},
		Paths: make(map[string]*OpenAPIPathItem),
		Components: OpenAPIComponents{
			Schemas: map[string]OpenAPISchema{
				"Error": {
					Type:        "object",
					Description: "标准错误响应体",
					Properties: map[string]interface{}{
						"error":   map[string]interface{}{"type": "string", "description": "错误码"},
						"message": map[string]interface{}{"type": "string", "description": "人类可读的错误描述"},
					},
					Required: []string{"error", "message"},
				},
			},
			SecuritySchemes: map[string]OpenAPISecurityScheme{
				"BearerAuth": {
					Type:        "http",
					Scheme:      "bearer",
					BearerFormat: "JWT",
					Description: "人类用户登录后获得的 JWT,通过 Authorization: Bearer <token> 头部传递",
				},
				"ApiKeyAuth": {
					Type:        "apiKey",
					In:          "header",
					Name:        "X-API-Key",
					Description: "Agent 使用的 API Token (tm_ 前缀) 或会话 Token (st_ 前缀)",
				},
			},
		},
		Security: []map[string][]interface{}{
			{"BearerAuth": {}},
		},
	}
}
