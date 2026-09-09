// bodylimit.go provides a request body size limit middleware to prevent oversized request bodies from exhausting server memory.
//
// Default limit: 10 MB (configurable via the TEAMS_MAX_BODY_SIZE environment variable).
// http.MaxBytesReader prevents reading of oversized request bodies; requests with Content-Length
// exceeding the limit are rejected with 413 at the middleware layer. Chunked / unknown-length
// requests are still blocked by MaxBytesReader.
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

// DefaultMaxBodySize is the default maximum number of bytes for a request body (10 MB).
const DefaultMaxBodySize = 10 << 20 // 10 MB

// maxBodySize is the currently configured maximum number of bytes for a request body.
var maxBodySize = DefaultMaxBodySize

func init() {
	if v := os.Getenv("TEAMS_MAX_BODY_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxBodySize = n
		}
	}
}

// BodyLimitMiddleware returns a middleware that limits the request body size to no more than maxBodySize bytes.
func BodyLimitMiddleware() func(http.Handler) http.Handler {
	return BodyLimitMiddlewareWithSize(maxBodySize)
}

// BodyLimitMiddlewareWithSize creates a request body limit middleware with a custom size limit.
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

// IsMaxBytesError checks whether the error is an http.MaxBytesError (request body exceeded the limit).
// Handlers may call this after a render.Decode error to decide whether to return 413.
func IsMaxBytesError(err error) bool {
	if err == nil {
		return false
	}
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return true
	}
	// render.Decode may wrap the error; check whether the string contains the marker
	return errors.Is(err, io.ErrUnexpectedEOF) && containsStr(err.Error(), "http: request body too large")
}

// Write413 writes a 413 response.
func Write413(w http.ResponseWriter) {
	response.Error(w, http.StatusRequestEntityTooLarge, "request_too_large",
		fmt.Errorf("request body too large (max %d bytes)", maxBodySize))
}

// GetMaxBodySize returns the currently configured maximum number of bytes for a request body.
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
