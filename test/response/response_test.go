// Package response_test contains test cases for the response package.
package response_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/teammate/server/internal/server/response"
)

// TestBadRequest verifies the BadRequest function returns status 400 and the correct error response body.
func TestBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	response.BadRequest(w, "invalid input")

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}

	var body response.ErrorBody
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Error != "bad_request" {
		t.Errorf("expected error code 'bad_request', got %q", body.Error)
	}
	if body.Message != "invalid input" {
		t.Errorf("expected message 'invalid input', got %q", body.Message)
	}
}

// TestUnauthorized verifies the Unauthorized function returns status 401 and the correct error code.
func TestUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	response.Unauthorized(w, "not authenticated")

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}

	var body response.ErrorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "unauthorized" {
		t.Errorf("expected error code 'unauthorized', got %q", body.Error)
	}
}

// TestForbidden verifies the Forbidden function returns status 403 and the correct error code.
func TestForbidden(t *testing.T) {
	w := httptest.NewRecorder()
	response.Forbidden(w, "access denied")

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}

	var body response.ErrorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "forbidden" {
		t.Errorf("expected error code 'forbidden', got %q", body.Error)
	}
}

// TestNotFound verifies the NotFound function returns status 404 and the correct error code.
func TestNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	response.NotFound(w, "resource not found")

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", w.Code)
	}

	var body response.ErrorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "not_found" {
		t.Errorf("expected error code 'not_found', got %q", body.Error)
	}
}

// TestInternalServerErrorProduction verifies the InternalServerError function hides internal error details in production.
func TestInternalServerErrorProduction(t *testing.T) {
	os.Unsetenv("TEAMMATE_DEV")
	w := httptest.NewRecorder()
	response.InternalServerError(w, errors.New("database connection failed"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var body response.ErrorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	if body.Error != "internal" {
		t.Errorf("expected error code 'internal', got %q", body.Error)
	}
	// In production, internal error details should be hidden
	if body.Message != "internal" {
		t.Errorf("in production, message should be 'internal', got %q", body.Message)
	}
}

// TestInternalServerErrorDevMode verifies the InternalServerError function exposes internal error details in dev mode.
func TestInternalServerErrorDevMode(t *testing.T) {
	os.Setenv("TEAMMATE_DEV", "true")
	defer os.Unsetenv("TEAMMATE_DEV")

	w := httptest.NewRecorder()
	response.InternalServerError(w, errors.New("database connection failed"))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}

	var body response.ErrorBody
	json.Unmarshal(w.Body.Bytes(), &body)
	// In dev mode, internal error details should be exposed
	if body.Message != "database connection failed" {
		t.Errorf("in dev mode, message should contain error detail, got %q", body.Message)
	}
}

// TestErrorJSONFormat verifies the error response Content-Type is application/json and contains error and message fields.
func TestErrorJSONFormat(t *testing.T) {
	w := httptest.NewRecorder()
	response.BadRequest(w, "test message")

	contentType := w.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("body is not valid JSON: %v", err)
	}
	if _, ok := raw["error"]; !ok {
		t.Error("response missing 'error' field")
	}
	if _, ok := raw["message"]; !ok {
		t.Error("response missing 'message' field")
	}
}
