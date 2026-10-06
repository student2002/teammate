// Package testconfig centralizes the test-environment connection settings
// (PostgreSQL, Redis) shared by all test packages, so switching accounts,
// hosts or ports only requires environment variables.
//
// Full-connection overrides (highest precedence; the only way to express
// URL-special characters in credentials):
//
//	TEAMS_TEST_ADMIN_DATABASE_URL   admin connection used to create the test database
//	TEAMS_TEST_DATABASE_URL         connection to the test database itself
//	TEAMS_TEST_REDIS_URL            Redis connection
//
// Otherwise the PostgreSQL DSNs are composed from granular parts:
//
//	TEAMS_TEST_PG_HOST             default 127.0.0.1
//	TEAMS_TEST_PG_PORT             default 15432
//	TEAMS_TEST_PG_USER             default postgres
//	TEAMS_TEST_PG_PASSWORD         default teammate
//	TEAMS_TEST_PG_DATABASE         default teammate_test
//	TEAMS_TEST_PG_ADMIN_DATABASE   default postgres
//
// Redis defaults to redis://127.0.0.1:16379/0 when TEAMS_TEST_REDIS_URL is unset.
package testconfig

import (
	"fmt"
	"os"
)

// DefaultTestDatabase is the dedicated database all DB-backed tests run
// against, kept separate from the development/production database so test
// data never mixes with real data.
const DefaultTestDatabase = "teammate_test"

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func pgDSN(database string) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%s/%s?sslmode=disable",
		envOr("TEAMS_TEST_PG_USER", "postgres"),
		envOr("TEAMS_TEST_PG_PASSWORD", "teammate"),
		envOr("TEAMS_TEST_PG_HOST", "127.0.0.1"),
		envOr("TEAMS_TEST_PG_PORT", "15432"),
		database,
	)
}

// AdminDSN returns the connection string used to create/drop the test
// database. TEAMS_TEST_ADMIN_DATABASE_URL takes precedence over the
// TEAMS_TEST_PG_* parts, which address the maintenance database
// (TEAMS_TEST_PG_ADMIN_DATABASE).
func AdminDSN() string {
	if dsn := os.Getenv("TEAMS_TEST_ADMIN_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return pgDSN(envOr("TEAMS_TEST_PG_ADMIN_DATABASE", "postgres"))
}

// TestDSN returns the connection string for the test database.
// TEAMS_TEST_DATABASE_URL takes precedence over the TEAMS_TEST_PG_* parts.
func TestDSN() string {
	if dsn := os.Getenv("TEAMS_TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	return pgDSN(envOr("TEAMS_TEST_PG_DATABASE", DefaultTestDatabase))
}

// RedisURL returns the Redis connection string for ws/scheduler tests
// (TEAMS_TEST_REDIS_URL, default redis://127.0.0.1:16379/0).
func RedisURL() string {
	return envOr("TEAMS_TEST_REDIS_URL", "redis://127.0.0.1:16379/0")
}
