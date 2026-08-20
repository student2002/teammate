// config.go 提供 Teammate HTTP 服务器的配置加载功能。
//
// 本文件包含：
//   - Config 结构体：保存服务器所有配置项，包括端口、数据库、Redis、JWT、OAuth、超时等
//   - LoadConfig：从 TEAMS_ 前缀环境变量读取配置，未设置时使用默认值
//   - envStr/envInt/envDuration：内部辅助函数，按类型从环境变量安全读取值
//
// 配置优先级：环境变量 > 默认值
//
// 环境变量列表：
//   - TEAMS_PORT（默认 8080）
//   - TEAMS_DATABASE_URL（默认 postgres://postgres:teammate@localhost:15432/teammate）
//   - TEAMS_REDIS_URL（默认 redis://localhost:16379/0）
//   - TEAMS_JWT_SECRET（默认 dev-secret-change-me，生产环境必须更改）
//   - TEAMS_ALLOWED_ORIGINS（默认 *，生产环境禁止通配符）
//   - TEAMS_READ_TIMEOUT（默认 15s）、TEAMS_WRITE_TIMEOUT（默认 60s）
package server

import (
	"os"
	"strconv"
	"time"
)

// Config 保存服务器的所有配置项。
// 所有字段通过环境变量加载，开发环境使用默认值，生产环境必须显式配置关键项。
type Config struct {
	// Port 是 HTTP 服务器监听的端口号，默认 8080。
	Port int
	// DatabaseURL 是 PostgreSQL 数据库连接字符串。
	DatabaseURL string
	// RedisURL 是 Redis 服务器连接字符串，用于缓存、Pub/Sub 和分布式锁。
	RedisURL string
	// JWTSecret 是 JWT 签名密钥，生产环境必须更改默认值。
	JWTSecret string
	// AllowedOrigins 是 CORS 允许的源列表（逗号分隔），"*" 表示允许所有源。
	// 生产环境禁止使用通配符 "*"，必须显式配置允许的源。
	AllowedOrigins string
	// BaseURL 是服务器的基础 URL，用于 OAuth 回调等场景。
	BaseURL string
	// GitHubClientID 是 GitHub OAuth 应用的客户端 ID。
	GitHubClientID string
	// GitHubSecret 是 GitHub OAuth 应用的客户端密钥。
	GitHubSecret string
	// GoogleClientID 是 Google OAuth 应用的客户端 ID。
	GoogleClientID string
	// GoogleSecret 是 Google OAuth 应用的客户端密钥。
	GoogleSecret string
	// ReadTimeout 是 HTTP 服务器的读取超时时间，默认 15 秒。
	ReadTimeout time.Duration
	// WriteTimeout 是 HTTP 服务器的写入超时时间，默认 60 秒。
	WriteTimeout time.Duration
}

// LoadConfig 从以 TEAMS_ 为前缀的环境变量中读取配置。
// 环境变量未设置时使用合理的默认值。
//
// 返回：
//   - Config: 加载后的配置结构体
func LoadConfig() Config {
	return Config{
		Port:           envInt("TEAMS_PORT", 8080),
		DatabaseURL:    envStr("TEAMS_DATABASE_URL", "postgres://postgres:teammate@localhost:15432/teammate?sslmode=disable"),
		RedisURL:       envStr("TEAMS_REDIS_URL", "redis://localhost:16379/0"),
		JWTSecret:      envStr("TEAMS_JWT_SECRET", "dev-secret-change-me"),
		AllowedOrigins: envStr("TEAMS_ALLOWED_ORIGINS", "*"),
		BaseURL:        envStr("TEAMS_BASE_URL", "http://localhost:8080"),
		GitHubClientID: envStr("TEAMS_GITHUB_CLIENT_ID", ""),
		GitHubSecret:   envStr("TEAMS_GITHUB_SECRET", ""),
		GoogleClientID: envStr("TEAMS_GOOGLE_CLIENT_ID", ""),
		GoogleSecret:   envStr("TEAMS_GOOGLE_SECRET", ""),
		ReadTimeout:    envDuration("TEAMS_READ_TIMEOUT", 15*time.Second),
		WriteTimeout:   envDuration("TEAMS_WRITE_TIMEOUT", 60*time.Second),
	}
}

// envStr 从环境变量读取字符串值，未设置或为空时返回 fallback。
//
// 参数：
//   - key: 环境变量名
//   - fallback: 默认值
//
// 返回：
//   - string: 环境变量值或默认值
func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt 从环境变量读取整数值，未设置或解析失败时返回 fallback。
//
// 参数：
//   - key: 环境变量名
//   - fallback: 默认值
//
// 返回：
//   - int: 环境变量值或默认值
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// envDuration 从环境变量读取时间间隔值，未设置或解析失败时返回 fallback。
// 支持 Go 的时间间隔格式，如 "15s"、"1m"、"5m30s"。
//
// 参数：
//   - key: 环境变量名
//   - fallback: 默认值
//
// 返回：
//   - time.Duration: 环境变量值或默认值
func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
