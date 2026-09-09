// recovery.go provides a panic-recovery middleware that catches panics in handler functions and returns a 500 error response.
//
// This file contains:
//   - Recovery: a chi-compatible panic-recovery middleware, implemented via the defer+recover mechanism
//
// Security features:
//   - Prevents a panic in a single request from crashing the entire HTTP server, improving system stability
//   - After catching a panic the server keeps running, without affecting other concurrent requests
//   - Stack trace information is logged only to the server log and not returned to the client, avoiding information leakage
//   - The client receives only a generic 500 error message, without exposing internal implementation details
//
// Log output:
//   - error: the caught panic value
//   - method: the request's HTTP method
//   - path: the request path
//   - remote_addr: the client IP address
//   - stack: the full stack trace
package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"
)

// Recovery returns a chi-compatible panic-recovery middleware.
// Via the defer + recover mechanism it catches panics in downstream handler functions,
// logs the error and stack trace, then returns a 500 Internal Server Error JSON response,
// preventing the server process from crashing.
//
// Security notes:
//   - After catching a panic the server keeps running, without affecting other requests
//   - Stack trace information is logged only to the server log and not returned to the client
//   - The client receives only a generic error message, without exposing internal implementation details
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
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
