// contextx 提供跨层共享的 context key 定义和读取工具。
// 解决 service 层需要读取由 middleware 注入的 context 值时的反向依赖问题。
package contextx

import (
	"context"

	"github.com/google/uuid"
)

// requestIDKey 是请求上下文中存储请求 ID 的键类型。
type requestIDKey struct{}

// SetRequestID 将请求 ID 注入上下文（供 middleware 调用）。
func SetRequestID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// GetRequestIDFromContext 从上下文中读取请求 ID，不存在则返回 uuid.Nil。
func GetRequestIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(requestIDKey{}).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
