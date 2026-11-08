// cleanup.go provides test database cleanup helper functions.
package testdb

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/teammate/server/test/testconfig"
)

// GetTestDSN returns the connection string for the test database.
// It uses a dedicated "teammate_test" database to avoid polluting development data.
func GetTestDSN() string {
	return testconfig.TestDSN()
}

// getAdminDSN returns the admin database connection string (used to create/drop the test database).
func getAdminDSN() string {
	return testconfig.AdminDSN()
}

// SetupTestDB ensures the teammate_test database exists and runs migrations.
// Returns a connection to the test database. Call CleanupTestDB to clean up when done.
func SetupTestDB() (*sql.DB, error) {
	// Connect to the admin database to ensure the test database exists
	adminDB, err := sql.Open("pgx", getAdminDSN())
	if err != nil {
		return nil, fmt.Errorf("connect admin db: %w", err)
	}
	defer adminDB.Close()

	// Create the test database if it does not exist (ignore error if it already exists)
	var exists bool
	err = adminDB.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", testconfig.DefaultTestDatabase).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check test db existence: %w", err)
	}
	if !exists {
		if _, err := adminDB.Exec(fmt.Sprintf("CREATE DATABASE %s", testconfig.DefaultTestDatabase)); err != nil {
			// Parallel test packages can pass the existence check simultaneously;
			// the losers of CREATE DATABASE get 42P04, or 23505 from the
			// pg_database unique index instead of the pre-check.
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || (pgErr.Code != "42P04" && pgErr.Code != "23505") {
				return nil, fmt.Errorf("create test db: %w", err)
			}
		}
	}

	// Connect to the test database
	testDB, err := sql.Open("pgx", GetTestDSN())
	if err != nil {
		return nil, fmt.Errorf("connect test db: %w", err)
	}
	if err := testDB.Ping(); err != nil {
		testDB.Close()
		return nil, fmt.Errorf("ping test db: %w", err)
	}

	// Run migrations if the schema does not exist
	var tableExists bool
	err = testDB.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'workspaces')").Scan(&tableExists)
	if err != nil {
		testDB.Close()
		return nil, fmt.Errorf("check schema existence: %w", err)
	}
	if !tableExists {
		migrationPath := findMigrationPath()
		if migrationPath != "" {
			sqlBytes, err := os.ReadFile(migrationPath)
			if err != nil {
				testDB.Close()
				return nil, fmt.Errorf("read migration file: %w", err)
			}
			if _, err := testDB.Exec(string(sqlBytes)); err != nil {
				// A concurrent test package may have already run the migration;
				// re-check before confirming failure
				var existsNow bool
				if checkErr := testDB.QueryRow("SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = 'workspaces')").Scan(&existsNow); checkErr == nil && existsNow {
					return testDB, nil
				}
				testDB.Close()
				return nil, fmt.Errorf("run migration: %w", err)
			}
		}
	}
	return testDB, nil
}

// findMigrationPath locates the initial migration file relative to the test binary.
func findMigrationPath() string {
	// Try common relative paths from the working directory
	candidates := []string{
		"internal/db/migrations/001_init.up.sql",
		"../internal/db/migrations/001_init.up.sql",
		"../../internal/db/migrations/001_init.up.sql",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	// Fallback: use the module root directory if available
	if root := findModuleRoot(); root != "" {
		p := filepath.Join(root, "internal/db/migrations/001_init.up.sql")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// findModuleRoot searches upward from the current directory for a go.mod file.
func findModuleRoot() string {
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// DeleteWorkspace deletes a workspace by ID. All related data is cascade-deleted
// by PostgreSQL foreign key constraints (projects, agents, tasks, memories, etc.).
// This bypasses business logic (such as is_default checks) for test cleanup.
func DeleteWorkspace(db *sql.DB, workspaceID string) error {
	_, err := db.Exec("DELETE FROM workspaces WHERE id = $1", workspaceID)
	return err
}

// DeleteMember deletes a member by ID. Related workspace_members and project_members
// are cascade-deleted. Auth tokens owned by the member are also deleted.
func DeleteMember(db *sql.DB, memberID string) error {
	// Auth tokens reference owner_id without a foreign key constraint, so manual cleanup is required
	_, _ = db.Exec("DELETE FROM auth_tokens WHERE owner_type = 'member' AND owner_id = $1", memberID)
	_, err := db.Exec("DELETE FROM members WHERE id = $1", memberID)
	return err
}
