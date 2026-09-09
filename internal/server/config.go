// config.go provides configuration loading for the Teammate HTTP server.
//
// This file contains:
//   - Config struct: holds all server configuration items, including port, database, Redis, JWT, OAuth, timeouts
//   - LoadConfig: reads configuration from TEAMS_-prefixed environment variables, falling back to defaults
//   - envStr/envInt/envDuration: internal helpers that read typed values from environment variables safely
//
// Configuration precedence: environment variable > default value
//
// Environment variables:
//   - TEAMS_PORT (default 8080)
//   - TEAMS_DATABASE_URL (default postgres://postgres:teammate@localhost:15432/teammate)
//   - TEAMS_REDIS_URL (default redis://localhost:16379/0)
//   - TEAMS_JWT_SECRET (default dev-secret-change-me, must be changed in production)
//   - TEAMS_ALLOWED_ORIGINS (default *, wildcard forbidden in production)
//   - TEAMS_READ_TIMEOUT (default 15s), TEAMS_WRITE_TIMEOUT (default 60s)
package server

import (
	"os"
	"strconv"
	"time"
)

// Config holds all server configuration items.
// All fields are loaded from environment variables; development uses defaults,
// while production must explicitly configure critical items.
type Config struct {
	// Port is the HTTP server listen port, default 8080.
	Port int
	// DatabaseURL is the PostgreSQL connection string.
	DatabaseURL string
	// RedisURL is the Redis connection string, used for cache, Pub/Sub and distributed locks.
	RedisURL string
	// JWTSecret is the JWT signing key; the default must be changed in production.
	JWTSecret string
	// AllowedOrigins is the comma-separated list of CORS-allowed origins; "*" allows all.
	// Production must not use the wildcard "*" and must configure allowed origins explicitly.
	AllowedOrigins string
	// BaseURL is the server base URL, used for OAuth callbacks and similar scenarios.
	BaseURL string
	// GitHubClientID is the GitHub OAuth application client ID.
	GitHubClientID string
	// GitHubSecret is the GitHub OAuth application client secret.
	GitHubSecret string
	// GoogleClientID is the Google OAuth application client ID.
	GoogleClientID string
	// GoogleSecret is the Google OAuth application client secret.
	GoogleSecret string
	// ReadTimeout is the HTTP server read timeout, default 15 seconds.
	ReadTimeout time.Duration
	// WriteTimeout is the HTTP server write timeout, default 60 seconds.
	WriteTimeout time.Duration
}

// LoadConfig reads configuration from TEAMS_-prefixed environment variables.
// Reasonable defaults are used when an environment variable is not set.
//
// Returns:
//   - Config: the loaded configuration struct
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

// envStr reads a string value from the environment variable, returning fallback when unset or empty.
//
// Parameters:
//   - key: environment variable name
//   - fallback: default value
//
// Returns:
//   - string: environment variable value or default
func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt reads an integer value from the environment variable, returning fallback when unset or parse fails.
//
// Parameters:
//   - key: environment variable name
//   - fallback: default value
//
// Returns:
//   - int: environment variable value or default
func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

// envDuration reads a duration value from the environment variable, returning fallback when unset or parse fails.
// It supports Go duration format such as "15s", "1m", "5m30s".
//
// Parameters:
//   - key: environment variable name
//   - fallback: default value
//
// Returns:
//   - time.Duration: environment variable value or default
func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}
