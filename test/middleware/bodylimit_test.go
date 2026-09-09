// bodylimit_test.go covers tests for the request body size limit middleware.
package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/teammate/server/internal/server/middleware"
)

func TestBodyLimitReturns413WhenContentLengthExceedsLimit(t *testing.T) {
	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	handler := middleware.BodyLimitMiddlewareWithSize(4)(next)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("12345"))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if nextCalled {
		t.Fatal("next handler should not be called for oversized request")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected 413, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "request_too_large") {
		t.Fatalf("expected request_too_large error code, got %s", rec.Body.String())
	}
}
