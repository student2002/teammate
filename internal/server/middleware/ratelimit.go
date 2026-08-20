// ratelimit.go 提供基于 Redis 的分布式速率限制中间件，支持滑动窗口算法。
// Redis 不可用时自动降级为内存计数器，保证服务可用性。
// 包含多种预定义的限速配置：登录限速、API 限速、Agent 心跳限速、密码重置限速。
// 安全说明：
//   - 登录限速（5次/分钟/IP）防止暴力破解
//   - 密码重置限速（5次/分钟/IP）防止重置攻击
//   - API 限速（100次/分钟/用户）防止 API 滥用
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

// RateLimitConfig 定义速率限制的配置参数。
type RateLimitConfig struct {
	// Window 是速率限制的时间窗口，超过该时间窗口后计数器重置。
	Window time.Duration
	// MaxRequests 是时间窗口内允许的最大请求数。
	MaxRequests int
	// KeyPrefix 是 Redis 中此速率限制器的键前缀，用于区分不同类型的限速。
	KeyPrefix string
}

// localRateEntry 跟踪单个键在内存中的计数和过期时间。
type localRateEntry struct {
	count  int64
	expire time.Time
}

// localRateLimiter 是一个简单的内存速率限制器，当 Redis 不可用时作为降级方案。
// 使用互斥锁保证并发安全。
type localRateLimiter struct {
	mu      sync.Mutex
	entries map[string]*localRateEntry
}

// localLimiter 是包级别的内存速率限制器实例。
var localLimiter = &localRateLimiter{
	entries: make(map[string]*localRateEntry),
}

func init() {
	// 每 5 分钟清理一次过期条目，防止内存泄漏
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			localLimiter.cleanup()
		}
	}()
}

// increment 递增指定键的计数器，如果键不存在或已过期则重新开始计数。
//
// 参数：
//   - key: 限速键（如 "login:192.168.1.1"）
//   - window: 时间窗口
//
// 返回：
//   - int64: 当前窗口内的请求计数
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

// cleanup 清理所有已过期的条目，释放内存。
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

// RateLimitMiddleware 返回一个速率限制中间件，根据 key 函数提取的键进行请求限制。
//
// 算法说明：
//   - Redis 模式：使用 INCR + EXPIRE 实现固定窗口计数器，支持分布式部署
//   - 内存模式：Redis 不可用时降级为进程内计数器，重启后重置
//
// 响应头说明：
//   - X-RateLimit-Limit: 时间窗口内允许的最大请求数
//   - X-RateLimit-Remaining: 当前窗口内剩余可用请求数
//   - Retry-After: 超限时等待的秒数（仅在超限响应中设置）
//
// 参数：
//   - rdb: Redis 客户端，为 nil 时使用内存降级方案
//   - config: 速率限制配置
//   - keyFunc: 从请求中提取限速键的函数（如 IP、用户 ID 等）
//
// 返回：
//   - func(http.Handler) http.Handler: chi 中间件函数
func RateLimitMiddleware(rdb *redis.Client, config RateLimitConfig, keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rdb == nil {
				// Redis 未配置 — 使用内存降级方案
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

			// 滑动窗口：递增计数器并设置过期时间
			count, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				// Redis 故障 — 降级到内存速率限制
				slog.Warn("redis rate limit failed, falling back to in-memory", "key", key, "err", err)
				count = localLimiter.increment(key, config.Window)
			}

			// 窗口内首次请求时设置过期时间（仅 Redis 模式）
			if count == 1 && err == nil {
				rdb.Expire(ctx, key, config.Window)
			}

			// 设置速率限制响应头
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

// IPKeyFunc 从请求中提取客户端 IP 地址用于速率限制。
// 优先级：X-Real-IP > X-Forwarded-For（第一个 IP）> RemoteAddr
//
// 参数：
//   - r: HTTP 请求对象
//
// 返回：
//   - string: 客户端 IP 地址字符串
func IPKeyFunc(r *http.Request) string {
	// 优先检查 X-Real-IP（由可信反向代理设置）
	if rip := r.Header.Get("X-Real-IP"); rip != "" {
		if parsed := net.ParseIP(rip); parsed != nil {
			return parsed.String()
		}
		return rip
	}
	// X-Forwarded-For：取第一个（最左侧）IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		ip := strings.TrimSpace(ips[0])
		if parsed := net.ParseIP(ip); parsed != nil {
			return parsed.String()
		}
		return ip
	}
	// 降级到 RemoteAddr（host:port 格式）
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// UserKeyFunc 从认证上下文中提取用户 ID 用于速率限制。
// 未认证时降级到 IP 限速。
//
// 参数：
//   - r: HTTP 请求对象
//
// 返回：
//   - string: 用户 ID 或客户端 IP 地址
func UserKeyFunc(r *http.Request) string {
	if claims, ok := GetAuthFromContext(r.Context()); ok {
		return claims.UserID.String()
	}
	return IPKeyFunc(r)
}

// AgentKeyFunc 从认证上下文中提取 Agent ID 用于速率限制。
// 未认证时降级到 IP 限速。
//
// 参数：
//   - r: HTTP 请求对象
//
// 返回：
//   - string: Agent ID 或客户端 IP 地址
func AgentKeyFunc(r *http.Request) string {
	if claims, ok := GetAuthFromContext(r.Context()); ok {
		return claims.UserID.String()
	}
	return IPKeyFunc(r)
}

// 预定义的速率限制配置
var (
	// LoginRateLimit 限制每 IP 的登录尝试次数（每分钟 5 次），防止暴力破解攻击。
	LoginRateLimit = RateLimitConfig{
		Window:      1 * time.Minute,
		MaxRequests: 5,
		KeyPrefix:   "login",
	}

	// APIRateLimit 限制每用户的通用 API 请求次数（每分钟 100 次），防止 API 滥用。
	APIRateLimit = RateLimitConfig{
		Window:      1 * time.Minute,
		MaxRequests: 100,
		KeyPrefix:   "api",
	}

	// AgentHeartbeatRateLimit 限制 Agent 心跳请求（每 10 秒 1 次），防止心跳风暴。
	AgentHeartbeatRateLimit = RateLimitConfig{
		Window:      10 * time.Second,
		MaxRequests: 1,
		KeyPrefix:   "agent-heartbeat",
	}

	// PasswordResetRateLimit 限制每 IP 的密码重置请求（每分钟 5 次），防止重置攻击。
	PasswordResetRateLimit = RateLimitConfig{
		Window:      1 * time.Minute,
		MaxRequests: 5,
		KeyPrefix:   "password-reset",
	}
)
