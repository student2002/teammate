// recovery.go 提供 panic 恢复中间件，捕获处理函数中的 panic 并返回 500 错误响应。
//
// 本文件包含：
//   - Recovery：chi 兼容的 panic 恢复中间件，通过 defer+recover 机制实现
//
// 安全特性：
//   - 防止单个请求的 panic 导致整个 HTTP 服务器崩溃，提高系统稳定性
//   - 捕获 panic 后服务器继续运行，不影响其他并发请求
//   - 堆栈跟踪信息仅记录到服务器日志，不返回给客户端，避免信息泄露
//   - 客户端仅收到通用的 500 错误消息，不暴露内部实现细节
//
// 日志输出：
//   - error: 捕获的 panic 值
//   - method: 请求的 HTTP 方法
//   - path: 请求路径
//   - remote_addr: 客户端 IP 地址
//   - stack: 完整的堆栈跟踪信息
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery 返回一个 chi 兼容的 panic 恢复中间件。
// 通过 defer + recover 机制捕获下游处理函数中的 panic，记录错误日志和堆栈跟踪，
// 然后返回 500 Internal Server Error JSON 响应，防止服务器进程崩溃。
//
// 安全说明：
//   - 捕获 panic 后服务器继续运行，不影响其他请求
//   - 堆栈跟踪信息仅记录到服务器日志，不返回给客户端
//   - 客户端仅收到通用的错误消息，不暴露内部实现细节
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
func Recovery() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					slog.Error("panic recovered",
						slog.Any("error", err),
						slog.String("method", r.Method),
						slog.String("path", r.URL.Path),
						slog.String("remote_addr", r.RemoteAddr),
						slog.String("stack", string(debug.Stack())),
					)
					http.Error(w, `{"error":"internal_server_error","message":"an unexpected error occurred"}`, http.StatusInternalServerError)
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}
