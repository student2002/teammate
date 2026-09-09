// connect.go provides PostgreSQL connection-pool management and database migration functionality.
//
// This file contains:
//   - Connect: opens a PostgreSQL connection pool via database/sql using the pgx driver
//   - Migrate: runs SQL migration files from a specified path using golang-migrate
//   - ensurePgxScheme: internal helper that converts the DSN scheme to a pgx-driver-compatible format
//
// Default connection-pool configuration:
//   - Max open connections: 25
//   - Max idle connections: 5
//   - Connection max lifetime: 5 minutes
//
// Design decisions:
//   - Uses the pgx stdlib wrapper, compatible with the standard database/sql interface
//   - Pings before connecting to verify reachability, surfacing database issues early at startup
//   - Automatically converts the postgres:// prefix in the DSN to pgx:// to satisfy golang-migrate requirements
package db

import (
	"database/sql"
	"fmt"
	"time"

	// Registers the pgx driver via the stdlib approach
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Connect opens a PostgreSQL connection pool via database/sql using the pgx driver.
// It pings the database before returning to verify reachability.
//
// Connection-pool configuration:
//   - Max open connections: 25
//   - Max idle connections: 5
//   - Connection max lifetime: 5 minutes
//
// Parameters:
//   - databaseURL: PostgreSQL connection string, e.g. "postgres://user:pass@host:port/dbname"
//
// Returns:
//   - *sql.DB: the verified database connection pool
//   - error: returned when the connection fails
func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return db, nil
}

// Migrate runs database migrations from the specified filesystem path using golang-migrate.
// migrationsPath should point to a directory containing .sql files.
// It automatically converts a "postgres://" or "postgresql://" DSN to the "pgx://" scheme required by the pgx driver.
//
// Parameters:
//   - databaseURL: PostgreSQL connection string
//   - migrationsPath: directory path of the SQL migration files
//
// Returns:
//   - error: returned when the migration fails
func Migrate(databaseURL string, migrationsPath string) error {
	// golang-migrate requires the database URL scheme to match the driver name
	// Convert "postgres://" to "pgx://" to satisfy the pgx driver
	dsn := ensurePgxScheme(databaseURL)

	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return fmt.Errorf("create migrate instance: %w", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}

// ensurePgxScheme converts a "postgres://" or "postgresql://" DSN to the
// "pgx://" scheme required by the golang-migrate pgx database driver.
//
// Parameters:
//   - dsn: the original database connection string
//
// Returns:
//   - string: the converted connection string
func ensurePgxScheme(dsn string) string {
	if len(dsn) >= 11 && dsn[:11] == "postgres://" {
		return "pgx://" + dsn[11:]
	}
	if len(dsn) >= 13 && dsn[:13] == "postgresql://" {
		return "pgx://" + dsn[13:]
	}
	return dsn
}
