// bodylimit.go 提供请求体大小限制中间件，防止大请求体耗尽服务器内存。
//
// 默认限制：10 MB（可通过 TEAMS_MAX_BODY_SIZE 环境变量配置）
// http.MaxBytesReader 会阻止超限请求体的读取；带 Content-Length 的超限请求
// 会在中间件层直接返回 413。chunked/未知长度请求仍由 MaxBytesReader 阻断。
package middleware

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/teammate/server/internal/server/response"
)

// DefaultMaxBodySize 是默认的请求体最大字节数（10 MB）。
const DefaultMaxBodySize = 10 << 20 // 10 MB

// maxBodySize 是当前配置的请求体最大字节数。
var maxBodySize = DefaultMaxBodySize

func init() {
	if v := os.Getenv("TEAMS_MAX_BODY_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxBodySize = n
		}
	}
}

// BodyLimitMiddleware 返回一个中间件，限制请求体大小不超过 maxBodySize 字节。
func BodyLimitMiddleware() func(http.Handler) http.Handler {
	return BodyLimitMiddlewareWithSize(maxBodySize)
}

// BodyLimitMiddlewareWithSize 使用自定义大小限制创建请求体限制中间件。
func BodyLimitMiddlewareWithSize(maxBytes int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > int64(maxBytes) {
				Write413(w)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, int64(maxBytes))
			next.ServeHTTP(w, r)
		})
	}
}

// IsMaxBytesError 检查错误是否为 http.MaxBytesError（请求体超限）。
// handler 可在 render.Decode 错误后调用，判断是否需要返回 413。
func IsMaxBytesError(err error) bool {
	if err == nil {
		return false
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return true
	}
	// render.Decode 可能包装错误，检查字符串包含
	return errors.Is(err, io.ErrUnexpectedEOF) && containsStr(err.Error(), "http: request body too large")
}

// Write413 写入 413 响应。
func Write413(w http.ResponseWriter) {
	response.Error(w, http.StatusRequestEntityTooLarge, "request_too_large",
		fmt.Errorf("request body too large (max %d bytes)", maxBodySize))
}

// GetMaxBodySize 返回当前配置的请求体最大字节数。
func GetMaxBodySize() int {
	return maxBodySize
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && containsStrImpl(s, substr)
}

func containsStrImpl(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
