// cors.go 提供跨域资源共享（CORS）中间件，根据允许的源列表设置 Access-Control-Allow-* 响应头。
// 处理浏览器的预检请求（OPTIONS），支持凭据模式（credentials）。
// 安全说明：
//   - 仅允许在允许列表中的源（Origin）发起跨域请求
//   - 生产环境禁止使用通配符 "*"，必须显式配置允许的源
//   - 预检请求结果缓存 86400 秒（24 小时），减少 OPTIONS 请求开销
package middleware

import (
	"net/http"
	"strings"
)

// CORS 返回一个 chi 兼容的跨域中间件，根据允许的源列表设置 Access-Control-Allow-* 响应头。
//
// 处理逻辑：
//  1. 解析允许的源列表（逗号分隔字符串），"*" 表示允许所有源
//  2. 检查请求的 Origin 是否在允许列表中
//  3. 匹配成功时设置 Allow-Origin 和 Allow-Credentials 响应头
//  4. 始终设置 Allow-Methods、Allow-Headers 和 Max-Age 响应头
//  5. 预检请求（OPTIONS）直接返回 204 No Content
//
// 参数：
//   - allowedOrigins: 逗号分隔的允许源列表，"*" 表示允许所有源
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
	origins := parseOrigins(allowedOrigins)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			allowed := matchOrigin(origin, origins)

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-CSRF-Token, X-API-Key")
			w.Header().Set("Access-Control-Max-Age", "86400")

			// 处理预检请求（OPTIONS），直接返回 204
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// parseOrigins 将逗号分隔的字符串解析为去重的源列表。
// 返回 nil 表示允许所有源（原始值为空或 "*"）。
//
// 参数：
//   - s: 逗号分隔的源字符串，如 "http://localhost:3000,https://app.example.com"
//
// 返回：
//   - []string: 解析后的源列表，nil 表示允许所有源
func parseOrigins(s string) []string {
	if s == "" || s == "*" {
		return nil // nil 表示允许所有源
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// matchOrigin 检查请求的 Origin 是否在允许列表中。
// origins 为 nil 时允许所有源（配置了 "*" 或未配置时）。
//
// 参数：
//   - origin: 请求的 Origin 头值
//   - origins: 允许的源列表，nil 表示允许所有源
//
// 返回：
//   - bool: 是否匹配允许列表
func matchOrigin(origin string, origins []string) bool {
	if origin == "" {
		return false
	}
	if origins == nil {
		return true
	}
	for _, o := range origins {
		if strings.EqualFold(o, origin) {
			return true
		}
	}
	return false
}
