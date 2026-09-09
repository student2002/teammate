// ratelimit_test.go covers tests for the rate limiting middleware.
package middleware_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/teammate/server/internal/server/middleware"
)

// TestRateLimitMemoryFallback tests rate limiting behavior in memory fallback mode, verifying that 429 is returned after exceeding the max request count.
func TestRateLimitMemoryFallback(t *testing.T) {
	config := middleware.RateLimitConfig{
		Window:      60 * time.Second,
		MaxRequests: 3,
		KeyPrefix:   fmt.Sprintf("test-fb-%s", t.Name()),
	}

	handler := middleware.RateLimitMiddleware(nil, config, middleware.IPKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = "1.2.3.4:12345"
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "1.2.3.4:12345"
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
}

// TestRateLimitDifferentKeys tests that different IP addresses use independent rate limit keys and do not interfere with each other.
func TestRateLimitDifferentKeys(t *testing.T) {
	config := middleware.RateLimitConfig{
		Window:      60 * time.Second,
		MaxRequests: 1,
		KeyPrefix:   fmt.Sprintf("test-diff-%s", t.Name()),
	}

	handler := middleware.RateLimitMiddleware(nil, config, middleware.IPKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("IP-A req1: expected 200, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("IP-A req2: expected 429, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.2:12345"
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("IP-B req1: expected 200, got %d", w.Code)
	}
}

// TestRateLimitHeaders verifies the rate limiting middleware correctly sets X-RateLimit-Limit and X-RateLimit-Remaining response headers.
func TestRateLimitHeaders(t *testing.T) {
	config := middleware.RateLimitConfig{
		Window:      60 * time.Second,
		MaxRequests: 5,
		KeyPrefix:   fmt.Sprintf("test-hdr-%s", t.Name()),
	}

	handler := middleware.RateLimitMiddleware(nil, config, middleware.IPKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.1:12345"
	handler.ServeHTTP(w, req)

	if w.Header().Get("X-RateLimit-Limit") != "5" {
		t.Errorf("expected X-RateLimit-Limit=5, got %q", w.Header().Get("X-RateLimit-Limit"))
	}
	if w.Header().Get("X-RateLimit-Remaining") != "4" {
		t.Errorf("expected X-RateLimit-Remaining=4, got %q", w.Header().Get("X-RateLimit-Remaining"))
	}
}

// TestRateLimitXForwardedFor tests extracting client IP from the X-Forwarded-For header for rate limiting.
func TestRateLimitXForwardedFor(t *testing.T) {
	config := middleware.RateLimitConfig{
		Window:      60 * time.Second,
		MaxRequests: 1,
		KeyPrefix:   fmt.Sprintf("test-xff-%s", t.Name()),
	}

	handler := middleware.RateLimitMiddleware(nil, config, middleware.IPKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("req1: expected 200, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "203.0.113.1")
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("req2: expected 429, got %d", w.Code)
	}
}

// TestRateLimitRetryAfterHeader tests that the Retry-After header is included in the response when rate limited, indicating the retry wait time.
func TestRateLimitRetryAfterHeader(t *testing.T) {
	config := middleware.RateLimitConfig{
		Window:      60 * time.Second,
		MaxRequests: 1,
		KeyPrefix:   fmt.Sprintf("test-retry-%s", t.Name()),
	}

	handler := middleware.RateLimitMiddleware(nil, config, middleware.IPKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.16.0.1:12345"
	handler.ServeHTTP(w, req)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.16.0.1:12345"
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") != "60" {
		t.Errorf("expected Retry-After=60, got %q", w.Header().Get("Retry-After"))
	}
}

func TestRateLimitConcurrent(t *testing.T) {
	config := middleware.RateLimitConfig{
		Window:      60 * time.Second,
		MaxRequests: 10,
		KeyPrefix:   fmt.Sprintf("test-conc-%s", t.Name()),
	}

	handler := middleware.RateLimitMiddleware(nil, config, middleware.IPKeyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	passed := make(chan bool, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = "10.10.10.10:12345"
			handler.ServeHTTP(w, req)
			passed <- w.Code == http.StatusOK
		}()
	}

	wg.Wait()
	close(passed)

	passCount := 0
	for p := range passed {
		if p {
			passCount++
		}
	}

	if passCount != 10 {
		t.Errorf("expected exactly 10 requests to pass, got %d", passCount)
	}
}
