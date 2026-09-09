// ratelimit.go provides a Redis-based distributed rate-limiting middleware supporting the sliding-window algorithm.
// When Redis is unavailable it automatically degrades to an in-memory counter, preserving service availability.
// It includes several predefined rate-limit configurations: login rate limit, API rate limit,
// agent heartbeat rate limit, and password-reset rate limit.
//
// Security notes:
//   - Login rate limit (5/min/IP) prevents brute-force attacks
//   - Password-reset rate limit (5/min/IP) prevents reset attacks
//   - API rate limit (100/min/user) prevents API abuse
package middleware

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimitConfig defines the configuration parameters for rate limiting.
type RateLimitConfig struct {
	// Window is the rate-limit time window; the counter resets after this window elapses.
	Window time.Duration
	// MaxRequests is the maximum number of requests allowed within the time window.
	MaxRequests int
	// KeyPrefix is the Redis key prefix for this rate limiter, used to distinguish different rate-limit types.
	KeyPrefix string
}

// localRateEntry tracks the in-memory count and expiration for a single key.
type localRateEntry struct {
	count  int64
	expire time.Time
}

// localRateLimiter is a simple in-memory rate limiter used as a fallback when Redis is unavailable.
// It uses a mutex to guarantee concurrency safety.
type localRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*localRateEntry
}

// localLimiter is the package-level in-memory rate limiter instance.
var localLimiter = &localRateLimiter{
	entries: make(map[string]*localRateEntry),
}

func init() {
	// Clean up expired entries every 5 minutes to prevent memory leaks
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			localLimiter.cleanup()
		}
	}()
}

// increment increments the counter for the given key, restarting the count if the key does not exist or has expired.
//
// Parameters:
//   - key: rate-limit key (e.g. "login:192.168.1.1")
//   - window: time window
//
// Returns:
//   - int64: the request count within the current window
func (l *localRateLimiter) increment(key string, window time.Duration) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	entry, ok := l.entries[key]
	if !ok || now.After(entry.expire) {
		l.entries[key] = &localRateEntry{
			count:  1,
			expire: now.Add(window),
		}
		return 1
	}
	entry.count++
	return entry.count
}

// cleanup removes all expired entries, freeing memory.
func (l *localRateLimiter) cleanup() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for key, entry := range l.entries {
		if now.After(entry.expire) {
			delete(l.entries, key)
		}
	}
}

// RateLimitMiddleware returns a rate-limiting middleware that limits requests based on the key extracted by the key function.
//
// Algorithm notes:
//   - Redis mode: uses INCR + EXPIRE to implement a fixed-window counter, supporting distributed deployment
//   - In-memory mode: degrades to an in-process counter when Redis is unavailable; resets on restart
//
// Response header notes:
//   - X-RateLimit-Limit: the maximum number of requests allowed within the time window
//   - X-RateLimit-Remaining: the remaining number of requests available in the current window
//   - Retry-After: the number of seconds to wait when the limit is exceeded (set only on limit-exceeded responses)
//
// Parameters:
//   - rdb: Redis client; when nil, the in-memory fallback is used
//   - config: rate-limit configuration
//   - keyFunc: function that extracts the rate-limit key from the request (e.g. IP, user ID)
//
// Returns:
//   - func(http.Handler) http.Handler: chi middleware function
func RateLimitMiddleware(rdb *redis.Client, config RateLimitConfig, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				// Redis not configured — use the in-memory fallback
				key := fmt.Sprintf("%s:%s", config.KeyPrefix, keyFunc(r))
				count := localLimiter.increment(key, config.Window)
				w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", config.MaxRequests))
				w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, int64(config.MaxRequests)-count)))
				if count > int64(config.MaxRequests) {
					w.Header().Set("Retry-After", fmt.Sprintf("%d", config.Window/time.Second))
					http.Error(w, `{"error":"too_many_requests","message":"rate limit exceeded"}`, http.StatusTooManyRequests)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			key := fmt.Sprintf("ratelimit:%s:%s", config.KeyPrefix, keyFunc(r))
			ctx := r.Context()

			// Sliding window: increment the counter and set the expiration
			count, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				// Redis failure — degrade to in-memory rate limiting
				slog.Warn("redis rate limit failed, falling back to in-memory", "key", key, "err", err)
				count = localLimiter.increment(key, config.Window)
			}

			// Set the expiration on the first request within the window (Redis mode only)
			if count == 1 && err == nil {
				rdb.Expire(ctx, key, config.Window)
			}

			// Set the rate-limit response headers
			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", config.MaxRequests))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", max(0, int64(config.MaxRequests)-count)))

			if count > int64(config.MaxRequests) {
				w.Header().Set("Retry-After", fmt.Sprintf("%d", config.Window/time.Second))
				http.Error(w, `{"error":"too_many_requests","message":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// IPKeyFunc extracts the client IP address from the request for rate limiting.
// Priority: X-Real-IP > X-Forwarded-For (first IP) > RemoteAddr
//
// Parameters:
//   - r: HTTP request object
//
// Returns:
//   - string: the client IP address string
func IPKeyFunc(r *http.Request) string {
	// Check X-Real-IP first (set by a trusted reverse proxy)
	if rip := r.Header.Get("X-Real-IP"); rip != "" {
		if parsed := net.ParseIP(rip); parsed != nil {
			return parsed.String()
		}
		return rip
	}
	// X-Forwarded-For: take the first (leftmost) IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		ip := strings.TrimSpace(ips[0])
		if parsed := net.ParseIP(ip); parsed != nil {
			return parsed.String()
		}
		return ip
	}
	// Fall back to RemoteAddr (host:port format)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// UserKeyFunc extracts the user ID from the authentication context for rate limiting.
// When not authenticated it degrades to IP-based rate limiting.
//
// Parameters:
//   - r: HTTP request object
//
// Returns:
//   - string: the user ID or the client IP address
func UserKeyFunc(r *http.Request) string {
	if claims, ok := GetAuthFromContext(r.Context()); ok {
		return claims.UserID.String()
	}
	return IPKeyFunc(r)
}

// AgentKeyFunc extracts the agent ID from the authentication context for rate limiting.
// When not authenticated it degrades to IP-based rate limiting.
//
// Parameters:
//   - r: HTTP request object
//
// Returns:
//   - string: the agent ID or the client IP address
func AgentKeyFunc(r *http.Request) string {
	if claims, ok := GetAuthFromContext(r.Context()); ok {
		return claims.UserID.String()
	}
	return IPKeyFunc(r)
}

// Predefined rate-limit configurations
var (
	// LoginRateLimit limits the number of login attempts per IP (5 per minute) to prevent brute-force attacks.
	LoginRateLimit = RateLimitConfig{
		Window:      1 * time.Minute,
		MaxRequests: 5,
		KeyPrefix:   "login",
	}

	// APIRateLimit limits the number of general API requests per user (100 per minute) to prevent API abuse.
	APIRateLimit = RateLimitConfig{
		Window:      1 * time.Minute,
		MaxRequests: 100,
		KeyPrefix:   "api",
	}

	// AgentHeartbeatRateLimit limits agent heartbeat requests (1 per 10 seconds) to prevent heartbeat storms.
	AgentHeartbeatRateLimit = RateLimitConfig{
		Window:      10 * time.Second,
		MaxRequests: 1,
		KeyPrefix:   "agent-heartbeat",
	}

	// PasswordResetRateLimit limits the number of password-reset requests per IP (5 per minute) to prevent reset attacks.
	PasswordResetRateLimit = RateLimitConfig{
		Window:      1 * time.Minute,
		MaxRequests: 5,
		KeyPrefix:   "password-reset",
	}
)
