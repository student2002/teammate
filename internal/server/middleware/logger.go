// logger.go provides an HTTP request logging middleware that uses log/slog structured logging
// to record detailed information for each request.
//
// This file contains:
//   - Logger: a chi-compatible request logging middleware that records method, path, status, duration, remote_addr
//   - responseWriter: wraps http.ResponseWriter to capture the response status code
//   - Flush/Hijack/Unwrap: ensure SSE streaming and WebSocket protocol upgrades work correctly
//
// Features:
//   - Automatically skips the /health health-check endpoint to reduce log noise
//   - Uses log/slog structured logging for easy log aggregation and querying
//   - Implements the http.Flusher interface to support SSE real-time push scenarios
//   - Implements the http.Hijacker interface to support WebSocket protocol upgrades
//   - Implements the Unwrap method for compatibility with Go 1.20+ http.ResponseController
package middleware

import (
	"bufio"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// responseWriter wraps http.ResponseWriter to capture the response status code.
// It also implements the http.Flusher (for SSE) and http.Hijacker (for WebSocket upgrade) interfaces,
// ensuring that middleware wrapping does not affect streaming and protocol upgrades.
type responseWriter struct {
	http.ResponseWriter
	status int
}

// newResponseWriter creates a new responseWriter with a default status code of 200 OK.
//
// Parameters:
//   - w: the original http.ResponseWriter
//
// Returns:
//   - *responseWriter: the wrapped response writer
func newResponseWriter(w http.ResponseWriter) *responseWriter {
	return &responseWriter{ResponseWriter: w, status: http.StatusOK}
}

// WriteHeader intercepts status code setting, recording the status code and calling the underlying ResponseWriter.
//
// Parameters:
//   - code: HTTP status code
func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush implements the http.Flusher interface, ensuring that SSE (Server-Sent Events) and streaming endpoints
// can still push data to the client in real time after middleware wrapping.
func (rw *responseWriter) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack implements the http.Hijacker interface, allowing WebSocket protocol upgrades after middleware wrapping.
// If the underlying ResponseWriter does not support Hijack, it returns http.ErrNotSupported.
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}

// Unwrap returns the underlying ResponseWriter for interface detection by Go 1.20+ http.ResponseController.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Logger returns a chi-compatible request logging middleware that uses log/slog structured logging
// to record detailed information for each request.
//
// Recorded fields:
//   - method: HTTP method (GET/POST/PUT/DELETE, etc.)
//   - path: request path
//   - status: response status code
//   - duration: request processing duration
//   - remote_addr: client IP address
//
// Optimization: logging is skipped for the /health health-check endpoint to reduce noise.
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func Logger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip the health-check endpoint
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
