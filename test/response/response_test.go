// Package response_test 包含 response 包的测试用例。
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

// TestBadRequest 验证 BadRequest 函数返回 400 状态码和正确的错误响应体。
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

// TestUnauthorized 验证 Unauthorized 函数返回 401 状态码和正确的错误码。
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

// TestForbidden 验证 Forbidden 函数返回 403 状态码和正确的错误码。
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

// TestNotFound 验证 NotFound 函数返回 404 状态码和正确的错误码。
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

// TestInternalServerErrorProduction 验证生产环境下 InternalServerError 函数隐藏内部错误详情。
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
	// 生产环境下应隐藏内部错误详情
	if body.Message != "internal" {
		t.Errorf("in production, message should be 'internal', got %q", body.Message)
	}
}

// TestInternalServerErrorDevMode 验证开发环境下 InternalServerError 函数暴露内部错误详情。
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
	// 开发环境下应暴露内部错误详情
	if body.Message != "database connection failed" {
		t.Errorf("in dev mode, message should contain error detail, got %q", body.Message)
	}
}

// TestErrorJSONFormat 验证错误响应的 Content-Type 为 application/json 且包含 error 和 message 字段。
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
