// errors.go defines the shared API error codes, sentinel errors, and error-response structs.
//
// This file contains:
//   - ErrCode* constants: unified API error codes
//   - Err* sentinel errors: domain-layer errors thrown by Store/Service and recognized by Handler
//   - IsNotFound helper: identifies "resource not found" errors (compatible with the sql.ErrNoRows wrapping chain)
//   - ErrorResponse: the standard API error-response format
package types

import (
	"errors"
)

// API error-code constants for responses.
const (
	ErrCodeBadRequest         = "bad_request"         // Bad request parameters
	ErrCodeUnauthorized       = "unauthorized"        // Unauthenticated
	ErrCodeForbidden          = "forbidden"           // No permission
	ErrCodeNotFound           = "not_found"           // Resource not found
	ErrCodeInternal           = "internal"            // Internal server error
	ErrCodeConflict           = "conflict"            // Resource conflict (e.g. concurrent claim)
	ErrCodeServiceUnavailable = "service_unavailable" // Service unavailable
)

// ErrNotFound is the domain-layer "resource not found" sentinel error.
//
// When an underlying Store-layer query returns sql.ErrNoRows, it should be wrapped as ErrNotFound:
//
//	if errors.Is(err, sql.ErrNoRows) {
//	    return types.Task{}, fmt.Errorf("get task: %w", types.ErrNotFound)
//	}
//
// This way the Handler layer only checks types.IsNotFound(err) and no longer depends directly on database/sql.
var ErrNotFound = errors.New("resource not found")

// IsNotFound reports whether err represents "resource not found".
//
// It walks the error chain via errors.Is looking for ErrNotFound. Any error wrapped as ErrNotFound
// is recognized (including errors converted from sql.ErrNoRows at the Store layer).
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// ErrNodeStateConflict indicates that the node's current state does not allow the operation (e.g. asking to approve/reject/complete a node that is still pending).
var ErrNodeStateConflict = errors.New("node is not in the expected state")

// IsNodeStateConflict reports whether the error chain contains a node-state conflict.
func IsNodeStateConflict(err error) bool {
	return errors.Is(err, ErrNodeStateConflict)
}

// ErrorResponse is the standard API error-response format.
type ErrorResponse struct {
	Code    string `json:"code"`    // Error code
	Message string `json:"message"` // Error message
	Details any    `json:"details"` // Details (optional)
}
