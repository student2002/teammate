// openapi_reflect.go 通过反射 chi 路由树生成 OpenAPI 3.1 spec。
//
// 核心原理:
//   - routes_*.go 中的 r.Get/r.Post/r.Put/r.Delete 是路由注册的唯一真相源
//   - chi.Walk 遍历路由树,反射出 method+pattern,必然与代码一致
//   - 任何路由变更后重跑 gen-openapi,spec 自动同步——零手工维护
//
// 设计取舍:
//   - 不解析 handler 函数签名提取请求/响应 schema(那需要 AST 解析或运行时反射,
//     复杂度高且易出错)
//   - 转而生成"结构正确、schema 精简"的 spec:每个端点有正确的 method/path/tags,
//     请求/响应统一引用通用 schema(object + 说明)
//   - 这足以让 Apifox/Postman 正确分组所有端点,开发者再按需补充具体 schema
//
// 与 swag 注解方案的对比:
//   - swag:每个 handler 加 10 行注释,手工维护 @Router/@Param/@Success,易漂移
//   - 反射:零注释,路由树即真相源,改路由自动同步 spec
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

// BuildRouterForOpenAPI 构建用于 OpenAPI 生成的生产路由树。
//
// 与 server.New() 的区别:
//   - 不连接真实 DB/Redis(传入 nil)
//   - 不启动 Hub/Gateway(传入 nil)
//   - 仅构建路由结构,供 chi.Walk 反射
//
// 注意:中间件(AuthMiddleware/RateLimitMiddleware 等)会正常注册,
// 但因为不执行任何 HTTP 请求,它们不会 panic。路由树结构完全等同于生产环境。
func BuildRouterForOpenAPI() (chi.Router, error) {
	cfg := LoadConfig()

	// 构造最小 Server,仅设置 Config 和必要的 nil 依赖
	s := &Server{
		Config:  &cfg,
		DB:      nil, // gen-openapi 不需要真实 DB
		Redis:   nil, // gen-openapi 不需要真实 Redis
		Hub:     nil, // SSE Hub 不启动
		Gateway: nil, // WebSocket Gateway 不启动
	}

	// 使用 buildRouter 构建与生产完全一致的路由树
	// service.New(nil, nil, nil) 是安全的——它只构造结构体,不执行查询
	svc := buildServiceForReflection(s)
	return s.buildRouter(svc), nil
}

// buildServiceForReflection 构造用于路由反射的最小 service.Service。
//
// gen-openapi 只需要路由结构,不需要真实数据访问。
// service.New 内部调用 store.New(pgDB),pgDB 为 nil 时 store 方法会返回错误,
// 但这不会影响路由树构建——路由注册不执行 store 方法。
func buildServiceForReflection(s *Server) *service.Service {
	return service.New(s.DB, s.Hub, s.Redis)
}

// ReflectOpenAPI 遍历 chi 路由树,生成 OpenAPI 3.1 文档。
//
// 生成内容:
//   - info: 标题、版本、描述
//   - servers: 本地开发、Next.js 代理
//   - paths: 每个端点的 method/path/summary/tags
//   - components.schemas: 通用 Error schema
//   - components.securitySchemes: BearerAuth + ApiKeyAuth
//
// 端点元数据(tags/summary)通过 pathToTagAndSummary 从 URL 路径推断,
// 无需手工维护注释。
func ReflectOpenAPI(router chi.Router) (*OpenAPIDoc, error) {
	doc := newOpenAPIDoc()

	// 收集所有路由
	type routeEntry struct {
		method string
		path   string
	}
	var routes []routeEntry

	walkFn := func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		// chi.Walk 会返回 HEAD 方法(对应 GET),我们只记录显式注册的方法
		// 跳过 chi 内部路由(如 /* 静态文件回退)和空方法
		if method == "" || route == "" {
			return nil
		}
		// 规范化路径(修复审核问题 1/2/3):
		//   - 剥离 /api 前缀:tag 推断和 Apifox 分组需要原始资源路径
		//   - chi Mount("/", ...) 反射出 * 段,OpenAPI 3.1 仅允许 {param},替换为 {nodeBase}
		//   - Mount 子路由产生 trailing slash(/comments/),OpenAPI 视 /x 与 /x/ 为不同路径,裁剪
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

	// 按路径+方法排序,确保生成结果稳定(deterministic)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].path != routes[j].path {
			return routes[i].path < routes[j].path
		}
		return routes[i].method < routes[j].method
	})

	// 将路由填入 OpenAPI paths
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

	// 反射 handler DTO Response 结构体,注册业务 schema $ref(修复审核问题 8)
	// 使成功响应引用具体 schema 而非不透明 object,Apifox 可渲染示例响应
	if err := reflectResponseSchemas(doc, resolveHandlerDir()); err != nil {
		// 反射失败不中断生成,降级为不透明 object(已在 buildStandardResponses 中处理)
		// 但记录到 stderr 提醒开发者修复 handler DTO 声明
		fmt.Fprintf(os.Stderr, "warn: reflect response schemas: %v\n", err)
	}

	// 为成功响应注入业务 schema $ref(基于路径推断匹配的 Response 类型)
	injectResponseSchemaRefs(doc)

	return doc, nil
}

// buildOperation 从 method+path 构建一个 OpenAPI Operation。
//
// 元数据推断规则:
//   - tags: 从路径首段推断(如 /auth/login → "认证",/workspaces → "工作区")
//   - summary: 从 method+path 生成简短描述
//   - operationId: method_path_去参数
//   - responses: 统一 200/400/401/403/404/500,引用通用 Error schema
func buildOperation(method, path string) *OpenAPIOperation {
	tag, _ := pathToTag(path)

	// 生成 operationId: 去掉路径参数,用下划线连接
	opID := method + "_" + sanitizePathForID(path)

	// 推断 summary
	summary := inferSummary(method, path)

	op := &OpenAPIOperation{
		Tags:        []string{tag},
		Summary:     summary,
		Description: fmt.Sprintf("%s %s — 由 chi 路由反射自动生成", method, path),
		OperationID: opID,
		Responses:   buildStandardResponses(method),
	}

	// 注入 per-operation security(修复审核问题 6:全局仅 BearerAuth,Agent 端点无法标为 ApiKeyAuth)
	// Agent 专用端点用 API Key(st_/tm_ Token)认证,需显式声明覆盖全局 BearerAuth
	security := inferSecurity(path)
	if security != nil {
		op.Security = security
	}

	// POST/PUT/PATCH 通常有请求体
	switch strings.ToLower(method) {
	case "post", "put", "patch":
		op.RequestBody = &OpenAPIRequestBody{
			Required: true,
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{
						"type":       "object",
						"description": "请求体结构参见对应 handler 的 DTO 定义",
					},
				},
			},
		}
	}

	return op
}

// inferSecurity 从路径推断端点的认证方式。
//
// 推断规则(修复审核问题 6:全局仅 BearerAuth,Agent 专用端点需标为 ApiKeyAuth):
//   - Agent 专用端点(/runtimes/*、/token-usage、/messages、/logs、/git-branch、
//     /token-exchange、/agents/*/rotate-token、/agents/*/in-progress-nodes、
//     /agents/*/execution/*)使用 API Key(st_/tm_ Token)认证
//   - 其他端点使用全局 BearerAuth(返回 nil 表示沿用全局)
//
// 返回:
//   - nil: 沿用全局 BearerAuth
//   - []map: per-operation security 声明(含 ApiKeyAuth 或两者皆可)
func inferSecurity(path string) []map[string][]interface{} {
	// Agent 专用端点判定:用路径末段和前缀识别
	lastSeg := ""
	segments := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if !strings.HasPrefix(seg, "{") && seg != "" {
			lastSeg = seg
			break
		}
	}

	// runtime 相关端点(agentd 守护进程注册/心跳/同步/公钥/SSE)
	if strings.Contains(path, "/runtimes") {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}, {"BearerAuth": {}}}
	}

	// token-exchange 端点(API Token → 会话 Token,agentd 调用)
	if lastSeg == "token-exchange" {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}}
	}

	// 任务级 agentd 上报端点
	// 注意:用 strings.Contains 判定而非末段匹配,因为 /logs/ws 末段是 ws 而非 logs
	agentTaskPaths := map[string]bool{
		"/messages":    true, // 日志消息上报
		"/logs":        true, // 历史日志查询(daemon 回放) + /logs/ws WebSocket
		"/git-branch":  true, // Git 分支上报
		"/token-usage": true, // Token 用量上报
	}
	for key := range agentTaskPaths {
		if strings.Contains(path, "/tasks/") && strings.Contains(path, key) {
			return []map[string][]interface{}{{"ApiKeyAuth": {}}, {"BearerAuth": {}}}
		}
	}

	// Agent 自身管理端点(轮换 Token、查询进行中节点、执行 MCP)
	agentSelfEndpoints := map[string]bool{
		"rotate-token":        true,
		"in-progress-nodes":   true,
	}
	if strings.Contains(path, "/agents/") && agentSelfEndpoints[lastSeg] {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}, {"BearerAuth": {}}}
	}
	// execution/mcp-servers 是 daemon-only,仅 ApiKeyAuth
	if strings.Contains(path, "/agents/") && lastSeg == "mcp-servers" &&
		strings.Contains(path, "/execution/") {
		return []map[string][]interface{}{{"ApiKeyAuth": {}}}
	}

	return nil // 沿用全局 BearerAuth
}

// buildStandardResponses 为每个端点生成标准响应集。
//
// 根据方法推断成功响应码:
//   - GET/PUT/PATCH/DELETE → 200
//   - POST → 201(创建)或 200(动作)
//
// 错误响应统一引用 #/components/schemas/Error。
func buildStandardResponses(method string) map[string]OpenAPIResponse {
	successCode := "200"
	if strings.ToLower(method) == "post" {
		successCode = "201"
	}

	resps := map[string]OpenAPIResponse{
		successCode: {
			Description: "成功响应",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{
						"type":        "object",
						"description": "响应体结构参见对应 handler 的 Response DTO",
					},
				},
			},
		},
		"400": {
			Description: "请求参数错误",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{
						"$ref": "#/components/schemas/Error",
					},
				},
			},
		},
		"401": {
			Description: "未认证或 Token 失效",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
		"403": {
			Description: "无权限",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
		"404": {
			Description: "资源不存在",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
		"500": {
			Description: "服务器内部错误",
			Content: map[string]OpenAPIMediaType{
				"application/json": {
					Schema: map[string]interface{}{"$ref": "#/components/schemas/Error"},
				},
			},
		},
	}

	// DELETE 通常是 204 无响应体
	if strings.ToLower(method) == "delete" {
		delete(resps, successCode)
		resps["204"] = OpenAPIResponse{Description: "删除成功,无响应体"}
	}

	return resps
}

// pathToTag 从 URL 路径推断 OpenAPI tag。
//
// 推断规则(按路径首段):
//   - /auth/*          → "认证"
//   - /workspaces/*    → "工作区"
//   - /projects/*      → "项目"
//   - /tasks/*         → "任务"
//   - /agents/*        → "代理"
//   - /memories/*      → "记忆"
//   - /community/*     → "社区"
//   - /health, /ready  → "系统"
//   - 其他             → "通用"
//
// 返回 (tagName, tagDescription)。
func pathToTag(path string) (string, string) {
	// 去掉前导 /
	clean := strings.TrimPrefix(path, "/")

	// 取第一段
	firstSeg := clean
	if idx := strings.Index(clean, "/"); idx >= 0 {
		firstSeg = clean[:idx]
	}

	switch firstSeg {
	case "auth":
		return "认证", "用户认证、注册、密码管理"
	case "workspaces":
		return "工作区", "工作区 CRUD、成员管理"
	case "projects":
		return "项目", "项目 CRUD、Git 凭据"
	case "tasks":
		return "任务", "任务 CRUD、节点操作、评论"
	case "agents":
		return "代理", "AI 代理 CRUD、技能/MCP 绑定"
	case "memories":
		return "记忆", "共享记忆 CRUD、语义搜索"
	case "community":
		return "社区", "社区工作流市场"
	case "templates":
		return "工作流", "工作流模板 CRUD"
	case "skills":
		return "技能", "技能 CRUD"
	case "mcp-servers":
		return "MCP", "MCP 服务器 CRUD"
	case "runtimes":
		return "运行时", "Agent 守护进程运行时管理"
	case "notifications":
		return "通知", "通知列表"
	case "search":
		return "搜索", "任务、代理搜索"
	case "board":
		return "看板", "看板数据"
	case "review":
		return "审查", "审查队列、自我审查检测"
	case "stats":
		return "统计", "项目/代理/模板统计"
	case "health", "ready":
		return "系统", "健康检查、Webhook"
	case "token-usage":
		return "Token用量", "Token 用量查询和上报"
	case "git-credentials":
		return "Git凭据", "Git 凭据管理"
	case "webhooks":
		return "系统", "Webhook 入口"
	case "agent-roles":
		return "系统", "Agent 角色查询"
	default:
		return "通用", "未分类端点"
	}
}

// normalizeRoute 规范化 chi.Walk 反射出的路径,使其符合 OpenAPI 3.1 path 模板规范。
//
// 处理规则(修复审核问题 1/2/3):
//   - 剥离 /api 前缀:tag 推断和 Apifox 分组需要原始资源路径,而非 /api/* 前缀
//   - chi Mount("/", ...) 反射出 * 段(通配符),OpenAPI 3.1 仅允许 {param},替换为 {nodeBase}
//   - Mount 子路由产生 trailing slash(/comments/),OpenAPI 视 /x 与 /x/ 为不同路径,裁剪(保留根 /)
//   - 多段 * 逐个替换为 {nodeBase},确保 path 模板合法
//
// 参数:
//   - route: chi.Walk 反射出的原始路径(如 /api/tasks/{taskId}/nodes/*/{id}/approve)
//
// 返回:
//   - string: 规范化后的路径(如 /tasks/{taskId}/nodes/{nodeBase}/{id}/approve),空字符串表示应跳过
func normalizeRoute(route string) string {
	// 剥离 /api 前缀(修复问题 3:tag 错位)
	clean := strings.TrimPrefix(route, "/api")

	// chi Mount("/", ...) 反射出的 * 段替换为 {nodeBase}(修复问题 1:通配符违反 OpenAPI 规范)
	// chi 用 * 表示 mount 回退路由,OpenAPI 3.1 仅允许 {param} 模板
	clean = strings.ReplaceAll(clean, "/*", "/{nodeBase}")
	// 处理路径末尾单独的 *(如 /nodes/*/)
	if clean == "*" || clean == "/*" {
		return "/{nodeBase}"
	}

	// 裁剪 trailing slash(修复问题 2:Mount 子路由产生 /comments/ 等)
	// 保留根 /(根路径是合法的 OpenAPI path)
	if len(clean) > 1 && strings.HasSuffix(clean, "/") {
		clean = strings.TrimRight(clean, "/")
	}

	return clean
}

// inferSummary 从 method+path 推断简短的端点摘要。
//
// 推断优先级(修复审核问题 7:原实现 POST 一律返回"创建 xxx",login/logout/claim/approve 等语义错误):
//  1. 特殊端点白名单(/health、/ready)
//  2. 路径末段若为已知动词(login/logout/claim/approve/reject/...),直接用动词中文映射
//  3. 含动作子串的路径(import/heartbeat/transfer/switch/...)识别为对应动作
//  4. 回退到 method→中文动作映射(GET=查询/POST=创建/PUT=更新/DELETE=删除/PATCH=部分更新)
func inferSummary(method, path string) string {
	m := strings.ToUpper(method)

	// 优先级 1:特殊端点白名单
	switch path {
	case "/health":
		return "存活检查"
	case "/ready":
		return "就绪检查"
	}

	// 从路径推断资源名和末段
	clean := strings.TrimPrefix(path, "/")
	segments := strings.Split(clean, "/")

	// 找到最后一个非参数段作为资源名
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

	// 优先级 2:路径末段动词映射(覆盖 POST 动作端点:login/logout/claim/approve/reject/...)
	// 这些路径段本身就是动词,语义优先于 method 的"创建"
	verbMap := map[string]string{
		"login":                   "用户登录",
		"logout":                  "用户登出",
		"register":                "用户注册",
		"whoami":                  "查询当前用户",
		"token-exchange":          "Token 交换",
		"change-password":         "修改密码",
		"reset-password":          "重置密码",
		"request-password-reset":  "请求密码重置",
		"accept-invitation":       "接受邀请",
		"switch-workspace":        "切换工作区",
		"claim":                   "认领节点",
		"approve":                 "审批通过节点",
		"reject":                  "驳回节点",
		"manual":                  "人工干预",
		"resolve":                 "解决人工干预",
		"skip-claim":              "跳过认领",
		"summary":                 "更新执行摘要",
		"interrupt":               "中断任务",
		"interrupt-ack":           "中断确认",
		"complete":                "完成节点",
		"heartbeat":               "运行时心跳",
		"public-key":              "上传运行时公钥",
		"sync":                    "同步运行时",
		"rotate-token":            "轮换 API Token",
		"grant-role":              "授予角色",
		"grant":                   "授予权限",
		"import":                  "导入社区工作流",
		"transfer-ownership":      "转移所有权",
		"self-review-check":       "自我审查检测",
		"review-queue":            "获取审查队列",
		"agent-stats":             "获取代理统计",
		"agent-roles":             "列出 Agent 角色",
		"health-check":            "MCP 健康检查",
		"in-progress-nodes":       "查询进行中节点",
		"git-branch":              "更新 Git 分支",
	}
	if summary, ok := verbMap[lastSeg]; ok {
		return summary
	}

	// 优先级 3:method→中文动作映射(回退)
	actionMap := map[string]string{
		"GET":    "查询",
		"POST":   "创建",
		"PUT":    "更新",
		"DELETE": "删除",
		"PATCH":  "部分更新",
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

// sanitizePathForID 将路径转换为合法的 operationId 片段。
//
// 规则(符合 OpenAPI operationId 模式 ^[a-zA-Z0-9_-]+$):
//   - 去掉前导 /
//   - {param} → param(保留参数名)
//   - / → _
//   - * → root(chi Mount 回退路由,normalizeRoute 已转为 {nodeBase},此处兜底处理残留 *)
//   - 去掉其他非法字符(如 - 后紧跟的非法符)
func sanitizePathForID(path string) string {
	clean := strings.TrimPrefix(path, "/")
	clean = strings.ReplaceAll(clean, "{", "")
	clean = strings.ReplaceAll(clean, "}", "")
	clean = strings.ReplaceAll(clean, "/", "_")
	// 兜底:normalizeRoute 已把 /* 换成 /{nodeBase},但若有残留 * 替换为 root
	clean = strings.ReplaceAll(clean, "*", "root")
	return clean
}

// injectResponseSchemaRefs 为成功响应注入业务 schema $ref(修复审核问题 8)。
//
// 原实现所有端点成功响应为不透明 {"type":"object"},Apifox 无法渲染示例响应。
// 本函数基于路径推断匹配的 Response 类型,将成功响应的 schema 替换为具体 $ref。
//
// 匹配规则(路径 → Response 类型名):
//   - /auth/login, /auth/register, /auth/accept-invitation → authResponse
//   - /auth/token-exchange → tokenExchangeResponse
//   - /auth/switch-workspace → switchWorkspaceResponse
//   - /tasks/*(单条) → taskResponse
//   - /agents/*(单条) → agentResponse 或 createAgentResponse
//   - /workflows/*(单条) → templateResponse
//   - /projects/*/stats → projectStatsResponse
//   - /agents/*/stats/agent-stats → agentStatsResponse
//   - /projects/*/git-credentials(单条) → credentialResponse
//
// 未匹配的端点保持不透明 object(降级安全)。
func injectResponseSchemaRefs(doc *OpenAPIDoc) {
	// 路径模式 → Response 类型名
	type patternMatch struct {
		contains    string
		lastSegIs   string // 末段精确匹配(空表示不限)
		responseRef string
	}
	matches := []patternMatch{
		// 认证(具体端点优先,避免被通用规则盖过)
		{contains: "/auth/login", responseRef: "authResponse"},
		{contains: "/auth/register", responseRef: "authResponse"},
		{contains: "/auth/accept-invitation", responseRef: "authResponse"},
		{contains: "/auth/whoami", responseRef: "authResponse"},
		{contains: "/auth/token-exchange", responseRef: "tokenExchangeResponse"},
		{contains: "/auth/switch-workspace", responseRef: "switchWorkspaceResponse"},
		// 统计(具体路径优先,在代理通用规则之前匹配)
		{contains: "/agents/", lastSegIs: "agent-stats", responseRef: "agentStatsResponse"},
		{contains: "/projects/", lastSegIs: "stats", responseRef: "projectStatsResponse"},
		// 任务(单条优先,在列表通用规则之前)
		{contains: "/tasks/", lastSegIs: "{taskId}", responseRef: "taskResponse"},
		{contains: "/tasks/", lastSegIs: "{id}", responseRef: "taskResponse"},
		// 代理(单条优先,在列表通用规则之前)
		{contains: "/agents/", lastSegIs: "{agentId}", responseRef: "agentResponse"},
		{contains: "/agents/", lastSegIs: "{id}", responseRef: "agentResponse"},
		// 工作流(单条优先)
		{contains: "/workflows/", lastSegIs: "{workflowId}", responseRef: "templateResponse"},
		{contains: "/workflows/", lastSegIs: "{id}", responseRef: "templateResponse"},
		// Git 凭据(单条优先)
		{contains: "/git-credentials", lastSegIs: "{credentialId}", responseRef: "credentialResponse"},
	}

	for path, pi := range doc.Paths {
		for _, m := range []string{"get", "post", "put", "delete", "patch"} {
			op := getOperation(pi, m)
			if op == nil {
				continue
			}

			// 推断成功响应码(与 buildStandardResponses 逻辑一致)
			successCode := "200"
			if m == "post" {
				successCode = "201"
			}
			if m == "delete" {
				successCode = "204"
			}

			// DELETE 204 无响应体,跳过 schema 注入
			if successCode == "204" {
				continue
			}

			resp, ok := op.Responses[successCode]
			if !ok {
				continue
			}

			// 匹配路径 → Response 类型
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
				// 仅当该 schema 已反射注册才注入 $ref(避免引用悬空)
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

// getOperation 从 PathItem 取指定方法的 operation,未注册返回 nil。
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
