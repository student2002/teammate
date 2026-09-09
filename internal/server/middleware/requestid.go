// requestid.go provides a request-ID injection middleware that generates a unique UUID v4 identifier for each HTTP request.
//
// The context key for the request ID is defined in the internal/contextx package;
// the service layer reads it via contextx.GetRequestIDFromContext, avoiding a reverse dependency on middleware.
package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"github.com/teammate/server/internal/contextx"
)

// GetRequestIDFromContext retrieves the request ID from the request context.
// It delegates to contextx.GetRequestIDFromContext for backward compatibility.
func GetRequestIDFromContext(ctx context.Context) uuid.UUID {
	return contextx.GetRequestIDFromContext(ctx)
}

// RequestID generates a unique request ID (UUID v4) for each request and injects it into the request context.
// The ID is also set on the X-Request-ID response header, so the client can use it for issue feedback and request tracing.
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
