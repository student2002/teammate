// response.go provides unified HTTP response formatting utilities, including JSON responses and standard error responses.
// It automatically cleans sqlc-generated NullXxx nullable wrapper types, making API responses more concise.
// It provides a set of convenience functions for generating standard error responses (400/401/403/404/409/500/503).
// Security note: in production, InternalServerError hides internal error details and returns only the error code.
package response

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
	"os"

	"github.com/teammate/server/internal/types"
)

// nullWrapperKeys is the list of known field names for sqlc NullXxx wrappers.
// When a map contains exactly one of these keys plus a "Valid" key, it is treated as a sqlc nullable wrapper.
var nullWrapperKeys = []string{
	"String",
	"UUID",
	"Time",
	"Int64",
	"Float64",
	"Bool",
	"RawMessage",
}

// CleanValue recursively cleans sqlc NullXxx wrappers from a single value.
// sqlc-generated nullable types serialize as JSON objects like {"String":"x","Valid":true};
// this function unwraps them to the original value (e.g. "x"), making API responses more concise.
//
// Cleaning rules:
//   - {"String":"x","Valid":true}  → "x"
//   - {"String":"","Valid":false}  → nil
//   - {"UUID":"...","Valid":true}  → "..."
//   - {"UUID":"00000000-...","Valid":false} → nil
//   - {"Time":"...","Valid":true}  → "..."
//   - {"Time":"...","Valid":false} → nil
//   - {"RawMessage":...,"Valid":true}  → original JSON value
//   - {"RawMessage":null,"Valid":false} → nil
//   - {"Int64":0,"Valid":true}     → 0
//   - {"Int64":0,"Valid":false}    → nil
//   - {"Float64":0,"Valid":true}   → 0
//   - {"Float64":0,"Valid":false}  → nil
//   - {"Bool":false,"Valid":true}  → false
//   - {"Bool":false,"Valid":false} → nil
//
// Parameters:
//   - v: the value to clean (can be of any type)
//
// Returns:
//   - interface{}: the cleaned value
func CleanValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[string]interface{}:
		return cleanMapValue(val)
	case []interface{}:
		cleaned := make([]interface{}, len(val))
		for i, elem := range val {
			cleaned[i] = CleanValue(elem)
		}
		return cleaned
	default:
		return v
	}
}

// cleanMapValue processes a map: unwraps it if it is a NullXxx wrapper, otherwise recursively cleans all values.
//
// Parameters:
//   - m: the map to clean
//
// Returns:
//   - interface{}: the unwrapped original value or the cleaned map
func cleanMapValue(m map[string]interface{}) interface{} {
	// First recursively clean all values in the map
	cleaned := make(map[string]interface{}, len(m))
	for k, v := range m {
		cleaned[k] = CleanValue(v)
	}

	// Check whether the map itself is a NullXxx wrapper (contains a "Valid" key and exactly two keys)
	if validVal, hasValid := cleaned["Valid"]; hasValid && len(cleaned) == 2 {
		for _, wrapperKey := range nullWrapperKeys {
			if innerVal, hasKey := cleaned[wrapperKey]; hasKey {
				valid, _ := validVal.(bool)
				if valid {
					if wrapperKey == "RawMessage" {
						return unwrapRawMessage(innerVal)
					}
					return innerVal
				}
				return nil
			}
		}
	}

	return cleaned
}

// unwrapRawMessage handles the special case for RawMessage, where the inner value may be a string that needs to be parsed back into JSON.
//
// Parameters:
//   - v: the inner value within a RawMessage wrapper
//
// Returns:
//   - interface{}: the parsed JSON value
func unwrapRawMessage(v interface{}) interface{} {
	// If the original message was serialized as a string, attempt to deserialize it
	if s, ok := v.(string); ok {
		var parsed interface{}
		if err := json.Unmarshal([]byte(s), &parsed); err == nil {
			return CleanValue(parsed)
		}
		return s
	}
	// If it is already a parsed JSON value (object/array, etc.), clean it recursively
	return CleanValue(v)
}

// JSON serializes a value to JSON, cleans sqlc NullXxx wrappers, and writes the response.
// It replaces render.JSON for handling all handler responses.
//
// Optimization: if the serialized output contains no NullXxx wrappers (no "Valid": key),
// it skips the deserialize/clean/re-serialize loop and writes the raw JSON directly.
//
// Parameters:
//   - w: HTTP response writer
//   - r: HTTP request object
//   - v: the value to serialize as JSON
func JSON(w http.ResponseWriter, r *http.Request, v interface{}) {
	raw, err := json.Marshal(v)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Fast check: if no NullXxx wrappers are present, skip the expensive deserialize -> clean -> re-serialize flow
	needsCleaning := bytes.Contains(raw, []byte(`"Valid":`))

	var out []byte
	if needsCleaning {
		var generic interface{}
		if err := json.Unmarshal(raw, &generic); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		cleaned := CleanValue(generic)
		out, err = json.Marshal(cleaned)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		log.Printf("response.JSON: cleaned %d bytes -> %d bytes", len(raw), len(out))
	} else {
		out = raw
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}

// isDevMode checks whether the server runs in development mode (TEAMMATE_DEV=true).
// In development mode detailed error information is returned; in production only the error code is returned.
func isDevMode() bool {
	return os.Getenv("TEAMMATE_DEV") == "true"
}

// ErrorBody is the standard JSON error response body structure.
type ErrorBody struct {
	// Error is the error code constant, e.g. "bad_request", "unauthorized", "internal".
	Error string `json:"error"`
	// Message is the human-readable error description; in development mode it includes detailed error info.
	Message string `json:"message"`
}

// Error writes a JSON-formatted error response. In production, internal error details are hidden.
//
// Parameters:
//   - w: HTTP response writer
//   - httpStatus: HTTP status code
//   - code: error code constant (from the types package)
//   - err: original error object (returned to the client only in development mode)
func Error(w http.ResponseWriter, httpStatus int, code string, err error) {
	msg := code
	if isDevMode() && err != nil {
		msg = err.Error()
	}

	body := ErrorBody{Error: code, Message: msg}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(httpStatus)
	json.NewEncoder(w).Encode(body)
}

// BadRequest writes a 400 error response, indicating invalid request parameters or a malformed request.
//
// Parameters:
//   - w: HTTP response writer
//   - message: error description
func BadRequest(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeBadRequest, Message: message})
}

// Unauthorized writes a 401 error response, indicating not authenticated or an invalid/expired auth token.
//
// Parameters:
//   - w: HTTP response writer
//   - message: error description
func Unauthorized(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeUnauthorized, Message: message})
}

// Forbidden writes a 403 error response, indicating authenticated but insufficient permissions to access the target resource.
//
// Parameters:
//   - w: HTTP response writer
//   - message: error description
func Forbidden(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeForbidden, Message: message})
}

// NotFound writes a 404 error response, indicating the requested resource does not exist.
//
// Parameters:
//   - w: HTTP response writer
//   - message: error description
func NotFound(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeNotFound, Message: message})
}

// Conflict writes a 409 error response, indicating a resource conflict (e.g. concurrent claim failure, optimistic lock conflict).
//
// Parameters:
//   - w: HTTP response writer
//   - message: error description
func Conflict(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeConflict, Message: message})
}

// InternalServerError writes a 500 error response, hiding internal error details in production.
// Error details are logged to the server log for troubleshooting.
//
// Parameters:
//   - w: HTTP response writer
//   - err: original error object
func InternalServerError(w http.ResponseWriter, err error) {
	log.Printf("[error] internal server error: %v", err)
	Error(w, http.StatusInternalServerError, types.ErrCodeInternal, err)
}

// ServiceUnavailable writes a 503 error response, indicating the service is temporarily unavailable (e.g. under maintenance or overloaded).
//
// Parameters:
//   - w: HTTP response writer
//   - message: error description
func ServiceUnavailable(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusServiceUnavailable)
	json.NewEncoder(w).Encode(ErrorBody{Error: types.ErrCodeServiceUnavailable, Message: message})
}
