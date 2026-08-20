// requestid.go 提供请求 ID 注入中间件，为每个 HTTP 请求生成唯一的 UUID v4 标识符。
//
// 请求 ID 的 context key 定义在 internal/contextx 包中，
// service 层通过 contextx.GetRequestIDFromContext 读取，避免反向依赖 middleware。
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/contextx"
)

// GetRequestIDFromContext 从请求上下文中获取请求 ID。
// 委托给 contextx.GetRequestIDFromContext，保持向后兼容。
func GetRequestIDFromContext(ctx context.Context) uuid.UUID {
	return contextx.GetRequestIDFromContext(ctx)
}

// RequestID 为每个请求生成唯一的请求 ID（UUID v4），并注入到请求上下文中。
// 该 ID 会设置到 X-Request-ID 响应头，客户端可用于问题反馈和请求追踪。
func RequestID() func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := uuid.New()
			ctx := contextx.SetRequestID(r.Context(), requestID)
			w.Header().Set("X-Request-ID", requestID.String())
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
