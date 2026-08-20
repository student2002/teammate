// ratelimit_test.go 覆盖速率限制中间件的测试。
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

// TestRateLimitMemoryFallback 测试限流在内存后备模式下的行为，验证超过最大请求数后返回 429。
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

// TestRateLimitDifferentKeys 测试不同 IP 地址使用独立的限流键，互不影响。
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

// TestRateLimitHeaders 验证限流中间件正确设置 X-RateLimit-Limit 和 X-RateLimit-Remaining 响应头。
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

// TestRateLimitXForwardedFor 测试通过 X-Forwarded-For 头部提取客户端 IP 进行限流。
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

// TestRateLimitRetryAfterHeader 测试被限流时响应中包含 Retry-After 头部，指示重试等待时间。
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
