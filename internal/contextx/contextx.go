// contextx provides cross-layer shared context key definitions and access helpers.
// It resolves the reverse-dependency problem when the service layer needs to read context values injected by middleware.
package contextx

import (
	"context"

	"github.com/google/uuid"
)

// requestIDKey is the key type used to store the request ID in the request context.
type requestIDKey struct{}

// SetRequestID injects the request ID into the context (called by middleware).
func SetRequestID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// GetRequestIDFromContext reads the request ID from the context; returns uuid.Nil if absent.
func GetRequestIDFromContext(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(requestIDKey{}).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}
