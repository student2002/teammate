// store.go provides the core definitions and initialization methods for the data access layer (DAL).
//
// Store is the unified entry point for data access, wrapping the sqlc-generated query methods and custom transactional operations.
// All business modules access the database through Store and never touch the underlying connection directly.
//
// Design principles:
//   - Simple CRUD operations are delegated to sqlc Queries
//   - Operations that require transactions are implemented within Store methods (e.g. ApproveNodeInTx, CreateTask)
//   - All methods accept a context.Context parameter to support timeout and cancellation
//   - Errors are wrapped with fmt.Errorf to preserve the original error chain
package store

import (
	"database/sql"

	"github.com/teammate/server/internal/clock"
	db "github.com/teammate/server/internal/db/generated"
)

// Store wraps the database connection and sqlc query object, providing a unified data access entry point.
//
// Usage:
//
//	s := store.New(pgDB)
//	node, err := s.GetTaskNode(ctx, nodeID)
//
// For test scenarios that require a custom clock, use NewWithClock to create the Store.
type Store struct {
	// q is the sqlc auto-generated query object (private, enforcing access through Store wrapper methods).
	q *db.Queries

	// db is the underlying database connection, used by Store for internal custom SQL queries and transaction operations.
	db *sql.DB

	// Clock provides a time abstraction, used to inject a fake clock during testing.
	Clock clock.Clock
}

// New creates a new Store instance using the system clock.
//
// Parameters:
//   - pgDB: a connected PostgreSQL database connection
//
// Returns:
//   - an initialized Store instance
func New(pgDB *sql.DB) *Store {
	return &Store{
		q:     db.New(pgDB),
		db:    pgDB,
		Clock: clock.RealClock{},
	}
}

// NewWithClock creates a Store instance that uses a custom clock.
//
// Mainly used in test scenarios, allowing control over the passage of time to test timeout, expiration, and other time-related logic.
//
// Parameters:
//   - pgDB: a connected PostgreSQL database connection
//   - c: a custom clock implementation (e.g. FakeClock)
//
// Returns:
//   - a Store instance using the custom clock
func NewWithClock(pgDB *sql.DB, c clock.Clock) *Store {
	return &Store{
		q:     db.New(pgDB),
		db:    pgDB,
		Clock: c,
	}
}
