// logger.go 提供 HTTP 请求日志中间件，使用 log/slog 结构化日志记录每个请求的详细信息。
//
// 本文件包含：
//   - Logger：chi 兼容的请求日志中间件，记录 method、path、status、duration、remote_addr
//   - responseWriter：包装 http.ResponseWriter 以捕获响应状态码
//   - Flush/Hijack/Unwrap：确保 SSE 流式传输和 WebSocket 协议升级正常工作
//
// 特性：
//   - 自动跳过 /health 健康检查端点以减少日志噪音
//   - 使用 log/slog 结构化日志，便于日志聚合和查询
//   - 实现 http.Flusher 接口，支持 SSE 实时推送场景
//   - 实现 http.Hijacker 接口，支持 WebSocket 协议升级
//   - 实现 Unwrap 方法，兼容 Go 1.20+ 的 http.ResponseController
package middleware

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// responseWriter 封装 http.ResponseWriter 以捕获响应状态码。
// 同时实现 http.Flusher（用于 SSE）和 http.Hijacker（用于 WebSocket 升级）接口，
// 确保中间件包装不会影响流式传输和协议升级。
type responseWriter struct {
	http.ResponseWriter
	status int
}

// newResponseWriter 创建一个新的 responseWriter，默认状态码为 200 OK。
//
// 参数：
//   - w: 原始的 http.ResponseWriter
//
// 返回：
//   - *responseWriter: 包装后的响应写入器
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader 拦截状态码设置，同时记录状态码和调用底层 ResponseWriter。
//
// 参数：
//   - code: HTTP 状态码
func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush 实现 http.Flusher 接口，确保 SSE（Server-Sent Events）和流式端点
// 在中间件包装后仍能将数据实时推送到客户端。
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack 实现 http.Hijacker 接口，允许中间件包装后仍能进行 WebSocket 协议升级。
// 如果底层 ResponseWriter 不支持 Hijack，返回 http.ErrNotSupported。
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap 返回底层 ResponseWriter，供 Go 1.20+ 的 http.ResponseController 进行接口检测。
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Logger 返回一个 chi 兼容的请求日志中间件，使用 log/slog 结构化日志记录每个请求的详细信息。
//
// 记录字段：
//   - method: HTTP 方法（GET/POST/PUT/DELETE 等）
//   - path: 请求路径
//   - status: 响应状态码
//   - duration: 请求处理耗时
//   - remote_addr: 客户端 IP 地址
//
// 优化：对 /health 健康检查端点跳过日志记录以减少噪音。
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
func Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 跳过健康检查端点
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rw := newResponseWriter(w)

			next.ServeHTTP(rw, r)

			duration := time.Since(start)

			slog.Info("http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rw.status),
				slog.Duration("duration", duration),
				slog.String("remote_addr", r.RemoteAddr),
			)
		})
	}
}
